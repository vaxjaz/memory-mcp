package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var ctx = context.Background()

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func recordDraft() Draft {
	return Draft{Kind: "record", Scope: "project:demo", Title: "离开编辑页面", Body: "当没有未保存内容时，用户反馈：每次离开都弹确认很烦。没有讨论不可恢复操作。", Origin: "host_quote", Host: "test-host"}
}
func put(t *testing.T, s *Store, d Draft, prev string) Entry {
	t.Helper()
	e, err := s.Put(ctx, d, prev)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func hypothesis(source Entry) Draft {
	return Draft{Kind: "hypothesis", Scope: source.Scope, Title: "可能偏好减少无效打断", Body: "仅在没有未保存内容的编辑场景有依据；高风险操作未知。", Origin: "model_inference", Links: []Link{{TargetID: source.ID, Relation: "supports"}}}
}

func TestCorrectionPreservesHistoryAndInvalidatesTransitiveInterpretations(t *testing.T) {
	s := testStore(t)
	original := put(t, s, recordDraft(), "")
	h := put(t, s, hypothesis(original), "")
	caseDraft := Draft{Kind: "case", Scope: original.Scope, Title: "后续案例", Body: "依赖旧假设的后续解释", Origin: "model_inference", Links: []Link{{TargetID: h.ID, Relation: "context"}}}
	c := put(t, s, caseDraft, "")
	d := recordDraft()
	d.Body = "纠正：用户仅反对没有修改时的弹窗，而非所有离开提示。"
	updated := put(t, s, d, original.ID)
	if updated.RootID != original.ID || updated.PreviousID != original.ID {
		t.Fatal("revision lineage lost")
	}
	r, err := s.Read(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Entry.Body != original.Body || r.Entry.Current || r.CurrentID != updated.ID {
		t.Fatal("original was overwritten or current revision missing")
	}
	for _, id := range []string{h.ID, c.ID} {
		r, err := s.Read(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !r.Entry.NeedsReview {
			t.Fatal("dependent not invalidated", id)
		}
	}
	if _, err := s.Put(ctx, d, original.ID); err == nil {
		t.Fatal("accepted superseded revision")
	}
	if _, err := s.Put(ctx, hypothesis(original), ""); err == nil {
		t.Fatal("accepted superseded source")
	}
	h2 := put(t, s, hypothesis(updated), h.ID)
	if h2.NeedsReview {
		t.Fatal("freshly grounded revision still stale")
	}
	old, err := s.Read(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Entry.Links[0].TargetID != original.ID {
		t.Fatal("historical evidence rewritten")
	}
}

func TestScopeAndEvidenceBoundaries(t *testing.T) {
	s := testStore(t)
	e := put(t, s, recordDraft(), "")
	tests := []struct {
		name  string
		draft Draft
	}{
		{"missing scope", Draft{Kind: "record", Title: "x", Body: "x", Origin: "host_quote"}},
		{"inference as raw", Draft{Kind: "record", Scope: e.Scope, Title: "x", Body: "x", Origin: "model_inference"}},
		{"ungrounded hypothesis", Draft{Kind: "hypothesis", Scope: e.Scope, Title: "x", Body: "x", Origin: "model_inference"}},
		{"inference as user", Draft{Kind: "hypothesis", Scope: e.Scope, Title: "x", Body: "x", Origin: "user_supplied", Links: []Link{{e.ID, "supports"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Put(ctx, tt.draft, ""); err == nil {
				t.Fatal("accepted invalid draft")
			}
		})
	}
	d := hypothesis(e)
	d.Scope = "global"
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("project preference leaked globally")
	}
	d.Scope = "project:other"
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("cross-project evidence allowed")
	}
	d = hypothesis(e)
	d.Links[0].TargetID = "missing"
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("fabricated evidence allowed")
	}
	h := put(t, s, hypothesis(e), "")
	d = hypothesis(h)
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("model-only confirmation chain allowed")
	}
	d = recordDraft()
	d.Links = []Link{{h.ID, "context"}}
	if _, err := s.Put(ctx, d, e.ID); err == nil {
		t.Fatal("lineage cycle allowed")
	}
	global := recordDraft()
	global.Scope = "global"
	g := put(t, s, global, "")
	d = hypothesis(g)
	d.Scope = e.Scope
	put(t, s, d, "")
}

func TestSearchUnicodeLiteralScopeAndConflicts(t *testing.T) {
	s := testStore(t)
	e := put(t, s, recordDraft(), "")
	d := recordDraft()
	d.Scope = "global"
	put(t, s, d, "")
	d.Scope = "project:other"
	put(t, s, d, "")
	d = hypothesis(e)
	d.Body = "相反案例：有未保存修改时应该提示。"
	d.Links[0].Relation = "contradicts"
	counter := put(t, s, d, "")
	hits, err := s.Search(ctx, SearchInput{Scope: e.Scope, Query: "确认"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != e.ID {
		t.Fatalf("scope or Unicode failed: %+v", hits)
	}
	hits, err = s.Search(ctx, SearchInput{Scope: e.Scope, Query: "确认", IncludeGlobal: true})
	if err != nil || len(hits) != 2 {
		t.Fatal(hits, err)
	}
	hits, err = s.Search(ctx, SearchInput{Scope: e.Scope, Query: "%_' OR 1=1"})
	if err != nil || len(hits) != 0 {
		t.Fatal("query treated as SQL/pattern", hits, err)
	}
	r, err := s.Read(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ReferencedBy) != 1 || r.ReferencedBy[0].EntryID != counter.ID || r.ReferencedBy[0].Relation != "contradicts" {
		t.Fatal("incoming counterevidence missing", r)
	}
	long := strings.Repeat("甲", 600) + "保存" + strings.Repeat("乙", 600)
	if !strings.Contains(excerpt(long, []string{"保存"}), "保存") {
		t.Fatal("excerpt hid matching text")
	}
	// Unicode lowercasing can change byte lengths; never index original bytes with it.
	if !strings.Contains(excerpt(strings.Repeat("Ⱥ", 600)+"保存", []string{"保存"}), "保存") {
		t.Fatal("Unicode case folding corrupted the excerpt")
	}
}

func TestPersistentExportAndForgetClosure(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := put(t, s, recordDraft(), "")
	h := put(t, s, hypothesis(e), "")
	updated := put(t, s, recordDraft(), e.ID)
	independent := put(t, s, recordDraft(), "")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Read(ctx, h.ID)
	if err != nil || !r.Entry.NeedsReview {
		t.Fatal("persistence failed", err)
	}
	export, err := s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(export.Entries) != 4 {
		t.Fatal("history missing")
	}
	p := filepath.Join(t.TempDir(), "export.json")
	if err = WriteExport(p, export); err != nil {
		t.Fatal(err)
	}
	if err = WriteExport(p, export); err == nil {
		t.Fatal("export overwrote file")
	}
	data, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(data), e.Body) {
		t.Fatal("export lost source text", err)
	}
	n, err := s.Forget(ctx, updated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("deleted %d, expected 3", n)
	}
	for _, id := range []string{e.ID, h.ID, updated.ID} {
		if _, err := s.Read(ctx, id); err == nil {
			t.Fatal("retained deleted entry", id)
		}
	}
	if _, err = s.Read(ctx, independent.ID); err != nil {
		t.Fatal("deleted unrelated entry", err)
	}
}

func TestConcurrentHostsCannotBothReviseSameRevision(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	e := put(t, a, recordDraft(), "")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, s := range []*Store{a, b} {
		wg.Add(1)
		go func(s *Store) { defer wg.Done(); <-start; _, err := s.Put(ctx, recordDraft(), e.ID); results <- err }(s)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !strings.Contains(err.Error(), "revision conflict") {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("%d successful revisions; expected 1", success)
	}
}
