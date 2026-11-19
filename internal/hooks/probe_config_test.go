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

	"github.com/vaxjaz/memory-mcp/internal/memory"
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

func TestWriteProbeExplainsPathAndTextRejections(t *testing.T) {
	store, r, project := trialFixture(t)
	const scope = "project:demo"
	const content = "private fixture text"
	saveProbeFixture(t, store.Dir, probeConfig{Projects: map[string][]probeRule{
		scope: {{Tool: "Write", Mode: "replace", PathField: "file_path", TextFields: []string{"content"}, Extensions: []string{".sql"}}},
	}})
	for _, tc := range []struct {
		name, path, outcome, fieldStatus string
		content                          any
		bytes                            int
	}{
		{"outside", filepath.Join(t.TempDir(), "out.sql"), "path_outside_project", "present", content, len(content)},
		{"suffix", "out.txt", "extension_mismatch", "present", content, len(content)},
		{"large", "out.sql", "text_budget_exceeded", "present", strings.Repeat("x", customProbeTextLimit+1), customProbeTextLimit + 1},
		{"empty", "out.sql", "text_fields_unavailable", "empty", "", 0},
		{"missing", "out.sql", "text_fields_unavailable", "missing", nil, 0},
		{"wrong-type", "out.sql", "text_fields_unavailable", "not_string", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]any{"file_path": tc.path}
			if tc.content != nil {
				input["content"] = tc.content
			}
			handle(t, r, Event{Name: "PreToolUse", CWD: project, Session: tc.name, Tool: "Write", Input: input})
			diagnostics, err := store.HookDiagnostics(testCtx, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			events := diagnostics["recent_events"].([]map[string]string)
			for _, event := range events {
				if event["result"] != "custom_probe_input" {
					continue
				}
				var report probeInputReport
				if err := json.Unmarshal([]byte(event["detail"]), &report); err != nil {
					t.Fatal(err)
				}
				wantField := probeInputField{Field: "content", Status: tc.fieldStatus, Bytes: tc.bytes}
				if report.Outcome != tc.outcome || len(report.TextFields) != 1 || report.TextFields[0] != wantField {
					t.Fatalf("unexpected diagnostic: %+v", report)
				}
				if strings.Contains(event["detail"], content) || strings.Contains(event["detail"], tc.path) {
					t.Fatal("diagnostic included a raw input value")
				}
				return
			}
			t.Fatalf("missing diagnostic: %v", events)
		})
	}

	const cue = "LYN 创建准入"
	for _, path := range []string{"combined.sql", "combined.SQL"} {
		result := handle(t, r, Event{Name: "PreToolUse", CWD: project, Session: path, Tool: "Write", Input: map[string]any{
			"file_path": path, "content": cue,
		}})
		if result["decision"] != "block" {
			t.Fatalf("combined path and content did not match: %v", result)
		}
	}

	saveProbeFixture(t, store.Dir, probeConfig{Projects: map[string][]probeRule{
		scope: {{Tool: "Write", Mode: "replace", TextFields: []string{"content"},
			When: &probeCondition{Field: "input.file_path", Op: "suffix", Value: ".sql"}}},
	}})
	otherRepo := t.TempDir()
	for path, block := range map[string]bool{"report.sql": true, "report.txt": false} {
		result := handle(t, r, Event{Name: "PreToolUse", CWD: project, Session: path, Tool: "Write", Input: map[string]any{
			"file_path": filepath.Join(otherRepo, path), "content": cue,
		}})
		if (result["decision"] == "block") != block {
			t.Fatalf("unexpected cross-repository suffix result: %v", result)
		}
	}
}

func TestCustomProbeDispatchAndScope(t *testing.T) {
	store, r, project := trialFixture(t)
	const scope = "project:demo"
	rules := []probeRule{
		{Tool: "Write", PathField: "file_path", TextFields: []string{"content"}, Extensions: []string{".sql"}},
		{Tool: "Edit", PathField: "file_path", TextFields: []string{"new_string"}, Extensions: []string{".sql"}},
		{MCPTool: "company_select", TextFields: []string{"arguments.sql"}},
		{Tool: "Grep", TextFields: []string{"regex2"}},
		{Tool: "mcp__fixture__breadth", TextFields: []string{"pattern"}},
		{Tool: "mcp__special__company_select", Mode: "replace", TextFields: []string{"statement"}},
	}
	config := probeConfig{Projects: map[string][]probeRule{scope: rules}}
	saveProbeFixture(t, store.Dir, config)
	policy, err := loadProbeConfig(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	matcher := regexp.MustCompile(policy.matcher("decision-memory"))
	for _, tool := range []string{"Write", "Edit", "Grep", "mcp__one__company_select", "mcp__two-name__company_select"} {
		if !matcher.MatchString(tool) {
			t.Fatal("custom native name was not dispatched", tool)
		}
	}
	for _, tool := range []string{"TodoWrite", "TaskOutput", "company_select", "mcp__one__company_select_other", "EditOther"} {
		if matcher.MatchString(tool) {
			t.Fatal("unrelated tool dispatched", tool)
		}
	}
	const clue = "LYN 创建准入"
	for _, tc := range []struct {
		label string
		tool  string
		input map[string]any
		block bool
	}{
		{"write-new", "Write", map[string]any{"file_path": filepath.Join("new", "report.SQL"), "content": clue}, true},
		{"non-sql", "Write", map[string]any{"file_path": "report.txt", "content": clue}, false},
		{"relative-outside", "Write", map[string]any{"file_path": filepath.Join("..", "report.sql"), "content": clue}, false},
		{"absolute-outside", "Write", map[string]any{"file_path": filepath.Join(t.TempDir(), "report.sql"), "content": clue}, false},
		{"edit-old", "Edit", map[string]any{"file_path": "report.sql", "old_string": clue, "new_string": "safe"}, false},
		{"edit-new", "Edit", map[string]any{"file_path": "report.sql", "new_string": clue}, true},
		{"nested-mcp", "mcp__two-name__company_select", map[string]any{"arguments": map[string]any{"sql": clue}}, true},
		{"specific-mcp-rule", "mcp__special__company_select", map[string]any{"statement": clue}, true},
		{"specific-rule-replaces-suffix", "mcp__special__company_select", map[string]any{"arguments": map[string]any{"sql": clue}}, false},
		{"misplaced-mcp-field", "mcp__two-name__company_select", map[string]any{"sql": clue}, false},
		{"large-field", "mcp__two-name__company_select", map[string]any{"arguments": map[string]any{"sql": clue + strings.Repeat("x", 32769)}}, false},
		{"append-default", "Grep", map[string]any{"pattern": clue}, true},
		{"append-extra", "Grep", map[string]any{"regex2": clue}, true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			out := handle(t, r, Event{Name: "PreToolUse", CWD: project, Session: tc.label, Tool: tc.tool, Input: tc.input})
			if (out["decision"] == "block") != tc.block {
				t.Fatal(out)
			}
		})
	}

	rules[3].Mode = "replace"
	saveProbeFixture(t, store.Dir, config)
	for _, field := range []string{"pattern", "regex2"} {
		out := handle(t, r, Event{Name: "PreToolUse", CWD: project, Session: "replace-" + field, Tool: "Grep", Input: map[string]any{field: clue}})
		if (out["decision"] == "block") != (field == "regex2") {
			t.Fatalf("replacement still uses a default field or lost its custom field: %v", out)
		}
	}

	other := t.TempDir()
	if err := store.HookBind(testCtx, other, "project:other"); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfigureExperienceTrial(testCtx, "project:other", true); err != nil {
		t.Fatal(err)
	}
	_, err = store.RecordExperience(testCtx, memory.ExperienceWrite{
		Scope: "project:other", Project: other, Title: "Other scope test",
		Experience: memory.Experience{
			Cues: []memory.ExperienceCue{{ID: "admission", Label: "准入覆盖", Text: "Fixture counterexample", EvidenceRefs: []string{"other-scope"}, Entrances: []memory.CueEntrance{{Kind: "business_signal", Value: "LYN 创建准入"}}}}, Pitfall: "Fixture inference", Counterexample: "Fixture counterexample",
			Impact: "Fixture risk", Applicability: "Synthetic scope regression", Boundary: "Fixture only",
			Evidence: []memory.ExperienceEvidence{{Kind: "runtime", Reference: "other-scope", Observation: "Isolated fixture"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"pattern": clue}
	if out := handle(t, r, Event{Name: "PreToolUse", CWD: other, Session: "other-probe", Tool: "mcp__fixture__breadth", Input: input}); len(out) != 0 {
		t.Fatal("host matcher union leaked custom or substring fallback to another project", out)
	}
	if out := handle(t, r, Event{Name: "PreToolUse", CWD: other, Session: "other-grep", Tool: "Grep", Input: input}); out["decision"] != "block" {
		t.Fatal("default probe was lost in another project", out)
	}
}

func TestCustomProbeConfigValidationAndPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, probeConfigName)
	for _, content := range []string{
		`{"projects":{"project:demo":[{"tool":"Write","mcp_tool":"write","path_field":"file_path"}]}}`,
		`{"projects":{"project:demo":[{"tool":"Write","text_fields":["content"],"extensions":[".sql"]}]}}`,
		`{"projects":{"project:demo":[{"tool":"Write|Edit","path_field":"file_path"}]}}`,
		`{"projects":{"project:demo":[{"mcp_tool":"memory_experience_lookup","text_fields":["task"]}]}}`,
		`{"projects":{"project:demo":[{"tool":"Write","path_feld":"file_path"}]}}`,
		`{"projects":{}} {}`,
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
	if got, reason := probePath(canonical, root, filepath.Join("new dir", "report.sql")); got != "new dir/report.sql" || reason != "" {
		t.Fatal(got)
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
		if got, reason := probePath(canonical, root, filepath.Join("external", "missing.sql")); got != "" || reason == "" {
			t.Fatal("redirect outside project contributed clues", got)
		}
	})
}
