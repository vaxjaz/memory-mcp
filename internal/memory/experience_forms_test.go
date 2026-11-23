package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// Two-form body contract, write boundary, legacy compatibility, body-derived
// reminders and the schema-10 upgrade path (design EXPERIENCE_MODEL_GLM_HANDOFF
// §3/§4 plus the external review fixes).

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

// storeLegacyShapedRecord persists a five-field body WITH cue text through the
// storage path, matching what a pre-upgrade binary commits. New writes reject
// cue text; stored history keeps reading and delivering it.
func storeLegacyShapedRecord(t *testing.T, s *Store, in ExperienceWrite) Entry {
	t.Helper()
	x := &in.Experience
	body := fmt.Sprintf("易错推断：%s\n关键反例：%s\nBug影响：%s\n适用条件：%s\n边界与未知：%s", x.Pitfall, x.Counterexample, x.Impact, x.Applicability, x.Boundary)
	d := Draft{Kind: "case", Scope: in.Scope, Title: in.Title, Body: body, Origin: "model_inference",
		SourceRef: x.Evidence[0].Reference,
		Profile:   &Profile{Class: "long_term", FutureUse: x.Applicability, CounterCheck: "legacy stored shape", Applicability: x.Applicability, Boundary: x.Boundary, Experience: x}}
	d.experienceValidated = true
	d.experienceKey = experienceKey(x)
	e, err := s.Put(ctx, d, "")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func legacyFiveFieldWrite(project string) ExperienceWrite {
	return ExperienceWrite{Scope: "project:demo", Project: project, Title: "多入口覆盖风险",
		Admission: &ExperienceAdmission{FutureDecision: "跨入口改动时逐个核对调用路径是否覆盖。", EvidenceRef: "fixture:legacy"},
		Experience: Experience{
			Pitfall: "在眼前调用处添加校验就能覆盖所有入口", Counterexample: "调查发现另一个入口不经过该调用处",
			Impact: "要求全入口一致的规则可能只在部分入口生效", Applicability: "调整多入口创建准入时核对覆盖范围", Boundary: "单入口或不同业务约定不据此推断",
			Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:legacy", Observation: "合成旧形态记录"}},
			Cues:     []ExperienceCue{{ID: "admission", Label: "核验入口", Text: "隔离实验发现另一入口不经过本次检查。", EvidenceRefs: []string{"fixture:legacy"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"canDispatchLynOrder"}}}}},
		}}
}

func TestExperienceFactFormWriteAndRecall(t *testing.T) {
	s, project, in := factFixture(t)
	receipt, err := s.RecordExperience(ctx, in)
	if err != nil || receipt.Reused {
		t.Fatal(receipt, err)
	}
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
	in.Experience.Counterexample = "调查发现另一个入口不经过该调用处；仅在该项目的隔离夹具中观察"
	_, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
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

func TestExperienceLegacyColumnsStillAcceptedWithoutCueText(t *testing.T) {
	s, project, _ := factFixture(t)
	in := legacyFiveFieldWrite(project)
	in.Experience.Cues[0].Text = ""
	first, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
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
}

func TestExperienceWriteBoundaryRejectsAuthoredCueText(t *testing.T) {
	s, project, fact := factFixture(t)
	in := legacyFiveFieldWrite(project)
	if _, err := s.RecordExperience(ctx, in); err == nil || !strings.Contains(err.Error(), "experience.cues[0].text") {
		t.Fatal("authored cue text accepted on a new write", err)
	}
	fact.Experience.Cues[0].Text = "生产环境禁止直接套用该编码。"
	if _, err := s.RecordExperience(ctx, fact); err == nil || !strings.Contains(err.Error(), "experience.cues[0].text") {
		t.Fatal("authored cue text accepted on a fact-form write", err)
	}
}

func TestFactFormRejectsSeparateQualifierColumns(t *testing.T) {
	s, _, in := factFixture(t)
	in.Experience.Boundary = "仅在测试环境观测到"
	if _, err := s.RecordExperience(ctx, in); err == nil || !strings.Contains(err.Error(), "experience.boundary") || !strings.Contains(err.Error(), "experience.fact") {
		t.Fatal("fact with a separate qualifier column accepted", err)
	}
}

func TestCommittedRequestRetrySkipsTightenedValidation(t *testing.T) {
	s, _, in := factFixture(t)
	committed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// A payload that the tightened write rules reject, retried under a
	// request_id that already committed the same material payload.
	boundary := in
	boundary.RequestID = "committed-then-tightened"
	boundary.Experience.Boundary = "仅在测试环境观测到"
	boundary.Experience.Cues[0].Text = "旧客户端附带的独立文案。"
	retryPayload := boundary
	retryPayload.Admission = nil
	raw, _ := json.Marshal(retryPayload)
	fingerprint := "experience-v1:" + digestText(string(raw))
	if _, err = s.db.ExecContext(ctx, "INSERT INTO write_requests(scope,request_id,fingerprint,entry_id)VALUES(?,?,?,?)", in.Scope, boundary.RequestID, fingerprint, committed.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := s.RecordExperience(ctx, boundary)
	if err != nil || !retry.Reused || retry.ID != committed.ID {
		t.Fatal("committed retry was re-validated and rejected", retry, err)
	}
	different := in
	different.RequestID = boundary.RequestID
	different.Experience.Pitfall = "另一个载荷"
	different.Experience.Counterexample = "另一个反例"
	if _, err = s.RecordExperience(ctx, different); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatal("same request_id accepted a different payload", err)
	}
}

func TestLegacyStoredCueTextStillHonoredOnReadAndDelivery(t *testing.T) {
	s, project, in := factFixture(t)
	legacy := legacyFiveFieldWrite(project)
	stored := storeLegacyShapedRecord(t, s, legacy)
	// The stored record stays in the recall scan and matches normally.
	entries, err := s.Experiences(ctx, in.Scope)
	if err != nil || len(entries) != 1 || entries[0].ID != stored.ID {
		t.Fatal("stored cue-text record dropped from recall scan", entries, err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	m, err := s.MatchExperiences(ctx, in.Scope, "canDispatchLynOrder")
	if err != nil || len(m) != 1 {
		t.Fatal("stored cue no longer matches", m, err)
	}
	c := probeDelivery(t, s, project, "canDispatchLynOrder")
	o := nativeLookup(t, s, project, "legacy-body", c.ID, 2)
	if len(o.Items) != 1 || o.Items[0].Experience == nil {
		t.Fatal("stored record not delivered", o)
	}
	observeDelivery(t, s, "legacy-body", o)
	reminder := probeDelivery(t, s, project, "canDispatchLynOrder RemarkInput.java")
	if reminder.Outcome != "silent" {
		// The stored cue has one entrance; a second cue on the same record
		// would emit the stored text. Here the same association is silent.
		t.Fatal("same association re-emitted", reminder)
	}
	// A stored second cue with text keeps delivering that exact text.
	legacyTwo := legacyFiveFieldWrite(project)
	legacyTwo.Experience.Cues = append(legacyTwo.Experience.Cues, ExperienceCue{ID: "second", Label: "第二入口", Text: "隔离实验发现另一入口不经过本次检查。", EvidenceRefs: []string{"fixture:legacy"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"RemarkInput.java"}}}})
	storedTwo := storeLegacyShapedRecord(t, s, legacyTwo)
	m2, err := s.MatchExperiences(ctx, in.Scope, "RemarkInput.java")
	if err != nil || len(m2) != 1 || m2[0].ID != storedTwo.ID {
		t.Fatal("stored second cue no longer matches", m2, err)
	}
	r2 := probeDelivery(t, s, project, "RemarkInput.java")
	o2 := nativeLookup(t, s, project, "legacy-two-body", r2.ID, 2)
	observeDelivery(t, s, "legacy-two-body", o2)
	// The first cue of the two-cue stored record is now a NEW association.
	// Its reminder shares the first-delivery path and quotes THIS version's
	// body (counterexample with stored qualifiers), never the legacy cue text.
	reminderTwo := probeDelivery(t, s, project, "canDispatchLynOrder")
	if reminderTwo.Outcome != "cues" {
		t.Fatal("stored first cue did not emit a reminder", reminderTwo)
	}
	if !strings.Contains(reminderTwo.Context, "调查发现另一个入口不经过该调用处") || !strings.Contains(reminderTwo.Context, "单入口或不同业务约定不据此推断") {
		t.Fatal("reminder does not quote the current body with qualifiers", reminderTwo.Context)
	}
	if strings.Contains(reminderTwo.Context, "隔离实验发现另一入口不经过本次检查。") {
		t.Fatal("legacy cue text resurfaced as a reminder", reminderTwo.Context)
	}
	finishEmission(t, s, reminderTwo, true)
	// The stored cue text keeps its identity: readable by entry ID and listed
	// by the migration inventory pending evidence confirmation.
	page, err := s.ReadPage(ctx, storedTwo.ID, "")
	if err != nil || page.Entry.Profile == nil || page.Entry.Profile.Experience == nil ||
		page.Entry.Profile.Experience.Cues[0].Text != "隔离实验发现另一入口不经过本次检查。" {
		t.Fatal("stored cue text no longer readable by ID", err)
	}
	inspection, err := InspectCueMigration(ctx, s.Dir)
	if err != nil || len(inspection.CueFactsBeyondBody) != 2 {
		t.Fatal("cue facts beyond body not inventoried", inspection, err)
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
	c := probeDelivery(t, s, project, "settlementCode")
	if c.Outcome != ChallengeQueryRequired {
		t.Fatal("first hit did not challenge", c)
	}
	o := nativeLookup(t, s, project, "call1", c.ID, 2)
	observeDelivery(t, s, "call1", o)
	reminder := probeDelivery(t, s, project, "线下结算")
	if reminder.Outcome != "cues" || !strings.Contains(reminder.Context, "结算编码由各环境的线下结算系统分别配置") {
		t.Fatal("reminder does not quote the body fact", reminder)
	}
	if !strings.Contains(reminder.Context, "测试环境") {
		t.Fatal("reminder lost the qualifier", reminder.Context)
	}
	finishEmission(t, s, reminder, true)
	if again := probeDelivery(t, s, project, "线下结算"); again.Outcome != "silent" {
		t.Fatal("same association re-emitted", again)
	}
}

func TestCueReminderFallsBackToMemoryReadLocator(t *testing.T) {
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
	receipt, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	c := probeDelivery(t, s, project, "configAlpha")
	o := nativeLookup(t, s, project, "call1", c.ID, 2)
	observeDelivery(t, s, "call1", o)
	reminder := probeDelivery(t, s, project, "configBeta configGamma")
	if reminder.Outcome != "cues" {
		t.Fatal("expected cue reminders", reminder)
	}
	if !strings.Contains(reminder.Context, longFact) {
		t.Fatal("first reminder missing the full fact", reminder.Context)
	}
	if !strings.Contains(reminder.Context, "memory_read 读取") {
		t.Fatal("overflow fallback missing memory_read locator", reminder.Context)
	}
	finishEmission(t, s, reminder, true)
	// The locator names the exact version ID and memory_read actually returns
	// the omitted fact with its qualifiers.
	id := receipt.ID
	if !strings.Contains(reminder.Context, id) {
		t.Fatal("fallback does not name the version ID", reminder.Context)
	}
	page, err := s.ReadPage(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Entry.Body, longFact) {
		t.Fatal("memory_read did not return the omitted fact", page.Entry.Body)
	}
}

func TestStoredFactReminderCarriesLegacyQualifiers(t *testing.T) {
	s, project, _ := factFixture(t)
	in := ExperienceWrite{Scope: "project:demo"}
	// A dev.1-shaped stored record: fact body with a separate boundary column.
	devOne := Experience{Fact: "结算编码是 X。", Boundary: "仅在测试环境验证",
		Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:dev1", Observation: "dev.1 存量形态"}},
		Cues: []ExperienceCue{
			{ID: "settle", Label: "结算编码", EvidenceRefs: []string{"fixture:dev1"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"settlementCode"}}}},
			{ID: "envcfg", Label: "环境配置", EvidenceRefs: []string{"fixture:dev1"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"offlineSettlement"}}}},
		}}
	d := Draft{Kind: "case", Scope: in.Scope, Title: "dev.1 存量 fact", Body: "业务事实：" + devOne.Fact, Origin: "model_inference",
		SourceRef: "fixture:dev1", Profile: &Profile{Class: "long_term", FutureUse: devOne.Fact, CounterCheck: "stored shape", Experience: &devOne}}
	d.experienceValidated = true
	d.experienceKey = experienceKey(&devOne)
	if _, err := s.Put(ctx, d, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	c := probeDelivery(t, s, project, "settlementCode")
	o := nativeLookup(t, s, project, "dev1-body", c.ID, 2)
	observeDelivery(t, s, "dev1-body", o)
	reminder := probeDelivery(t, s, project, "offlineSettlement")
	if reminder.Outcome != "cues" {
		t.Fatal("stored fact record emitted no reminder", reminder)
	}
	if !strings.Contains(reminder.Context, "结算编码是 X") || !strings.Contains(reminder.Context, "仅在测试环境验证") {
		t.Fatal("stored fact reminder lost its qualifier", reminder.Context)
	}
	finishEmission(t, s, reminder, true)
}

func TestAliasRevisionInheritsStoredCueText(t *testing.T) {
	s, project, _ := factFixture(t)
	legacy := legacyFiveFieldWrite(project)
	stored := storeLegacyShapedRecord(t, s, legacy)
	// Adding a match entrance without cue text must stay an alias update: same
	// version, delivery receipts intact, historical cue text preserved.
	revision := legacyFiveFieldWrite(project)
	revision.PreviousID = stored.ID
	revision.Experience.Cues[0].Text = ""
	revision.Experience.Cues[0].Matches = append(revision.Experience.Cues[0].Matches, CueMatch{Rule: matching.Rule{Op: "token"}, Entrances: []string{"RemarkInput.java"}})
	result, err := s.RecordExperience(ctx, revision)
	if err != nil || !result.Reused || result.ID != stored.ID {
		t.Fatal("alias revision forked the version", result, err)
	}
	kept, err := s.Read(ctx, stored.ID)
	if err != nil || kept.Entry.Profile.Experience.Cues[0].Text != "隔离实验发现另一入口不经过本次检查。" {
		t.Fatal("inherited cue text lost", kept.Entry.Profile.Experience.Cues[0].Text, err)
	}
	// Rewriting the historical text is still rejected at the boundary.
	rewrite := legacyFiveFieldWrite(project)
	rewrite.PreviousID = stored.ID
	rewrite.Experience.Cues[0].Text = "改写后的历史文案。"
	if _, err = s.RecordExperience(ctx, rewrite); err == nil || !strings.Contains(err.Error(), "experience.cues[0].text") {
		t.Fatal("cue text rewrite accepted", err)
	}
}

func TestMaterialRevisionStopsDeliveringSupersededText(t *testing.T) {
	s, project, in := factFixture(t)
	// Stored legacy shape: OLD fact in the body, OLD conclusion in cue text.
	old := Experience{Fact: "Code is OLD.", Boundary: "仅历史版本",
		Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:rev", Observation: "旧版本"}},
		Cues: []ExperienceCue{
			{ID: "settle", Label: "结算编码", Text: "Code is OLD.", EvidenceRefs: []string{"fixture:rev"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"settlementCode"}}}},
			{ID: "envcfg", Label: "环境配置", Text: "Code is OLD.", EvidenceRefs: []string{"fixture:rev"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"offlineSettlement"}}}},
		}}
	d := Draft{Kind: "case", Scope: in.Scope, Title: "旧事实", Body: "业务事实：Code is OLD.", Origin: "model_inference",
		SourceRef: "fixture:rev", Profile: &Profile{Class: "long_term", FutureUse: "Code is OLD.", CounterCheck: "stored shape", Boundary: "仅历史版本", Experience: &old}}
	d.experienceValidated = true
	d.experienceKey = experienceKey(&old)
	stored, err := s.Put(ctx, d, "")
	if err != nil {
		t.Fatal(err)
	}
	// A MATERIAL correction to NEW3: the superseded cue text must not be
	// inherited into the new version through any path.
	revision := ExperienceWrite{Scope: in.Scope, Project: project, Title: "新事实", PreviousID: stored.ID,
		Admission: &ExperienceAdmission{FutureDecision: "按新事实核对。", EvidenceRef: "fixture:rev2"},
		Experience: Experience{Fact: "Code is NEW3.", Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:rev2", Observation: "修正后事实"}},
			Cues: []ExperienceCue{
				{ID: "settle", Label: "结算编码", EvidenceRefs: []string{"fixture:rev2"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"settlementCode"}}}},
				{ID: "envcfg", Label: "环境配置", EvidenceRefs: []string{"fixture:rev2"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"offlineSettlement"}}}},
			}}}
	result, err := s.RecordExperience(ctx, revision)
	if err != nil || result.Reused || result.ID == stored.ID {
		t.Fatal("material revision did not create a new version", result, err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	c := probeDelivery(t, s, project, "settlementCode")
	o := nativeLookup(t, s, project, "new-body", c.ID, 2)
	if len(o.Items) != 1 || o.Items[0].Experience == nil {
		t.Fatal("revised body not delivered", o)
	}
	body := o.Items[0].Experience
	if body.Fact != "Code is NEW3." {
		t.Fatal("current version fact missing from the first lookup", body)
	}
	if encoded, _ := json.Marshal(body); strings.Contains(string(encoded), "Code is OLD.") {
		t.Fatal("superseded text leaked into the first lookup", body)
	}
	observeDelivery(t, s, "new-body", o)
	reminder := probeDelivery(t, s, project, "offlineSettlement")
	if reminder.Outcome != "cues" {
		t.Fatal("new association emitted no reminder", reminder)
	}
	if !strings.Contains(reminder.Context, "Code is NEW3.") || strings.Contains(reminder.Context, "Code is OLD.") {
		t.Fatal("reminder quotes the superseded conclusion", reminder.Context)
	}
	finishEmission(t, s, reminder, true)
}

func TestStoredCueTextReminderCarriesMissingQualifiers(t *testing.T) {
	s, project, in := factFixture(t)
	// dev.1 shape: fact + separate boundary + authored cue text WITHOUT the
	// qualifier. The reminder must regain the boundary on every path.
	devOne := Experience{Fact: "Code is STAGING.", Boundary: "Only verified in TEST.",
		Evidence: []ExperienceEvidence{{Kind: "business", Reference: "fixture:dev1b", Observation: "dev.1 存量形态"}},
		Cues: []ExperienceCue{
			{ID: "settle", Label: "Settlement", Text: "Code is STAGING.", EvidenceRefs: []string{"fixture:dev1b"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"settlementCode"}}}},
			{ID: "envcfg", Label: "Env", Text: "Code is STAGING.", EvidenceRefs: []string{"fixture:dev1b"}, Matches: []CueMatch{{Rule: matching.Rule{Op: "token"}, Entrances: []string{"offlineSettlement"}}}},
		}}
	d := Draft{Kind: "case", Scope: in.Scope, Title: "dev.1 存量", Body: "业务事实：Code is STAGING.", Origin: "model_inference",
		SourceRef: "fixture:dev1b", Profile: &Profile{Class: "long_term", FutureUse: "Code is STAGING.", CounterCheck: "stored shape", Boundary: "Only verified in TEST.", Experience: &devOne}}
	d.experienceValidated = true
	d.experienceKey = experienceKey(&devOne)
	if _, err := s.Put(ctx, d, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	c := probeDelivery(t, s, project, "settlementCode")
	o := nativeLookup(t, s, project, "dev1b-body", c.ID, 2)
	observeDelivery(t, s, "dev1b-body", o)
	reminder := probeDelivery(t, s, project, "offlineSettlement")
	if reminder.Outcome != "cues" {
		t.Fatal("stored record emitted no reminder", reminder)
	}
	if !strings.Contains(reminder.Context, "Code is STAGING.") || !strings.Contains(reminder.Context, "Only verified in TEST.") {
		t.Fatal("stored cue-text reminder lost the qualifier", reminder.Context)
	}
	if strings.Count(reminder.Context, "Only verified in TEST.") != 1 {
		t.Fatal("qualifier duplicated in the reminder", reminder.Context)
	}
	finishEmission(t, s, reminder, true)
}

func TestReplayHonorsCurrentBinding(t *testing.T) {
	s, project, in := factFixture(t)
	in.RequestID = "bound-once"
	committed, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// HookBind stores the canonical path (lowercased on Windows); raw tempdir
	// casing with uppercase test names would miss the row and fail the rebind.
	bound, err := canonicalPath(project)
	if err != nil {
		t.Fatal(err)
	}
	// The directory is later rebound to another project scope; the replayed
	// request must not return the old scope's receipt.
	if _, err = s.db.ExecContext(ctx, "DELETE FROM hook_bindings WHERE path=?", bound); err != nil {
		t.Fatal(err)
	}
	if err = s.HookBind(ctx, project, "project:other"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordExperience(ctx, in); err == nil || !strings.Contains(err.Error(), "binding mismatch") {
		t.Fatal("replay bypassed the current binding", err)
	}
	// Restoring the binding lets the same committed retry succeed again.
	if _, err = s.db.ExecContext(ctx, "DELETE FROM hook_bindings WHERE path=?", bound); err != nil {
		t.Fatal(err)
	}
	if err = s.HookBind(ctx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	again, err := s.RecordExperience(ctx, in)
	if err != nil || !again.Reused || again.ID != committed.ID {
		t.Fatal("retry no longer returns its committed receipt", again, err)
	}
}

func TestSchema10UpgradeFromSchema9KeepsDataAndBaseline(t *testing.T) {
	s := testStore(t)
	project := t.TempDir()
	if err := s.HookBind(ctx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	// One stored legacy record with cue text beyond the body (as a pre-upgrade
	// binary commits it), one fact-form record through the new write path.
	legacy := legacyFiveFieldWrite(project)
	stored := storeLegacyShapedRecord(t, s, legacy)
	_, _, fact := factFixture(t)
	fact.Project = project
	if _, err := s.RecordExperience(ctx, fact); err != nil {
		t.Fatal(err)
	}
	dir := s.Dir
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
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
	inspection, err := InspectCueMigration(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.LegacyQualifierEntries) != 1 || inspection.LegacyQualifierEntries[0].ID != stored.ID {
		t.Fatal("legacy narrative qualifiers not tracked", inspection)
	}
	if len(inspection.CueFactsBeyondBody) != 1 || inspection.CueFactsBeyondBody[0].ID != stored.ID {
		t.Fatal("cue facts beyond body not tracked", inspection)
	}
	if len(inspection.MissingEvidence) != 0 {
		t.Fatal("unexpected missing evidence", inspection)
	}
}
