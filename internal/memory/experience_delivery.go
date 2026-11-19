package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const CueOutputBudget = 1024
const cueRemainder = "\n另有未展示关联。"

func (s *Store) initExperienceTrial() error {
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS experience_delivery_protocol(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);`); err != nil {
		return err
	}
	var version int
	err = tx.QueryRow("SELECT version FROM experience_delivery_protocol WHERE id=1").Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if version == 0 {
		// Only obsolete derived workflow state is discarded. Never promote old
		// generated receipts/cooldowns to host delivery; source history is untouched.
		if _, err = tx.Exec(`DROP TABLE IF EXISTS recall_responses;
DROP TABLE IF EXISTS recall_wakeup_responses;
DROP TABLE IF EXISTS experience_challenges;
DROP TABLE IF EXISTS experience_wakeups;
DROP TABLE IF EXISTS experience_cooldowns;
DROP TABLE IF EXISTS experience_cooldown_settings;
DROP TABLE IF EXISTS experience_epochs;
DROP TABLE IF EXISTS experience_events;
DROP TABLE IF EXISTS business_observations;`); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS experience_keys(scope TEXT NOT NULL,fingerprint TEXT NOT NULL,root_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,PRIMARY KEY(scope,fingerprint));
CREATE TABLE IF NOT EXISTS experience_policy(scope TEXT PRIMARY KEY,enabled INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS experience_epochs(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,generation TEXT NOT NULL,native_turn TEXT NOT NULL,PRIMARY KEY(host,scope,session));
CREATE TABLE IF NOT EXISTS experience_context_events(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,event_id TEXT NOT NULL,PRIMARY KEY(host,scope,session,event_id));
CREATE TABLE IF NOT EXISTS experience_input_events(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,input_key TEXT NOT NULL,PRIMARY KEY(host,scope,session,input_key));
CREATE TABLE IF NOT EXISTS experience_challenges(id TEXT PRIMARY KEY,consumer TEXT NOT NULL,host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,generation TEXT NOT NULL,project TEXT NOT NULL,candidate_key TEXT NOT NULL,ids_json TEXT NOT NULL,created_at TEXT NOT NULL,completed INTEGER NOT NULL DEFAULT 0,faulted INTEGER NOT NULL DEFAULT 0,UNIQUE(consumer,candidate_key));
CREATE TABLE IF NOT EXISTS business_observations(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,generation TEXT NOT NULL,matches_json TEXT NOT NULL,PRIMARY KEY(host,scope,session));
CREATE TABLE IF NOT EXISTS probe_context_calls(seq INTEGER PRIMARY KEY AUTOINCREMENT,consumer TEXT NOT NULL,tool TEXT NOT NULL,call TEXT NOT NULL,turn TEXT NOT NULL,definition TEXT NOT NULL,expires INTEGER NOT NULL,used INTEGER NOT NULL DEFAULT 0,UNIQUE(consumer,tool,call));
CREATE INDEX IF NOT EXISTS probe_context_calls_expiry ON probe_context_calls(expires);
CREATE TABLE IF NOT EXISTS probe_context_facts(consumer TEXT NOT NULL,definition TEXT NOT NULL,key TEXT NOT NULL,turn TEXT NOT NULL,value INTEGER NOT NULL,seq INTEGER NOT NULL,expires INTEGER NOT NULL,PRIMARY KEY(consumer,definition,key));
CREATE INDEX IF NOT EXISTS probe_context_facts_expiry ON probe_context_facts(expires);
CREATE TABLE IF NOT EXISTS experience_body_deliveries(consumer TEXT NOT NULL,root_id TEXT NOT NULL,entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,receipt_id TEXT NOT NULL,PRIMARY KEY(consumer,entry_id));
CREATE TABLE IF NOT EXISTS experience_cue_deliveries(consumer TEXT NOT NULL,root_id TEXT NOT NULL,entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,cue_id TEXT NOT NULL,status TEXT NOT NULL,emission_id TEXT NOT NULL,owner TEXT NOT NULL,PRIMARY KEY(consumer,entry_id,cue_id));
CREATE TABLE IF NOT EXISTS experience_response_receipts(id TEXT PRIMARY KEY,scope TEXT NOT NULL,project TEXT NOT NULL,challenge_id TEXT NOT NULL,items_digest TEXT NOT NULL,items_json TEXT NOT NULL,observed_consumer TEXT NOT NULL DEFAULT '',call_id TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS experience_lookup_calls(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,call_id TEXT NOT NULL,tool TEXT NOT NULL,project TEXT NOT NULL,generation TEXT NOT NULL,consumer TEXT NOT NULL,challenge_id TEXT NOT NULL,receipt_id TEXT NOT NULL DEFAULT '',PRIMARY KEY(host,scope,session,call_id));
CREATE TABLE IF NOT EXISTS experience_entrances(entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,cue_id TEXT NOT NULL,kind TEXT NOT NULL,value TEXT NOT NULL,match_key TEXT NOT NULL,scope TEXT NOT NULL,PRIMARY KEY(entry_id,cue_id,kind,value));
INSERT INTO experience_delivery_protocol VALUES(1,1) ON CONFLICT(id)DO NOTHING;`); err != nil {
		return err
	}
	return tx.Commit()
}

// All adapters resolve the binding root, not the current subdirectory. Agent
// identity is encoded by Runtime into session without an ambiguous delimiter.
func (s *Store) ExperienceProjectBinding(ctx context.Context, cwd, scope string) (string, error) {
	bindings, err := s.HookBindings(ctx)
	if err != nil {
		return "", err
	}
	p, err := canonicalPath(cwd)
	if err != nil {
		return "", err
	}
	best := ""
	for _, b := range bindings {
		if b.Scope != scope {
			continue
		}
		rel, e := filepath.Rel(b.Path, p)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) && len(b.Path) > len(best) {
			best = b.Path
		}
	}
	if best == "" {
		return "", errors.New("project binding not found")
	}
	return best, nil
}

func (s *Store) ExperienceEnsureContext(ctx context.Context, host, scope, session string) error {
	if session == "" || len(session) > 1024 {
		return errors.New("bounded native session required")
	}
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO experience_epochs VALUES(?,?,?,?,?)", host, scope, session, experienceNonce(), "")
	return err
}

func deliveryConsumer(host, scope, session, project, generation string) string {
	b, _ := json.Marshal([]string{host, scope, project, session, generation})
	return digestText(string(b))
}

func (s *Store) deliveryContext(ctx context.Context, host, scope, session, project string) (string, string, error) {
	var generation string
	err := s.db.QueryRowContext(ctx, "SELECT generation FROM experience_epochs WHERE host=? AND scope=? AND session=?", host, scope, session).Scan(&generation)
	if err != nil {
		return "", "", err
	}
	return deliveryConsumer(host, scope, session, project, generation), generation, nil
}

type DeliveryFact struct {
	Match ExperienceMatch
	Body  bool
	Cue   bool
}
type DeliveryDecision struct {
	Kind    string
	Matches []ExperienceMatch
}

// This function contains no task, tool-name, time, count or semantic judgement.
func DecideExperienceDelivery(facts []DeliveryFact) DeliveryDecision {
	unseen, cues := []ExperienceMatch{}, []ExperienceMatch{}
	seen := map[string]bool{}
	for _, f := range facts {
		key := f.Match.ID + ":" + f.Match.CueID
		if seen[key] {
			continue
		}
		seen[key] = true
		if !f.Body {
			unseen = append(unseen, f.Match)
		} else if !f.Cue && f.Match.CueText != "" {
			cues = append(cues, f.Match)
		}
	}
	if len(unseen) > 0 {
		return DeliveryDecision{"challenge", unseen}
	}
	if len(cues) > 0 {
		return DeliveryDecision{"cues", cues}
	}
	return DeliveryDecision{Kind: "silent"}
}

func (s *Store) ChallengeExperience(ctx context.Context, host, scope, session, nativeTurn, project string, matches []ExperienceMatch) (ExperienceChallenge, error) {
	out := ExperienceChallenge{}
	if len(matches) == 0 || session == "" {
		return out, nil
	}
	binding, err := s.ExperienceProjectBinding(ctx, project, scope)
	if err != nil {
		return out, err
	}
	project = binding
	consumer, generation, err := s.deliveryContext(ctx, host, scope, session, project)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// Acquire the writer lock before reading claim state; independent processes
	// cannot both read 'absent' then create competing normal-path emissions.
	if _, err = tx.ExecContext(ctx, "UPDATE experience_delivery_protocol SET version=version WHERE id=1"); err != nil {
		return out, err
	}
	var facts []DeliveryFact
	for _, m := range matches {
		var valid int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE id=? AND root_id=? AND scope=? AND current=1 AND needs_review=0 AND NOT EXISTS(SELECT 1 FROM managed_entries s WHERE s.root_id=entries.root_id AND s.state<>'active')", m.ID, m.RootID, scope).Scan(&valid); err != nil {
			return out, err
		}
		if valid == 0 {
			continue
		}
		var body, cue int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM experience_body_deliveries WHERE consumer=? AND entry_id=?", consumer, m.ID).Scan(&body); err != nil {
			return out, err
		}
		var status, owner string
		err = tx.QueryRowContext(ctx, "SELECT status,owner FROM experience_cue_deliveries WHERE consumer=? AND entry_id=? AND cue_id=?", consumer, m.ID, m.CueID).Scan(&status, &owner)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if err == nil {
			if status == "output" || status == "body_receipt" || processAlive(owner) {
				cue = 1
			} else {
				if _, err = tx.ExecContext(ctx, "DELETE FROM experience_cue_deliveries WHERE consumer=? AND entry_id=? AND cue_id=? AND status='reserved'", consumer, m.ID, m.CueID); err != nil {
					return out, err
				}
			}
		}
		facts = append(facts, DeliveryFact{m, body > 0, cue > 0})
	}
	decision := DecideExperienceDelivery(facts)
	out.Outcome = decision.Kind
	if decision.Kind == "silent" {
		return out, tx.Commit()
	}
	if decision.Kind == "challenge" {
		selected := decision.Matches
		keys := []string{}
		for _, m := range selected {
			keys = append(keys, m.ID+":"+m.CueID)
		}
		sort.Strings(keys)
		candidateKey := digestText(strings.Join(keys, "\n"))
		var id, created string
		var faulted bool
		err = tx.QueryRowContext(ctx, "SELECT id,created_at,faulted FROM experience_challenges WHERE consumer=? AND candidate_key=?", consumer, candidateKey).Scan(&id, &created, &faulted)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if err == nil {
			at, e := time.Parse(time.RFC3339Nano, created)
			// A stuck technical request fails open. It never becomes a delivery and
			// never schedules another reminder on new turns or elapsed cooldowns.
			if faulted || e != nil || time.Since(at) > ExperienceChallengeTTL {
				out.Outcome = "fault_fail_open_not_delivered"
				return out, tx.Commit()
			}
			out.ID = id
			out.Matches = selected
			out.Outcome = "query_required"
			return out, tx.Commit()
		}
		id = experienceNonce()
		if id == "" {
			return out, errors.New("cannot create challenge")
		}
		data, _ := json.Marshal(selected)
		if _, err = tx.ExecContext(ctx, "INSERT INTO experience_challenges(id,consumer,host,scope,session,generation,project,candidate_key,ids_json,created_at)VALUES(?,?,?,?,?,?,?,?,?,?)", id, consumer, host, scope, session, generation, project, candidateKey, string(data), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return out, err
		}
		out.ID = id
		out.Matches = selected
		out.Outcome = "query_required"
		return out, tx.Commit()
	}
	emission := experienceNonce()
	owner := processIdentity()
	if emission == "" || owner == "" {
		return out, errors.New("cannot identify cue emitter")
	}
	lines := []string{}
	size := 0
	remaining := false
	for _, m := range decision.Matches {
		line := fmt.Sprintf("%s@%s · %s：%s", m.RootID, m.ID, m.CueLabel, m.CueText)
		extra := len(line)
		if len(lines) > 0 {
			extra++
		}
		if size+extra+len(cueRemainder) > CueOutputBudget {
			remaining = true
			continue
		}
		result, er := tx.ExecContext(ctx, "INSERT OR IGNORE INTO experience_cue_deliveries VALUES(?,?,?,?,?,?,?)", consumer, m.RootID, m.ID, m.CueID, "reserved", emission, owner)
		if er != nil {
			return out, er
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			continue
		}
		lines = append(lines, line)
		size += extra
	}
	if len(lines) > 0 {
		out.Context = strings.Join(lines, "\n")
		if remaining {
			out.Context += cueRemainder
		}
		out.EmissionID = emission
	}
	return out, tx.Commit()
}

// stdout and SQLite cannot commit atomically. Failure rolls reservations back;
// a dead emitter is recovered conservatively on the next relevant operation.
func (s *Store) FinishCueEmission(ctx context.Context, id string, success bool) error {
	if id == "" {
		return nil
	}
	query := "DELETE FROM experience_cue_deliveries WHERE emission_id=? AND status='reserved'"
	if success {
		query = "UPDATE experience_cue_deliveries SET status='output',owner='' WHERE emission_id=? AND status='reserved'"
	}
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

type CandidateState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
