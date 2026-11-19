package memory

import (
"github.com/vaxjaz/memory-mcp/internal/matching"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every fact in this fixture is synthetic and stays in t.TempDir().
func deliveryFixture(t *testing.T) (*Store, string, ExperienceWrite) {
	s, p, in := experienceFixture(t)
	in.Experience.Counterexample = "隔离实验中备注更新后日志仍取旧文本；另一取值入口读取独立参数。"
	in.Admission = &ExperienceAdmission{FutureDecision: "修改备注时单独核对日志的数据来源。", EvidenceRef: "fixture:counterexample"}
	in.Experience.Evidence = []ExperienceEvidence{{Kind: "business", Reference: "fixture:counterexample", Observation: "合成实验只验证投递协议，不是真实业务事故"}}
	in.Experience.Cues = []ExperienceCue{
		{ID: "remark", Label: "备注变化", Text: "隔离实验中备注更新后日志仍取旧文本。", EvidenceRefs: []string{"fixture:counterexample"}, Matches: []CueMatch{{Rule:matching.Rule{Op:"token"},Entrances:[]string{"WorkOrder.remark"}}, {Rule:matching.Rule{Op:"token"},Entrances:[]string{"RemarkInput.java"}}, {Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{"调整回馈备注"}}}},
		{ID: "log_input", Label: "取值入口", Text: "隔离实验中日志取值入口读取独立参数，未接入更新后的备注。", EvidenceRefs: []string{"fixture:counterexample"}, Matches: []CueMatch{{Rule:matching.Rule{Op:"token"},Entrances:[]string{"FeedbackLogBuilder.build"}}, {Rule:matching.Rule{Op:"token"},Entrances:[]string{"FeedbackLogBuilder.java"}}, {Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{"生成回馈日志"}}}},
	}
	if _, err := s.RecordExperience(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "e1"); err != nil {
		t.Fatal(err)
	}
	return s, p, in
}
func probeDelivery(t *testing.T, s *Store, p, clues string) ExperienceChallenge {
	t.Helper()
	m, err := s.MatchExperiences(ctx, "project:demo", clues)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.ChallengeExperience(ctx, "codex", "project:demo", "consumer", "t1", p, m)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func nativeLookup(t *testing.T, s *Store, p, call, challenge string, limit int) ExperienceLookupResult {
	t.Helper()
	if err := s.BeginExperienceLookup(ctx, "codex", "project:demo", "consumer", p, call, "lookup", challenge); err != nil {
		t.Fatal(err)
	}
	o, err := s.LookupExperience(ctx, ExperienceLookup{Scope: "project:demo", Project: p, Task: "合成隔离任务，按当前证据比较条件", Questions: "备注与日志的历史反例是否涉及当前入口", Queries: []RecallQuery{{Terms: []string{"不匹配查询"}}}, ChallengeID: challenge, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func observeDelivery(t *testing.T, s *Store, call string, o ExperienceLookupResult) {
	t.Helper()
	if err := s.ObserveExperienceLookup(ctx, "codex", "project:demo", "consumer", call, "lookup", o); err != nil {
		t.Fatal(err)
	}
}
func finishEmission(t *testing.T, s *Store, c ExperienceChallenge, success bool) {
	t.Helper()
	if err := s.FinishCueEmission(ctx, c.EmissionID, success); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryA01ToA10A18A23A26A34A37A38A41A42(t *testing.T) {
	s, p, in := deliveryFixture(t)
	start := time.Now()
	if c := probeDelivery(t, s, p, "UnknownSymbol"); c.ID != "" || c.Context != "" {
		t.Fatal("A01", c)
	}
	first := probeDelivery(t, s, p, "WorkOrder.remark")
	if first.ID == "" || first.Context != "" {
		t.Fatal("A02", first)
	}
	lookup := nativeLookup(t, s, p, "first", first.ID, 0)
	bytes, _ := json.Marshal(lookup)
	if lookup.ChallengeOutcome != "response_generated" || len(lookup.Items) != 1 || strings.Contains(string(bytes), "entrances") || strings.Contains(string(bytes), "anchor_prefixes") {
		t.Fatal("A03/A34", string(bytes))
	}
	if c := probeDelivery(t, s, p, "WorkOrder.remark"); c.ID != first.ID {
		t.Fatal("A12 generation falsely delivered")
	}
	observeDelivery(t, s, "first", lookup)
	for i := 0; i < 100; i++ {
		clue := []string{"WorkOrder.remark", "RemarkInput.java", "调整 回馈 备注"}[i%3]
		if c := probeDelivery(t, s, p, clue); c.ID != "" || c.Context != "" {
			t.Fatal("A04/A05/A26", i, c)
		}
	}
	s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t2", "continue")
	if c := probeDelivery(t, s, p, "RemarkInput.java"); c.ID != "" || c.Context != "" {
		t.Fatal("A18", c)
	}
	b := probeDelivery(t, s, p, "FeedbackLogBuilder.build FeedbackLogBuilder.java 生成回馈日志")
	if b.ID != "" || !strings.Contains(b.Context, in.Experience.Cues[1].Text) || strings.Count(b.Context, "取值入口：") != 1 {
		t.Fatal("A06/A08/A10", b)
	}
	finishEmission(t, s, b, true)
	for i := 0; i < 100; i++ {
		if c := probeDelivery(t, s, p, "生成回馈日志"); c.ID != "" || c.Context != "" {
			t.Fatal("A07/A26", c)
		}
	}
	// An agent may judge 'inapplicable' or 'already repaired' and do nothing.
	// There is no API, ledger state, mandatory write or next-step continuation.
	before, _ := s.Stats(ctx)
	again, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: p, Task: "当前证据显示条件不同", Questions: "主动重新读取反例", Queries: []RecallQuery{{Terms: []string{"备注"}}}})
	if err != nil || len(again.Items) != 1 {
		t.Fatal("A23", again, err)
	}
	after, _ := s.Stats(ctx)
	if before != after {
		t.Fatal("A37/A38/A42 generated experience from non-adoption")
	}
	t.Logf("A→repeat A→B→repeat B: block=1 lookup=1 native_success=1 repeated_A_bytes=0 B_bytes=%d repeated_B_bytes=0 lookup_bytes=%d elapsed=%s", len(b.Context), len(bytes), time.Since(start))
}

func TestDeliveryA09A17A22A43(t *testing.T) {
	s, p, in := deliveryFixture(t)
	a := probeDelivery(t, s, p, "WorkOrder.remark")
	o := nativeLookup(t, s, p, "e1", a.ID, 0)
	observeDelivery(t, s, "e1", o)
	other := in
	other.Experience.Counterexample = "合成独立机制：事务回滚后日志未回滚"
	other.Experience.Cues = append([]ExperienceCue(nil), in.Experience.Cues[:1]...)
	saved, err := s.RecordExperience(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	e2 := probeDelivery(t, s, p, "WorkOrder.remark")
	if e2.ID == "" || len(e2.Matches) != 1 || e2.Matches[0].ID != saved.ID {
		t.Fatal("A09", e2)
	}
	page := nativeLookup(t, s, p, "e2", e2.ID, 1)
	if len(page.Items) != 1 || page.Items[0].ID != saved.ID {
		t.Fatal("A17", page)
	}
	observeDelivery(t, s, "e2", page)
	other.PreviousID = saved.ID
	other.Experience.Boundary = "合成新增版本边界：事务失败路径另行核对"
	revised, err := s.RecordExperience(ctx, other)
	if err != nil || revised.ID == saved.ID || revised.RootID != saved.RootID {
		t.Fatal("A22/A43", revised, err)
	}
	if c := probeDelivery(t, s, p, "WorkOrder.remark"); c.ID == "" || c.Matches[0].ID != revised.ID {
		t.Fatal("old receipt covered revision", c)
	}
	old, _ := s.Read(ctx, saved.ID)
	if old.Entry.Current || old.Entry.Profile.Experience.Boundary == other.Experience.Boundary {
		t.Fatal("history overwritten")
	}
}

func TestDeliveryA16ActualPageOnly(t *testing.T) {
	s, p, in := deliveryFixture(t)
	other := in
	other.Experience.Counterexample = "另一合成反例：独立事务失败后仍输出成功日志"
	second, err := s.RecordExperience(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	c := probeDelivery(t, s, p, "WorkOrder.remark")
	if len(c.Matches) != 2 {
		t.Fatal("missing candidates", c)
	}
	page := nativeLookup(t, s, p, "page-1", c.ID, 1)
	if len(page.Items) != 1 || !page.HasMore || page.NextCursor == "" {
		t.Fatal(page)
	}
	observeDelivery(t, s, "page-1", page)
	var delivered int
	s.db.QueryRow("SELECT count(*) FROM experience_body_deliveries").Scan(&delivered)
	if delivered != 1 {
		t.Fatal("unreturned page credited", delivered)
	}
	remaining := probeDelivery(t, s, p, "WorkOrder.remark")
	if remaining.ID == "" || len(remaining.Matches) != 1 || remaining.Matches[0].ID == page.Items[0].ID {
		t.Fatal("A16 unreturned candidate lost", remaining)
	}
	if err = s.BeginExperienceLookup(ctx, "codex", in.Scope, "consumer", p, "page-2", "lookup", c.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: p, Task: "合成隔离任务，按当前证据比较条件", Questions: "备注与日志的历史反例是否涉及当前入口", Queries: []RecallQuery{{Terms: []string{"不匹配查询"}}}, ChallengeID: c.ID, Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatal(next, err)
	}
	observeDelivery(t, s, "page-2", next)
	if done := probeDelivery(t, s, p, "WorkOrder.remark"); done.ID != "" || done.Context != "" {
		t.Fatal("pages did not accumulate", done)
	}
	_ = second
}

func TestDeliveryA11A27A29(t *testing.T) {
	s, p, in := deliveryFixture(t)
	a := probeDelivery(t, s, p, "WorkOrder.remark")
	o := nativeLookup(t, s, p, "body", a.ID, 0)
	observeDelivery(t, s, "body", o)
	var wg sync.WaitGroup
	results := make(chan ExperienceChallenge, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, e := s.MatchExperiences(ctx, in.Scope, "FeedbackLogBuilder.build")
			if e != nil {
				errs <- e
				return
			}
			c, e := s.ChallengeExperience(ctx, "codex", in.Scope, "consumer", "", p, m)
			if e != nil {
				errs <- e
				return
			}
			results <- c
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	count := 0
	var emitted ExperienceChallenge
	for c := range results {
		if c.Context != "" {
			count++
			emitted = c
		}
	}
	if count != 1 {
		t.Fatal("A11 concurrent emitters", count)
	}
	finishEmission(t, s, emitted, false)
	retry := probeDelivery(t, s, p, "FeedbackLogBuilder.build")
	if retry.Context == "" {
		t.Fatal("A29 failed output swallowed cue")
	}
	s.db.Exec("UPDATE experience_cue_deliveries SET owner='99999999:dead' WHERE emission_id=?", retry.EmissionID)
	recovered := probeDelivery(t, s, p, "FeedbackLogBuilder.build")
	if recovered.Context == "" {
		t.Fatal("A29 dead process not recovered")
	}
	finishEmission(t, s, recovered, true)
	// Budget over multiple distinct associations, all already present in body.
	in.PreviousID = o.Items[0].ID
	in.Experience.Cues = nil
	for i := 0; i < 4; i++ {
		in.Experience.Cues = append(in.Experience.Cues, ExperienceCue{ID: fmt.Sprintf("fact_%d", i), Label: "隔离关联", Text: fmt.Sprintf("合成条件%d：%s。", i, strings.Repeat("反例", 45)), EvidenceRefs: []string{"fixture:counterexample"}, Matches: []CueMatch{{Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{fmt.Sprintf("业务关联%d", i)}}}})
	}
	if _, e := s.RecordExperience(ctx, in); e != nil {
		t.Fatal(e)
	}
	all := "业务关联0 业务关联1 业务关联2 业务关联3"
	c := probeDelivery(t, s, p, all)
	body := nativeLookup(t, s, p, "newbody", c.ID, 0)
	// Ordinary lookup delivery consumes no trigger cue.
	body = func() ExperienceLookupResult {
		v, e := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: p, Task: "合成任务", Questions: "读取全部事实", Queries: []RecallQuery{{Terms: []string{"业务关联0"}}}})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}()
	if e := s.BeginExperienceLookup(ctx, "codex", in.Scope, "consumer", p, "ordinary", "lookup", ""); e != nil {
		t.Fatal(e)
	}
	observeDelivery(t, s, "ordinary", body)
	batch := probeDelivery(t, s, p, all)
	if batch.ID != "" || len(batch.Context) > CueOutputBudget || !strings.Contains(batch.Context, "未展示") {
		t.Fatal("A27", len(batch.Context), batch)
	}
	finishEmission(t, s, batch, true)
	rest := probeDelivery(t, s, p, all)
	if rest.Context == "" {
		t.Fatal("A27 undisplayed items consumed")
	}
	finishEmission(t, s, rest, true)
	if c := probeDelivery(t, s, p, all); c.Context != "" {
		t.Fatal("A27 already output items repeated")
	}
}

func TestDeliveryEmitterChild(t *testing.T) {
	if os.Getenv("MEMORY_DELIVERY_CHILD") != "1" {
		return
	}
	s, err := Open(os.Getenv("MEMORY_DELIVERY_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m, err := s.MatchExperiences(ctx, "project:demo", "FeedbackLogBuilder.build")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.ChallengeExperience(ctx, "codex", "project:demo", "consumer", "", os.Getenv("MEMORY_DELIVERY_PROJECT"), m)
	if err != nil {
		t.Fatal(err)
	}
	if c.Context != "" {
		if os.Getenv("MEMORY_DELIVERY_CRASH") == "1" {
			os.Exit(0)
		}
		if err = s.FinishCueEmission(ctx, c.EmissionID, true); err != nil {
			t.Fatal(err)
		}
		fmt.Println("DELIVERY_EMITTER=1")
	}
}

func TestDeliveryA11A29IndependentProcesses(t *testing.T) {
	s, p, _ := deliveryFixture(t)
	c := probeDelivery(t, s, p, "WorkOrder.remark")
	o := nativeLookup(t, s, p, "body-process", c.ID, 0)
	observeDelivery(t, s, "body-process", o)
	command := func(crash bool) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDeliveryEmitterChild$")
		cmd.Env = append(os.Environ(), "MEMORY_DELIVERY_CHILD=1", "MEMORY_DELIVERY_DIR="+s.Dir, "MEMORY_DELIVERY_PROJECT="+p)
		if crash {
			cmd.Env = append(cmd.Env, "MEMORY_DELIVERY_CRASH=1")
		}
		return cmd
	}
	// A real short-lived process reserves a cue and exits before stdout commit.
	if out, err := command(true).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	var wg sync.WaitGroup
	outputs := make(chan string, 6)
	errors := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := command(false).CombinedOutput()
			outputs <- string(out)
			errors <- err
		}()
	}
	wg.Wait()
	close(outputs)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for out := range outputs {
		count += strings.Count(out, "DELIVERY_EMITTER=1")
	}
	if count != 1 {
		t.Fatal("independent processes emitted", count)
	}
	if again := probeDelivery(t, s, p, "FeedbackLogBuilder.build"); again.Context != "" {
		t.Fatal("process output repeated", again)
	}
}

func TestDeliveryA12A14A15A19A20A35A36(t *testing.T) {
	s, p, in := deliveryFixture(t)
	a := probeDelivery(t, s, p, "WorkOrder.remark")
	o := nativeLookup(t, s, p, "late", a.ID, 0)
	var n int
	s.db.QueryRow("SELECT count(*) FROM experience_body_deliveries").Scan(&n)
	if n != 0 {
		t.Fatal("A12", n)
	}
	forged := o
	forged.Items = append([]ExperienceItem(nil), o.Items...)
	forged.Items[0].Title = "forged"
	if e := s.ObserveExperienceLookup(ctx, "codex", in.Scope, "consumer", "late", "lookup", forged); e == nil {
		t.Fatal("A14 forged item accepted")
	}
	if e := s.ObserveExperienceLookup(ctx, "codex", in.Scope, "child", "late", "lookup", o); e == nil {
		t.Fatal("A14/A19 foreign agent")
	}
	s.ExperienceContextReset(ctx, "codex", in.Scope, "consumer", "compact-1")
	consumerBefore, _, _ := s.deliveryContext(ctx, "codex", in.Scope, "consumer", p)
	s.ExperienceContextReset(ctx, "codex", in.Scope, "consumer", "compact-1")
	consumerAfter, _, _ := s.deliveryContext(ctx, "codex", in.Scope, "consumer", p)
	if consumerBefore != consumerAfter {
		t.Fatal("A20 compact replay reset")
	}
	observeDelivery(t, s, "late", o)
	observeDelivery(t, s, "late", o)
	if c := probeDelivery(t, s, p, "WorkOrder.remark"); c.ID == "" || c.ID == a.ID {
		t.Fatal("A15/A35 late result released new epoch", c)
	}
	// Native pre-call associations remain independent through concurrent ordinary
	// lookups and an intervening compact. No 'last query' singleton is involved.
	for _, call := range []string{"ordinary-old", "ordinary-new"} {
		if call == "ordinary-new" {
			s.ExperienceContextReset(ctx, "codex", in.Scope, "consumer", "compact-2")
		}
		if e := s.BeginExperienceLookup(ctx, "codex", in.Scope, "consumer", p, call, "lookup", ""); e != nil {
			t.Fatal(e)
		}
	}
	ordinary, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: p, Task: "主动合成查询", Questions: "比较条件", Queries: []RecallQuery{{Terms: []string{"备注"}}}})
	if err != nil {
		t.Fatal(err)
	}
	observeDelivery(t, s, "ordinary-old", ordinary)
	if e := s.ObserveExperienceLookup(ctx, "codex", in.Scope, "consumer", "ordinary-new", "lookup", ordinary); e == nil {
		t.Fatal("A36 receipt cross-call replay")
	}
	newResult, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: p, Task: "主动合成查询", Questions: "比较条件", Queries: []RecallQuery{{Terms: []string{"备注"}}}})
	if err != nil {
		t.Fatal(err)
	}
	observeDelivery(t, s, "ordinary-new", newResult)
	cue := probeDelivery(t, s, p, "WorkOrder.remark")
	if cue.ID != "" || cue.Context == "" {
		t.Fatal("ordinary lookup invented trigger cue", cue)
	}
}

func TestDeliveryA21A24A28A39Atomic(t *testing.T) {
	s, p, in := deliveryFixture(t)
	a := probeDelivery(t, s, p, "WorkOrder.remark")
	o := nativeLookup(t, s, p, "body", a.ID, 0)
	observeDelivery(t, s, "body", o)
	alias := in
	alias.Experience.Cues = append([]ExperienceCue(nil), in.Experience.Cues...)
	alias.Experience.Cues[0].Matches = append(append([]CueMatch(nil), in.Experience.Cues[0].Matches...), CueMatch{Rule:matching.Rule{Op:"token"},Entrances:[]string{"remark.sync.enabled"}})
	saved, err := s.RecordExperience(ctx, alias)
	if err != nil || saved.ID != o.Items[0].ID || !saved.Reused {
		t.Fatal("A21", saved, err)
	}
	if c := probeDelivery(t, s, p, "remark.sync.enabled"); c.ID != "" || c.Context != "" {
		t.Fatal("alias reset cue", c)
	}
	// cue changes are material even when a caller forgets to edit the primary
	// counterexample: first delivery includes the changed related fact as body.
	alias.PreviousID = saved.ID
	alias.Experience.Cues[1].Text = "合成新边界：另一事务失败时独立参数也未提交。"
	revision, err := s.RecordExperience(ctx, alias)
	if err != nil || revision.ID == saved.ID {
		t.Fatal("A39", revision, err)
	}
	c := probeDelivery(t, s, p, "FeedbackLogBuilder.build")
	if c.ID == "" || c.Context != "" {
		t.Fatal("new risk demoted to cue", c)
	}
	response := nativeLookup(t, s, p, "revised", c.ID, 0)
	encoded, _ := json.Marshal(response)
	if !strings.Contains(string(encoded), alias.Experience.Cues[1].Text) {
		t.Fatal("A39 risk missing from first body")
	}
	bad := alias
	bad.Experience.Cues = append([]ExperienceCue(nil), alias.Experience.Cues...)
	bad.Experience.Cues[0].Text = strings.Repeat("汉", 161)
	if _, e := s.RecordExperience(ctx, bad); e == nil {
		t.Fatal("A28 oversized cue accepted")
	}
	bad.Experience.Cues[0].Text = "合成反例"
	bad.Experience.Cues[0].EvidenceRefs = []string{"missing-evidence"}
	if _, e := s.RecordExperience(ctx, bad); e == nil {
		t.Fatal("A28 missing evidence accepted")
	}
	// A failed entrance-index update rolls back body, revision and request ID.
	s.db.Exec("CREATE TRIGGER entrance_failure BEFORE INSERT ON experience_entrances BEGIN SELECT RAISE(ABORT,'fixture failure');END")
	alias.PreviousID = revision.ID
	alias.Experience.Boundary = "事务失败回滚的合成边界"
	if _, e := s.RecordExperience(ctx, alias); e == nil {
		t.Fatal("atomic index failure accepted")
	}
	current, _ := s.Read(ctx, revision.ID)
	if !current.Entry.Current {
		t.Fatal("failed index retired body")
	}
	// Pure business experience required no source file/class and is searchable.
	if in.Experience.Evidence[0].Path != "" || in.Experience.Cues[0].Matches[0].Op == "" {
		t.Fatal("A24 fixture requires code or omitted its entrance")
	}
}

func TestDeliveryA21CodeReferenceRelocation(t *testing.T) {
	s, p, in := experienceFixture(t)
	in.Experience.Cues = []ExperienceCue{{ID: "admission", Label: "准入入口", Text: "合成反例中，本地入口未覆盖另一调用方。", EvidenceRefs: []string{in.Experience.Evidence[0].Reference}, Matches: []CueMatch{{Rule:matching.Rule{Op:"token"},Entrances:[]string{"canDispatchLynOrder"}}}}}
	saved, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	s.ExperienceStartTurn(ctx, "codex", in.Scope, "consumer", "t1", "")
	c := probeDelivery(t, s, p, "canDispatchLynOrder")
	o := nativeLookup(t, s, p, "code-first", c.ID, 0)
	observeDelivery(t, s, "code-first", o)
	file := filepath.Join(p, in.Experience.Evidence[0].Path)
	raw, _ := os.ReadFile(file)
	os.WriteFile(file, append([]byte("// synthetic line relocation\n"), raw...), 0600)
	oldRef := in.Experience.Evidence[0].Reference
	in.Experience.Evidence[0].SHA256 = ""
	in.Experience.Evidence[0].Reference = "fixture:relocated-commit-line"
	in.Admission.EvidenceRef = in.Experience.Evidence[0].Reference
	in.Experience.Cues[0].EvidenceRefs = []string{in.Experience.Evidence[0].Reference}
	in.Experience.Cues[0].Matches = append(in.Experience.Cues[0].Matches,CueMatch{Rule:matching.Rule{Op:"token"},Entrances:[]string{"admission.fixture.enabled"}})
	again, err := s.RecordExperience(ctx, in)
	if err != nil || again.ID != saved.ID || !again.Reused {
		t.Fatal(again, err)
	}
	stored, err := s.Read(ctx, saved.ID)
	if err != nil || stored.Entry.Profile.Experience.Cues[0].EvidenceRefs[0] != oldRef {
		t.Fatal("historical evidence reference broken", stored, err)
	}
	if c := probeDelivery(t, s, p, "admission.fixture.enabled"); c.ID != "" || c.Context != "" {
		t.Fatal("relocation reset delivery", c)
	}
}

func TestDeliveryA32MigrationAndRollback(t *testing.T) {
	s, p, in := experienceFixture(t)
	r, e := s.RecordExperience(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	in.PreviousID = r.ID
	in.Experience.Boundary = "合成修订边界"
	s.RecordExperience(ctx, in)
	before, _ := s.Export(ctx)
	b, _ := json.Marshal(before.Entries)
	backup := filepath.Join(t.TempDir(), "before.db")
	if e = s.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	// Simulate genuine old receipt/cooldown tables; never imply host receipt.
	if _, e = s.db.Exec(`DROP TABLE experience_delivery_protocol; DROP TABLE experience_challenges; CREATE TABLE experience_challenges(id TEXT PRIMARY KEY,completed INTEGER);INSERT INTO experience_challenges VALUES('old-generated',1);CREATE TABLE experience_cooldowns(root_id TEXT);INSERT INTO experience_cooldowns VALUES('old-generated');PRAGMA user_version=4;`); e != nil {
		t.Fatal(e)
	}
	dir := s.Dir
	s.Close()
	upgraded, e := openStore(dir, 10000, filepath.Join(t.TempDir(), "migration.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer upgraded.Close()
	after, _ := upgraded.Export(ctx)
	a, _ := json.Marshal(after.Entries)
	if string(a) != string(b) {
		t.Fatal("A32 history changed")
	}
	var n int
	upgraded.db.QueryRow("SELECT count(*) FROM experience_body_deliveries").Scan(&n)
	if n != 0 {
		t.Fatal("old generated receipt promoted")
	}
	m, e := upgraded.MatchExperiences(ctx, in.Scope, "LYN 创建准入")
	if e != nil || len(m) != 1 || m[0].CueID != "admission" {
		t.Fatal("A32 legacy disabled", m, e)
	}
	restored := t.TempDir()
	raw, _ := os.ReadFile(backup)
	os.WriteFile(filepath.Join(restored, "memory.db"), raw, 0600)
	rollback, e := Open(restored)
	if e != nil {
		t.Fatal(e)
	}
	defer rollback.Close()
	snapshot, _ := rollback.Export(ctx)
	rb, _ := json.Marshal(snapshot.Entries)
	if string(rb) != string(b) {
		t.Fatal("A32 backup restore changed history")
	}
	_ = p
}
