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
)

const SchemaVersion = 2

// Entry bodies are free text. These fields track provenance, not semantic truth.
type Entry struct {
	ID          string `json:"id"`
	RootID      string `json:"root_id"`
	PreviousID  string `json:"previous_id,omitempty"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	BodyBytes   int    `json:"body_bytes"`
	BodySHA256  string `json:"body_sha256"`
	Origin      string `json:"origin"`
	SourceRef   string `json:"source_ref,omitempty"`
	Host        string `json:"host,omitempty"`
	Model       string `json:"model,omitempty"`
	Session     string `json:"session,omitempty"`
	CreatedAt   string `json:"created_at"`
	Current     bool   `json:"current"`
	NeedsReview bool   `json:"needs_review"`
	Links       []Link `json:"links"`
}

type Link struct {
	TargetID string `json:"target_id" jsonschema:"ID of an existing source entry; read it before interpreting it"`
	Relation string `json:"relation" jsonschema:"supports, contradicts, or context; this describes the host's interpretation, not verified truth"`
}

type Draft struct {
	Kind      string `json:"kind" jsonschema:"record for supplied material; case for a contextual decision; hypothesis is only created through memory_revise"`
	Scope     string `json:"scope" jsonschema:"Explicit scope: project:<stable project identifier> or global; never infer global applicability"`
	Title     string `json:"title" jsonschema:"Short descriptive title, not a mandatory preference conclusion"`
	Body      string `json:"body" jsonschema:"Free text preserving context, alternatives, exact feedback when available, and unknowns; distinguish quotes from inference"`
	Origin    string `json:"origin" jsonschema:"host_quote (host-relayed quotation, not verified transcript), user_supplied, imported, or model_inference"`
	SourceRef string `json:"source_ref,omitempty" jsonschema:"Optional source locator; use only a real supplied reference, never invent one"`
	Host      string `json:"host,omitempty" jsonschema:"Host name if known; omit rather than guess"`
	Model     string `json:"model,omitempty" jsonschema:"Model identifier if actually known; omit rather than guess"`
	Session   string `json:"session,omitempty" jsonschema:"Optional originating session identifier"`
	Links     []Link `json:"links,omitempty" jsonschema:"Optional links to existing supporting, contradicting, or contextual entries"`
	RequestID string `json:"request_id,omitempty" jsonschema:"Optional stable ID for one logical write. Reuse it with the same payload when retrying an uncertain response; use a new ID for a different write. Scoped to this project or global scope."`
}

type Store struct {
	db  *sql.DB
	Dir string
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
		// Independent hook processes may race the first WAL transition. SQLite
		// can report BUSY for that PRAGMA before its busy handler can wait.
		deadline := time.Now().Add(3 * time.Second)
		for {
			err := conn.Exec(`PRAGMA busy_timeout=10000; PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA secure_delete=ON;`)
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
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) init() error {
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
	return tx.Commit()
}

func Validate(d Draft) error {
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
	if strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Body) == "" {
		return errors.New("title and body are required")
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
			return entry, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Entry{}, err
		}
	}
	if previousID != "" {
		old, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", previousID))
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, errors.New("revision target not found")
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
	grounded := false
	for _, l := range d.Links {
		source, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", l.TargetID))
		if err != nil {
			return Entry{}, fmt.Errorf("source %s not found or unavailable", l.TargetID)
		}
		if source.Scope != d.Scope && source.Scope != "global" {
			return Entry{}, errors.New("source outside scope: global hypotheses cannot depend on project data; projects may use their own or global sources")
		}
		if !source.Current || source.NeedsReview {
			return Entry{}, errors.New("source is superseded or needs review; resolve it before deriving new content")
		}
		if previousID != "" {
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
		if source.Kind != "hypothesis" {
			grounded = true
		}
	}
	if d.Kind == "hypothesis" && !grounded {
		return Entry{}, errors.New("hypothesis needs at least one record or case; repeated model hypotheses alone are not evidence")
	}
	if previousID != "" {
		// Invalidate all dependent interpretations, not just direct children.
		_, err = tx.ExecContext(ctx, `WITH RECURSIVE affected(id) AS (
 SELECT ? UNION SELECT l.entry_id FROM links l JOIN affected a ON l.target_id=a.id
) UPDATE entries SET needs_review=1 WHERE id IN (SELECT id FROM affected) AND id<>?`, previousID, previousID)
		if err != nil {
			return Entry{}, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE entries SET current=0 WHERE id=?", previousID); err != nil {
			return Entry{}, err
		}
		// A source may have become stale through the invalidation above.
		for _, l := range d.Links {
			var stale bool
			if err = tx.QueryRowContext(ctx, "SELECT needs_review FROM entries WHERE id=?", l.TargetID).Scan(&stale); err != nil {
				return Entry{}, err
			}
			if stale {
				return Entry{}, errors.New("source became stale during revision")
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (`+columns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.RootID, e.PreviousID, e.Kind, e.Scope, e.Title, e.Body, e.Origin, e.SourceRef, e.Host, e.Model, e.Session, e.CreatedAt, e.Current, e.NeedsReview)
	if err != nil {
		return Entry{}, err
	}
	for _, l := range e.Links {
		if _, err = tx.ExecContext(ctx, "INSERT INTO links(entry_id,target_id,relation) VALUES (?,?,?)", e.ID, l.TargetID, l.Relation); err != nil {
			return Entry{}, err
		}
	}
	if d.RequestID != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO write_requests(scope,request_id,fingerprint,entry_id) VALUES (?,?,?,?)", d.Scope, d.RequestID, fingerprint, e.ID); err != nil {
			return Entry{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

type ReadResult struct {
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
	}
	// One snapshot prevents a concurrent revision from mixing old/new states.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ReadResult{}, err
	}
	defer tx.Rollback()
	e, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
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
	Scope         string `json:"scope" jsonschema:"Required explicit scope: project:<identifier> or global"`
	Query         string `json:"query,omitempty" jsonschema:"Literal keywords separated by spaces; empty returns recent entries; no semantic model is used"`
	IncludeGlobal bool   `json:"include_global,omitempty" jsonschema:"Also include global entries when searching a project; opt in explicitly"`
	Kind          string `json:"kind,omitempty" jsonschema:"Optional record, case, or hypothesis filter"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Result limit, default 10, maximum 30"`
	Cursor        string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from a prior result; keep scope, query, include_global and kind unchanged"`
	IncludeEvents bool   `json:"include_events,omitempty" jsonschema:"Opt in to raw hook input/output events. Normally hidden so captured chat is not mistaken for a business decision."`
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
	if len(in.Query) > 2048 {
		return SearchPage{}, errors.New("query too long")
	}
	if in.Kind != "" && in.Kind != "record" && in.Kind != "case" && in.Kind != "hypothesis" {
		return SearchPage{}, errors.New("invalid kind")
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
		if _, err = time.Parse(time.RFC3339Nano, anchor.Sort); err != nil {
			return SearchPage{}, errors.New("invalid search cursor")
		}
	}
	q := "SELECT id,kind,scope,title,body,origin,needs_review,created_at FROM entries WHERE current=1 AND (scope=?"
	args := []any{in.Scope}
	if in.IncludeGlobal && in.Scope != "global" {
		q += " OR scope='global'"
	}
	q += ")"
	if !in.IncludeEvents {
		q += " AND source_ref NOT LIKE 'hook-event:%'"
	}
	if in.Kind != "" {
		q += " AND kind=?"
		args = append(args, in.Kind)
	}
	words := strings.Fields(in.Query)
	if len(words) > 8 {
		return SearchPage{}, errors.New("at most 8 keywords per search")
	}
	if len(words) > 0 {
		q += " AND ("
		for i, w := range words {
			if i > 0 {
				q += " OR "
			}
			q += "instr(lower(title || char(10) || body),lower(?))>0"
			args = append(args, w)
		}
		q += ")"
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
