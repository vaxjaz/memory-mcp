package memory

import (
"github.com/vaxjaz/memory-mcp/internal/matching"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecallSearchesEvidenceAndFusesIndependentHostQueries(t *testing.T) {
	s, project, in := experienceFixture(t)
	in.Experience.Counterexample = "取消工单还受关联影响和状态约束限制；仅为隔离检索测试材料"
	in.Experience.Applicability = "取消工单时核对状态约束与关联影响"
	saved, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		d := in
		d.Experience.Counterexample = fmt.Sprintf("isolated unrelated counterexample %d", i)
		d.Experience.Applicability = "仅测试另一种业务条件"
		if _, err = s.RecordExperience(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "调查当前取消要求", Questions: "核对条件与关联影响", Queries: []RecallQuery{{Terms: []string{"LYN", "状态约束"}}, {Terms: []string{"关联影响"}}}})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID != saved.ID || len(out.Items[0].MatchedQueries) != 2 || out.QueryCount != 2 {
		t.Fatal(out, err)
	}
	// Compatibility keywords now search the recorded counterexample/boundary,
	// not only the small passive intervention anchor list.
	out, err = s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "当前调查", Questions: "查看状态约束", Keywords: []string{"状态约束"}})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID != saved.ID {
		t.Fatal(out, err)
	}
	b, _ := json.Marshal(out)
	if len(b) > ExperienceLookupBudget {
		t.Fatal("response budget", len(b))
	}
}

func TestRecallWriteAndIndexCommitOrRollbackTogether(t *testing.T) {
	s, project, in := experienceFixture(t)
	in.RequestID = "atomic"
	if _, err := s.db.Exec("CREATE TRIGGER fail_recall BEFORE INSERT ON recall_terms BEGIN SELECT RAISE(ABORT,'isolated index failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordExperience(ctx, in); err == nil {
		t.Fatal("reported saved after failed index")
	}
	stats, _ := s.Stats(ctx)
	if stats.Entries != 0 {
		t.Fatal("orphaned unsearchable record", stats)
	}
	s.db.Exec("DROP TRIGGER fail_recall")
	r, err := s.RecordExperience(ctx, in)
	if err != nil || !r.Indexed {
		t.Fatal(r, err)
	}
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "same current task", Questions: "check current evidence", Queries: []RecallQuery{{Terms: []string{"LYN"}}}})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID != r.ID {
		t.Fatal(out, err)
	}
}

func TestRecallPaginationSnapshotAndDuplicateQueries(t *testing.T) {
	s, project, in := experienceFixture(t)
	for i := 0; i < 4; i++ {
		d := in
		d.Experience.Counterexample = fmt.Sprintf("independent synthetic mechanism %d", i)
		if _, err := s.RecordExperience(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	q := ExperienceLookup{Scope: in.Scope, Project: project, Task: "current investigation", Questions: "compare conditions", Limit: 1, Queries: []RecallQuery{{Terms: []string{"LYN", "创建准入"}}, {Terms: []string{"创建准入", "LYN"}}}}
	seen := map[string]bool{}
	firstCursor := ""
	for {
		out, err := s.LookupExperience(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Items) != 1 || out.QueryCount != 1 || seen[out.Items[0].ID] {
			t.Fatal("duplicate query inflated rank or paging repeated", out)
		}
		seen[out.Items[0].ID] = true
		if !out.HasMore {
			break
		}
		if firstCursor == "" {
			firstCursor = out.NextCursor
		}
		q.Cursor = out.NextCursor
	}
	if len(seen) != 4 {
		t.Fatal("silently lost candidates", seen)
	}
	q.Cursor = firstCursor
	q.Queries = []RecallQuery{{Terms: []string{"创建准入"}}}
	if _, err := s.LookupExperience(ctx, q); err == nil {
		t.Fatal("changed query accepted old cursor")
	}
	q.Queries = []RecallQuery{{Terms: []string{"LYN", "创建准入"}}, {Terms: []string{"创建准入", "LYN"}}}
	in.Experience.Counterexample = "new independently observed fixture"
	s.RecordExperience(ctx, in)
	if _, err := s.LookupExperience(ctx, q); err == nil {
		t.Fatal("changed corpus accepted old cursor")
	}
}

func TestLegacyIsSearchableWithoutBecomingAnIntervention(t *testing.T) {
	s, project, in := experienceFixture(t)
	legacy, err := s.Put(ctx, Draft{Kind: "record", Scope: in.Scope, Title: "Historical source", Body: "旧有资料保留了特殊核验条件", Origin: "user_supplied"}, "")
	if err != nil {
		t.Fatal(err)
	}
	s.Put(ctx, Draft{Kind: "record", Scope: in.Scope, Title: "Raw event", Body: "特殊核验条件", Origin: "host_quote", SourceRef: HookSourcePrefix + "fixture"}, "")
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "current historical investigation", Questions: "特殊核验条件在哪里", Queries: []RecallQuery{{Terms: []string{"特殊核验条件"}}}})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID != legacy.ID || out.Items[0].SourceKind != "legacy_evidence" {
		t.Fatal(out, err)
	}
	matches, err := s.MatchExperiences(ctx, in.Scope, "特殊核验条件")
	if err != nil || len(matches) != 0 {
		t.Fatal("legacy was auto promoted", matches, err)
	}
}

func TestAgreementIsNotAFabricatedExperienceAndRevalidatesSource(t *testing.T) {
	s, project, in := experienceFixture(t)
	source := "Only a synthetic confirmed convention for isolated tests."
	os.WriteFile(filepath.Join(project, "agreement.txt"), []byte(source), 0600)
	a := AgreementWrite{Scope: in.Scope, Project: project, Title: "隔离业务约定", RequestID: "reference-once", Agreement: Agreement{
		ObjectIDPrefixes: []string{"WXN"},
		Cues:             []ExperienceCue{{ID: "cancel", Label: "取消前核验", Text: "先核验实际数据再形成附件，仅为隔离测试", EvidenceRefs: []string{"fixture:explicit-owner-agreement"}, Matches: []CueMatch{{Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{"WXN-001 取消"}}}}},
		Requirement:      "先核验实际数据再形成附件，仅为隔离测试", Applicability: "测试业务范围内的工单处理", Boundary: "不覆盖审批撤回，也不授予数据库权限",
		Authority: "isolated fixture owner", ConfirmationReference: "fixture:explicit-owner-agreement",
		Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:explicit-owner-agreement", Path: "agreement.txt", Observation: "synthetic agreement source exists"}},
	}}
	r, err := s.RecordAgreement(ctx, a)
	if err != nil || !r.Indexed {
		t.Fatal(r, err)
	}
	again, err := s.RecordAgreement(ctx, a)
	if err != nil || again.ID != r.ID || !again.Reused {
		t.Fatal(again, err)
	}
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "处理当前工单", Questions: "核对取消的实际前提", Queries: []RecallQuery{{Terms: []string{"WXN", "取消"}}}})
	if err != nil || len(out.Items) != 1 || out.Items[0].Experience != nil || out.Items[0].Agreement == nil || out.Items[0].SourceKind != "agreement_reference" {
		t.Fatal(out, err)
	}
	os.WriteFile(filepath.Join(project, "agreement.txt"), []byte(source+" changed"), 0600)
	out, err = s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "处理当前工单", Questions: "来源是否漂移", Keywords: []string{"WXN"}})
	if err != nil || !out.Items[0].Locations[0].ContentChanged {
		t.Fatal(out, err)
	}
	for _, clues := range []string{"WXN-001", "otherWXN-001 取消"} {
		m, e := s.MatchExperiences(ctx, in.Scope, clues)
		if e != nil || len(m) != 0 {
			t.Fatal("weak/prefix-substring false pause", clues, m, e)
		}
	}
	m, err := s.MatchExperiences(ctx, in.Scope, "WXN-001 取消")
	if err != nil || len(m) != 1 {
		t.Fatal(m, err)
	}
	bad := a
	bad.RequestID = "unconfirmed"
	bad.Agreement.Authority = ""
	if _, err = s.RecordAgreement(ctx, bad); err == nil {
		t.Fatal("unconfirmed agreement accepted")
	}
	read, _ := s.Read(ctx, r.ID)
	if strings.Contains(read.Entry.Body, "关键反例") {
		t.Fatal("fabricated failure story", read.Entry.Body)
	}
}

func TestSchemaThreeIndexMigrationPreservesSourceAndHistory(t *testing.T) {
	s, project, in := experienceFixture(t)
	old, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviousID = old.ID
	in.Experience.Boundary = "隔离测试补充了实际适用边界"
	current, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Read(ctx, old.ID)
	beforeJSON, _ := json.Marshal(before.Entry)
	dir := s.Dir
	s.Close()
	// A schema-3-compatible fixture has no recall postings. Migration must derive
	// its index without rewriting the historical source/profile.
	legacy, err := OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TABLE recall_terms; DROP TABLE recall_documents; PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	upgraded, err := openStore(dir, 10000, filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	after, err := upgraded.Read(ctx, old.ID)
	afterJSON, _ := json.Marshal(after.Entry)
	if err != nil || string(beforeJSON) != string(afterJSON) {
		t.Fatal("migration changed source history", err)
	}
	out, err := upgraded.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "current investigation", Questions: "current boundary", Queries: []RecallQuery{{Terms: []string{"实际适用边界"}}}})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID != current.ID {
		t.Fatal(out, err)
	}
}

func TestChallengeCanReturnLargeRequiredCasesAcrossBoundedPages(t *testing.T) {
	s, project, in := experienceFixture(t)
	in.Experience.Evidence = nil
	for i := 0; i < 3; i++ {
		in.Experience.Evidence = append(in.Experience.Evidence, ExperienceEvidence{Kind: "runtime", Reference: fmt.Sprintf("fixture:%d:%s", i, strings.Repeat("锚", 180)), Observation: "仅为隔离测试来源，不是真实事故"})
	}
	in.Experience.Cues[0].EvidenceRefs = []string{in.Experience.Evidence[0].Reference}
	for i := 0; i < 2; i++ {
		d := in
		d.Experience.Counterexample = fmt.Sprintf("隔离测试中不同的反例分支%d", i)
		if _, err := s.RecordExperience(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := s.MatchExperiences(ctx, in.Scope, "LYN 创建准入")
	if err != nil || len(matches) != 2 {
		t.Fatal(matches, err)
	}
	s.ExperienceStartTurn(ctx, "zcode", in.Scope, "large-session", "large-turn", "large-event")
	challenge, err := s.ChallengeExperience(ctx, "zcode", in.Scope, "large-session", "large-turn", project, matches)
	if err != nil {
		t.Fatal(err)
	}
	input := ExperienceLookup{Scope: in.Scope, Task: "current synthetic investigation", Questions: "inspect required current evidence", Queries: []RecallQuery{{Terms: []string{"unrelated"}}}, ChallengeID: challenge.ID}
	first, err := s.LookupExperience(ctx, input)
	if err != nil || len(first.Items) != 1 || !first.HasMore || first.ChallengeOutcome != "response_generated" {
		t.Fatal(first, err)
	}
	b, _ := json.Marshal(first)
	if len(b) > ExperienceLookupBudget {
		t.Fatal(len(b))
	}
	input.Cursor = first.NextCursor
	second, err := s.LookupExperience(ctx, input)
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID || second.HasMore || second.ChallengeOutcome != "response_generated" {
		t.Fatal(second, err)
	}
}
