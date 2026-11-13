package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func experienceFixture(t *testing.T) (*Store, string, ExperienceWrite) {
	t.Helper()
	s := testStore(t)
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "Policy.java"), []byte("class Policy {\n boolean canDispatchLynOrder() { return true; }\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.HookBind(ctx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	in := ExperienceWrite{Scope: "project:demo", Project: project, Title: "多入口准入覆盖风险", Experience: Experience{
		Anchors: []string{"LYN", "canDispatchLynOrder"}, Signals: []string{"创建准入"},
		Pitfall:        "在眼前调用处添加校验就能覆盖所有入口",
		Counterexample: "调查发现另一个入口不经过该调用处；本测试仅验证机制，不宣称模型推理正确",
		Impact:         "要求全入口一致的规则可能只在部分入口生效",
		Applicability:  "调整多入口创建准入时核对覆盖范围", Boundary: "单入口或不同业务约定不据此推断",
		Evidence: []ExperienceEvidence{{Kind: "code", Reference: "isolated fixture; not production", Observation: "当前候选判定方法需结合实际调用者核查", Path: "Policy.java", Symbol: "canDispatchLynOrder"}},
	}}
	return s, project, in
}

func TestExperienceSyncAdmissionDedupAndRetry(t *testing.T) {
	s, project, in := experienceFixture(t)
	in.RequestID = "first"
	first, err := s.RecordExperience(ctx, in)
	if err != nil || first.Reused {
		t.Fatal(first, err)
	}
	if stat, _ := s.Stats(ctx); stat.Entries != 1 {
		t.Fatal("experience copied raw sources or duplicated data", stat)
	}
	m, _ := s.Maintenance(ctx, in.Scope)
	if m.Changes != 1 {
		t.Fatal("write was not counted once", m)
	}
	// A request retry returns its committed outcome even after source changes.
	os.WriteFile(filepath.Join(project, "Policy.java"), []byte("// added\nclass Policy { boolean canDispatchLynOrder() { return false; } }\n"), 0600)
	retry, err := s.RecordExperience(ctx, in)
	if err != nil || retry.ID != first.ID || !retry.Reused {
		t.Fatal("retry changed committed write", retry, err)
	}
	in.RequestID = "repeat"
	in.Experience.Evidence[0].Reference = "another commit; same observed failure features"
	repeat, err := s.RecordExperience(ctx, in)
	if err != nil || repeat.ID != first.ID || !repeat.Reused {
		t.Fatal("code relocation created a business revision", repeat, err)
	}
	m, _ = s.Maintenance(ctx, in.Scope)
	if m.Changes != 1 {
		t.Fatal("reuse advanced cleanup count", m)
	}
	in.RequestID = "material-correction"
	in.PreviousID = first.ID
	in.Experience.Boundary = "补充：该观察不覆盖恢复入口，仍需当前代码核查"
	second, err := s.RecordExperience(ctx, in)
	if err != nil || second.ID == first.ID || second.RootID != first.RootID {
		t.Fatal(second, err)
	}
	old, _ := s.Read(ctx, first.ID)
	if old.Entry.Current || !strings.Contains(old.Entry.Body, "单入口") {
		t.Fatal("history overwritten")
	}
	in.Experience.Impact = "changed payload"
	if _, err = s.RecordExperience(ctx, in); err == nil {
		t.Fatal("request_id accepted changed payload")
	}
}

func TestExperienceAdmissionRejectsDriftAndEscapingSources(t *testing.T) {
	s, _, in := experienceFixture(t)
	in.Experience.Evidence[0].SHA256 = strings.Repeat("0", 64)
	if _, err := s.RecordExperience(ctx, in); err == nil {
		t.Fatal("accepted stale code evidence")
	}
	in.Experience.Evidence[0].SHA256 = ""
	in.Experience.Evidence[0].Path = "../outside.java"
	if _, err := s.RecordExperience(ctx, in); err == nil {
		t.Fatal("read outside project")
	}
	in.Experience.Evidence[0].Path = "Policy.java"
	in.Experience.Counterexample = ""
	if _, err := s.RecordExperience(ctx, in); err == nil {
		t.Fatal("accepted unsupported generic lesson")
	}
	in.Experience.Counterexample = "explicit observed counterexample"
	in.Experience.Anchors = []string{"代码"}
	if _, err := s.RecordExperience(ctx, in); err == nil {
		t.Fatal("accepted generic automatic anchor")
	}
}

func TestExperienceMatchingRequiresIndependentFeaturesAndRelocatesOnly(t *testing.T) {
	s, project, in := experienceFixture(t)
	r, err := s.RecordExperience(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if found, _ := s.MatchExperiences(ctx, in.Scope, "LYN"); len(found) != 0 {
		t.Fatal("domain alone became a gate")
	}
	if found, _ := s.MatchExperiences(ctx, "project:other", "LYN 创建准入"); len(found) != 0 {
		t.Fatal("cross-project recall")
	}
	matches, err := s.MatchExperiences(ctx, in.Scope, "LYN 创建准入")
	if err != nil || len(matches) != 1 || matches[0].ID != r.ID {
		t.Fatal(matches, err)
	}
	os.WriteFile(filepath.Join(project, "Policy.java"), []byte("// new line\n// another\nclass Policy { boolean canDispatchLynOrder() { return true; } }\n"), 0600)
	out, err := s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "调整当前项目 LYN 创建准入", Questions: "眼前入口是否完整覆盖", Keywords: []string{"LYN"}})
	if err != nil || len(out.Items) != 1 {
		t.Fatal(out, err)
	}
	loc := out.Items[0].Locations[0]
	if !loc.ContentChanged || len(loc.Lines) == 0 || loc.Lines[0] != 3 || strings.Contains(loc.Status, "verified") {
		t.Fatal("location falsely certified semantics", loc)
	}
	os.Rename(filepath.Join(project, "Policy.java"), filepath.Join(project, "MovedPolicy.java"))
	out, err = s.LookupExperience(ctx, ExperienceLookup{Scope: in.Scope, Project: project, Task: "检查当前创建链", Questions: "历史锚点是否仍可定位", Keywords: []string{"LYN"}})
	if err != nil || out.Items[0].Locations[0].Status != "unresolved" {
		t.Fatal(out, err)
	}
	current, _ := s.Read(ctx, r.ID)
	if current.CurrentID != r.ID || !current.Entry.Current {
		t.Fatal("locator miss revised business history")
	}
	serialized, _ := json.Marshal(out)
	if len(serialized) > ExperienceLookupBudget || strings.Contains(string(serialized), "class Policy") {
		t.Fatal("source text leaked or budget exceeded")
	}
}

func TestExperienceChallengeDoesNotUseDeniedOrOldTurnAsPermit(t *testing.T) {
	s, project, in := experienceFixture(t)
	s.RecordExperience(ctx, in)
	matches, _ := s.MatchExperiences(ctx, in.Scope, "LYN 创建准入")
	s.ExperienceStartTurn(ctx, "codex", in.Scope, "session", "turn-1", "event-1")
	c, err := s.ChallengeExperience(ctx, "codex", in.Scope, "session", "turn-1", project, matches)
	if err != nil || c.ID == "" {
		t.Fatal(c, err)
	}
	again, _ := s.ChallengeExperience(ctx, "codex", in.Scope, "session", "turn-1", project, matches)
	if again.ID != c.ID {
		t.Fatal("denial alone granted a permit")
	}
	bad := ExperienceLookup{Scope: in.Scope, Task: "", Questions: "coverage", Keywords: []string{"LYN"}, ChallengeID: c.ID}
	if _, err = s.LookupExperience(ctx, bad); err == nil {
		t.Fatal("empty task completed challenge")
	}
	s.ExperienceStartTurn(ctx, "codex", in.Scope, "session", "turn-2", "event-2")
	newC, _ := s.ChallengeExperience(ctx, "codex", in.Scope, "session", "turn-2", project, matches)
	if newC.ID == "" || newC.ID == c.ID {
		t.Fatal("new input inherited old challenge")
	}
	bad.Task = "current task reconstructed from conversation"
	out, err := s.LookupExperience(ctx, bad)
	if err != nil || out.ChallengeOutcome != "stale_no_current_turn_release" {
		t.Fatal(out, err)
	}
	still, _ := s.ChallengeExperience(ctx, "codex", in.Scope, "session", "turn-2", project, matches)
	if still.ID != newC.ID {
		t.Fatal("late result released current input")
	}
	// An out-of-order duplicate native input cannot reactivate turn-1.
	s.ExperienceStartTurn(ctx, "codex", in.Scope, "session", "turn-1", "event-1")
	bad.ChallengeID = newC.ID
	out, err = s.LookupExperience(ctx, bad)
	if err != nil || out.ChallengeOutcome != "query_completed_not_applicability_verified" {
		t.Fatal(out, err)
	}
	allowed, _ := s.ChallengeExperience(ctx, "codex", in.Scope, "session", "turn-2", project, matches)
	if allowed.ID != "" {
		t.Fatal("successful actual lookup did not release", allowed)
	}
}

func TestExperienceChallengeConcurrencyAndExpiry(t *testing.T) {
	s, project, in := experienceFixture(t)
	s.RecordExperience(ctx, in)
	matches, _ := s.MatchExperiences(ctx, in.Scope, "LYN 创建准入")
	s.ExperienceStartTurn(ctx, "zcode", in.Scope, "session", "", "native-event")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.ChallengeExperience(ctx, "zcode", in.Scope, "session", "", project, matches)
			if err != nil {
				ids <- err.Error()
			} else {
				ids <- c.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id == "" || id != first {
			t.Fatal("parallel searches created independent gates", id, first)
		}
	}
	s.db.ExecContext(ctx, "UPDATE experience_challenges SET created_at=?", time.Now().Add(-3*time.Minute).UTC().Format(time.RFC3339Nano))
	expired, err := s.ChallengeExperience(ctx, "zcode", in.Scope, "session", "", project, matches)
	if err != nil || expired.ID != "" || expired.Outcome != "expired_fail_open_not_completed" {
		t.Fatal(expired, err)
	}
	var completed bool
	s.db.QueryRowContext(ctx, "SELECT completed FROM experience_challenges WHERE id=?", first).Scan(&completed)
	if completed {
		t.Fatal("expiry falsely recorded query completion")
	}
}
