package memory

// Hook tables are an optional extension of the v2 database. Existing entry,
// revision, backup and forget semantics remain unchanged; raw events are entries.
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const HookSourcePrefix = "hook-event:"

func hookRequestID(session, event string) string {
	b, _ := json.Marshal([]string{session, event})
	return "zcode-user:" + digestText(string(b))
}

type HookTurn struct {
	CaseIDs          []string          `json:"case_ids,omitempty"`
	ReusedCaseIDs    []string          `json:"reused_case_ids,omitempty"`
	ResponseMode     string            `json:"response_mode,omitempty"`
	SourceID         string            `json:"source_id"`
	Scope            string            `json:"scope"`
	Session          string            `json:"session,omitempty"`
	CWD              string            `json:"cwd,omitempty"`
	Query            string            `json:"query,omitempty"`
	AnswerID         string            `json:"answer_id,omitempty"`
	Attempts         int               `json:"review_attempts"`
	Status           string            `json:"status"`
	Recall           string            `json:"-"`
	Note             string            `json:"note,omitempty"`
	Created          string            `json:"created_at,omitempty"`
	ReviewNote       string            `json:"review_note,omitempty"`
	ReviewNoteBytes  int               `json:"review_note_bytes,omitempty"`
	ReviewNoteSHA256 string            `json:"review_note_sha256,omitempty"`
	TextIssues       []textguard.Issue `json:"text_issues,omitempty"`
}

func (s *Store) InitHooks(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS hook_bindings(path TEXT PRIMARY KEY,scope TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS hook_turns(source_id TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
 scope TEXT NOT NULL,session TEXT NOT NULL,cwd TEXT NOT NULL,query_text TEXT NOT NULL,
 answer_id TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'pending',
 recall TEXT NOT NULL DEFAULT '',note TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS hook_turn_session ON hook_turns(scope,session,created_at DESC);
 CREATE TABLE IF NOT EXISTS hook_runs(id INTEGER PRIMARY KEY AUTOINCREMENT,event TEXT NOT NULL,scope TEXT NOT NULL,
 session TEXT NOT NULL,result TEXT NOT NULL,detail TEXT NOT NULL,created_at TEXT NOT NULL);`)
	return err
}

func canonicalPath(p string) (string, error) {
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if r, e := filepath.EvalSymlinks(a); e == nil {
		a = r
	}
	a = filepath.Clean(a)
	if runtime.GOOS == "windows" {
		a = strings.ToLower(a)
	}
	return a, nil
}
func (s *Store) HookBind(ctx context.Context, p, scope string) error {
	if !strings.HasPrefix(scope, "project:") || strings.TrimSpace(strings.TrimPrefix(scope, "project:")) == "" {
		return errors.New("hook binding requires project:<identifier>; global is not inferred")
	}
	p, err := canonicalPath(p)
	if err != nil {
		return err
	}
	if err = s.InitHooks(ctx); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO hook_bindings(path,scope)VALUES(?,?)`, p, scope)
	if err != nil {
		return err
	}
	var existing string
	if err = s.db.QueryRowContext(ctx, `SELECT scope FROM hook_bindings WHERE path=?`, p).Scan(&existing); err != nil {
		return err
	}
	if existing != scope {
		return errors.New("project already bound to " + existing + "; refusing to silently replace its scope")
	}
	return nil
}
func (s *Store) HookResolve(ctx context.Context, cwd string) (string, error) {
	if cwd == "" {
		return "", errors.New("hook event has no cwd")
	}
	p, err := canonicalPath(cwd)
	if err != nil {
		return "", err
	}
	if err = s.InitHooks(ctx); err != nil {
		return "", err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT path,scope FROM hook_bindings ORDER BY length(path) DESC`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var base, scope string
		if err = rows.Scan(&base, &scope); err != nil {
			return "", err
		}
		rel, e := filepath.Rel(base, p)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return scope, nil
		}
	}
	return "", errors.New("project is not bound; run setup for this project; no global fallback")
}

const hookColumns = "source_id,scope,session,cwd,query_text,answer_id,attempts,status,recall,note,created_at"

type HookBinding struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
}

func (s *Store) HookBindings(ctx context.Context) ([]HookBinding, error) {
	if err := s.InitHooks(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT path,scope FROM hook_bindings ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HookBinding{}
	for rows.Next() {
		var b HookBinding
		if err = rows.Scan(&b.Path, &b.Scope); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func scanTurn(row rowScanner) (HookTurn, error) {
	var t HookTurn
	err := row.Scan(&t.SourceID, &t.Scope, &t.Session, &t.CWD, &t.Query, &t.AnswerID, &t.Attempts, &t.Status, &t.Recall, &t.Note, &t.Created)
	var receipt struct {
		Explanation string `json:"explanation"`
	}
	if textguard.JSON([]byte(t.Note)) == nil && json.Unmarshal([]byte(t.Note), &receipt) == nil {
		t.ReviewNote = receipt.Explanation
		t.ReviewNoteBytes = len(receipt.Explanation)
		t.ReviewNoteSHA256 = digestText(receipt.Explanation)
	}
	if issue := textguard.Inspect("note", t.Note); issue != nil {
		t.TextIssues = []textguard.Issue{*issue}
	}
	return t, err
}
func (s *Store) HookLatest(ctx context.Context, scope, session string) (HookTurn, error) {
	return scanTurn(s.db.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE scope=? AND session=? ORDER BY created_at DESC,source_id DESC LIMIT 1", scope, session))
}
func (s *Store) HookTurn(ctx context.Context, id string) (HookTurn, error) {
	return scanTurn(s.db.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE source_id=?", id))
}
func (s *Store) HookStart(ctx context.Context, scope, session, cwd, prompt, eventID string) (HookTurn, error) {
	return s.HookStartForHost(ctx, "zcode", scope, session, cwd, prompt, eventID)
}
func (s *Store) HookStartForHost(ctx context.Context, host, scope, session, cwd, prompt, eventID string) (HookTurn, error) {
	if host != "zcode" && host != "codex" && host != "qoder" && host != "qoder-ide" {
		return HookTurn{}, errors.New("unsupported hook host")
	}
	if err := s.InitHooks(ctx); err != nil {
		return HookTurn{}, err
	}
	if session == "" || strings.TrimSpace(prompt) == "" {
		return HookTurn{}, errors.New("session_id and nonempty prompt are required")
	}
	if eventID != "" {
		var id string
		err := s.db.QueryRowContext(ctx, `SELECT entry_id FROM write_requests WHERE scope=? AND request_id=?`, scope, hostRequestID(host, session, eventID)).Scan(&id)
		if err == nil {
			r, e := s.Read(ctx, id)
			if e != nil {
				return HookTurn{}, e
			}
			if r.Entry.Body != prompt {
				return HookTurn{}, errors.New("hook event ID reused with different prompt")
			}
			if t, e := s.HookTurn(ctx, id); e == nil {
				return t, nil
			}
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return HookTurn{}, err
		}
	}
	prev, err := s.HookLatestForHost(ctx, host, scope, session)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HookTurn{}, err
	}
	query := prompt
	switch strings.Trim(strings.TrimSpace(prompt), "。.!！ ") {
	case "继续", "好的", "好", "可以", "continue", "ok":
		if prev.Query != "" {
			query = prev.Query
		}
	}
	d := Draft{Kind: "record", Scope: scope, Title: "原始用户输入（尚未整理为业务决策）", Body: prompt, Origin: "host_quote", Host: host, Session: session, SourceRef: HookSourcePrefix + host + ":user", AllowReplacementCharacters: true}
	if eventID != "" {
		d.RequestID = hostRequestID(host, session, eventID)
	}
	if prev.AnswerID != "" {
		if r, e := s.Read(ctx, prev.AnswerID); e == nil && r.Entry.Current {
			d.Links = []Link{{TargetID: prev.AnswerID, Relation: "context"}}
		}
	}
	e, err := s.Put(ctx, d, "")
	if err != nil {
		return HookTurn{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO hook_turns(source_id,scope,session,cwd,query_text,created_at)VALUES(?,?,?,?,?,?)`, e.ID, scope, session, cwd, query, e.CreatedAt)
	if err != nil {
		return HookTurn{}, err
	}
	return s.HookTurn(ctx, e.ID)
}

type ReviewInput struct {
	ResponseMode               string   `json:"response_mode,omitempty" jsonschema:"compact: short review receipt with note integrity metadata; full or omitted: legacy response"`
	ReusedCaseIDs              []string `json:"reused_case_ids,omitempty" jsonschema:"Existing current cases fully covering this turn, without changing their body or source links; read first. Does not create revisions or invalidate dependents."`
	Scope                      string   `json:"scope"`
	SourceID                   string   `json:"source_id" jsonschema:"Raw user entry ID delivered by a hook. Read it and its contextual links before reviewing."`
	CaseIDs                    []string `json:"case_ids,omitempty" jsonschema:"Current cases in this project linked OR transactionally associated to source_id. Do not revise to add a bookkeeping link. Empty only when none are associated and no case needs writing."`
	Note                       string   `json:"note" jsonschema:"Explain cases saved, reused or revised, or why no case needs creating or revising. This is host reasoning, not independent verification."`
	AllowReplacementCharacters bool     `json:"allow_replacement_characters,omitempty" jsonschema:"Only true for U+FFFD intentionally present in the original note, not for forcing through already damaged text."`
}

func (s *Store) HookReviewDone(ctx context.Context, in ReviewInput) (HookTurn, error) {
	if err := ValidateResponseMode(in.ResponseMode); err != nil {
		return HookTurn{}, err
	}
	if err := textguard.Check("note", in.Note, in.AllowReplacementCharacters); err != nil {
		return HookTurn{}, err
	}
	if err := s.InitHooks(ctx); err != nil {
		return HookTurn{}, err
	}
	t, err := s.HookTurn(ctx, in.SourceID)
	if err != nil {
		return t, err
	}
	if t.Scope != in.Scope {
		return t, errors.New("source scope mismatch")
	}
	if strings.TrimSpace(in.Note) == "" {
		return t, errors.New("review note required")
	}
	if len(in.CaseIDs)+len(in.ReusedCaseIDs) > 20 || len(in.Note) > 8192 {
		return t, errors.New("review receipt too large")
	}
	if in.CaseIDs == nil {
		in.CaseIDs = []string{}
	}
	for _, id := range in.ReusedCaseIDs {
		r, e := s.Read(ctx, id)
		if e != nil {
			return t, e
		}
		if !r.Entry.Current || r.Entry.Scope != t.Scope || r.Entry.Kind != "case" || r.Entry.NeedsReview || r.Entry.State.State != "active" {
			return t, errors.New("reused case must be current, active, source-reviewed and in this project")
		}
	}
	receipt := map[string]any{"case_ids": in.CaseIDs, "explanation": in.Note}
	if len(in.ReusedCaseIDs) > 0 {
		receipt["reused_case_ids"] = in.ReusedCaseIDs
	}
	payload, _ := json.Marshal(receipt)
	if t.Status == "reviewed" || t.Status == "skipped" {
		if t.Note == string(payload) {
			return t, nil
		}
		return t, errors.New("review already completed; this receipt is closed and unchanged. Stop memory housekeeping for this turn; do not revise a case merely to change the receipt")
	}
	for _, id := range in.CaseIDs {
		r, e := s.Read(ctx, id)
		if e != nil {
			return t, e
		}
		if !r.Entry.Current || r.Entry.Scope != t.Scope || r.Entry.Kind != "case" || r.Entry.State.State != "active" {
			return t, errors.New("receipt must reference a current case in the source project")
		}
		linked := r.Entry.RecordedSourceID == t.SourceID
		for _, l := range r.Entry.Links {
			if l.TargetID == t.SourceID {
				linked = true
			}
		}
		if !linked {
			return t, fmt.Errorf("case %s must link the original user source", id)
		}
	}
	status := "reviewed"
	if len(in.CaseIDs) == 0 && len(in.ReusedCaseIDs) == 0 {
		status = "skipped"
	}
	query := `UPDATE hook_turns SET status=?,note=? WHERE source_id=? AND status IN ('pending','review_requested')`
	args := []any{status, string(payload), t.SourceID}
	if len(in.CaseIDs) == 0 {
		// Check in the UPDATE itself so another process cannot attach a current
		// case between a preflight check and committing an irreversible skip.
		query += ` AND NOT EXISTS (` + casesForSourceSQL + `)`
		args = append(args, t.Scope, t.SourceID, t.SourceID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	for _, id := range append(append([]string{}, in.CaseIDs...), in.ReusedCaseIDs...) {
		var current bool
		var scope, kind, root string
		var needs bool
		if err = tx.QueryRowContext(ctx, "SELECT current,scope,kind,root_id,needs_review FROM entries WHERE id=?", id).Scan(&current, &scope, &kind, &root, &needs); err != nil {
			return t, err
		}
		m, er := stateFor(ctx, tx, root)
		if er != nil {
			return t, er
		}
		if !current || scope != t.Scope || kind != "case" || m.State != "active" {
			return t, errors.New("case changed while recording receipt; read its current version")
		}
		for _, reused := range in.ReusedCaseIDs {
			if reused == id && needs {
				return t, errors.New("reused case became stale")
			}
		}
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return t, err
	}
	n, _ := result.RowsAffected()
	current, err := scanTurn(tx.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE source_id=?", t.SourceID))
	if err == nil && n == 0 && len(in.CaseIDs) == 0 && (current.Status == "pending" || current.Status == "review_requested") {
		rows, e := tx.QueryContext(ctx, casesForSourceSQL+` ORDER BY e.id LIMIT 20`, t.Scope, t.SourceID, t.SourceID)
		if e != nil {
			return current, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return current, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return current, e
		}
		if len(ids) > 0 {
			return current, fmt.Errorf("source already has current cases in this project: %s; supply these case_ids without creating duplicates; review status was not changed", strings.Join(ids, ", "))
		}
	}
	if err == nil && n == 0 && current.Note != string(payload) {
		return current, errors.New("review receipt conflict")
	}
	if err == nil && n > 0 {
		candidateState := "dismissed"
		if len(in.CaseIDs) > 0 {
			candidateState = "saved"
		}
		if _, e := tx.ExecContext(ctx, `UPDATE memory_candidates SET state=?,reason=?,review_at_stop=0,updated_at=? WHERE source_id=? AND state='pending'`, candidateState, in.Note, time.Now().UTC().Format(time.RFC3339Nano), t.SourceID); e != nil {
			return current, e
		}
		for _, id := range in.ReusedCaseIDs {
			if _, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO turn_case_reuse(source_id,case_id,note)VALUES(?,?,?)", t.SourceID, id, in.Note); e != nil {
				return current, e
			}
		}
	}
	if err != nil {
		return current, err
	}
	return current, tx.Commit()
}
func (s *Store) HookLog(ctx context.Context, event, scope, session, result, detail string) error {
	if err := s.InitHooks(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO hook_runs(event,scope,session,result,detail,created_at)VALUES(?,?,?,?,?,?)`, event, scope, session, result, detail, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) HookDiagnostics(ctx context.Context) (map[string]any, error) {
	if err := s.InitHooks(ctx); err != nil {
		return nil, err
	}
	var pending int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM memory_candidates WHERE state='pending'`).Scan(&pending); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event,scope,session,result,detail,created_at FROM hook_runs ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []map[string]string{}
	for rows.Next() {
		var ev, sc, se, re, de, at string
		if err = rows.Scan(&ev, &sc, &se, &re, &de, &at); err != nil {
			return nil, err
		}
		runs = append(runs, map[string]string{"event": ev, "scope": sc, "session": se, "result": re, "detail": de, "created_at": at})
	}
	return map[string]any{"pending_reviews": pending, "recent_events": runs, "model_used_material": "unknown unless verified in the host"}, rows.Err()
}
