package memory

import (
	"strings"
	"testing"
)

func TestDamagedEvidenceRejectedBeforeCommitAndRetryKeyRemainsUsable(t *testing.T) {
	s := testStore(t)
	d := recordDraft()
	d.RequestID = "damaged-original"
	d.Body = "关键信���"
	if _, err := s.Put(ctx, d, ""); err == nil || !strings.Contains(err.Error(), "body") {
		t.Fatal("damaged body accepted", err)
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Entries != 0 {
		t.Fatal("rejection wrote data", stats, err)
	}
	d.Body = "关键信息、助手、同级——、须追到、用户未表态😀"
	stored := put(t, s, d, "")
	read, err := s.Read(ctx, stored.ID)
	if err != nil || read.Entry.Body != d.Body || len(read.Entry.TextIssues) != 0 {
		t.Fatal(read, err)
	}
	if got := put(t, s, d, ""); got.ID != stored.ID {
		t.Fatal("intact retry duplicated entry")
	}
}

func TestLiteralReplacementCanBePreservedAndLegacyCorruptionFlagged(t *testing.T) {
	s := testStore(t)
	d := recordDraft()
	d.Body = "此处有意讨论替换符号 �，不是自动修复后的业务原文"
	d.AllowReplacementCharacters = true
	e := put(t, s, d, "")
	read, err := s.Read(ctx, e.ID)
	if err != nil || read.Entry.Body != d.Body || len(read.Entry.TextIssues) != 1 {
		t.Fatal(read, err)
	}
	d.AllowReplacementCharacters = false
	d.Body = "已从原始来源恢复完整的业务原文。"
	d.Links = nil
	fixed := put(t, s, d, e.ID)
	old, _ := s.Read(ctx, e.ID)
	if old.Entry.Body != e.Body || old.CurrentID != fixed.ID {
		t.Fatal("repair overwrote old evidence")
	}
}

func TestReviewNoteHasIntegrityReceiptAndRejectsDamagedStateChange(t *testing.T) {
	s := testStore(t)
	turn, err := s.HookStart(ctx, "project:demo", "unicode-session", t.TempDir(), "本轮只有普通交流", "event-note")
	if err != nil {
		t.Fatal(err)
	}
	in := ReviewInput{Scope: turn.Scope, SourceID: turn.SourceID, Note: "助手推断、用��未表态"}
	if _, err = s.HookReviewDone(ctx, in); err == nil {
		t.Fatal("damaged note accepted")
	}
	pending, err := s.HookTurn(ctx, turn.SourceID)
	if err != nil || pending.Status != "pending" {
		t.Fatal("damaged note changed review", pending, err)
	}
	in.Note = "助手推断、用户未表态；本轮没有已确认的业务决定。"
	receipt, err := s.HookReviewDone(ctx, in)
	if err != nil || receipt.ReviewNote != in.Note || receipt.ReviewNoteBytes != len(in.Note) || receipt.ReviewNoteSHA256 != digestText(in.Note) || len(receipt.TextIssues) != 0 {
		t.Fatal(receipt, err)
	}
	retry, err := s.HookReviewDone(ctx, in)
	if err != nil || retry.ReviewNoteSHA256 != receipt.ReviewNoteSHA256 {
		t.Fatal(retry, err)
	}
}

func TestTextAuditFindsLegacyDamageWithoutRewritingIt(t *testing.T) {
	s := testStore(t)
	d := recordDraft()
	entry := put(t, s, d, "")
	// Simulate data written by the previous decoder, bypassing the new guard.
	legacy := "历史关键信���"
	if _, err := s.db.Exec(`UPDATE entries SET body=? WHERE id=?`, legacy, entry.ID); err != nil {
		t.Fatal(err)
	}
	turn, err := s.HookStart(ctx, d.Scope, "audit-session", t.TempDir(), "普通结束语", "audit-source")
	if err != nil {
		t.Fatal(err)
	}
	note := `{"case_ids":[],"explanation":"用\ufffd\ufffd未表态"}`
	if _, err = s.db.Exec(`UPDATE hook_turns SET note=?,status='skipped' WHERE source_id=?`, note, turn.SourceID); err != nil {
		t.Fatal(err)
	}
	audit, err := s.AuditText(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entryFound, receiptFound := false, false
	for _, finding := range audit.Findings {
		if finding.ID == entry.ID && finding.Type == "entry" {
			entryFound = true
		}
		if finding.ID == turn.SourceID && finding.Type == "review_receipt" {
			receiptFound = true
		}
	}
	if !entryFound || !receiptFound {
		t.Fatal(audit)
	}
	read, _ := s.Read(ctx, entry.ID)
	receipt, _ := s.HookTurn(ctx, turn.SourceID)
	if read.Entry.Body != legacy || receipt.Note != note {
		t.Fatal("audit rewrote evidence")
	}
}
