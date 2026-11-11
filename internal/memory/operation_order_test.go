package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApprovalOrderSurvivesEqualTimestampsAndRejectsOldFutureDatedMessages(t *testing.T) {
	s := testStore(t)
	target, current := staleCase(t, s)
	old := approval(t, s, target.Scope, "Approve this displayed plan")
	p, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: target.Scope, Operations: []CleanupOperation{reviewOp(target, current.ID)}})
	if err != nil {
		t.Fatal(err)
	}
	// A future wall-clock timestamp must not turn a pre-preview message into approval.
	if _, err = s.db.Exec("UPDATE entries SET created_at=? WHERE id=?", "2099-01-01T00:00:00Z", old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, old.ID, old.Body}, false); err == nil {
		t.Fatal("pre-preview message bypassed freshness with future wall clock")
	}
	fresh := approval(t, s, p.Scope, "Approve this displayed plan")
	if _, err = s.db.Exec("UPDATE entries SET created_at=? WHERE id=?", p.CreatedAt, fresh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, fresh.ID, fresh.Body}, false); err != nil {
		t.Fatal("distinct new confirmation rejected on equal timestamp", err)
	}
}

func TestUndoRequiresAnActualPostApplyRecordEvenWhenClocksRollBack(t *testing.T) {
	s := testStore(t)
	target, current := staleCase(t, s)
	p, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: target.Scope, Operations: []CleanupOperation{reviewOp(target, current.ID)}})
	if err != nil {
		t.Fatal(err)
	}
	earlyUndo := approval(t, s, p.Scope, "Undo this source-review plan")
	applyReview(t, s, p)
	if _, err = s.db.Exec("UPDATE entries SET created_at=? WHERE id=?", "2099-01-01T00:00:00Z", earlyUndo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, earlyUndo.ID, earlyUndo.Body}, true); err == nil {
		t.Fatal("pre-apply message could authorize undo")
	}
	actualUndo := approval(t, s, p.Scope, "Undo this source-review plan")
	if _, err = s.db.Exec("UPDATE entries SET created_at=? WHERE id=?", "2000-01-01T00:00:00Z", actualUndo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, actualUndo.ID, actualUndo.Body}, true); err != nil {
		t.Fatal("actual post-apply record rejected due to wall-clock rollback", err)
	}
}

func TestOrderMetadataUpgradesV3AndPersistsAcrossBackup(t *testing.T) {
	s := testStore(t)
	target, current := staleCase(t, s)
	dir := s.Dir
	// Recreate the v0.2.0 metadata layout without rewriting any entry body/history.
	for _, statement := range []string{"ALTER TABLE entries DROP COLUMN write_order", "ALTER TABLE cleanup_plans DROP COLUMN created_order", "ALTER TABLE cleanup_plans DROP COLUMN decided_order", "DROP TABLE operation_clock"} {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	upgraded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	original, err := upgraded.Read(ctx, target.ID)
	if err != nil || original.Entry.Body != target.Body || !original.Entry.NeedsReview {
		t.Fatal("upgrade rewrote historical content or cleared flag", original, err)
	}
	p, err := upgraded.PreviewCleanup(ctx, CleanupPreview{Scope: target.Scope, Operations: []CleanupOperation{reviewOp(target, current.ID)}})
	if err != nil {
		t.Fatal(err)
	}
	copyDir := t.TempDir()
	backup := filepath.Join(copyDir, "memory.db")
	if err = upgraded.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(copyDir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	applyReview(t, restored, p)
	r, err := restored.Read(ctx, target.ID)
	if err != nil || r.Entry.NeedsReview {
		t.Fatal("backup lost approval ordering", r, err)
	}
	r, _ = upgraded.Read(ctx, target.ID)
	if !r.Entry.NeedsReview {
		t.Fatal("isolated backup test mutated original library")
	}
	// The metadata survives an ordinary reopen too, without using filesystem rowids.
	if _, err = os.Stat(backup); err != nil {
		t.Fatal(err)
	}
}

func TestEntryWriteRetryKeepsItsOriginalOrder(t *testing.T) {
	s := testStore(t)
	d := recordDraft()
	d.RequestID = "stable-input-delivery"
	first := put(t, s, d, "")
	var before, after int64
	if err := s.db.QueryRow("SELECT value FROM operation_clock WHERE id=1").Scan(&before); err != nil {
		t.Fatal(err)
	}
	repeated := put(t, s, d, "")
	if err := s.db.QueryRow("SELECT value FROM operation_clock WHERE id=1").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if repeated.ID != first.ID || before != after {
		t.Fatal("retry fabricated a fresh user receipt order")
	}
}
