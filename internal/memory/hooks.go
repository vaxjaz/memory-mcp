package memory

// Hook tables are an optional extension of the v2 database. Existing entry,
// revision, backup and forget semantics remain unchanged; raw events are entries.
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"
)

const HookSourcePrefix = "hook-event:"

func hookRequestID(session, event string) string {
	b, _ := json.Marshal([]string{session, event})
	return "zcode-user:" + digestText(string(b))
}

type HookTurn struct {
	SourceID string `json:"source_id"`
	Scope    string `json:"scope"`
	Session  string `json:"session"`
	CWD      string `json:"cwd"`
	Query    string `json:"query"`
	AnswerID string `json:"answer_id,omitempty"`
	Attempts int    `json:"review_attempts"`
	Status   string `json:"status"`
	Recall   string `json:"-"`
	Note     string `json:"note,omitempty"`
	Created  string `json:"created_at"`
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

func scanTurn(row rowScanner) (HookTurn, error) {
	var t HookTurn
	err := row.Scan(&t.SourceID, &t.Scope, &t.Session, &t.CWD, &t.Query, &t.AnswerID, &t.Attempts, &t.Status, &t.Recall, &t.Note, &t.Created)
	return t, err
}
func (s *Store) HookLatest(ctx context.Context, scope, session string) (HookTurn, error) {
	return scanTurn(s.db.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE scope=? AND session=? ORDER BY created_at DESC,source_id DESC LIMIT 1", scope, session))
}
func (s *Store) HookTurn(ctx context.Context, id string) (HookTurn, error) {
	return scanTurn(s.db.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE source_id=?", id))
}
func (s *Store) HookStart(ctx context.Context, scope, session, cwd, prompt, eventID string) (HookTurn, error) {
	if err := s.InitHooks(ctx); err != nil {
		return HookTurn{}, err
	}
	if session == "" || strings.TrimSpace(prompt) == "" {
		return HookTurn{}, errors.New("session_id and nonempty prompt are required")
	}
	if eventID != "" {
		var id string
		err := s.db.QueryRowContext(ctx, `SELECT entry_id FROM write_requests WHERE scope=? AND request_id=?`, scope, hookRequestID(session, eventID)).Scan(&id)
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
	prev, err := s.HookLatest(ctx, scope, session)
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
	d := Draft{Kind: "record", Scope: scope, Title: "原始用户输入（尚未整理为业务决策）", Body: prompt, Origin: "host_quote", Host: "zcode", Session: session, SourceRef: HookSourcePrefix + "zcode:user"}
	if eventID != "" {
		d.RequestID = hookRequestID(session, eventID)
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
func (s *Store) HookAttachAnswer(ctx context.Context, t HookTurn, answer string) (HookTurn, error) {
	if answer == "" || t.AnswerID != "" {
		return t, nil
	}
	e, err := s.Put(ctx, Draft{Kind: "record", Scope: t.Scope, Title: "本轮业务答复原文（宿主交付）", Body: answer, Origin: "host_quote", Host: "zcode", Session: t.Session, SourceRef: HookSourcePrefix + "zcode:assistant", RequestID: "hook-answer:" + t.SourceID, Links: []Link{{TargetID: t.SourceID, Relation: "context"}}}, "")
	if err != nil {
		return t, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE hook_turns SET answer_id=? WHERE source_id=? AND answer_id=''`, e.ID, t.SourceID)
	if err != nil {
		return t, err
	}
	return s.HookTurn(ctx, t.SourceID)
}
func (s *Store) HookClaimReview(ctx context.Context, id string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE hook_turns SET attempts=1,status='review_requested' WHERE source_id=? AND attempts=0 AND status='pending'`, id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *Store) HookSetRecall(ctx context.Context, id, fingerprint string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE hook_turns SET recall=? WHERE source_id=?`, fingerprint, id)
	return err
}

type ReviewInput struct {
	Scope    string   `json:"scope"`
	SourceID string   `json:"source_id" jsonschema:"Raw user entry ID delivered by a hook. Read it and its contextual links before reviewing."`
	CaseIDs  []string `json:"case_ids,omitempty" jsonschema:"Already saved case/revision IDs linked to this source. Empty only when no business decision or correction needs recording."`
	Note     string   `json:"note" jsonschema:"Free-text explanation of what was recorded, or why this turn contains no business decision. This is host reasoning, not independent verification."`
}

func (s *Store) HookReviewDone(ctx context.Context, in ReviewInput) (HookTurn, error) {
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
	if len(in.CaseIDs) > 20 || len(in.Note) > 8192 {
		return t, errors.New("review receipt too large")
	}
	if in.CaseIDs == nil {
		in.CaseIDs = []string{}
	}
	payload, _ := json.Marshal(map[string]any{"case_ids": in.CaseIDs, "explanation": in.Note})
	if t.Status == "reviewed" || t.Status == "skipped" {
		if t.Note == string(payload) {
			return t, nil
		}
		return t, errors.New("review already completed; correct the case with memory_revise instead of overwriting its receipt")
	}
	for _, id := range in.CaseIDs {
		r, e := s.Read(ctx, id)
		if e != nil {
			return t, e
		}
		if !r.Entry.Current || r.Entry.Scope != t.Scope || r.Entry.Kind != "case" {
			return t, errors.New("receipt must reference a current case in the source project")
		}
		linked := false
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
	if len(in.CaseIDs) == 0 {
		status = "skipped"
	}
	result, err := s.db.ExecContext(ctx, `UPDATE hook_turns SET status=?,note=? WHERE source_id=? AND status IN ('pending','review_requested')`, status, string(payload), t.SourceID)
	if err != nil {
		return t, err
	}
	n, _ := result.RowsAffected()
	current, err := s.HookTurn(ctx, t.SourceID)
	if err == nil && n == 0 && current.Note != string(payload) {
		return current, errors.New("review receipt conflict")
	}
	return current, err
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
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM hook_turns WHERE status IN ('pending','review_requested')`).Scan(&pending); err != nil {
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

// Hook recall compares literal words and Han bigrams across current project
// materials. It does not summarize, embed, or claim semantic completeness.
func recallTerms(text string) map[string]int {
	out := map[string]int{}
	text = strings.ToLower(text)
	var word, han []rune
	flush := func() {
		if len(word) > 1 {
			out[string(word)] = 1
		}
		word = nil
		if len(han) > 1 {
			for i := 0; i < len(han)-1; i++ {
				out[string(han[i:i+2])] = 1
			}
		}
		han = nil
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			if len(word) > 0 {
				flush()
			}
			han = append(han, r)
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if len(han) > 0 {
				flush()
			}
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	for _, w := range []string{"一个", "这个", "我们", "你们", "可以", "需要", "进行", "相关", "一下", "现在", "怎么", "什么", "是否", "应该", "用户", "业务", "本轮", "原话", "项目", "请问", "帮我", "the", "this", "with", "please"} {
		delete(out, w)
	}
	if strings.Contains(text, "migration") || strings.Contains(text, ".sql") {
		out["迁移"] = 1
		out["数据库"] = 1
	}
	return out
}

type RecallResult struct {
	Entries   []Entry
	Truncated bool
	Scanned   int
}

func (s *Store) HookRecall(ctx context.Context, scope, query string) (RecallResult, error) {
	terms := recallTerms(query)
	result := RecallResult{Entries: []Entry{}}
	if len(terms) == 0 {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM entries WHERE current=1 AND scope=? AND source_ref NOT LIKE 'hook-event:%' ORDER BY created_at DESC,id DESC LIMIT 1001", scope)
	if err != nil {
		return result, err
	}
	type ranked struct {
		e     Entry
		score int
	}
	found := []ranked{}
	for rows.Next() {
		e, er := scanEntry(rows)
		if er != nil {
			rows.Close()
			return result, er
		}
		result.Scanned++
		if result.Scanned > 1000 {
			result.Truncated = true
			break
		}
		body, title := strings.ToLower(e.Body), strings.ToLower(e.Title)
		score := 0
		for w := range terms {
			if strings.Contains(title, w) {
				score += 4
			}
			if strings.Contains(body, w) {
				score++
			}
		}
		if score > 0 {
			found = append(found, ranked{e, score})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	if len(found) > 3 {
		found = found[:3]
	}
	for _, r := range found {
		read, er := s.Read(ctx, r.e.ID)
		if er != nil {
			return result, er
		}
		result.Entries = append(result.Entries, read.Entry)
	}
	return result, nil
}
