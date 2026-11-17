package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
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
CREATE TABLE IF NOT EXISTS business_observations(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,generation TEXT NOT NULL,matches_json TEXT NOT NULL,PRIMARY KEY(host,scope,session));
CREATE TABLE IF NOT EXISTS recall_responses(challenge_id TEXT NOT NULL REFERENCES experience_challenges(id) ON DELETE CASCADE,root_id TEXT NOT NULL,entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,PRIMARY KEY(challenge_id,root_id));
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
	ID      string   `json:"id"`
	RootID  string   `json:"root_id"`
	Anchor  string   `json:"anchor"`
	Signal  string   `json:"signal"`
	Objects []string `json:"objects,omitempty"`
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
		anchors, signals, prefixes := recognitionFeatures(item)
		var anchor, signal string
		score := 0
		for _, a := range anchors {
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
		for _, p := range prefixes {
			if identifierPrefixMatch(clues, p) && 100+len(p) > score {
				anchor = p
				score = 100 + len(p)
			}
		}
		if anchor == "" {
			continue
		}
		for _, c := range signals {
			if !strings.EqualFold(c, anchor) && literalExperienceMatch(clues, c) {
				if signal == "" {
					signal = c
				}
				score += 10
			}
		}
		// Two independent observable features, never a domain name alone.
		if signal != "" {
			rankedMatches = append(rankedMatches, ranked{ExperienceMatch{ID: item.ID, RootID: item.RootID, Anchor: anchor, Signal: signal}, score})
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
	Queries     []RecallQuery `json:"queries,omitempty" jsonschema:"1-4 host-generated search perspectives; server does lexical retrieval only."`
	Cursor      string        `json:"cursor,omitempty" jsonschema:"next_cursor for unchanged query/results; otherwise restart."`
	Limit       int           `json:"limit,omitempty" jsonschema:"1-3 complete items, default 2, within 8 KiB."`
	Scope       string        `json:"scope"`
	Project     string        `json:"project,omitempty" jsonschema:"Bound current project; a current challenge can supply this exact directory"`
	Task        string        `json:"task" jsonschema:"Current task reconstructed from visible conversation and project; not just the latest sentence"`
	Questions   string        `json:"questions" jsonschema:"The current inference or uncertainty you intend to cross-check; no private chain of thought"`
	Keywords    []string      `json:"keywords,omitempty" jsonschema:"Compatibility OR search; ignored when queries is present."`
	ChallengeID string        `json:"challenge_id,omitempty" jsonschema:"Actual short-lived hook challenge. Never invent; ordinary autonomous lookup needs none."`
}
type ExperienceLookupResult struct {
	NextCursor       string           `json:"next_cursor,omitempty"`
	QueryCount       int              `json:"query_count"`
	ContentRole      string           `json:"content_role"`
	Items            []ExperienceItem `json:"items"`
	HasMore          bool             `json:"has_more"`
	ChallengeOutcome string           `json:"challenge_outcome,omitempty"`
	MaintenanceDue   bool             `json:"maintenance_due,omitempty"`
	Notice           string           `json:"notice"`
}
