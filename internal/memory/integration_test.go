package memory

import (
	"encoding/json"
	"testing"
)

func TestAssistLookupDoesNotManufactureCheckpointsOrCandidates(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	saved := put(t, s, usefulCase(raw), "")
	counts := func() [3]int {
		var n [3]int
		for i, table := range []string{"context_checks", "context_reads", "memory_candidates"} {
			if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n[i]); err != nil {
				t.Fatal(err)
			}
		}
		return n
	}
	before := counts()
	in := ContextInput{Action: "lookup", Scope: raw.Scope, Task: "Test login arguments in current project", Keywords: []string{"login"}, ResponseMode: "compact"}
	r, err := s.Context(ctx, in)
	if err != nil || r.Status != "candidates" || r.SourceID != "" || r.NextAction != "read_relevant_evidence_if_needed" {
		t.Fatal(r, err)
	}
	found := false
	for _, m := range r.Matches {
		if m.ID == saved.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("lookup missed grounded fixture", r)
	}
	payload, _ := json.Marshal(r)
	if len(payload) > CompactContextBudget {
		t.Fatal("lookup exceeded preview budget")
	}
	if after := counts(); after != before {
		t.Fatal("read-only lookup wrote workflow state", before, after)
	}
	in.SaveCandidate = true
	if _, err = s.Context(ctx, in); err == nil {
		t.Fatal("lookup accepted an implicit candidate write")
	}
	in.SaveCandidate = false
	in.Task = ""
	if _, err = s.Context(ctx, in); err == nil {
		t.Fatal("lookup accepted an unreconstructed task")
	}
}

func TestAssistProvenanceDoesNotBypassIdentityOrStrictCompatibility(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	d := usefulCase(raw)
	d.ContextSourceID = raw.ID
	if err := s.ValidateWriteProvenance(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateWriteContext(ctx, d); err == nil {
		t.Fatal("strict checkpoint unexpectedly optional")
	}
	if _, err := s.Put(ctx, d, ""); err != nil {
		t.Fatal("assist write requires an unnecessary complete", err)
	}
	d.ContextSourceID = "deadbeef"
	if err := s.ValidateWriteProvenance(ctx, d); err == nil {
		t.Fatal("assist accepted nonexistent source")
	}
	other := recordDraft()
	other.Scope = "project:other"
	foreign := put(t, s, other, "")
	d.ContextSourceID = foreign.ID
	if err := s.ValidateWriteProvenance(ctx, d); err == nil {
		t.Fatal("assist accepted cross-project source")
	}
	d.ContextSourceID = ""
	d.Links[0].TargetID = "deadbeef"
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("assist accepted broken evidence")
	}
}
