package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

// Profiles are navigation and admission metadata, not an ontology or proof of truth.
type Profile struct {
	Class         string   `json:"class" jsonschema:"long_term or temporary; task progress must not become permanent preference"`
	FutureUse     string   `json:"future_use" jsonschema:"A concrete future situation and the decision/action this information changes"`
	CounterCheck  string   `json:"counter_check" jsonschema:"Why save despite challenges: one-off, redundant, unverified, readily recoverable? Do not invent a negative example"`
	Applicability string   `json:"applicability" jsonschema:"Known project/environment/conditions; explicitly retain unknowns"`
	Boundary      string   `json:"boundary" jsonschema:"When not to reuse, real counterexamples, or what remains unknown"`
	SearchHints   []string `json:"search_hints,omitempty" jsonschema:"Observed symptoms, real names and careful paraphrases; not keyword stuffing"`
	ExpiresAt     string   `json:"expires_at,omitempty" jsonschema:"RFC3339 explicit expiry for temporary material; absent does not imply permanent validity"`
}

func validateProfile(p *Profile) error {
	if p == nil {
		return errors.New("case/hypothesis requires profile: future_use, counter_check, applicability, boundary and class")
	}
	if p.Class != "long_term" && p.Class != "temporary" {
		return errors.New("profile.class must be long_term or temporary")
	}
	for name, v := range map[string]string{"future_use": p.FutureUse, "counter_check": p.CounterCheck, "applicability": p.Applicability, "boundary": p.Boundary} {
		if strings.TrimSpace(v) == "" || len(v) > 4096 {
			return fmt.Errorf("profile.%s must be nonblank and at most 4096 bytes", name)
		}
		if err := textguard.Check("profile."+name, v, false); err != nil {
			return err
		}
	}
	if len(p.SearchHints) > 12 {
		return errors.New("at most 12 grounded search hints")
	}
	for _, v := range p.SearchHints {
		if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > 120 {
			return errors.New("search hint must contain 1..120 characters")
		}
		if err := textguard.Check("search_hint", v, false); err != nil {
			return err
		}
	}
	if p.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, p.ExpiresAt); err != nil {
			return errors.New("expires_at must be RFC3339")
		}
		if p.Class != "temporary" {
			return errors.New("explicit expiry requires temporary class")
		}
	}
	return nil
}

// Enforced at the MCP boundary. Existing historical material can still be read
// and migrated without inventing retrospective admission reasons.
func ValidateAdmission(d Draft) error {
	if d.Kind == "case" || d.Kind == "hypothesis" {
		if err := validateProfile(d.Profile); err != nil {
			return err
		}
		if len(d.Links) == 0 {
			return errors.New("case/hypothesis needs actual source links")
		}
	}
	return nil
}

type ManagedState struct {
	State             string `json:"state"`
	ReplacementRootID string `json:"replacement_root_id,omitempty"`
	DisplaySummary    string `json:"display_summary,omitempty"`
	SummaryRevisionID string `json:"summary_revision_id,omitempty"`
	Reason            string `json:"reason,omitempty"`
	Version           int    `json:"version"`
}

func migrateGovernance(tx *sql.Tx) error {
	_, err := tx.Exec(`
 CREATE TABLE IF NOT EXISTS entry_profiles(entry_id TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,profile_json TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS managed_entries(root_id TEXT PRIMARY KEY,state TEXT NOT NULL DEFAULT 'active' CHECK(state IN ('active','superseded','archived')),replacement_root_id TEXT NOT NULL DEFAULT '',display_summary TEXT NOT NULL DEFAULT '',summary_revision_id TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',version INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS maintenance_policy(scope TEXT PRIMARY KEY,changes INTEGER NOT NULL DEFAULT 0,interval INTEGER NOT NULL DEFAULT 100,next_at INTEGER NOT NULL DEFAULT 100,enabled INTEGER NOT NULL DEFAULT 1,repeating INTEGER NOT NULL DEFAULT 1,initial_due INTEGER NOT NULL DEFAULT 0,notified_at INTEGER NOT NULL DEFAULT -1);
 CREATE TABLE IF NOT EXISTS maintenance_commands(scope TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,payload TEXT NOT NULL,PRIMARY KEY(scope,source_id));
 INSERT OR IGNORE INTO maintenance_policy(scope,initial_due) SELECT DISTINCT scope,1 FROM entries WHERE kind IN ('case','hypothesis');
 CREATE TABLE IF NOT EXISTS cleanup_plans(id TEXT PRIMARY KEY,scope TEXT NOT NULL,operations_json TEXT NOT NULL,snapshots_json TEXT NOT NULL,before_json TEXT NOT NULL DEFAULT '{}',status TEXT NOT NULL DEFAULT 'preview',created_at TEXT NOT NULL,approval_source_id TEXT NOT NULL DEFAULT '',approval_quote TEXT NOT NULL DEFAULT '',backup_path TEXT NOT NULL DEFAULT '',decided_at TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS context_checks(source_id TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,scope TEXT NOT NULL,task TEXT NOT NULL,questions TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'prepared',selected_json TEXT NOT NULL DEFAULT '[]',decision TEXT NOT NULL DEFAULT '',save_candidate INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS context_reads(source_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,PRIMARY KEY(source_id,entry_id));
 CREATE TABLE IF NOT EXISTS turn_case_reuse(source_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,case_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,note TEXT NOT NULL,PRIMARY KEY(source_id,case_id));
 CREATE TABLE IF NOT EXISTS reviewed_sources(entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,source_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,PRIMARY KEY(entry_id,source_id));
 CREATE TABLE IF NOT EXISTS source_review_receipts(id TEXT PRIMARY KEY,entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,sources_json TEXT NOT NULL,note TEXT NOT NULL,created_at TEXT NOT NULL);
 PRAGMA user_version=3;`)
	return err
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func stateFor(ctx context.Context, q queryer, root string) (ManagedState, error) {
	m := ManagedState{State: "active"}
	err := q.QueryRowContext(ctx, "SELECT state,replacement_root_id,display_summary,summary_revision_id,reason,version FROM managed_entries WHERE root_id=?", root).Scan(&m.State, &m.ReplacementRootID, &m.DisplaySummary, &m.SummaryRevisionID, &m.Reason, &m.Version)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return m, err
}
func loadGovernance(ctx context.Context, q queryer, e *Entry) error {
	var raw string
	err := q.QueryRowContext(ctx, "SELECT profile_json FROM entry_profiles WHERE entry_id=?", e.ID).Scan(&raw)
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &e.Profile); err != nil {
			return err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	e.State, err = stateFor(ctx, q, e.RootID)
	if e.State.SummaryRevisionID != e.ID {
		e.State.DisplaySummary = ""
	}
	return err
}
func (s *Store) ResolveCurrent(ctx context.Context, id string) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	e, err := resolveCurrent(ctx, tx, id)
	if err != nil {
		return e, err
	}
	return e, tx.Commit()
}
func resolveCurrent(ctx context.Context, q queryer, id string) (Entry, error) {
	e, err := scanEntry(q.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, missingLookup("memory_entry_not_found", "id", id, "", "Verify the full entry ID in this same library; search/read results for another entry do not establish this dependency exists.")
	}
	if err != nil {
		return e, err
	}
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		if seen[e.RootID] {
			return e, errors.New("replacement cycle")
		}
		seen[e.RootID] = true
		root, scope := e.RootID, e.Scope
		e, err = scanEntry(q.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE root_id=? AND current=1", root))
		if errors.Is(err, sql.ErrNoRows) {
			return e, missingLookup("current_revision_not_found", "root_id", root, scope, "No current revision exists for this lineage. Inspect the library/backup before revising; do not invent a replacement entry.")
		}
		if err != nil {
			return e, err
		}
		if err = loadGovernance(ctx, q, &e); err != nil {
			return e, err
		}
		if e.State.State != "superseded" {
			return e, nil
		}
		replacement := e.State.ReplacementRootID
		e, err = scanEntry(q.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE root_id=? AND current=1", replacement))
		if errors.Is(err, sql.ErrNoRows) {
			return e, missingLookup("replacement_revision_not_found", "replacement_root_id", replacement, scope, "The replacement pointer has no current entry. Inspect the library/backup; do not silently bypass the missing replacement.")
		}
		if err != nil {
			return e, err
		}
	}
	return e, errors.New("replacement chain exceeds 32 steps")
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func validScope(scope string) error {
	if scope == "global" || strings.HasPrefix(scope, "project:") && strings.TrimSpace(strings.TrimPrefix(scope, "project:")) != "" {
		return nil
	}
	return errors.New("explicit scope required")
}

type MaintenanceStatus struct {
	Scope      string `json:"scope"`
	Changes    int    `json:"changes"`
	Interval   int    `json:"interval"`
	NextAt     int    `json:"next_at"`
	Enabled    bool   `json:"enabled"`
	Repeating  bool   `json:"repeating"`
	InitialDue bool   `json:"initial_due"`
	NotifiedAt int    `json:"notified_at"`
	Due        bool   `json:"due"`
}

func (s *Store) Maintenance(ctx context.Context, scope string) (MaintenanceStatus, error) {
	m := MaintenanceStatus{Scope: scope, Interval: 100, NextAt: 100, Enabled: true, Repeating: true, NotifiedAt: -1}
	if err := validScope(scope); err != nil {
		return m, err
	}
	err := s.db.QueryRowContext(ctx, "SELECT changes,interval,next_at,enabled,repeating,initial_due,notified_at FROM maintenance_policy WHERE scope=?", scope).Scan(&m.Changes, &m.Interval, &m.NextAt, &m.Enabled, &m.Repeating, &m.InitialDue, &m.NotifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	m.Due = m.Enabled && (m.InitialDue || m.Changes >= m.NextAt) && m.NotifiedAt < m.NextAt
	return m, err
}

type MaintenanceConfig struct {
	Scope     string `json:"scope"`
	Interval  int    `json:"interval" jsonschema:"Positive count of case/hypothesis writes from now; raw events and reuse do not count"`
	Enabled   bool   `json:"enabled"`
	Repeating bool   `json:"repeating" jsonschema:"true for every N changes; false for once after N more changes"`
	SourceID  string `json:"source_id" jsonschema:"Actual current user instruction requesting this configuration"`
}

func userSource(ctx context.Context, q queryer, id, scope string) (Entry, error) {
	e, err := scanEntry(q.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, missingLookup("user_source_not_found", "source_id", id, scope, "This user-source ID is absent from the opened library. Verify the full ID and that MCP and hooks use the same data_dir; a readable target case does not establish that its new source exists. Do not create a duplicate case or invent a hook source ID.")
	}
	if err != nil {
		return e, err
	}
	if e.Scope != scope || e.Kind != "record" || e.Origin == "model_inference" || strings.HasPrefix(e.SourceRef, HookSourcePrefix) && (e.Host == "" || e.SourceRef != HookSourcePrefix+e.Host+":user") {
		return e, errors.New("requires an actual user-source record in this scope, not an assistant answer")
	}
	return e, nil
}
func (s *Store) ConfigureMaintenance(ctx context.Context, in MaintenanceConfig) (MaintenanceStatus, error) {
	if err := validScope(in.Scope); err != nil {
		return MaintenanceStatus{}, err
	}
	if in.Interval < 1 || in.Interval > 1000000 {
		return MaintenanceStatus{}, errors.New("interval must be 1..1000000")
	}
	if _, err := userSource(ctx, s.db, in.SourceID, in.Scope); err != nil {
		return MaintenanceStatus{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MaintenanceStatus{}, err
	}
	defer tx.Rollback()
	payload, _ := json.Marshal(in)
	var prior string
	err = tx.QueryRowContext(ctx, "SELECT payload FROM maintenance_commands WHERE scope=? AND source_id=?", in.Scope, in.SourceID).Scan(&prior)
	if err == nil {
		if prior != string(payload) {
			return MaintenanceStatus{}, errors.New("configuration source already used with different settings; use a new user instruction")
		}
		if err = tx.Commit(); err != nil {
			return MaintenanceStatus{}, err
		}
		return s.Maintenance(ctx, in.Scope)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MaintenanceStatus{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO maintenance_policy(scope,interval,next_at,enabled,repeating)VALUES(?,?,?,?,?) ON CONFLICT(scope) DO UPDATE SET interval=excluded.interval,next_at=changes+excluded.interval,enabled=excluded.enabled,repeating=excluded.repeating,initial_due=0,notified_at=-1`, in.Scope, in.Interval, in.Interval, in.Enabled, in.Repeating)
	if err != nil {
		return MaintenanceStatus{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO maintenance_commands(scope,source_id,payload)VALUES(?,?,?)", in.Scope, in.SourceID, string(payload)); err != nil {
		return MaintenanceStatus{}, err
	}
	if err = tx.Commit(); err != nil {
		return MaintenanceStatus{}, err
	}
	return s.Maintenance(ctx, in.Scope)
}
func (s *Store) DeferMaintenance(ctx context.Context, scope string, more int) error {
	if err := validScope(scope); err != nil {
		return err
	}
	if more < 1 || more > 1000000 {
		return errors.New("defer count must be 1..1000000")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO maintenance_policy(scope,next_at)VALUES(?,?) ON CONFLICT(scope)DO UPDATE SET next_at=changes+?,initial_due=0,notified_at=-1`, scope, more, more)
	return err
}
func (s *Store) AcknowledgeMaintenance(ctx context.Context, scope string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE maintenance_policy SET next_at=changes+interval,initial_due=0,notified_at=-1,enabled=CASE WHEN repeating=0 THEN 0 ELSE enabled END WHERE scope=?`, scope)
	return err
}
func (s *Store) MarkMaintenanceNotified(ctx context.Context, scope string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE maintenance_policy SET notified_at=CASE WHEN changes>=next_at THEN changes ELSE next_at-1 END,initial_due=0 WHERE scope=?", scope)
	return err
}

type InventoryItem struct {
	CreatedAt     string       `json:"-"`
	Applicability string       `json:"applicability,omitempty"`
	Boundary      string       `json:"boundary,omitempty"`
	ID            string       `json:"id"`
	RootID        string       `json:"root_id"`
	Kind          string       `json:"kind"`
	Title         string       `json:"title"`
	Summary       string       `json:"summary"`
	NeedsReview   bool         `json:"needs_review"`
	Profile       *Profile     `json:"profile,omitempty"`
	State         ManagedState `json:"management"`
}

// Inventory is bounded and paginated. It is a navigation directory, not semantic
// search. The host chooses entries using its current conversation and project.
func (s *Store) Inventory(ctx context.Context, scope, cursor string, limit int) ([]InventoryItem, string, error) {
	if limit == 0 {
		limit = 30
	}
	p, err := s.SearchPage(ctx, SearchInput{Scope: scope, Cursor: cursor, Limit: limit})
	if err != nil {
		return nil, "", err
	}
	out := []InventoryItem{}
	for _, hit := range p.Results {
		r, er := s.Read(ctx, hit.ID)
		if er != nil {
			return nil, "", er
		}
		e := r.Entry
		if e.Profile != nil && e.Profile.ExpiresAt != "" {
			t, _ := time.Parse(time.RFC3339, e.Profile.ExpiresAt)
			if time.Now().After(t) {
				continue
			}
		}
		summary := e.State.DisplaySummary
		if summary == "" {
			rs := []rune(e.Body)
			if len(rs) > 220 {
				rs = rs[:220]
			}
			summary = string(rs)
		}
		out = append(out, InventoryItem{CreatedAt: e.CreatedAt, ID: e.ID, RootID: e.RootID, Kind: e.Kind, Title: e.Title, Summary: summary, NeedsReview: e.NeedsReview, Profile: e.Profile, State: e.State})
	}
	return out, p.NextCursor, nil
}
