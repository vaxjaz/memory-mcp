package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestHostAndNativeTurnIsolation(t *testing.T) {
	s, r, p := fixture(t)
	ids := map[string]string{}
	for _, host := range []string{"zcode", "codex", "qoder", "qoder-ide"} {
		r.Host = host
		e := event("UserPromptSubmit", p, "same-session", "相同原话")
		e.EventID = "same-event"
		e.TurnID = "turn1"
		handle(t, r, e)
		turn, err := s.HookNativeTurn(testCtx, host, "project:demo", e.Session, e.TurnID)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if id == turn.SourceID {
				t.Fatal("cross-host deduplication")
			}
		}
		ids[host] = turn.SourceID
		raw, err := s.Read(testCtx, turn.SourceID)
		if err != nil || raw.Entry.Host != host || raw.Entry.SourceRef != "hook-event:"+host+":user" {
			t.Fatal(raw, err)
		}
		_, err = s.Context(testCtx, memory.ContextInput{Action: "skip", Scope: "project:demo", SourceID: turn.SourceID, Task: "回复普通问候", Decision: "普通问候不涉及历史决定"})
		if err != nil {
			t.Fatal(err)
		}
	}
	r.Host = "codex"
	e := event("UserPromptSubmit", p, "same-session", "第二轮原话")
	e.EventID = "second"
	e.TurnID = "turn2"
	handle(t, r, e)
	// An old, delayed Stop must attach its answer to turn1, never latest turn2.
	stop := event("Stop", p, "same-session", "")
	stop.TurnID = "turn1"
	stop.Answer = "旧轮答案"
	handle(t, r, stop)
	first, _ := s.HookTurn(testCtx, ids["codex"])
	second, _ := s.HookNativeTurn(testCtx, "codex", "project:demo", "same-session", "turn2")
	if first.AnswerID == "" || second.AnswerID != "" {
		t.Fatal(first, second)
	}
	answer, err := s.Read(testCtx, first.AnswerID)
	if err != nil || answer.Entry.Host != "codex" {
		t.Fatal(answer, err)
	}
	// Unknown turn cannot fall back to current input.
	stop.TurnID = "absent"
	handle(t, r, stop)
	second, _ = s.HookTurn(testCtx, second.SourceID)
	if second.AnswerID != "" {
		t.Fatal("unknown turn fell back")
	}
}

func TestStopContinuationIsNotUserEvidence(t *testing.T) {
	s, r, p := fixture(t)
	r.Host = "codex"
	input := event("UserPromptSubmit", p, "s", "真正用户输入")
	input.TurnID = "t1"
	input.EventID = "t1"
	handle(t, r, input)
	first, _ := s.HookNativeTurn(testCtx, "codex", "project:demo", "s", "t1")
	stop := event("Stop", p, "s", "")
	stop.TurnID = "t1"
	stop.Answer = "业务答复"
	out := handle(t, r, stop)
	reason, _ := out["reason"].(string)
	if reason == "" {
		t.Fatal(out)
	}
	count, _ := s.Stats(testCtx)
	synthetic := event("UserPromptSubmit", p, "s", reason)
	synthetic.TurnID = "t2"
	synthetic.EventID = "t2"
	handle(t, r, synthetic)
	handle(t, r, synthetic)
	after, _ := s.Stats(testCtx)
	mapped, err := s.HookNativeTurn(testCtx, "codex", "project:demo", "s", "t2")
	if err != nil || after.Entries != count.Entries || mapped.SourceID != first.SourceID {
		t.Fatal(after, count, mapped, err)
	}
	stop.TurnID = "t2"
	stop.StopActive = true
	if handle(t, r, stop)["decision"] == "block" {
		t.Fatal("continuation loop")
	}
	// An ordinary repeated user prompt remains a new actual event.
	input.TurnID = "t3"
	input.EventID = "t3"
	handle(t, r, input)
	next, _ := s.HookNativeTurn(testCtx, "codex", "project:demo", "s", "t3")
	if next.SourceID == first.SourceID {
		t.Fatal("user text wrongly deduplicated")
	}
}

func TestAdapterWireContracts(t *testing.T) {
	a, _ := AdapterFor("codex")
	if _, e := a.Decode([]byte(`{"hook_event_name":"Stop","cwd":"/p","session_id":"s"}`)); e == nil {
		t.Fatal("missing native turn accepted")
	}
	if _, e := AdapterFor("unknown"); e == nil {
		t.Fatal("unknown adapter accepted")
	}
	for _, name := range []string{"qoder", "qoder-ide", "codex", "zcode"} {
		a, _ := AdapterFor(name)
		response, e := a.Encode(Event{Name: "PreToolUse"}, map[string]any{"decision": "block", "reason": "先检查当前项目"})
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasPrefix(name, "qoder") {
			if response.ExitCode != 2 || len(response.Stdout) != 0 || response.Stderr != "先检查当前项目" {
				t.Fatal(response)
			}
		} else {
			if response.ExitCode != 0 || !json.Valid(response.Stdout) {
				t.Fatal(response)
			}
		}
	}
}

func TestTOMLEditPreservesUnownedText(t *testing.T) {
	original := []byte("# user comment\nmodel = 'model-x'\nnote = '''\n[mcp_servers.decision-memory]\nnot really a table\n'''\n[mcp_servers.other]\ncommand='node'\nargs=[]\n# before owned table\n[mcp_servers.decision-memory]\ncommand='old'\n[mcp_servers.decision-memory.env]\nTOKEN='retained'\n[features]\nfoo=true\n")
	entry := map[string]any{"command": "new", "env": map[string]any{"TOKEN": "retained"}, "args": []string{"serve"}}
	out, e := replaceTOMLServer(original, "decision-memory", entry)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(out, []byte("# user comment\nmodel = 'model-x'\nnote = '''\n[mcp_servers.decision-memory]\nnot really a table\n'''")) || !bytes.Contains(out, []byte("command='node'\nargs=[]")) {
		t.Fatal(string(out))
	}
	twice, e := replaceTOMLServer(out, "decision-memory", entry)
	if e != nil || !bytes.Equal(out, twice) {
		t.Fatal("non-idempotent", string(twice), e)
	}
	// Dotted/inline definitions are rejected safely rather than rewritten broadly.
	if _, e = replaceTOMLServer([]byte("mcp_servers={\"decision-memory\"={command='old'}}\n"), "decision-memory", entry); e == nil {
		t.Fatal("unsupported inline layout accepted")
	}
}

func TestAdapterSetupPreservationAndIdempotency(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	for _, host := range []string{"codex", "qoder", "qoder-ide"} {
		t.Run(host, func(t *testing.T) {
			root := t.TempDir()
			project := filepath.Join(root, "project")
			os.Mkdir(project, 0700)
			a, _ := AdapterFor(host)
			config := a.DefaultConfig(root)
			os.MkdirAll(filepath.Dir(config), 0700)
			initial := []byte(`{"theme":"keep","mcpServers":{"other":{"command":"node","args":[]}},"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo keep"}]}]}}`)
			if host == "codex" {
				initial = []byte("# keep this comment\nmodel='keep'\n[mcp_servers.other]\ncommand='node'\nargs=[]\n")
				os.WriteFile(a.DefaultHooks(config), []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo keep"}]}]}}`), 0600)
			}
			os.WriteFile(config, initial, 0600)
			o := SetupOptions{Host: host, Config: config, DataDir: filepath.Join(root, "data with spaces"), Project: project, Scope: "project:demo", Binary: filepath.Join(root, "bin with ' quote", "memory-mcp.exe"), Capture: true, Review: true}
			report, e := Setup(testCtx, o)
			if e != nil {
				t.Fatal(e)
			}
			if report["host_event_verified"] != false || report["binding_verified"] != true {
				t.Fatal(report)
			}
			first, _ := os.ReadFile(config)
			hooksFirst, _ := os.ReadFile(a.DefaultHooks(config))
			if _, e = Setup(testCtx, o); e != nil {
				t.Fatal(e)
			}
			second, _ := os.ReadFile(config)
			hooksSecond, _ := os.ReadFile(a.DefaultHooks(config))
			if !bytes.Equal(first, second) || !bytes.Equal(hooksFirst, hooksSecond) {
				t.Fatal("setup duplicated or changed config")
			}
			c, _, _, e := readAdapterConfig(a, config)
			if e != nil {
				t.Fatal(e)
			}
			servers := c["mcp"].(map[string]any)["servers"].(map[string]any)
			if servers["other"] == nil {
				t.Fatal("other server lost")
			}
			if !bytes.Contains(hooksSecond, []byte("echo keep")) {
				t.Fatal("other hook lost")
			}
			if host == "codex" {
				var parsed map[string]any
				if e = toml.Unmarshal(second, &parsed); e != nil || parsed["model"] != "keep" || !bytes.Contains(second, []byte("# keep this comment")) {
					t.Fatal(string(second), e)
				}
			}
			files, _ := os.ReadDir(project)
			if len(files) != 0 {
				t.Fatal("project modified")
			}
			wrong := o
			wrong.DataDir = filepath.Join(root, "wrong")
			if _, e = Setup(testCtx, wrong); e == nil {
				t.Fatal("data redirection allowed")
			}
			if host == "qoder" {
				other := o
				other.Host = "qoder-ide"
				if _, e = Setup(testCtx, other); e == nil {
					t.Fatal("two Qoder capture profiles accepted")
				}
			}
		})
	}
}

func TestConfigApplyRollsBackAndDetectsRace(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one")
	two := filepath.Join(root, "two")
	os.WriteFile(one, []byte("old"), 0600)
	os.WriteFile(two, []byte("concurrent change"), 0600)
	_, err := applyConfigPlans([]configPlan{{one, []byte("old"), []byte("new")}, {two, []byte("expected"), []byte("new")}})
	if err == nil {
		t.Fatal("race accepted")
	}
	actual, _ := os.ReadFile(one)
	if string(actual) != "old" {
		t.Fatal("partial config left behind")
	}
}
