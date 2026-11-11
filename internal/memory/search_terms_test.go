package memory

import (
	"fmt"
	"strings"
	"testing"
)

func TestContextSearchRetainsMoreThanEightTermsAndReportsNormalization(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	d := recordDraft()
	d.Title = "Only the last search term matches"
	d.Body = "fixture_marker_12"
	wanted := put(t, s, d, "")
	terms := []string{"   ", "DUPLICATE", "duplicate"}
	for i := 1; i <= 12; i++ {
		terms = append(terms, fmt.Sprintf("fixture_marker_%02d", i))
	}
	result, err := s.Context(ctx, ContextInput{Action: "prepare", Scope: raw.Scope, SourceID: raw.ID, Task: "结合当前项目和完整对话检查需求入口", Questions: "第十二条线索是否有实际依据？", Keywords: terms})
	if err != nil {
		t.Fatal(err)
	}
	if result.KeywordSearch == nil || result.KeywordSearch.Status != "searched" || len(result.KeywordSearch.UsedTerms) != 13 {
		t.Fatal(result.KeywordSearch)
	}
	found := false
	for _, match := range result.Matches {
		found = found || match.ID == wanted.ID
	}
	if !found {
		t.Fatal("late search term was dropped", result)
	}
	if result.Task != "结合当前项目和完整对话检查需求入口" {
		t.Fatal("task context was rewritten")
	}
}

func TestLiteralSearchUsesAllTermsWithStablePagination(t *testing.T) {
	s := testStore(t)
	terms := []string{}
	for i := 0; i < 300; i++ {
		terms = append(terms, fmt.Sprintf("k%03d", i))
	}
	want := map[string]bool{}
	for i := 0; i < 3; i++ {
		d := recordDraft()
		d.Title = "Late-term fixture"
		d.Body = "k299"
		want[put(t, s, d, "").ID] = true
	}
	query := strings.Join(terms, " ")
	first, err := s.SearchPage(ctx, SearchInput{Scope: "project:demo", Query: query, Limit: 1})
	if err != nil || !first.HasMore {
		t.Fatal(first, err)
	}
	seen := map[string]bool{first.Results[0].ID: true}
	cursor := first.NextCursor
	for cursor != "" {
		p, err := s.SearchPage(ctx, SearchInput{Scope: "project:demo", Query: "  " + query + " k299  ", Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range p.Results {
			if !want[h.ID] || seen[h.ID] {
				t.Fatal("duplicate or unrelated hit", h)
			}
			seen[h.ID] = true
		}
		cursor = p.NextCursor
	}
	if len(seen) != len(want) {
		t.Fatal("late terms or pages lost", seen)
	}
}

func TestOverlongContextSearchKeepsDirectoryAndExplicitlyReportsNoSearch(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	put(t, s, usefulCase(raw), "")
	long := strings.Repeat("检索线索", 200)
	result, err := s.Context(ctx, ContextInput{Action: "prepare", Scope: raw.Scope, SourceID: raw.ID, Task: "Inspect current project memory", Keywords: []string{long}})
	if err != nil || len(result.Directory) == 0 || result.Status != "prepared" {
		t.Fatal("directory workflow failed", result, err)
	}
	if result.KeywordSearch == nil || result.KeywordSearch.Status != "directory_only" || !strings.Contains(result.KeywordSearch.Notice, "NOT executed") || len(result.Matches) > 0 {
		t.Fatal("search omission was hidden", result)
	}
	if _, err = s.SearchPage(ctx, SearchInput{Scope: raw.Scope, Query: long}); err == nil {
		t.Fatal("direct search lost its total-length bound")
	}
}
