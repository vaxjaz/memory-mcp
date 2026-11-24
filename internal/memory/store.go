package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

// Schema 10 stores two experience body forms (counterexample/fact) in
// profile_json. Older binaries cannot project the fact form and are refused
// instead of silently dropping it; upgrade runs the existing backup flow.
const SchemaVersion = 10

// Entry bodies are free text. These fields track provenance, not semantic truth.
type Entry struct {
	writeReused            bool
	RecordedSourceID       string            `json:"recorded_source_id,omitempty" jsonschema:"Persisted turn bookkeeping association; separate from evidence links and not an assertion of support"`
	ResponseMode           string            `json:"response_mode,omitempty"`
	LinkCount              *int              `json:"link_count,omitempty"`
	CheckedContextSourceID string            `json:"checked_context_source_id,omitempty" jsonschema:"Transient write response: current checkpoint validated for this operation, not a stored evidence link"`
	ID                     string            `json:"id"`
	RootID                 string            `json:"root_id"`
	PreviousID             string            `json:"previous_id,omitempty"`
	Kind                   string            `json:"kind"`
	Scope                  string            `json:"scope"`
	Title                  string            `json:"title,omitempty"`
	Body                   string            `json:"body,omitempty"`
	BodyBytes              int               `json:"body_bytes"`
	BodySHA256             string            `json:"body_sha256"`
	Origin                 string            `json:"origin,omitempty"`
	SourceRef              string            `json:"source_ref,omitempty"`
	Host                   string            `json:"host,omitempty"`
	Model                  string            `json:"model,omitempty"`
	Session                string            `json:"session,omitempty"`
	CreatedAt              string            `json:"created_at,omitempty"`
	Current                bool              `json:"current"`
	NeedsReview            bool              `json:"needs_review"`
	Links                  []Link            `json:"links"`
	Profile                *Profile          `json:"profile,omitempty"`
	State                  ManagedState      `json:"management"`
	TextIssues             []textguard.Issue `json:"text_issues,omitempty"`
}

type Link struct {
	TargetID string `json:"target_id" jsonschema:"ID of an existing source entry; read it before interpreting it"`
	Relation string `json:"relation" jsonschema:"supports requires a current active reviewed source; contradicts/context may reference stale or historical material for correction or provenance, without validating it"`
}

type Draft struct {
	referenceValidated  bool
	experienceValidated bool
	experienceKey       string
	requestFingerprint  string
	// preCommit runs inside Put's write transaction, after dedup and revision
	// checks and immediately before the entry insert. The W(x) write judgment
	// re-checks comparison candidates here so a concurrent writer between the
	// outer search and the commit turns this write into a comparison instead
	// of a duplicate root.
	preCommit                  func(context.Context, *sql.Tx) error
	ResponseMode               string   `json:"response_mode,omitempty" jsonschema:"compact: short IDs/state/integrity receipt; full: full response. Assist defaults to compact, strict to legacy full. Not part of write identity."`
	ContextSourceID            string   `json:"context_source_id,omitempty" jsonschema:"Optional actual CURRENT user source for turn association, separate from evidence links. Assist validates source identity without requiring prepare/complete. Strict requires its checkpoint complete. Never fabricate source IDs."`
	Profile                    *Profile `json:"profile,omitempty" jsonschema:"For cases and hypotheses: concrete future use, counter-check against saving, applicability and boundaries. No fabricated counterexamples."`
	Kind                       string   `json:"kind" jsonschema:"record for supplied material; case for a contextual decision; hypothesis is only created through memory_revise"`
	Scope                      string   `json:"scope" jsonschema:"Explicit scope: project:<stable project identifier> or global; never infer global applicability"`
	Title                      string   `json:"title" jsonschema:"Short descriptive title, not a mandatory preference conclusion"`
	Body                       string   `json:"body" jsonschema:"Free text preserving context, alternatives, exact feedback when available, and unknowns; distinguish quotes from inference"`
	Origin                     string   `json:"origin" jsonschema:"host_quote (host-relayed quotation, not verified transcript), user_supplied, imported, or model_inference"`
	SourceRef                  string   `json:"source_ref,omitempty" jsonschema:"Optional source locator; use only a real supplied reference, never invent one"`
	Host                       string   `json:"host,omitempty" jsonschema:"Host name if known; omit rather than guess"`
	Model                      string   `json:"model,omitempty" jsonschema:"Model identifier if actually known; omit rather than guess"`
	Session                    string   `json:"session,omitempty" jsonschema:"Optional originating session identifier"`
	Links                      []Link   `json:"links,omitempty" jsonschema:"Optional links to existing supporting, contradicting, or contextual entries"`
	RequestID                  string   `json:"request_id,omitempty" jsonschema:"Optional stable ID for one logical write. Reuse it with the same payload when retrying an uncertain response; use a new ID for a different write. Scoped to this project or global scope."`
	AllowReplacementCharacters bool     `json:"allow_replacement_characters,omitempty" jsonschema:"Default false: reject U+FFFD which can indicate already damaged input. Set true only to preserve that character intentionally present in the original, never to bypass a corruption error."`
}

type Store struct {
	db       *sql.DB
	Dir      string
	readOnly bool
}

func DefaultDir() (string, error) {
	if p := os.Getenv("MEMORY_MCP_DATA_DIR"); p != "" {
		return filepath.Abs(p)
	}
	// UserConfigDir is Application Support on macOS and AppData on Windows.
	var p string
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		var err error
		p, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	} else {
		p = os.Getenv("XDG_DATA_HOME")
		if p == "" || !filepath.IsAbs(p) {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			p = filepath.Join(h, ".local", "share")
		}
	}
	return filepath.Join(p, "memory-mcp"), nil
}

func Open(dir string) (*Store, error) {
	return openStore(dir, 10000, "")
}

// Hooks are advisory and must fail open instead of waiting ten seconds on a
// writer lock. Normal writes/maintenance retain the original durability policy.
func OpenForHook(dir string) (*Store, error) {
	return openStore(dir, 200, "")
}

func openStore(dir string, busyMS int, migrationBackup string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	// Create the DB before opening it so Unix permissions never default to 0644.
	f, err := os.OpenFile(filepath.Join(abs, "memory.db"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	// Initialize EVERY physical connection, including replacements opened by database/sql.
	db, err := driver.Open(filepath.Join(abs, "memory.db"), func(conn *sqlite3.Conn) error {
		if migrationBackup == "" {
			stmt, _, err := conn.Prepare("PRAGMA user_version")
			if err != nil {
				return err
			}
			if stmt.Step() {
				version := stmt.ColumnInt(0)
				_ = stmt.Close()
				if version != 0 && version < SchemaVersion {
					return fmt.Errorf("database schema %d requires explicit cue migration to %d; run memory-mcp migrate --data-dir %q to inspect", version, SchemaVersion, abs)
				}
				if version > SchemaVersion {
					return fmt.Errorf("database version %d is newer than supported %d", version, SchemaVersion)
				}
			} else {
				err := stmt.Err()
				_ = stmt.Close()
				if err != nil {
					return err
				}
			}
		}
		// Independent hook processes may race the first WAL transition. SQLite
		// can report BUSY for that PRAGMA before its busy handler can wait.
		deadline := time.Now().Add(time.Duration(busyMS) * time.Millisecond)
		if busyMS == 10000 {
			deadline = time.Now().Add(3 * time.Second)
		}
		for {
			err := conn.Exec(fmt.Sprintf(`PRAGMA busy_timeout=%d; PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA secure_delete=ON;`, busyMS))
			if err == nil || !errors.Is(err, sqlite3.BUSY) || time.Now().After(deadline) {
				return err
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, Dir: abs}
	if err := s.init(migrationBackup); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.initOperationOrder(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.initDemandReview(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.initExperienceTrial(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.initRecognitionRecallIndex(busyMS == 10000); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) init(migrationBackup string) error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("database version %d is newer than supported %d", version, SchemaVersion)
	}
	if version == SchemaVersion {
		return nil
	}
	if version != 0 {
		if migrationBackup == "" {
			return fmt.Errorf("database schema %d requires explicit cue migration to %d; run memory-mcp migrate --data-dir %q to inspect", version, SchemaVersion, s.Dir)
		}
		if err := s.Backup(context.Background(), migrationBackup); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A second process may have migrated while this connection waited for the lock.
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("database version %d is newer than supported %d", version, SchemaVersion)
	}
	if version == SchemaVersion {
		return tx.Commit()
	}
	if version != 0 && migrationBackup == "" {
		return fmt.Errorf("database schema %d requires cue migration before opening", version)
	}
	_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS entries (
 id TEXT PRIMARY KEY, root_id TEXT NOT NULL, previous_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('record','case','hypothesis')),
 scope TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL,
 origin TEXT NOT NULL, source_ref TEXT NOT NULL, host TEXT NOT NULL,
 model TEXT NOT NULL, session TEXT NOT NULL, created_at TEXT NOT NULL,
 current INTEGER NOT NULL, needs_review INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS one_current_revision ON entries(root_id) WHERE current=1;
CREATE INDEX IF NOT EXISTS entries_scope ON entries(scope,current,created_at);
CREATE TABLE IF NOT EXISTS links (
 entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
 target_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
 relation TEXT NOT NULL CHECK(relation IN ('supports','contradicts','context')),
 PRIMARY KEY(entry_id,target_id,relation)
);
CREATE INDEX IF NOT EXISTS links_target ON links(target_id);
CREATE TABLE IF NOT EXISTS write_requests (
 scope TEXT NOT NULL, request_id TEXT NOT NULL, fingerprint TEXT NOT NULL,
 entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
 PRIMARY KEY(scope,request_id)
);
PRAGMA user_version=2;`)
	if err != nil {
		return err
	}
	if err = migrateGovernance(tx); err != nil {
		return err
	}
	if version != 0 && version < 6 {
		if err = migrateCueProfiles(tx, version); err != nil {
			return err
		}
	}
	if version > 0 && version < 7 {
		if err = migrateGroupedCues(tx); err != nil {
			return err
		}
	}
	if err = migrateRecall(tx); err != nil {
		return err
	}
	if _, err = tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", SchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func Validate(d Draft) error {
	if err := ValidateResponseMode(d.ResponseMode); err != nil {
		return err
	}
	for _, field := range []struct{ name, value string }{{"scope", d.Scope}, {"title", d.Title}, {"body", d.Body}, {"source_ref", d.SourceRef}, {"host", d.Host}, {"model", d.Model}, {"session", d.Session}, {"request_id", d.RequestID}, {"context_source_id", d.ContextSourceID}} {
		if err := textguard.Check(field.name, field.value, d.AllowReplacementCharacters); err != nil {
			return err
		}
	}
	for _, value := range []string{d.Scope, d.Title, d.Body, d.SourceRef, d.Host, d.Model, d.Session, d.RequestID} {
		if !utf8.ValidString(value) {
			return errors.New("text fields must be valid UTF-8; raw text is not normalized or repaired")
		}
	}
	if len(d.RequestID) > 256 || (d.RequestID != "" && strings.TrimSpace(d.RequestID) == "") {
		return errors.New("request_id must be nonblank and at most 256 bytes")
	}
	if d.Kind != "record" && d.Kind != "case" && d.Kind != "hypothesis" {
		return errors.New("kind must be record, case, or hypothesis")
	}
	if d.Scope != "global" && (!strings.HasPrefix(d.Scope, "project:") || strings.TrimSpace(strings.TrimPrefix(d.Scope, "project:")) == "") {
		return errors.New("scope must be global or project:<stable identifier>")
	}
	if strings.TrimSpace(d.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(d.Body) == "" {
		return errors.New("body is required")
	}
	if utf8.RuneCountInString(d.Title) > 200 || len(d.Body) > 256*1024 || len(d.Scope) > 512 {
		return errors.New("title, body, or scope exceeds size limit")
	}
	switch d.Origin {
	case "host_quote", "user_supplied", "imported", "model_inference":
	default:
		return errors.New("invalid origin")
	}
	if d.Kind == "hypothesis" && d.Origin != "model_inference" {
		return errors.New("hypotheses must have origin model_inference; they are not user instructions")
	}
	if d.Kind == "record" && d.Origin == "model_inference" {
		return errors.New("model inference belongs in a case or hypothesis, not a raw record")
	}
	if d.Kind == "hypothesis" && len(d.Links) == 0 {
		return errors.New("hypothesis requires source links")
	}
	if len(d.Links) > 32 {
		return errors.New("at most 32 source links per revision")
	}
	seen := map[Link]bool{}
	for _, l := range d.Links {
		if l.TargetID == "" || (l.Relation != "supports" && l.Relation != "contradicts" && l.Relation != "context") {
			return errors.New("invalid evidence link")
		}
		if seen[l] {
			return errors.New("duplicate evidence link")
		}
		seen[l] = true
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

const columns = "id,root_id,previous_id,kind,scope,title,body,origin,source_ref,host,model,session,created_at,current,needs_review"

func scanEntry(r rowScanner) (Entry, error) {
	var e Entry
	err := r.Scan(&e.ID, &e.RootID, &e.PreviousID, &e.Kind, &e.Scope, &e.Title, &e.Body, &e.Origin, &e.SourceRef, &e.Host, &e.Model, &e.Session, &e.CreatedAt, &e.Current, &e.NeedsReview)
	e.Links = []Link{}
	return withIntegrity(e), err
}

func (s *Store) Put(ctx context.Context, d Draft, previousID string) (Entry, error) {
	if err := Validate(d); err != nil {
		return Entry{}, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return Entry{}, err
	}
	id := hex.EncodeToString(b)
	e := Entry{ID: id, RootID: id, PreviousID: previousID, Kind: d.Kind, Scope: d.Scope, Title: d.Title, Body: d.Body, Origin: d.Origin, SourceRef: d.SourceRef, Host: d.Host, Model: d.Model, Session: d.Session, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Current: true, Links: d.Links}
	if e.Links == nil {
		e.Links = []Link{}
	}
	e = withIntegrity(e)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	fingerprint := writeFingerprint(d, previousID)
	if d.requestFingerprint != "" {
		fingerprint = d.requestFingerprint
	}
	if d.RequestID != "" {
		var oldFingerprint, entryID string
		err = tx.QueryRowContext(ctx, "SELECT fingerprint,entry_id FROM write_requests WHERE scope=? AND request_id=?", d.Scope, d.RequestID).Scan(&oldFingerprint, &entryID)
		if err == nil {
			if oldFingerprint != fingerprint {
				return Entry{}, errors.New("request_id conflict: this ID was used for a different write")
			}
			entry, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", entryID))
			if err != nil {
				return Entry{}, err
			}
			entry.Links, err = loadLinks(ctx, tx, entry.ID)
			if err != nil {
				return Entry{}, err
			}
			if err = loadGovernance(ctx, tx, &entry); err != nil {
				return Entry{}, err
			}
			if err = loadRecordedSource(ctx, tx, &entry); err != nil {
				return Entry{}, err
			}
			return entry, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Entry{}, err
		}
	}
	if previousID != "" {
		old, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", previousID))
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, missingLookup("revision_target_not_found", "previous_id", previousID, d.Scope, "Use the full current revision ID returned by memory_read in this library. Do not create a new case to bypass an unavailable revision target.")
		}
		if err != nil {
			return Entry{}, err
		}
		if !old.Current {
			return Entry{}, errors.New("revision conflict: target was superseded; read its current revision first")
		}
		if old.Scope != d.Scope || old.Kind != d.Kind {
			return Entry{}, errors.New("revision cannot change scope or kind")
		}
		e.RootID = old.RootID
	}
	if d.experienceValidated || d.referenceValidated {
		var root string
		er := tx.QueryRowContext(ctx, "SELECT root_id FROM experience_keys WHERE scope=? AND fingerprint=?", d.Scope, d.experienceKey).Scan(&root)
		if er == nil {
			old, re := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE root_id=? AND current=1", root))
			if re != nil {
				return Entry{}, re
			}
			if re = loadGovernance(ctx, tx, &old); re != nil {
				return Entry{}, re
			}
			if old.State.State == "active" && !old.NeedsReview && old.Profile != nil && ((d.experienceValidated && old.Profile.Experience != nil) || (d.referenceValidated && old.Profile.Agreement != nil)) {
				if previousID != "" && e.RootID != root {
					return Entry{}, fmt.Errorf("equivalent experience already exists: %s; inspect before merging lineages", old.ID)
				}
				// Keep the original evidence while refreshing entry selectors.
				if d.experienceValidated {
					old.Profile.Experience = recognitionUpdate(old.Profile.Experience, d.Profile.Experience)
				} else {
					old.Profile.Agreement.Cues = d.Profile.Agreement.Cues
					old.Profile.Agreement.ObjectIDPrefixes = d.Profile.Agreement.ObjectIDPrefixes
				}
				profile, _ := json.Marshal(old.Profile)
				if _, re = tx.ExecContext(ctx, "UPDATE entry_profiles SET profile_json=? WHERE entry_id=?", string(profile), old.ID); re != nil {
					return Entry{}, re
				}
				if re = indexRecallEntry(ctx, tx, old); re != nil {
					return Entry{}, re
				}
				old.Links, re = loadLinks(ctx, tx, old.ID)
				if re != nil {
					return Entry{}, re
				}
				if d.RequestID != "" {
					if _, re = tx.ExecContext(ctx, "INSERT INTO write_requests(scope,request_id,fingerprint,entry_id)VALUES(?,?,?,?)", d.Scope, d.RequestID, fingerprint, old.ID); re != nil {
						return Entry{}, re
					}
				}
				old.writeReused = true
				return old, tx.Commit()
			}
			if _, er = tx.ExecContext(ctx, "DELETE FROM experience_keys WHERE scope=? AND fingerprint=?", d.Scope, d.experienceKey); er != nil {
				return Entry{}, er
			}
		} else if !errors.Is(er, sql.ErrNoRows) {
			return Entry{}, er
		}
	}
	for index, l := range d.Links {
		source, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", l.TargetID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Entry{}, missingLookup("write_source_not_found", fmt.Sprintf("draft.links[%d].target_id", index), l.TargetID, d.Scope, "Verify the full source ID and MCP/hook data directories; preserve the current case and its history instead of creating a duplicate case.")
			}
			return Entry{}, fmt.Errorf("load draft.links[%d].target_id=%q: %w", index, l.TargetID, err)
		}
		if source.Scope != d.Scope && source.Scope != "global" {
			return Entry{}, errors.New("source outside scope: global hypotheses cannot depend on project data; projects may use their own or global sources")
		}
		if l.Relation == "supports" {
			if err = requireSupport(ctx, tx, source); err != nil {
				return Entry{}, err
			}
		}
		if previousID != "" && l.Relation == "supports" {
			// Prevent a revised lineage from depending on itself through its descendants.
			var cycle int
			err = tx.QueryRowContext(ctx, `WITH RECURSIVE ancestry(id) AS (
 SELECT ? UNION SELECT l.target_id FROM links l JOIN ancestry a ON l.entry_id=a.id
) SELECT count(*) FROM entries WHERE id IN (SELECT id FROM ancestry) AND root_id=?`, source.ID, e.RootID).Scan(&cycle)
			if err != nil {
				return Entry{}, err
			}
			if cycle > 0 {
				return Entry{}, errors.New("revision would create a lineage dependency cycle")
			}
		}
	}
	if previousID != "" {
		// Invalidate all dependent interpretations, not just direct children.
		_, err = tx.ExecContext(ctx, `WITH RECURSIVE affected(id) AS (
 SELECT ? UNION SELECT l.entry_id FROM links l JOIN affected a ON l.target_id=a.id
 UNION SELECT r.entry_id FROM reviewed_sources r JOIN affected a ON r.source_id=a.id
) UPDATE entries SET needs_review=1 WHERE id IN (SELECT id FROM affected) AND id<>?`, previousID, previousID)
		if err != nil {
			return Entry{}, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE entries SET current=0 WHERE id=?", previousID); err != nil {
			return Entry{}, err
		}
		// A source may have become stale through the invalidation above.
		for _, l := range d.Links {
			if l.Relation != "supports" {
				continue
			}
			var stale bool
			if err = tx.QueryRowContext(ctx, "SELECT needs_review FROM entries WHERE id=?", l.TargetID).Scan(&stale); err != nil {
				return Entry{}, err
			}
			if stale {
				return Entry{}, errors.New("source became stale during revision")
			}
		}
	}
	// Check after invalidation too: a background reference may have become stale
	// while revising its ancestor. It can remain a citation, never sole grounding.
	grounded := false
	for _, l := range d.Links {
		if l.Relation == "contradicts" {
			continue
		}
		source, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", l.TargetID))
		if err != nil {
			return Entry{}, err
		}
		valid, err := validGround(ctx, tx, source)
		if err != nil {
			return Entry{}, err
		}
		grounded = grounded || valid
	}
	if (d.Kind == "hypothesis" || d.Kind == "case" && d.Profile != nil) && !grounded && !d.experienceValidated {
		return Entry{}, errors.New("case/hypothesis needs at least one current active reviewed record or case as support or provenance; stale/context-only invalid material and contradiction targets cannot ground a new conclusion; preserve a factual correction as a record or add fresh evidence")
	}
	// Last read gate inside the write transaction; an error rolls everything
	// back with no partial rows, request receipt or index updates.
	if d.preCommit != nil {
		if err = d.preCommit(ctx, tx); err != nil {
			return Entry{}, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (`+columns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.RootID, e.PreviousID, e.Kind, e.Scope, e.Title, e.Body, e.Origin, e.SourceRef, e.Host, e.Model, e.Session, e.CreatedAt, e.Current, e.NeedsReview)
	if err != nil {
		return Entry{}, err
	}
	order, err := nextOperationOrder(ctx, tx)
	if err != nil {
		return Entry{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE entries SET write_order=? WHERE id=?", order, e.ID); err != nil {
		return Entry{}, err
	}
	for _, l := range e.Links {
		if _, err = tx.ExecContext(ctx, "INSERT INTO links(entry_id,target_id,relation) VALUES (?,?,?)", e.ID, l.TargetID, l.Relation); err != nil {
			return Entry{}, err
		}
	}
	if d.Profile != nil {
		if err = validateProfile(d.Profile); err != nil {
			return Entry{}, err
		}
		profile, _ := json.Marshal(d.Profile)
		if _, err = tx.ExecContext(ctx, "INSERT INTO entry_profiles(entry_id,profile_json) VALUES(?,?)", e.ID, string(profile)); err != nil {
			return Entry{}, err
		}
		e.Profile = d.Profile
	}
	if d.experienceValidated || d.referenceValidated {
		if _, err = tx.ExecContext(ctx, "DELETE FROM experience_keys WHERE scope=? AND root_id=?", d.Scope, e.RootID); err != nil {
			return Entry{}, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO experience_keys(scope,fingerprint,root_id)VALUES(?,?,?)", d.Scope, d.experienceKey, e.RootID); err != nil {
			return Entry{}, err
		}
	}
	if (d.Kind == "case" || d.Kind == "hypothesis") && d.ContextSourceID != "" {
		if _, err = userSource(ctx, tx, d.ContextSourceID, d.Scope); err != nil {
			return Entry{}, err
		}
		if d.Kind == "case" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO turn_case_writes(entry_id,source_id)VALUES(?,?)`, e.ID, d.ContextSourceID); err != nil {
				return Entry{}, err
			}
			e.RecordedSourceID = d.ContextSourceID
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memory_candidates SET state='saved',review_at_stop=0,updated_at=? WHERE source_id=? AND state='pending'`, e.CreatedAt, d.ContextSourceID); err != nil {
			return Entry{}, err
		}
	}
	if d.Kind == "case" || d.Kind == "hypothesis" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO maintenance_policy(scope,changes,next_at) VALUES(?,1,100)
 ON CONFLICT(scope) DO UPDATE SET changes=changes+1`, d.Scope); err != nil {
			return Entry{}, err
		}
	}
	if d.RequestID != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO write_requests(scope,request_id,fingerprint,entry_id) VALUES (?,?,?,?)", d.Scope, d.RequestID, fingerprint, e.ID); err != nil {
			return Entry{}, err
		}
	}
	// The immediate write response must report the same effective management
	// state/profile as a subsequent read, including unchanged lineage state.
	if err = loadGovernance(ctx, tx, &e); err != nil {
		return Entry{}, err
	}
	if err = indexRecallEntry(ctx, tx, e); err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

type ReadResult struct {
	ContentRole          string      `json:"content_role,omitempty"`
	EffectiveCurrentID   string      `json:"effective_current_id"`
	Entry                Entry       `json:"entry"`
	CurrentID            string      `json:"current_id"`
	ReferencedBy         []Reference `json:"referenced_by"`
	ReferencesHasMore    bool        `json:"references_has_more"`
	ReferencesNextCursor string      `json:"references_next_cursor,omitempty"`
}
type Reference struct {
	EntryID     string `json:"entry_id"`
	Relation    string `json:"relation"`
	Kind        string `json:"kind"`
	NeedsReview bool   `json:"needs_review"`
}

func (s *Store) Read(ctx context.Context, id string) (ReadResult, error) {
	return s.ReadPage(ctx, id, "")
}

func (s *Store) ReadPage(ctx context.Context, id, referenceCursor string) (ReadResult, error) {
	var anchor pageCursor
	var err error
	filter := digestText("references:" + id)
	if referenceCursor != "" {
		anchor, err = decodeCursor(referenceCursor, filter)
		if err != nil {
			return ReadResult{}, err
		}
		switch anchor.Sort {
		case "supports", "contradicts", "context":
		default:
			return ReadResult{}, errors.New("invalid reference cursor")
		}
	}
	// One snapshot prevents a concurrent revision from mixing old/new states.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ReadResult{}, err
	}
	defer tx.Rollback()
	e, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return ReadResult{}, missingLookup("memory_entry_not_found", "id", id, "", "Use the full ID and verify the same service/data_dir; absence of one ID is not proof that all reads or writes are broken.")
	}
	if err != nil {
		return ReadResult{}, err
	}
	r := ReadResult{Entry: e, ReferencedBy: []Reference{}}
	if err = tx.QueryRowContext(ctx, "SELECT id FROM entries WHERE root_id=? AND current=1", e.RootID).Scan(&r.CurrentID); err != nil {
		return r, err
	}
	r.Entry.Links, err = loadLinks(ctx, tx, id)
	if err != nil {
		return r, err
	}
	if err = loadGovernance(ctx, tx, &r.Entry); err != nil {
		return r, err
	}
	if err = loadRecordedSource(ctx, tx, &r.Entry); err != nil {
		return r, err
	}
	effective, er := resolveCurrent(ctx, tx, id)
	if er != nil {
		return r, er
	}
	r.EffectiveCurrentID = effective.ID
	query := `SELECT l.entry_id,l.relation,e.kind,e.needs_review FROM links l JOIN entries e ON e.id=l.entry_id WHERE l.target_id=? AND e.current=1`
	args := []any{id}
	if referenceCursor != "" {
		query += " AND (l.entry_id>? OR (l.entry_id=? AND l.relation>?))"
		args = append(args, anchor.ID, anchor.ID, anchor.Sort)
	}
	query += " ORDER BY l.entry_id,l.relation LIMIT 101"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var v Reference
		if err = rows.Scan(&v.EntryID, &v.Relation, &v.Kind, &v.NeedsReview); err != nil {
			rows.Close()
			return r, err
		}
		r.ReferencedBy = append(r.ReferencedBy, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	if len(r.ReferencedBy) > 100 {
		r.ReferencesHasMore = true
		r.ReferencedBy = r.ReferencedBy[:100]
		last := r.ReferencedBy[99]
		r.ReferencesNextCursor = encodeCursor(pageCursor{Version: 1, Filter: filter, ID: last.EntryID, Sort: last.Relation})
	}
	return r, tx.Commit()
}

type SearchInput struct {
	NeedsReview     *bool  `json:"needs_review,omitempty" jsonschema:"Filter source-change review queue; false selects entries without that flag"`
	IncludeInactive bool   `json:"include_inactive,omitempty" jsonschema:"Also list entries a cleanup archived or superseded. A revision replaced by a newer one is reached through memory_read and its entry.previous_id, not here."`
	Scope           string `json:"scope" jsonschema:"Required explicit scope: project:<identifier> or global"`
	Query           string `json:"query,omitempty" jsonschema:"Literal keywords separated by spaces; a record is listed when any one of them appears as a case-insensitive substring of its title, body or structured profile. Empty lists the recent entries. No ranking and no semantic model; memory_experience_lookup ranks."`
	IncludeGlobal   bool   `json:"include_global,omitempty" jsonschema:"Also include global entries when searching a project; opt in explicitly"`
	Kind            string `json:"kind,omitempty" jsonschema:"Optional filter: case for experiences, record for agreement references and earlier notes; hypothesis appears only in history."`
	Limit           int    `json:"limit,omitempty" jsonschema:"Result limit, default 10, maximum 30"`
	Cursor          string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the prior memory_search result. Keep scope, query and every filter unchanged; only the page size may change."`
	IncludeEvents   bool   `json:"include_events,omitempty" jsonschema:"Opt in to raw hook input/output events. Normally hidden so captured chat is not mistaken for a business decision."`
}
type Hit struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	Title       string `json:"title"`
	Excerpt     string `json:"excerpt"`
	Origin      string `json:"origin"`
	NeedsReview bool   `json:"needs_review"`
	CreatedAt   string `json:"created_at"`
}

func (s *Store) Search(ctx context.Context, in SearchInput) ([]Hit, error) {
	page, err := s.SearchPage(ctx, in)
	return page.Results, err
}

type SearchPage struct {
	Results    []Hit  `json:"results"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func (s *Store) SearchPage(ctx context.Context, in SearchInput) (SearchPage, error) {
	if in.Scope != "global" && (!strings.HasPrefix(in.Scope, "project:") || strings.TrimSpace(strings.TrimPrefix(in.Scope, "project:")) == "") {
		return SearchPage{}, errors.New("explicit scope required")
	}
	query, words, err := normalizeSearchQuery(in.Query)
	if err != nil {
		return SearchPage{}, err
	}
	in.Query = query
	if in.Kind != "" && in.Kind != "record" && in.Kind != "case" && in.Kind != "hypothesis" {
		return SearchPage{}, errors.New("kind must be case, record or hypothesis: case covers experiences, record covers agreement references and earlier notes, hypothesis appears only in history")
	}
	if in.Limit == 0 {
		in.Limit = 10
	}
	if in.Limit < 1 || in.Limit > 30 {
		return SearchPage{}, errors.New("limit must be 1..30")
	}
	filter := searchFingerprint(in)
	var anchor pageCursor
	if in.Cursor != "" {
		var err error
		anchor, err = decodeCursor(in.Cursor, filter)
		if err != nil {
			return SearchPage{}, err
		}
		// Seek by the exact values used in ORDER BY. Imported history can
		// carry a legacy timestamp format or an empty timestamp.
	}
	q := "SELECT id,kind,scope,title,body,origin,needs_review,created_at FROM entries WHERE current=1 AND (scope=?"
	args := []any{in.Scope}
	if in.IncludeGlobal && in.Scope != "global" {
		q += " OR scope='global'"
	}
	q += ")"
	if !in.IncludeInactive {
		q += " AND NOT EXISTS (SELECT 1 FROM managed_entries m WHERE m.root_id=entries.root_id AND m.state<>'active')"
	}
	if in.NeedsReview != nil {
		q += " AND needs_review=?"
		args = append(args, *in.NeedsReview)
	}
	q += " AND source_ref NOT LIKE 'maintenance-event:%'"
	if !in.IncludeEvents {
		q += " AND source_ref NOT LIKE 'hook-event:%'"
	}
	if in.Kind != "" {
		q += " AND kind=?"
		args = append(args, in.Kind)
	}
	if len(words) > 0 {
		// One bounded JSON parameter avoids SQL expression-depth/variable limits
		// without truncating terms or changing literal OR matching/pagination.
		encoded, _ := json.Marshal(words)
		q += " AND EXISTS (SELECT 1 FROM json_each(?) term WHERE instr(lower(title || char(10) || body || char(10) || coalesce((SELECT profile_json FROM entry_profiles p WHERE p.entry_id=entries.id),'')),lower(term.value))>0)"
		args = append(args, string(encoded))
	}
	if in.Cursor != "" {
		q += " AND (created_at<? OR (created_at=? AND id<?))"
		args = append(args, anchor.Sort, anchor.Sort, anchor.ID)
	}
	q += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, in.Limit+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return SearchPage{}, err
	}
	defer rows.Close()
	hits := []Hit{}
	for rows.Next() {
		var h Hit
		var body string
		if err = rows.Scan(&h.ID, &h.Kind, &h.Scope, &h.Title, &body, &h.Origin, &h.NeedsReview, &h.CreatedAt); err != nil {
			return SearchPage{}, err
		}
		h.Excerpt = excerpt(body, words)
		hits = append(hits, h)
	}
	if err = rows.Err(); err != nil {
		return SearchPage{}, err
	}
	page := SearchPage{Results: hits}
	if len(hits) > in.Limit {
		page.HasMore = true
		page.Results = hits[:in.Limit]
		last := page.Results[in.Limit-1]
		page.NextCursor = encodeCursor(pageCursor{Version: 1, Filter: filter, ID: last.ID, Sort: last.CreatedAt})
	}
	return page, nil
}

func excerpt(body string, words []string) string {
	r := []rune(body)
	start := 0
	lowerBody := strings.ToLower(body)
	for _, w := range words {
		i := strings.Index(lowerBody, strings.ToLower(w))
		if i >= 0 {
			start = utf8.RuneCountInString(lowerBody[:i]) - 100
			if start < 0 {
				start = 0
			}
			break
		}
	}
	end := start + 500
	if end > len(r) {
		end = len(r)
	}
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(r) {
		suffix = "…"
	}
	return prefix + string(r[start:end]) + suffix
}

type Snapshot struct {
	SchemaVersion int     `json:"schema_version"`
	ExportedAt    string  `json:"exported_at"`
	Entries       []Entry `json:"entries"`
}

func (s *Store) Export(ctx context.Context) (Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()
	out := Snapshot{SchemaVersion: SchemaVersion, ExportedAt: time.Now().UTC().Format(time.RFC3339Nano), Entries: []Entry{}}
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM entries ORDER BY created_at,id")
	if err != nil {
		return out, err
	}
	positions := map[string]int{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		positions[e.ID] = len(out.Entries)
		out.Entries = append(out.Entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for i := range out.Entries {
		if err = loadGovernance(ctx, tx, &out.Entries[i]); err != nil {
			return out, err
		}
		if err = loadRecordedSource(ctx, tx, &out.Entries[i]); err != nil {
			return out, err
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT entry_id,target_id,relation FROM links ORDER BY entry_id,target_id,relation")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		var l Link
		if err = rows.Scan(&id, &l.TargetID, &l.Relation); err != nil {
			rows.Close()
			return out, err
		}
		i := positions[id]
		out.Entries[i].Links = append(out.Entries[i].Links, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// Forget removes the complete revision lineage AND dependent interpretations.
// It cannot erase copies in exports, host transcripts, backups, or storage snapshots.
func (s *Store) Forget(ctx context.Context, id string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE id=?", id).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, errors.New("entry not found")
	}
	// Compute the closure before FK cascades remove its edges.
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE doomed(id) AS (
 SELECT id FROM entries WHERE id=?
 UNION SELECT e.id FROM entries e JOIN entries old ON e.root_id=old.root_id JOIN doomed d ON old.id=d.id
 UNION SELECT l.entry_id FROM links l JOIN doomed d ON l.target_id=d.id
) SELECT id FROM doomed`, id)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, v := range ids {
		// Management must not leave live replacement pointers into forgotten data,
		// nor retain derived summaries/plan excerpts for the forgotten lineages.
		if _, err = tx.ExecContext(ctx, `UPDATE managed_entries SET state='archived',replacement_root_id='',reason='Replacement was explicitly forgotten',version=version+1 WHERE replacement_root_id=(SELECT root_id FROM entries WHERE id=?)`, v); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM managed_entries WHERE root_id=(SELECT root_id FROM entries WHERE id=?)", v); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM cleanup_plans WHERE instr(operations_json,?)>0 OR instr(snapshots_json,?)>0 OR approval_source_id=?", v, v, v); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM source_review_receipts WHERE instr(sources_json,?)>0", v); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM context_checks WHERE instr(selected_json,?)>0", v); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM entries WHERE id=?", v); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	// Best-effort compaction; logical deletion has already committed.
	s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	s.db.ExecContext(ctx, "VACUUM")
	return int64(len(ids)), nil
}

func WriteExport(path string, snapshot Snapshot) error {
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	// Refuse overwrites, including symlinks, so a typo cannot replace other files.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
