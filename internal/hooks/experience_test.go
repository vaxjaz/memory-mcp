package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func trialFixture(t *testing.T) (*memory.Store, Runtime, string) {
	t.Helper()
	s, r, p := fixture(t)
	r.Host = "zcode"
	os.WriteFile(filepath.Join(p, "Policy.java"), []byte("boolean canDispatchLynOrder() { return true; }\n"), 0600)
	_, err := s.RecordExperience(testCtx, memory.ExperienceWrite{Scope: "project:demo", Project: p, Title: "isolated failure features", Experience: memory.Experience{
		Anchors: []string{"LYN"}, Signals: []string{"创建准入"},
		Pitfall: "局部调用覆盖所有入口", Counterexample: "隔离测试中的另一入口不经过该调用；不宣称真实业务核实", Impact: "部分入口漏校验",
		Applicability: "相同限制需要覆盖多个入口", Boundary: "不同业务目标需单独判断",
		Evidence: []memory.ExperienceEvidence{{Kind: "code", Reference: "isolated test", Observation: "只验证当前符号位置", Path: "Policy.java", Symbol: "canDispatchLynOrder"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureExperienceTrial(testCtx, "project:demo", true); err != nil {
		t.Fatal(err)
	}
	return s, r, p
}

func TestExperienceHookSelectiveWakeupAndNoTranscript(t *testing.T) {
	s, r, p := trialFixture(t)
	before, _ := s.Stats(testCtx)
	if len(handle(t, r, event("UserPromptSubmit", p, "session", "unique-private-user-text"))) != 0 {
		t.Fatal("generic per-turn reminder")
	}
	e := event("PreToolUse", p, "session", "")
	e.Tool = "Grep"
	e.Input = map[string]any{"pattern": "LYN"}
	if len(handle(t, r, e)) != 0 {
		t.Fatal("weak match interrupted")
	}
	e.Input["pattern"] = "LYN 创建准入"
	block := handle(t, r, e)
	if block["decision"] != "block" {
		t.Fatal("real two-feature match did not wake query", block)
	}
	reason := block["reason"].(string)
	if strings.Contains(reason, "部分入口漏校验") || strings.Contains(reason, "局部调用覆盖") {
		t.Fatal("case body injected before autonomous lookup")
	}
	token := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(reason)
	if len(token) != 2 {
		t.Fatal(reason)
	}
	if next := handle(t, r, e); next["decision"] != "block" {
		t.Fatal("first deny permanently opened session")
	}
	bootstrap := e
	bootstrap.Tool = "mcp__decision_memory__memory_bootstrap"
	if len(handle(t, r, bootstrap)) != 0 {
		t.Fatal("memory tool blocked")
	}
	if next := handle(t, r, e); next["decision"] != "block" {
		t.Fatal("bootstrap granted lookup completion")
	}
	lookup, err := s.LookupExperience(testCtx, memory.ExperienceLookup{Scope: "project:demo", Task: "本次创建准入需求", Questions: "当前入口是否完整覆盖", Keywords: []string{"LYN"}, ChallengeID: token[1]})
	if err != nil || len(lookup.Items) != 1 {
		t.Fatal(lookup, err)
	}
	if len(handle(t, r, e)) != 0 {
		t.Fatal("actual lookup not recognized")
	}
	if len(handle(t, r, event("Stop", p, "session", ""))) != 0 {
		t.Fatal("Stop continued model")
	}
	after, _ := s.Stats(testCtx)
	if before != after {
		t.Fatal("wakeup stored raw text or a business receipt", before, after)
	}
}

func TestExperienceNormReadingAndUnrelatedToolsStayAvailable(t *testing.T) {
	_, r, p := trialFixture(t)
	handle(t, r, event("UserPromptSubmit", p, "session", "task"))
	for _, test := range []struct {
		name  string
		input map[string]any
	}{
		{"Skill", map[string]any{"skill": "evolve"}},
		{"Read", map[string]any{"file_path": ".agents/skills/evolve/SKILL.md", "query": "LYN 创建准入"}},
		{"Bash", map[string]any{"command": "git status"}},
		{"Bash", map[string]any{"command": "echo LYN 创建准入"}},
		{"web_search", map[string]any{"query": "LYN 创建准入"}},
	} {
		e := event("PreToolUse", p, "session", "")
		e.Tool = test.name
		e.Input = test.input
		if len(handle(t, r, e)) != 0 {
			t.Fatal("blocked preparation/unrelated tool", test.name)
		}
	}
	e := event("PreToolUse", p, "session", "")
	e.Tool = "Bash"
	e.Input = map[string]any{"command": "rg -n 'LYN 创建准入' src"}
	if handle(t, r, e)["decision"] != "block" {
		t.Fatal("literal rg clue not recognized")
	}
}

func TestExperienceMatcherAvoidsLaunchingOnUnrelatedToolPaths(t *testing.T) {
	matcher := regexp.MustCompile(experienceToolMatcher)
	for _, name := range []string{"Grep", "Glob", "Read", "Bash", "exec_command", "run_in_terminal", "grep_code", "search_file", "mcp__filesystem__read_file"} {
		if !matcher.MatchString(name) {
			t.Fatal("documented code tool excluded", name)
		}
	}
	for _, name := range []string{"Skill", "Edit", "apply_patch", "memory_read", "mcp__decision_memory__memory_experience_lookup", "web_search", "spawn_agent"} {
		if matcher.MatchString(name) {
			t.Fatal("unrelated tool charged a probe process", name)
		}
	}
}

func TestDisabledAndUnboundHaveNoFallbackWorkflow(t *testing.T) {
	s, r, p := trialFixture(t)
	if err := s.ConfigureExperienceTrial(testCtx, "project:demo", false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"UserPromptSubmit", "PreToolUse", "Stop"} {
		e := event(name, p, "session", "private text")
		e.Tool = "Grep"
		e.Input = map[string]any{"pattern": "LYN 创建准入"}
		if out := handle(t, r, e); len(out) != 0 {
			t.Fatal("disabled wakeup activated an older workflow", name, out)
		}
	}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "Stop"} {
		e := event(name, t.TempDir(), "unbound", "private text")
		if out := handle(t, r, e); len(out) != 0 {
			t.Fatal("unbound project injected instructions", name, out)
		}
	}
}

func TestExperienceSetupIsProjectOptInAndCanDisable(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "project")
	os.Mkdir(p, 0700)
	config := filepath.Join(root, "home", "config.json")
	os.MkdirAll(filepath.Dir(config), 0700)
	os.WriteFile(config, []byte(`{"mcp":{"servers":{"other":{"command":"keep"}}},"hooks":{"events":{}}}`), 0600)
	o := SetupOptions{Host: "zcode", Config: config, DataDir: filepath.Join(root, "data"), Project: p, Scope: "project:demo", Binary: filepath.Join(root, "memory-mcp")}
	report, err := Setup(testCtx, o)
	if err != nil || report["wakeup_enabled"] != true {
		t.Fatal(report, err)
	}
	content, _ := os.ReadFile(config)
	if !strings.Contains(string(content), "PreToolUse") || strings.Contains(string(content), "Stop") {
		t.Fatal("wrong event registration", string(content))
	}
	// Reinstall preserves the explicit project policy.
	report, err = Setup(testCtx, o)
	if err != nil || report["wakeup_enabled"] != true {
		t.Fatal(report, err)
	}
	o.DisableWakeup = true
	report, err = Setup(testCtx, o)
	if err != nil || report["wakeup_enabled"] != false || report["experience_hooks_installed"] != false {
		t.Fatal(report, err)
	}
}
