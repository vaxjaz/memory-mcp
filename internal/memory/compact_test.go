package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCompactPrepareBoundsLargeProfilesAndPreservesAllCandidates(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	want := map[string]bool{raw.ID: true}
	for i := 0; i < 32; i++ {
		d := usefulCase(raw)
		d.Title = fmt.Sprintf("needle-%02d ", i) + strings.Repeat("中", 180)
		d.Body = strings.Repeat("历史上下文", 800) + " needle"
		d.Profile.FutureUse = strings.Repeat("<", 4000)
		d.Profile.CounterCheck = strings.Repeat("反向检验", 300)
		d.Profile.Applicability = strings.Repeat("适用", 600)
		d.Profile.Boundary = strings.Repeat("边界", 600)
		e := put(t, s, d, "")
		want[e.ID] = true
	}
	in := ContextInput{Action: "prepare", Scope: raw.Scope, SourceID: raw.ID, Task: strings.Repeat("t", 8000), Questions: strings.Repeat("q", 8000), Keywords: []string{"needle"}, ResponseMode: "compact"}
	seen := map[string]bool{}
	var first ContextResult
	for page := 0; page < 20; page++ {
		r, err := s.Context(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if page == 0 {
			first = r
		}
		body, _ := json.Marshal(r)
		if len(body) > CompactContextBudget || !utf8.Valid(body) || r.Task != "" || r.Questions != "" {
			t.Fatal("oversized/invalid preview or echoed task", len(body))
		}
		for _, m := range r.Matches {
			seen[m.ID] = true
		}
		for _, item := range r.Directory {
			if item.Profile != nil || len(item.Summary) > 180 || len(item.Boundary) > 90 {
				t.Fatal("full admission material leaked into directory", item.ID)
			}
			seen[item.ID] = true
		}
		if r.NextCursor == "" {
			break
		}
		in.Cursor = r.NextCursor
	}
	// Keyword pagination also retains candidates removed to fit the byte budget.
	cursor := first.MatchNextCursor
	for cursor != "" {
		p, err := s.SearchPage(ctx, SearchInput{Scope: raw.Scope, Query: "needle", Cursor: cursor, Limit: 8})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range p.Results {
			seen[m.ID] = true
		}
		cursor = p.NextCursor
	}
	for id := range want {
		if !seen[id] {
			t.Fatal("preview pagination lost candidate", id)
		}
	}
	state, err := s.ContextStatus(ctx, raw.ID)
	if err != nil || state.Task != in.Task || state.Questions != in.Questions {
		t.Fatal("compact response rewrote stored context", state, err)
	}
	var id string
	for candidate := range want {
		if candidate != raw.ID {
			id = candidate
			break
		}
	}
	r, _ := s.Read(ctx, id)
	if len(r.Entry.Profile.FutureUse) != 4000 || !strings.Contains(r.Entry.Body, "历史上下文") {
		t.Fatal("preview rewrote evidence")
	}
	in.Action, in.SelectedIDs, in.Decision = "complete", []string{id}, "Apply verified fixture constraints"
	if _, err = s.Context(ctx, in); err == nil {
		t.Fatal("preview falsely counted as a full read")
	}
	if err = s.TrackContextRead(ctx, raw.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Context(ctx, in); err != nil {
		t.Fatal(err)
	}
}

func TestCompactBudgetRepaginatesUnseenDirectoryAndMatches(t *testing.T) {
	// Worst-case JSON escaping and long scope exercise the byte-limit path,
	// independently of ordinary small fixtures.
	r := ContextResult{SourceID: strings.Repeat("a", 32), Status: "prepared", SelectedIDs: []string{}}
	in := ContextInput{Scope: "project:" + strings.Repeat("<", 480), Keywords: []string{strings.Repeat("<", 1900)}}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("%032x", i+1)
		r.Matches = append(r.Matches, Hit{ID: id, Scope: in.Scope, Title: strings.Repeat("<", 180), Excerpt: strings.Repeat("<", 500), CreatedAt: "2026-11-12T01:00:00Z"})
		r.Directory = append(r.Directory, InventoryItem{ID: fmt.Sprintf("%032x", i+20), Title: strings.Repeat("<", 180), Summary: strings.Repeat("<", 220), CreatedAt: "2026-11-12T01:00:00Z"})
	}
	r.KeywordSearch = &KeywordSearchInfo{Status: "searched", UsedTerms: in.Keywords}
	if err := compactContext(&r, in); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(r)
	if len(body) > CompactContextBudget || r.NextCursor == "" || r.MatchNextCursor == "" || !r.KeywordSearch.TermsOmitted {
		t.Fatal("budget did not paginate/declare omitted echo", len(body), r)
	}
	for _, p := range []struct {
		cursor string
		input  SearchInput
		id     string
	}{
		{r.NextCursor, SearchInput{Scope: in.Scope}, r.Directory[len(r.Directory)-1].ID},
		{r.MatchNextCursor, SearchInput{Scope: in.Scope, Query: in.Keywords[0]}, r.Matches[len(r.Matches)-1].ID},
	} {
		c, err := decodeCursor(p.cursor, searchFingerprint(p.input))
		if err != nil || c.ID != p.id {
			t.Fatal("cursor skipped items not returned", c, err)
		}
	}
}

func TestReceiptModeDoesNotChangeWriteIdentityOrStoredEvidence(t *testing.T) {
	s := testStore(t)
	d := recordDraft()
	d.RequestID, d.ResponseMode = "one-logical-write", "compact"
	e := put(t, s, d, "")
	receipt := CompactEntry(e, d.ResponseMode)
	if receipt.Body != "" || receipt.BodyBytes != len(d.Body) || receipt.BodySHA256 != e.BodySHA256 || receipt.State != e.State {
		t.Fatal(receipt)
	}
	d.ResponseMode = "full"
	retried := put(t, s, d, "")
	if retried.ID != e.ID || retried.Body != d.Body {
		t.Fatal("response mode duplicated logical write")
	}
	d.ResponseMode = "invalid"
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("invalid mode committed a write")
	}
}

func TestMatchPreviewKeepsActualClueAndUnicodeIntact(t *testing.T) {
	s := strings.Repeat("İ中", 100) + "关键线索" + strings.Repeat("尾", 100)
	p := matchPreview(s, []string{"关键线索"}, 240)
	if !utf8.ValidString(p) || len(p) > 240 || !strings.Contains(p, "关键线索") {
		t.Fatal("clue lost in preview", p)
	}
}
