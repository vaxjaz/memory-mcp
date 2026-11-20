package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// Two-form body contract, legacy compatibility, body-derived reminders and the
// schema-10 upgrade path (design EXPERIENCE_MODEL_GLM_HANDOFF §3/§4).

func factFixture(t *testing.T) (*Store, string, ExperienceWrite) {
	t.Helper()
	s := testStore(t)
	project := t.TempDir()
	if err := s.HookBind(ctx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	in := ExperienceWrite{Scope: "project:demo", Project: project, Title: "结算编码按环境配置",
		Admission:  &ExperienceAdmission{FutureDecision: "把结算编码写入方案前先核对目标环境的配置来源。", EvidenceRef: "fixture:settlement-config"},
		Experience: Experience{Fact: "结算编码由各环境的线下结算系统分别配置；本例只在测试环境观察到取值。",
			Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:settlement-config", Observation: "合成事实验证 fact 形态，不是真实业务"}},
			Cues: []ExperienceCue{
				{ID: "settle", Label: "结算编码", EvidenceRefs: []string{"fixture:settlement-config"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"settlementCode"}}, {Rule: matching.Rule{Op: "token", Normalize: "whitespace"}, Entrances: []string{"结算编码"}}}},
				{ID: "envcfg", Label: "环境配置", EvidenceRefs: []string{"fixture:settlement-config"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"offlineSettlement"}}, {Rule: matching.Rule{Op: "token", Normalize: "whitespace"}, Entrances: []string{"线下结算"}}}},
			}}}
	return s, project, in
}

func TestExperienceFactFormWriteAndRecall(t *testing.T) {
	s, project, in := factFixture(t)
	receipt, err := s.RecordExperience(ctx, in)
	if err != nil || receipt.Reused {
		t.Fatal(receipt, err)
	}
	// No pitfall/counterexample narrative columns were required.
	if receipt.TriggerPreview.EntranceCount != 4 {
		t.Fatal("entrances not indexed", receipt.TriggerPreview)
	}
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: "project:demo", Project: project,
		Task: "合成任务：确认结算编码来源", Questions: "结算编码是否全局一致",
		Queries: []RecallQuery{{Terms: []string{"线下结算"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].Experience == nil {
		t.Fatal("fact record not recalled", out)
	}
	body := out.Items[0].Experience
	if body.Form != "fact" || body.Fact == "" || body.Pitfall != "" || body.Counterexample != "" || body.Impact != "" {
		t.Fatal("projection is not the fact form", body)
	}
	if !strings.Contains(body.Fact, "各环境") || !strings.Contains(body.Fact, "测试环境") {
		t.Fatal("qualifiers were dropped from the fact", body.Fact)
	}
}

func TestExperienceCounterexampleFormWithoutNarrativeColumns(t *testing.T) {
	s, project, in := factFixture(t)
	in.Experience.Fact = ""
	in.Experience.Pitfall = "在眼前调用处添加校验就能覆盖所有入口"
	in.Experience.Counterexample = "调查发现另一个入口不经过该调用处"
	receipt, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	_ = receipt
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: "project:demo", Project: project,
		Task: "合成任务", Questions: "入口覆盖", Queries: []RecallQuery{{Terms: []string{"另一个入口"}}}})
	if err != nil || len(out.Items) != 1 {
		t.Fatal(out, err)
	}
	body := out.Items[0].Experience
	if body.Form != "counterexample" || body.Pitfall == "" || body.Counterexample == "" || body.Impact != "" || body.Applicability != "" || body.Boundary != "" {
		t.Fatal("optional narrative columns were required or lost", body)
	}
}

func TestExperienceLegacyFiveFieldSubmissionStaysValid(t *testing.T) {
	s, project, in := experienceFixture(t)
	first, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// Same legacy payload without a request id deduplicates against the
	// committed revision: the content key stays byte-compatible.
	again, err := s.RecordExperience(ctx, in)
	if err != nil || !again.Reused || again.ID != first.ID {
		t.Fatal("legacy payload stopped deduplicating", again, err)
	}
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: "project:demo", Project: project,
		Task: "合成任务", Questions: "入口", Queries: []RecallQuery{{Terms: []string{"canDispatchLynOrder"}}}})
	if err != nil || len(out.Items) != 1 || out.Items[0].Experience == nil {
		t.Fatal(out, err)
	}
	body := out.Items[0].Experience
	if body.Form != "counterexample" || body.Pitfall == "" || body.Counterexample == "" || body.Impact == "" || body.Applicability == "" || body.Boundary == "" {
		t.Fatal("legacy narrative fields were dropped from the projection", body)
	}
	if len(body.RelatedFacts) == 0 {
		t.Fatal("legacy cue text facts vanished", body)
	}
}

func TestExperienceFormMutualExclusion(t *testing.T) {
	s, _, in := factFixture(t)
	mixed := in
	mixed.Experience.Pitfall = "把测试读数当成各环境通用"
	if _, err := s.RecordExperience(ctx, mixed); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatal("fact+pitfall accepted", err)
	}
	empty := in
	empty.Experience.Fact = ""
	if _, err := s.RecordExperience(ctx, empty); err == nil || !strings.Contains(err.Error(), "one body form") {
		t.Fatal("empty body accepted", err)
	}
}

func TestAgreementStillRequiresCueText(t *testing.T) {
	s := testStore(t)
	_, err := s.RecordAgreement(ctx, AgreementWrite{Scope: "project:demo", Project: "/nonexistent",
		Agreement: Agreement{Requirement: "发布前双人复核", Applicability: "发布流程", Boundary: "无", Authority: "团队约定",
			ConfirmationReference: "fixture:authority",
			Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:authority", Observation: "合成约定"}},
			Cues: []ExperienceCue{{ID: "release", Label: "发布", EvidenceRefs: []string{"fixture:authority"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"releasePlan"}}}}}}})
	if err == nil || !strings.Contains(err.Error(), "cues[0].text") {
		t.Fatal("agreement cue text became optional", err)
	}
}

func TestCueReminderQuotesBodyFactWithQualifiers(t *testing.T) {
	s, project, in := factFixture(t)
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	// First body delivery runs through the challenge/lookup/receipt path.
	c := probeDelivery(t, s, project, "settlementCode")
	if c.Outcome != ChallengeQueryRequired {
		t.Fatal("first hit did not challenge", c)
	}
	o := nativeLookup(t, s, project, "call1", c.ID, 2)
	observeDelivery(t, s, "call1", o)
	// A NEW association emits one short reminder quoting the body fact.
	reminder := probeDelivery(t, s, project, "线下结算")
	if reminder.Outcome != "cues" || !strings.Contains(reminder.Context, "结算编码由各环境的线下结算系统分别配置") {
		t.Fatal("reminder does not quote the body fact", reminder)
	}
	if !strings.Contains(reminder.Context, "测试环境") {
		t.Fatal("reminder lost the qualifier", reminder.Context)
	}
	finishEmission(t, s, reminder, true)
	// The same association stays silent afterwards.
	if again := probeDelivery(t, s, project, "线下结算"); again.Outcome != "silent" {
		t.Fatal("same association re-emitted", again)
	}
}

func TestCueReminderFallsBackToLocatorWhenBudgetExhausted(t *testing.T) {
	s := testStore(t)
	project := t.TempDir()
	if err := s.HookBind(ctx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	longFact := strings.Repeat("环境配置来源各不相同需逐项核对。", 15)
	in := ExperienceWrite{Scope: "project:demo", Project: project, Title: "长事实预算回退",
		Admission:  &ExperienceAdmission{FutureDecision: "核对配置来源。", EvidenceRef: "fixture:budget"},
		Experience: Experience{Fact: longFact,
			Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:budget", Observation: "合成事实"}},
			Cues: []ExperienceCue{
				{ID: "cuea", Label: "配置甲", EvidenceRefs: []string{"fixture:budget"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"configAlpha"}}}},
				{ID: "cueb", Label: "配置乙", EvidenceRefs: []string{"fixture:budget"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"configBeta"}}}},
				{ID: "cuec", Label: "配置丙", EvidenceRefs: []string{"fixture:budget"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"configGamma"}}}},
			}}}
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	c := probeDelivery(t, s, project, "configAlpha")
	o := nativeLookup(t, s, project, "call1", c.ID, 2)
	observeDelivery(t, s, "call1", o)
	// Two pending new associations: the first full fact consumes the budget,
	// the second must keep a locator entry instead of a trimmed assertion.
	reminder := probeDelivery(t, s, project, "configBeta configGamma")
	if reminder.Outcome != "cues" {
		t.Fatal("expected cue reminders", reminder)
	}
	if !strings.Contains(reminder.Context, longFact) {
		t.Fatal("first reminder missing the full fact", reminder.Context)
	}
	if !strings.Contains(reminder.Context, "超出短提醒预算") {
		t.Fatal("overflowing reminder was dropped instead of keeping a locator", reminder.Context)
	}
	finishEmission(t, s, reminder, true)
}

func TestSchema10UpgradeFromSchema9KeepsDataAndBaseline(t *testing.T) {
	s := testStore(t)
	project := t.TempDir()
	if err := s.HookBind(ctx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	// One legacy five-field record with cue text beyond the body, one fact-form
	// record: both shapes must survive the schema-9 -> 10 upgrade intact.
	legacy := ExperienceWrite{Scope: "project:demo", Project: project, Title: "多入口覆盖风险",
		Admission: &ExperienceAdmission{FutureDecision: "跨入口改动时逐个核对。", EvidenceRef: "fixture:legacy"},
		Experience: Experience{
			Pitfall: "在眼前调用处添加校验就能覆盖所有入口", Counterexample: "调查发现另一个入口不经过该调用处",
			Impact: "要求全入口一致的规则可能只在部分入口生效", Applicability: "调整多入口准入时核对覆盖范围", Boundary: "单入口不据此推断",
			Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:legacy", Observation: "合成旧记录"}},
			Cues:     []ExperienceCue{{ID: "admission", Label: "核验入口", Text: "隔离实验发现另一入口不经过本次检查。", EvidenceRefs: []string{"fixture:legacy"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"canDispatchLynOrder"}}}}},
	}}
	if _, err := s.RecordExperience(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	_, _, fact := factFixture(t)
	fact.Project = project
	if _, err := s.RecordExperience(ctx, fact); err != nil {
		t.Fatal(err)
	}
	dir := s.Dir
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-upgrade schema-9 library in the fact library.
	{
		raw, err := openStore(dir, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = raw.db.Exec("PRAGMA user_version=9"); err != nil {
			t.Fatal(err)
		}
		if err = raw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatal("schema-9 library opened without an explicit migration", err)
	}
	backup := filepath.Join(t.TempDir(), "baseline.db")
	report, err := ApplyCueMigration(ctx, dir, backup)
	if err != nil || !report.Applied || report.FromSchema != 9 || report.TargetSchema != 10 {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("rollback baseline missing", err)
	}
	upgraded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	out, err := upgraded.LookupExperience(ctx, ExperienceLookup{Scope: "project:demo", Project: project,
		Task: "合成任务", Questions: "结算编码", Queries: []RecallQuery{{Terms: []string{"线下结算"}}}})
	if err != nil || len(out.Items) != 1 || out.Items[0].Experience == nil || out.Items[0].Experience.Form != "fact" {
		t.Fatal("fact form lost across upgrade", out, err)
	}
	var version int
	if err = upgraded.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 10 {
		t.Fatal("schema not upgraded", version, err)
	}
	// The inspect report tracks legacy qualifier and cue-text-beyond-body records.
	inspection, err := InspectCueMigration(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.LegacyQualifierEntries) == 0 {
		t.Fatal("legacy narrative qualifiers not tracked", inspection)
	}
	if len(inspection.CueFactsBeyondBody) == 0 {
		t.Fatal("cue facts beyond body not tracked", inspection)
	}
	if len(inspection.MissingEvidence) != 0 {
		t.Fatal("unexpected missing evidence", inspection)
	}
}
