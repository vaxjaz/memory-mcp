package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func saveProbeFixture(t *testing.T, dir string, config probeConfig) {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, probeConfigName), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestToolRegistrationDiagnostics(t *testing.T) {
	rule := toolTemplates["Write"]
	root := t.TempDir()
	const private = "private fixture text"
	for _, tc := range []struct {
		name    string
		content any
		status  string
	}{
		{"string", private, "present"},
		{"empty", "", "empty"},
		{"missing", nil, "missing"},
		// An array of strings is mapped element by element.
		{"array", []any{private}, "present"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := Event{Tool: "Write", CWD: root, Input: map[string]any{"file_path": "private.sql"}}
			if tc.content != nil {
				event.Input["content"] = tc.content
			}
			input, report := rule.extract(event, root)
			fields := report.Fields
			if len(fields) != 2 || fields[1].Field != "text" || fields[1].InputPath != "content" || fields[1].Status != tc.status ||
				input.Fields["path"] != "private.sql" {
				t.Fatal(report)
			}
			raw, _ := json.Marshal(describeToolFields(event.Input))
			if strings.Contains(string(raw), private) || strings.Contains(string(raw), "private.sql") {
				t.Fatal(string(raw))
			}
		})
	}
	_, report := rule.extract(Event{Tool: "Write", Input: map[string]any{"content": strings.Repeat("x", customProbeTextLimit+1)}}, root)
	// An oversized value is left out whole and named; it is never trimmed.
	if len(report.Fields) != 2 || report.Fields[1].Status != "field_too_large" || report.Outcome != "no_eligible_text" {
		t.Fatal(report)
	}
}

func TestRegisteredFieldsHaveOneEffectiveSource(t *testing.T) {
	store, r, project := trialFixture(t)
	const scope = "project:demo"
	const clue = "LYN 创建准入"
	disabled := false
	rules := []probeRule{
		toolTemplates["Write"],
		toolTemplates["Edit"],
		{MCPTool: "company_select", Fields: mapped("text", "arguments.sql")},
		{Tool: "Grep", Fields: mapped("text", "regex2")},
		{Tool: "mcp__special__company_select", Fields: mapped("text", "statement")},
		{Tool: "Read", Enabled: &disabled},
	}
	saveProbeFixture(t, store.Dir, probeConfig{Projects: map[string][]probeRule{scope: rules}})
	policy, err := loadProbeConfig(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	matcher := regexp.MustCompile(policy.matcher("decision-memory"))
	for _, tool := range []string{"Write", "Edit", "Grep", "mcp__a__company_select", "mcp__special__company_select", "Bash", "mcp__foo__mysql_query"} {
		if !matcher.MatchString(tool) {
			t.Fatal(tool)
		}
	}
	for _, tool := range []string{"TodoWrite", "TaskOutput", "company_select", "mcp__one__company_select_other"} {
		if matcher.MatchString(tool) {
			t.Fatal(tool)
		}
	}
	for _, tc := range []struct {
		name, tool string
		input      map[string]any
		block      bool
	}{
		{"write", "Write", map[string]any{"file_path": "reports/out.sql", "content": clue}, true},
		{"edit-old", "Edit", map[string]any{"old_string": clue, "new_string": "other"}, false},
		{"mcp-nested", "mcp__a__company_select", map[string]any{"arguments": map[string]any{"sql": clue}}, true},
		{"mcp-exact", "mcp__special__company_select", map[string]any{"statement": clue}, true},
		{"mcp-exact-shadow", "mcp__special__company_select", map[string]any{"arguments": map[string]any{"sql": clue}}, false},
		{"grep-preset-shadow", "Grep", map[string]any{"pattern": clue}, false},
		{"grep-configured", "Grep", map[string]any{"regex2": clue}, true},
		{"bash-rg", "Bash", map[string]any{"command": "rg '" + clue + "'"}, true},
		{"read-disabled", "Read", map[string]any{"file_path": clue}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := handle(t, r, Event{Name: "PreToolUse", CWD: project, Session: tc.name, Tool: tc.tool, Input: tc.input})
			if (out["decision"] == "block") != tc.block {
				t.Fatal(out)
			}
		})
	}
	if grep, ok := policy.ruleFor("project:other", "Grep"); !ok || grep.Fields["text"].Paths[0] != "pattern" {
		t.Fatal(grep)
	}
	// Another project sees the listed-but-disabled Write rule, never this project's registration.
	if write, ok := policy.ruleFor("project:other", "Write"); !ok || write.active() || write.Fields["text"].Paths[0] != "content" {
		t.Fatal(write)
	}
}

func TestToolInspectionCanDiscoverNativeFields(t *testing.T) {
	store, r, project := trialFixture(t)
	const scope = "project:demo"
	const tool = "mcp__one__custom_write"
	saveProbeFixture(t, store.Dir, probeConfig{Projects: map[string][]probeRule{scope: {{MCPTool: "custom_write"}}}})
	config, err := loadProbeConfig(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, matcher := range []string{config.matcher("decision-memory"), config.postMatcher("decision-memory")} {
		if !regexp.MustCompile(matcher).MatchString(tool) {
			t.Fatal(matcher)
		}
	}
	e := Event{Name: "PreToolUse", CWD: project, Session: "discover", Tool: tool, Input: map[string]any{
		"attachment": map[string]any{"body": "LYN 创建准入"},
	}}
	if out := handle(t, r, e); len(out) != 0 {
		t.Fatal(out)
	}
	e.Name = "PostToolUse"
	e.Response = map[string]any{"structuredContent": map[string]any{"ok": true, "status": "PRIVATE_RESULT"}}
	if out := handle(t, r, e); len(out) != 0 {
		t.Fatal(out)
	}
	report, err := store.HookDiagnostics(testCtx, "discover")
	if err != nil {
		t.Fatal(err)
	}
	rows := report["recent_events"].([]map[string]string)
	if len(rows) != 2 || rows[0]["result"] != "tool_field_shape" || rows[1]["result"] != "tool_field_shape" ||
		!strings.Contains(rows[0]["detail"], `"path":"status","type":"string"`) ||
		!strings.Contains(rows[1]["detail"], `"path":"attachment.body","type":"string"`) ||
		strings.Contains(rows[0]["detail"], "PRIVATE_RESULT") || strings.Contains(rows[1]["detail"], "LYN") {
		t.Fatal(rows)
	}
}

func TestCustomProbeConfigValidationAndPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, probeConfigName)
	for _, content := range []string{
		`{"projects":{"project:demo":[{"tool":"Write","mcp_tool":"write","path_field":"file_path"}]}}`,
		`{"projects":{"project:demo":[{"tool":"Write","text_fields":["content"],"extensions":[".sql"]}]}}`,
		`{"projects":{"project:demo":[{"tool":"Write|Edit","fields":{"text":"content"}}]}}`,
		`{"projects":{"project:demo":[{"mcp_tool":"memory_experience_lookup","fields":{"text":"task"}}]}}`,
		`{"projects":{"project:demo":[{"tool":"Write","path_feld":"file_path"}]}}`,
		`{"projects":{"project:demo":[{"tool":"Write","when":{"field":"tool","op":"eq","value":"Write"}}]}}`,
		`{"projects":{}} {}`,
		`{"tools":[{"tool":"ask","observe":[{"name":"ok","key":["input.connection_id"],"fields":{"ok":"ok"}}]}],"projects":{}}`,
		`{"tools":[{"tool":"Bash","scan":["grep"],"fields":{"path":"file_path"}}],"projects":{}}`,
		`{"tools":[{"tool":"ask","fields":{"text":"questions.0.question"},"discover":"query"}}],"projects":{}}`,
		`{"tools":[{"tool":"ask","fields":{"text":"questions..question"}}],"projects":{}}`,
		`{"tools":[{"tool":"ask","fields":{"text":"questions[].question"}}],"projects":{}}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadProbeConfig(root); err == nil {
			t.Fatal("accepted invalid config", content)
		}
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	// A target that does not exist yet is judged by its nearest existing parent.
	if reason := pathVerdict(canonical, root, filepath.Join("new dir", "report.sql")); reason != "" {
		t.Fatal(reason)
	}
	t.Run("external redirect", func(t *testing.T) {
		outside := t.TempDir()
		link := filepath.Join(root, "external")
		if runtime.GOOS == "windows" {
			cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
				"New-Item -ItemType Junction -Path $env:MEMORY_MCP_TEST_LINK -Target $env:MEMORY_MCP_TEST_TARGET -ErrorAction Stop | Out-Null")
			cmd.Env = append(os.Environ(), "MEMORY_MCP_TEST_LINK="+link, "MEMORY_MCP_TEST_TARGET="+outside)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("junction not available: %v %s", err, output)
			}
		} else if err := os.Symlink(outside, link); err != nil {
			t.Skipf("symlink not available: %v", err)
		}
		t.Cleanup(func() {
			if err := os.Remove(link); err != nil {
				t.Error(err)
			}
		})
		if reason := pathVerdict(canonical, root, filepath.Join("external", "missing.sql")); reason != "path_outside_project" {
			t.Fatal("redirect outside project contributed clues", reason)
		}
	})
}
