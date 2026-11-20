package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
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
	CueID    string `json:"cue_id"`
	CueLabel string `json:"-"`
	CueText  string `json:"-"`
	Field    string `json:"field,omitempty"`
	EntranceComparison
	ID       string   `json:"id"`
	RootID   string   `json:"root_id"`
	Entrance string   `json:"entrance"`
	Objects  []string `json:"objects,omitempty"`
	entry    *Entry
}

// shortFact is the one-line fact or counterexample a new-association reminder
// quotes. Legacy cue text stays honored; new records derive it from this
// version's body. A reminder must keep the same assertion boundary as the
// first body delivery: qualifiers stored beside the core fact are appended,
// never trimmed to fit a budget.
func (m ExperienceMatch) shortFact() string {
	if strings.TrimSpace(m.CueText) != "" {
		return m.CueText
	}
	if m.entry != nil && m.entry.Profile != nil {
		if x := m.entry.Profile.Experience; x != nil {
			// New fact-form writes keep qualifiers inside the sentence; records
			// stored by dev.1 with separate columns keep their qualifiers in
			// the reminder instead of an unconditional fact.
			if strings.TrimSpace(x.Fact) != "" {
				return withStoredQualifiers(x.Fact, x.Applicability, x.Boundary)
			}
			return withStoredQualifiers(x.Counterexample, x.Applicability, x.Boundary)
		}
		if a := m.entry.Profile.Agreement; a != nil {
			return withStoredQualifiers(a.Requirement, a.Applicability, a.Boundary)
		}
	}
	return ""
}

func withStoredQualifiers(fact, applicability, boundary string) string {
	for _, qualifier := range []struct{ label, value string }{{"适用", applicability}, {"边界", boundary}} {
		if strings.TrimSpace(qualifier.value) != "" {
			fact += "（" + qualifier.label + "：" + qualifier.value + "）"
		}
	}
	return fact
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
