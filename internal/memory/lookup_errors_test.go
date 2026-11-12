package memory

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMissingContextDependenciesAreNamedAndDoNotApproveChecks(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	missing := strings.Repeat("f", 32)
	_, err := s.Context(ctx, ContextInput{Action: "skip", Scope: raw.Scope, SourceID: missing, Task: "Ordinary task", Decision: "No memory applies"})
	if err == nil || !strings.Contains(err.Error(), "user_source_not_found") || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), strconv.Quote(s.Dir)) {
		t.Fatal(err)
	}
	status, err := s.Context(ctx, ContextInput{Action: "status", Scope: raw.Scope, SourceID: raw.ID})
	if err != nil || status.Status != "not_prepared" {
		t.Fatal(status, err)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM context_checks").Scan(&count)
	if count != 0 {
		t.Fatal("status silently initialized/approved missing state")
	}
	_, err = s.Context(ctx, ContextInput{Action: "complete", Scope: raw.Scope, SourceID: raw.ID, Decision: "Cannot bypass prepare"})
	if err == nil || !strings.Contains(err.Error(), "context_not_prepared") || !strings.Contains(err.Error(), raw.ID) {
		t.Fatal(err)
	}
	if err = s.ContextReady(ctx, raw.ID); err == nil {
		t.Fatal("missing context became ready")
	}
	if err = s.TrackContextRead(ctx, raw.ID, raw.ID); err == nil || !strings.Contains(err.Error(), "context_not_prepared") {
		t.Fatal(err)
	}
}

func TestMissingDraftLinkNamesExactTargetAndLeavesCaseUnchanged(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	entry := put(t, s, usefulCase(raw), "")
	before, _ := s.Stats(ctx)
	bad := usefulCase(raw)
	bad.Links = []Link{{strings.Repeat("f", 27), "context"}}
	err := s.CheckWriteContext(ctx, bad)
	if err == nil || !strings.Contains(err.Error(), "draft.links[0].target_id") || !strings.Contains(err.Error(), bad.Links[0].TargetID) {
		t.Fatal(err)
	}
	if _, err = s.Put(ctx, bad, entry.ID); err == nil {
		t.Fatal("write-side source validation missing")
	}
	after, _ := s.Stats(ctx)
	r, _ := s.Read(ctx, entry.ID)
	if !r.Entry.Current || before.Entries != after.Entries {
		t.Fatal("failed revision changed history or appended a case")
	}
	next := put(t, s, usefulCase(raw), entry.ID)
	read, _ := s.Read(ctx, next.ID)
	if next.State.State != "active" || next.State != read.Entry.State {
		t.Fatal("write response management disagrees with read", next.State, read.Entry.State)
	}
}

func TestExistingHookEntryWithoutTurnMetadataIsNotFabricated(t *testing.T) {
	s := testStore(t)
	d := recordDraft()
	d.Host = "codex"
	d.Session = "fixture"
	d.SourceRef = HookSourcePrefix + "codex:user"
	raw := put(t, s, d, "")
	s.InitHooks(ctx)
	_, err := s.Context(ctx, ContextInput{Action: "prepare", Scope: raw.Scope, SourceID: raw.ID, Task: "Reconstruct task"})
	if err == nil || !strings.Contains(err.Error(), "hook_turn_not_found") || !strings.Contains(err.Error(), raw.ID) {
		t.Fatal(err)
	}
	if _, err = s.HookTurn(ctx, raw.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("fabricated missing hook metadata", err)
	}
}

func TestReadOnlyDiagnosticsDoesNotMigrateOrRepairTheExaminedLibrary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "中文 # diagnostic library")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	raw := put(t, s, recordDraft(), "")
	entry := put(t, s, usefulCase(raw), "")
	if _, err = s.db.Exec("DELETE FROM operation_clock"); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	report, err := ro.DiagnoseLookups(ctx, entry.Scope, entry.ID, []string{raw.ID, strings.Repeat("f", 27)})
	if err != nil || report.WriteClockPresent || !report.ReadOnly || len(report.ForwardLinks) != 1 || report.MissingTargets != 0 {
		t.Fatal(report, err)
	}
	if !report.ProbeSources[0].Exists || report.ProbeSources[1].Exists || report.ProbeSources[1].Format32Hex {
		t.Fatal(report.ProbeSources)
	}
	if _, err = ro.db.Exec("DELETE FROM entries"); err == nil {
		t.Fatal("diagnostic connection can write")
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM operation_clock").Scan(&count); err != nil || count != 0 {
		t.Fatal("diagnose masked missing singleton", count, err)
	}
}

func TestForwardAndReverseLinksShareExactStoredRows(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	entry := put(t, s, usefulCase(raw), "")
	sourceRead, err := s.Read(ctx, raw.ID)
	if err != nil || len(sourceRead.ReferencedBy) != 1 || sourceRead.ReferencedBy[0].EntryID != entry.ID {
		t.Fatal(sourceRead, err)
	}
	// Simulate EXTERNAL SQLite mutation bypassing all MCP validation/FKs.
	bad := strings.Repeat("f", 27)
	if _, err = s.db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE links SET target_id=? WHERE entry_id=?", bad, entry.ID); err != nil {
		t.Fatal(err)
	}
	s.db.Exec("PRAGMA foreign_keys=ON")
	report, err := s.DiagnoseLookups(ctx, entry.Scope, entry.ID, nil)
	if err != nil || report.MissingTargets != 1 || report.ForwardLinks[0].Target.ID != bad {
		t.Fatal(report, err)
	}
	sourceRead, err = s.Read(ctx, raw.ID)
	if err != nil || len(sourceRead.ReferencedBy) != 0 {
		t.Fatal("independent stale reverse index exists", sourceRead, err)
	}
	caseRead, err := s.Read(ctx, entry.ID)
	if err != nil || caseRead.Entry.Links[0].TargetID != bad {
		t.Fatal("read did not report exact stored forward link", caseRead, err)
	}
}
