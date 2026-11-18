package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

const ExperienceLookupBudget = 8192
const ExperienceChallengeTTL = 2 * time.Minute

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

// User inputs invalidate only short-lived business observations. Body/cue
// delivery belongs to the context lifetime, never the turn or task wording.
func (s *Store) ExperienceStartTurn(ctx context.Context, host, scope, session, nativeTurn, eventID string) error {
	if session == "" || len(host) > 80 || len(session) > 1024 || len(nativeTurn) > 512 || len(eventID) > 512 {
		return errors.New("bounded native session required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, "SELECT native_turn FROM experience_epochs WHERE host=? AND scope=? AND session=?", host, scope, session).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// Native turn identity takes precedence. Some adapters provide only an input
	// event ID; retain its hash as an opaque observation boundary, never prompt
	// text. Remember seen identities so late replays cannot clear a newer input.
	boundary := nativeTurn
	key := ""
	if nativeTurn != "" {
		key = digestText("turn\x00" + nativeTurn)
	} else if eventID != "" {
		key = digestText("event\x00" + eventID)
		boundary = "event:" + key
	} else {
		// Without either native identity, replay cannot be distinguished from a
		// new input. Conservatively expire only temporary business observations.
		boundary = "unknown:" + experienceNonce()
	}
	if key != "" {
		result, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO experience_input_events VALUES(?,?,?,?)", host, scope, session, key)
		if e != nil {
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count == 0 {
			return tx.Commit()
		}
	}
	if current != boundary {
		if _, err = tx.ExecContext(ctx, "DELETE FROM business_observations WHERE host=? AND scope=? AND session=?", host, scope, session); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO experience_epochs VALUES(?,?,?,?,?) ON CONFLICT(host,scope,session)DO UPDATE SET native_turn=excluded.native_turn", host, scope, session, experienceNonce(), boundary); err != nil {
		return err
	}
	return tx.Commit()
}

type ExperienceMatch struct {
	CueID    string   `json:"cue_id"`
	CueLabel string   `json:"-"`
	CueText  string   `json:"-"`
	Trigger  string   `json:"trigger,omitempty"`
	ID       string   `json:"id"`
	RootID   string   `json:"root_id"`
	Anchor   string   `json:"anchor"`
	Signal   string   `json:"signal"`
	Objects  []string `json:"objects,omitempty"`
	entry    *Entry
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

func (s *Store) matchLegacyExperiences(ctx context.Context, scope, clues string) ([]ExperienceMatch, error) {
	items, err := s.experienceCandidates(ctx, scope, clues)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	humanText := newHumanSignalText(clues)
	type ranked struct {
		match ExperienceMatch
		score int
	}
	var rankedMatches []ranked
	for itemIndex := range items {
		if items[itemIndex].Profile.Experience != nil && len(items[itemIndex].Profile.Experience.Cues) > 0 {
			continue
		}
		item := items[itemIndex]
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		anchors, signals, prefixes := recognitionFeatures(item)
		var anchor, signal string
		strong := false
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
				if specificEvidenceAnchor(item, a) {
					strong = true
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
			if humanSignalKey(c) != humanSignalKey(anchor) && humanText.matches(c) {
				if signal == "" {
					signal = c
				}
			}
		}
		if signal != "" {
			score += 10
		}
		// Task wording is commonly absent during symbol-chain investigation.
		// A concrete evidence-backed locator is sufficient; broad anchors still
		// need an independent human signal. Alias count adds no confidence.
		trigger := "anchor_and_signal"
		if strong {
			trigger = "specific_code_anchor"
		}
		if signal != "" || strong {
			rankedMatches = append(rankedMatches, ranked{ExperienceMatch{ID: item.ID, RootID: item.RootID, CueID: "legacy_primary", Anchor: anchor, Signal: signal, Trigger: trigger, entry: &items[itemIndex]}, score})
		}
	}
	sort.SliceStable(rankedMatches, func(i, j int) bool { return rankedMatches[i].score > rankedMatches[j].score })
	var matches []ExperienceMatch
	for _, item := range rankedMatches {
		matches = append(matches, item.match)
	}
	return matches, nil
}

type ExperienceChallenge struct {
	Context    string            `json:"-"`
	EmissionID string            `json:"-"`
	ID         string            `json:"id"`
	Matches    []ExperienceMatch `json:"matches"`
	Outcome    string            `json:"outcome"`
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
	ReceiptID        string           `json:"receipt_id"`
	CandidateStates  []CandidateState `json:"candidate_states,omitempty"`
	NextCursor       string           `json:"next_cursor,omitempty"`
	QueryCount       int              `json:"query_count"`
	ContentRole      string           `json:"content_role"`
	Items            []ExperienceItem `json:"items"`
	HasMore          bool             `json:"has_more"`
	ChallengeOutcome string           `json:"challenge_outcome,omitempty"`
	MaintenanceDue   bool             `json:"maintenance_due,omitempty"`
	Notice           string           `json:"notice"`
}
