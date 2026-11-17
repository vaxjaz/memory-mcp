package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

const ExperienceLookupBudget = 8192
const ExperienceChallengeTTL = 2 * time.Minute

func (s *Store) initExperienceTrial() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS experience_keys(scope TEXT NOT NULL,fingerprint TEXT NOT NULL,root_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,PRIMARY KEY(scope,fingerprint));
CREATE TABLE IF NOT EXISTS experience_policy(scope TEXT PRIMARY KEY,enabled INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS experience_epochs(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,generation TEXT NOT NULL,native_turn TEXT NOT NULL,PRIMARY KEY(host,scope,session));
CREATE TABLE IF NOT EXISTS experience_events(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,event_id TEXT NOT NULL,generation TEXT NOT NULL,PRIMARY KEY(host,scope,session,event_id));
CREATE TABLE IF NOT EXISTS experience_challenges(id TEXT PRIMARY KEY,host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,generation TEXT NOT NULL,project TEXT NOT NULL,ids_json TEXT NOT NULL,created_at TEXT NOT NULL,completed INTEGER NOT NULL DEFAULT 0,UNIQUE(host,scope,session,generation));
`)
	return err
}

func experienceNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func (s *Store) ConfigureExperienceTrial(ctx context.Context, scope string, enabled bool) error {
	if err := validScope(scope); err != nil {
		return err
	}
	if scope == "global" {
		return errors.New("experience trial cannot be globally inferred")
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO experience_policy(scope,enabled)VALUES(?,?) ON CONFLICT(scope)DO UPDATE SET enabled=excluded.enabled", scope, enabled)
	return err
}
func (s *Store) ExperiencePolicyExists(ctx context.Context, scope string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM experience_policy WHERE scope=?", scope).Scan(&n)
	return n > 0, err
}
func (s *Store) ExperienceTrialEnabled(ctx context.Context, scope string) (bool, error) {
	var enabled bool
	err := s.db.QueryRowContext(ctx, "SELECT enabled FROM experience_policy WHERE scope=?", scope).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}

func (s *Store) OtherExperienceTrialsEnabled(ctx context.Context, scope string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM experience_policy WHERE enabled=1 AND scope<>?", scope).Scan(&count)
	return count > 0, err
}

// A native input epoch is an interruption budget, NOT a logical-task permit.
// No prompt body, query, assistant answer or keyword history is stored here.
func (s *Store) ExperienceStartTurn(ctx context.Context, host, scope, session, nativeTurn, eventID string) error {
	if session == "" {
		return errors.New("native session required")
	}
	if len(host) > 80 || len(session) > 512 || len(nativeTurn) > 512 || len(eventID) > 512 {
		return errors.New("native event identity exceeds bounded metadata size")
	}
	if nativeTurn != "" {
		eventID = "turn:" + nativeTurn
	}
	generation := experienceNonce()
	if generation == "" {
		return errors.New("cannot create input epoch")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if eventID != "" {
		var previous string
		err = tx.QueryRowContext(ctx, "SELECT generation FROM experience_events WHERE host=? AND scope=? AND session=? AND event_id=?", host, scope, session, eventID).Scan(&previous)
		if err == nil {
			return tx.Commit()
		} // Replay cannot reactivate an older epoch.
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO experience_events VALUES(?,?,?,?,?)", host, scope, session, eventID, generation); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO experience_epochs VALUES(?,?,?,?,?) ON CONFLICT(host,scope,session)DO UPDATE SET generation=excluded.generation,native_turn=excluded.native_turn", host, scope, session, generation, nativeTurn); err != nil {
		return err
	}
	return tx.Commit()
}

type ExperienceMatch struct {
	ID     string `json:"id"`
	RootID string `json:"root_id"`
	Anchor string `json:"anchor"`
	Signal string `json:"signal"`
}

func literalExperienceMatch(text, term string) bool {
	if strings.ContainsAny(term, "/\\") {
		return strings.Contains(normalizedResource(text), normalizedResource(term))
	}
	ascii := true
	for _, r := range term {
		if r > 127 {
			ascii = false
			break
		}
	}
	if ascii {
		return identifierMatch(text, term)
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(term))
}

func (s *Store) MatchExperiences(ctx context.Context, scope, clues string) ([]ExperienceMatch, error) {
	items, err := s.experienceCandidates(ctx, scope, clues)
	if err != nil {
		return nil, err
	}
	type ranked struct {
		match ExperienceMatch
		score int
	}
	var rankedMatches []ranked
	for _, item := range items {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		x := item.Profile.Experience
		var anchor, signal string
		score := 0
		for _, a := range x.Anchors {
			if literalExperienceMatch(clues, a) {
				value := 100 + len(a)
				if strings.ContainsAny(a, "/\\") {
					value += 300
				}
				if value > score {
					anchor = a
					score = value
				}
			}
		}
		if anchor == "" {
			continue
		}
		for _, c := range x.Signals {
			if !strings.EqualFold(c, anchor) && literalExperienceMatch(clues, c) {
				if signal == "" {
					signal = c
				}
				score += 10
			}
		}
		// Two independent observable features, never a domain name alone.
		if signal != "" {
			rankedMatches = append(rankedMatches, ranked{ExperienceMatch{item.ID, item.RootID, anchor, signal}, score})
		}
	}
	sort.SliceStable(rankedMatches, func(i, j int) bool { return rankedMatches[i].score > rankedMatches[j].score })
	var matches []ExperienceMatch
	for _, item := range rankedMatches {
		matches = append(matches, item.match)
		if len(matches) == 2 {
			break
		}
	}
	return matches, nil
}

type ExperienceChallenge struct {
	ID      string            `json:"id"`
	Matches []ExperienceMatch `json:"matches"`
	Outcome string            `json:"outcome"`
}

func (s *Store) ChallengeExperience(ctx context.Context, host, scope, session, nativeTurn, project string, matches []ExperienceMatch) (ExperienceChallenge, error) {
	var out ExperienceChallenge
	if len(matches) == 0 || session == "" {
		return out, nil
	}
	var generation, currentNative string
	err := s.db.QueryRowContext(ctx, "SELECT generation,native_turn FROM experience_epochs WHERE host=? AND scope=? AND session=?", host, scope, session).Scan(&generation, &currentNative)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if nativeTurn != "" && nativeTurn != currentNative {
		out.Outcome = "foreign_turn_not_attached"
		return out, nil
	}
	var oldID, idsJSON, created string
	var completed bool
	err = s.db.QueryRowContext(ctx, "SELECT id,ids_json,created_at,completed FROM experience_challenges WHERE host=? AND scope=? AND session=? AND generation=?", host, scope, session, generation).Scan(&oldID, &idsJSON, &created, &completed)
	if err == nil {
		if completed {
			out.Outcome = "input_budget_already_used"
			return out, nil
		}
		at, er := time.Parse(time.RFC3339Nano, created)
		if er != nil || time.Since(at) > ExperienceChallengeTTL {
			out.Outcome = "expired_fail_open_not_completed"
			return out, nil
		}
		var oldMatches []ExperienceMatch
		if er = json.Unmarshal([]byte(idsJSON), &oldMatches); er != nil {
			return out, er
		}
		for _, m := range matches {
			for _, old := range oldMatches {
				if m.ID == old.ID {
					return ExperienceChallenge{ID: oldID, Matches: oldMatches, Outcome: "query_required"}, nil
				}
			}
		}
		out.Outcome = "unrelated_operation_not_blocked"
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	id := experienceNonce()
	if id == "" {
		return out, errors.New("cannot create experience challenge")
	}
	encoded, _ := json.Marshal(matches)
	_, err = s.db.ExecContext(ctx, "INSERT OR IGNORE INTO experience_challenges(id,host,scope,session,generation,project,ids_json,created_at)VALUES(?,?,?,?,?,?,?,?)", id, host, scope, session, generation, project, string(encoded), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return out, err
	}
	// Concurrent code searches share the same actual challenge, not an invented
	// success flag. A deny never completes it.
	var actualID string
	if err = s.db.QueryRowContext(ctx, "SELECT id FROM experience_challenges WHERE host=? AND scope=? AND session=? AND generation=?", host, scope, session, generation).Scan(&actualID); err != nil {
		return out, err
	}
	if actualID != id {
		return s.ChallengeExperience(ctx, host, scope, session, nativeTurn, project, matches)
	}
	return ExperienceChallenge{ID: id, Matches: matches, Outcome: "query_required"}, nil
}

type ExperienceLookup struct {
	Scope       string   `json:"scope"`
	Project     string   `json:"project,omitempty" jsonschema:"Bound current project; a current challenge can supply this exact directory"`
	Task        string   `json:"task" jsonschema:"Current task reconstructed from visible conversation and project; not just the latest sentence"`
	Questions   string   `json:"questions" jsonschema:"The current inference or uncertainty you intend to cross-check; no private chain of thought"`
	Keywords    []string `json:"keywords" jsonschema:"Grounded business/symbol/mechanism terms from current context; literal matching is not semantic proof"`
	ChallengeID string   `json:"challenge_id,omitempty" jsonschema:"Actual short-lived hook challenge. Never invent; ordinary autonomous lookup needs none."`
}
type ExperienceLookupResult struct {
	ContentRole      string           `json:"content_role"`
	Items            []ExperienceItem `json:"items"`
	HasMore          bool             `json:"has_more"`
	ChallengeOutcome string           `json:"challenge_outcome,omitempty"`
	MaintenanceDue   bool             `json:"maintenance_due,omitempty"`
	Notice           string           `json:"notice"`
}

func (s *Store) LookupExperience(ctx context.Context, in ExperienceLookup) (ExperienceLookupResult, error) {
	result := ExperienceLookupResult{ContentRole: "historical_risk_features", Items: []ExperienceItem{},
		Notice: "Historical failure clues, not current conclusions or authorization. Locations prove only where literal symbols occur. Check current callers/conditions and runtime evidence as needed. No hit does not prove no risk. Do not revise a case merely because a locator misses."}
	for name, value := range map[string]string{"task": in.Task, "questions": in.Questions} {
		if strings.TrimSpace(value) == "" || len(value) > 4000 {
			return result, fmt.Errorf("%s needs 1-4000 bytes", name)
		}
		if err := textguard.Check(name, value, false); err != nil {
			return result, err
		}
	}
	if len(in.Keywords) == 0 {
		return result, errors.New("ground the query in current project/context keywords")
	}
	_, terms, err := normalizeSearchQuery(strings.Join(in.Keywords, " "))
	if err != nil {
		return result, err
	}
	if len(terms) == 0 {
		return result, errors.New("query keywords cannot consist only of whitespace")
	}
	var generation, host, session, challengeScope, project, created string
	var challengeMatches []ExperienceMatch
	challengeActive := false
	if in.ChallengeID != "" {
		var data string
		var completed bool
		err = s.db.QueryRowContext(ctx, "SELECT host,scope,session,generation,project,ids_json,created_at,completed FROM experience_challenges WHERE id=?", in.ChallengeID).Scan(&host, &challengeScope, &session, &generation, &project, &data, &created, &completed)
		if err != nil {
			return result, errors.New("unknown experience challenge; verify service/data_dir, do not copy another session's identifier")
		}
		if challengeScope != in.Scope {
			return result, errors.New("challenge scope mismatch")
		}
		if in.Project != "" {
			if a, er := s.experienceProject(ctx, in.Project, in.Scope); er != nil || a != project {
				return result, errors.New("challenge project mismatch")
			}
		}
		if err = json.Unmarshal([]byte(data), &challengeMatches); err != nil {
			return result, err
		}
		var current string
		err = s.db.QueryRowContext(ctx, "SELECT generation FROM experience_epochs WHERE host=? AND scope=? AND session=?", host, in.Scope, session).Scan(&current)
		at, er := time.Parse(time.RFC3339Nano, created)
		challengeActive = err == nil && er == nil && generation == current && time.Since(at) <= ExperienceChallengeTTL
		if !challengeActive {
			result.ChallengeOutcome = "stale_no_current_turn_release"
		}
	}
	if project == "" {
		project = in.Project
	}
	project, err = s.experienceProject(ctx, project, in.Scope)
	if err != nil {
		return result, err
	}
	// Reserve all envelope metadata before admitting items. Completing a lookup
	// must never produce a response larger than the promised context budget.
	if m, er := s.Maintenance(ctx, in.Scope); er == nil {
		result.MaintenanceDue = m.Due
	}
	if challengeActive {
		result.ChallengeOutcome = "query_completed_not_applicability_verified"
	}
	entries, err := s.Experiences(ctx, in.Scope)
	if err != nil {
		return result, err
	}
	// Explicit challenge candidates come first, while the host supplies its own
	// task/question. This avoids an empty/generic query bypassing the actual hit.
	wanted := map[string]bool{}
	for _, m := range challengeMatches {
		wanted[m.RootID] = true
	}
	var selected []Entry
	for _, e := range entries {
		if wanted[e.RootID] {
			selected = append(selected, e)
		}
	}
	for _, e := range entries {
		if wanted[e.RootID] {
			continue
		}
		x := e.Profile.Experience
		clues := strings.Join(append(append([]string{}, x.Anchors...), x.Signals...), " ")
		for _, term := range terms {
			if literalExperienceMatch(clues, term) {
				selected = append(selected, e)
				break
			}
		}
	}
	for _, e := range selected {
		if len(result.Items) == 2 {
			result.HasMore = true
			break
		}
		item := ExperienceItem{ID: e.ID, RootID: e.RootID, Title: e.Title, Experience: e.Profile.Experience,
			Locations: LocateExperience(ctx, project, e.Profile.Experience)}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
		body, _ := json.Marshal(result)
		if len(body) > ExperienceLookupBudget {
			result.Items = result.Items[:len(result.Items)-1]
			result.HasMore = true
			break // Never truncate conditions/evidence to squeeze in an item.
		}
	}
	if len(result.Items) == 0 && len(selected) > 0 {
		return result, errors.New("experience result exceeds bounded response; compact the record, not its conditions at read time")
	}
	if challengeActive {
		// Recheck the current epoch in the UPDATE. A late result can never release
		// an operation belonging to a newer user input.
		update, er := s.db.ExecContext(ctx, `UPDATE experience_challenges SET completed=1 WHERE id=? AND generation=?
AND julianday(created_at)>=julianday(?)
AND EXISTS(SELECT 1 FROM experience_epochs e WHERE e.host=experience_challenges.host AND e.scope=experience_challenges.scope AND e.session=experience_challenges.session AND e.generation=experience_challenges.generation)`, in.ChallengeID, generation, time.Now().Add(-ExperienceChallengeTTL).UTC().Format(time.RFC3339Nano))
		if er != nil {
			return result, er
		}
		n, _ := update.RowsAffected()
		if n > 0 {
			result.ChallengeOutcome = "query_completed_not_applicability_verified"
		} else {
			result.ChallengeOutcome = "stale_no_current_turn_release"
		}
	}
	return result, nil
}
