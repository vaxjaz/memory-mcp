package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// mapped builds field mappings from alias and path pairs.
func mapped(pairs ...string) map[string]fieldSource {
	fields := map[string]fieldSource{}
	for i := 0; i+1 < len(pairs); i += 2 {
		fields[pairs[i]] = fieldSource{[]string{pairs[i+1]}}
	}
	return fields
}

func statusOf(report probeInputReport, alias string) string {
	for _, field := range report.Fields {
		if field.Field == alias {
			return field.Status
		}
	}
	return "absent"
}

func TestToolRulesSeedCreatesTopsUpAndKeepsEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, probeConfigName)

	seed, err := seedToolRules(dir, true)
	if err != nil || !seed.Created || len(seed.Added) == 0 {
		t.Fatal(seed, err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry run wrote the file")
	}
	if _, err = seedToolRules(dir, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	config, err := loadProbeConfig(dir)
	if err != nil || config.toolsImplicit || len(config.Tools) != len(seed.Added) || len(config.Projects) != 0 {
		t.Fatal(len(config.Tools), len(seed.Added), err)
	}
	if bytes.Contains(raw, []byte("\r")) || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("the file must use LF line endings and end with a newline")
	}
	for _, name := range []string{"Bash", "Read", "Write", "Edit", "mcp:mysql_query"} {
		if !contains(seed.Added, name) {
			t.Fatal("default tool missing", name)
		}
	}

	before, _ := os.ReadFile(path)
	seed, err = seedToolRules(dir, false)
	after, _ := os.ReadFile(path)
	if err != nil || seed.Created || len(seed.Added) != 0 || seed.Backup != "" || !bytes.Equal(before, after) {
		t.Fatal("a complete file must be left alone", seed, err)
	}

	// An edited default is kept as written; a removed one is listed again.
	var document map[string]json.RawMessage
	json.Unmarshal(before, &document)
	var tools []map[string]any
	json.Unmarshal(document["tools"], &tools)
	kept := []map[string]any{}
	for _, tool := range tools {
		if tool["tool"] == "Read" {
			continue
		}
		if tool["tool"] == "Bash" {
			tool["scan"] = []string{"git"}
		}
		kept = append(kept, tool)
	}
	document["tools"], _ = json.Marshal(kept)
	edited, _ := json.Marshal(document)
	os.WriteFile(path, edited, 0600)
	seed, err = seedToolRules(dir, false)
	config, err = loadProbeConfig(dir)
	if err != nil || len(seed.Added) != 1 || seed.Added[0] != "Read" || seed.Backup == "" {
		t.Fatal(seed, err)
	}
	bash, _ := config.ruleFor("project:x", "Bash")
	if len(bash.Scan) != 1 || bash.Scan[0] != "git" {
		t.Fatal("the project's edit was overwritten", bash.Scan)
	}
	if backup, err := os.ReadFile(seed.Backup); err != nil || !bytes.Equal(backup, edited) {
		t.Fatal("the previous file was not backed up", err)
	}

	// A file from an earlier version has only projects.
	legacy := t.TempDir()
	old := `{"projects":{"project:x/y":[{"tool":"Write","fields":{"text":"content","path":"file_path"}}]}}`
	os.WriteFile(filepath.Join(legacy, probeConfigName), []byte(old), 0600)
	seed, err = seedToolRules(legacy, false)
	config, err = loadProbeConfig(legacy)
	if err != nil || seed.Created || len(seed.Added) != len(defaultToolRules()) || seed.Backup == "" || config.toolsImplicit {
		t.Fatal(len(seed.Added), seed.Created, err)
	}
	write, ok := config.ruleFor("project:x/y", "Write")
	if !ok || !write.active() || write.Fields["text"].Paths[0] != "content" {
		t.Fatal("the project rule was not preserved", write)
	}
	if other, ok := config.ruleFor("project:x/z", "Write"); !ok || other.active() {
		t.Fatal("the project rule escaped into another project", other)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// An untouched file keeps the shipped defaults; once "tools" is listed the
// file alone decides.
func TestListedToolsReplaceTheImplicitDefaults(t *testing.T) {
	untouched := t.TempDir()
	config, err := loadProbeConfig(untouched)
	if _, ok := config.ruleFor("project:x", "Read"); err != nil || !ok || !config.toolsImplicit {
		t.Fatal(ok, config.toolsImplicit, err)
	}
	listed := t.TempDir()
	os.WriteFile(filepath.Join(listed, probeConfigName), []byte(`{"tools":[],"projects":{}}`), 0600)
	config, err = loadProbeConfig(listed)
	if _, ok := config.ruleFor("project:x", "Read"); err != nil || ok || config.toolsImplicit {
		t.Fatal("a listed empty tools section must scan no default tool", ok, err)
	}
}

func TestFieldsAreJudgedIndependentlyAgainstTheBindingRoot(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "mod-a")
	sibling := filepath.Join(root, "mod-b")
	outside := t.TempDir()
	for _, dir := range []string{session, sibling, filepath.Join(root, "docs")} {
		os.MkdirAll(dir, 0700)
	}
	rule := probeRule{Tool: "Grep", Fields: mapped("text", "pattern", "path", "path")}

	// The session sits in one module; a sibling module is still the project.
	input, report := rule.extract(Event{Tool: "Grep", CWD: session, Input: map[string]any{"pattern": "findMe", "path": sibling}}, root)
	if report.Outcome != "scanned" || len(input.Texts) != 2 || statusOf(report, "path") != "present" {
		t.Fatal(report, input.Texts)
	}
	// One ineligible field never removes another.
	input, report = rule.extract(Event{Tool: "Grep", CWD: root, Input: map[string]any{"pattern": "findMe", "path": outside}}, root)
	if report.Outcome != "scanned" || len(input.Texts) != 1 || input.Texts[0].Field != "text" || statusOf(report, "path") != "path_outside_project" {
		t.Fatal(report, input.Texts)
	}
	// The same text under a name other than path is ordinary content.
	content := probeRule{Tool: "Note", Fields: mapped("note", "path")}
	if _, report = content.extract(Event{Tool: "Note", CWD: root, Input: map[string]any{"path": outside}}, root); statusOf(report, "note") != "present" {
		t.Fatal(report)
	}

	for _, tc := range []struct{ name, session, path, want string }{
		{"project root as the search scope", session, root, ""},
		{"a target that does not exist yet", session, filepath.Join(sibling, "later.go"), ""},
		{"outside the project", session, filepath.Join(outside, "x.go"), "path_outside_project"},
		{"a relative path that leaves the project", session, filepath.Join("..", "..", "elsewhere", "x.go"), "path_outside_project"},
		{"an unexpanded home directory", session, "~/notes/x.md", "path_unresolved"},
		{"an unexpanded environment variable", session, "$HOME/notes/x.md", "path_unresolved"},
		{"a Windows environment reference", session, `%USERPROFILE%\notes\x.md`, "path_unresolved"},
		{"an instruction file", session, filepath.Join("docs", "AGENTS.md"), "instruction_file"},
		{"an instruction directory written relative", session, filepath.Join(".codex", "hooks", "x.json"), "instruction_file"},
		{"another host's rules directory", session, filepath.Join(".qoder", "rules", "x.md"), "instruction_file"},
		{"a merely similar file name", session, filepath.Join("docs", "AGENTS-outline.md"), ""},
	} {
		if got := textStatus("path", tc.path, root, tc.session); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if input.Fields["mcp_tool"] != "" {
		t.Fatal("a native tool has no bare MCP name")
	}
}

func TestArrayInputsAreMappedElementByElement(t *testing.T) {
	root := t.TempDir()
	many := make([]any, probeElementLimit+6)
	for i := range many {
		many[i] = "tag"
	}
	e := Event{Tool: "mcp__asker__ask", CWD: root, Input: map[string]any{
		"questions": []any{map[string]any{"question": "first", "header": "h"}, map[string]any{"question": "second"}},
		"tags":      []any{"a", "b"},
		"numbers":   []any{float64(1), float64(2)},
		"mixed":     []any{"x", map[string]any{"k": "v"}},
		"empty":     []any{},
		"many":      many,
	}}
	extract := func(fields map[string]fieldSource) (*matching.Input, probeInputReport) {
		rule := probeRule{Tool: "mcp__asker__ask", Fields: fields}
		input, report := rule.extract(e, root)
		return input, report
	}
	for _, tc := range []struct {
		name   string
		fields map[string]fieldSource
		want   []string
	}{
		{"every element's field", mapped("text", "questions.*.question"), []string{"first", "second"}},
		{"one element by index", mapped("text", "questions.1.question"), []string{"second"}},
		{"an array of strings", mapped("text", "tags"), []string{"a", "b"}},
		{"the first candidate that is present", map[string]fieldSource{"text": {[]string{"absent", "tags"}}}, []string{"a", "b"}},
		{"the first element of an array", mapped("text", "tags.0"), []string{"a"}},
	} {
		input, report := extract(tc.fields)
		var got []string
		for _, text := range input.Texts {
			got = append(got, text.Value)
		}
		if report.Outcome != "scanned" || strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("%s: %v %v", tc.name, got, report)
		}
	}
	if input, report := extract(mapped("text", "questions.5.question")); statusOf(report, "text") != "missing" || len(input.Texts) != 0 {
		t.Fatal(report)
	}
	if input, report := extract(mapped("text", "empty")); statusOf(report, "text") != "empty" || len(input.Texts) != 0 {
		t.Fatal(report)
	}
	// A list of numbers is not one value a cue could compare.
	if input, report := extract(mapped("count", "numbers")); len(input.Texts) != 0 || statusOf(report, "count") != "unsupported_type" {
		t.Fatal(report, input.Fields)
	}
	// A typed value is exposed beside the text a cue's entrances are looked for in.
	if input, report := extract(mapped("flag", "numbers.1", "text", "tags")); input.Fields["flag"] != float64(2) || statusOf(report, "flag") != "present" || len(input.Texts) != 2 {
		t.Fatal(report, input.Fields)
	}
	if input, report := extract(mapped("text", "mixed")); len(input.Texts) != 1 || !hasStatus(report, "unsupported_type") {
		t.Fatal(report)
	}
	input, report := extract(mapped("text", "many"))
	if len(input.Texts) != probeElementLimit || !hasStatus(report, "element_limit") {
		t.Fatal(len(input.Texts), report.Fields[len(report.Fields)-1])
	}
	if input.Fields["mcp_tool"] != "ask" || input.Fields["tool"] != "mcp__asker__ask" {
		t.Fatal(input.Fields)
	}
}

func hasStatus(report probeInputReport, status string) bool {
	for _, field := range report.Fields {
		if field.Status == status {
			return true
		}
	}
	return false
}

func TestShellRuleScansListedCommandNames(t *testing.T) {
	root := t.TempDir()
	var shell probeRule
	for _, rule := range defaultToolRules() {
		if rule.Tool == "Bash" {
			shell = rule
		}
	}
	custom := probeRule{Tool: "Bash", Fields: mapped("text", "command"), Scan: []string{"git"}}
	for _, tc := range []struct {
		rule          probeRule
		command, want string
	}{
		{shell, "head -20 notes/entAlpha.txt", "scanned"},
		{shell, "tail -5 notes/entAlpha.txt", "scanned"},
		{shell, "find . -name '*entAlpha*'", "scanned"},
		{shell, "awk '/entAlpha/' notes.txt", "scanned"},
		{shell, "sudo cat notes/entAlpha.txt", "scanned"},
		{shell, "git grep entAlpha", "scanned"},
		{shell, "cd src && ls entAlpha", "scanned"},
		{shell, "git log --grep entAlpha", "operation_not_scanned"},
		{shell, "echo entAlpha", "operation_not_scanned"},
		{shell, "concatenate entAlpha", "operation_not_scanned"},
		{shell, "cat AGENTS.md", "instruction_file"},
		{shell, "cat .codex/config.toml", "instruction_file"},
		{shell, "cat docs/agents.md.bak", "scanned"},
		{custom, "git checkout entAlpha", "scanned"},
		{custom, "cat notes/entAlpha.txt", "operation_not_scanned"},
	} {
		_, report := tc.rule.extract(Event{Tool: "Bash", CWD: root, Input: map[string]any{"command": tc.command}}, root)
		if report.Outcome != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.command, report.Outcome, tc.want)
		}
	}
	if _, report := shell.extract(Event{Tool: "Bash", CWD: root, Input: map[string]any{"command": []any{"bash", "-lc", "cat notes/entAlpha.txt"}}}, root); report.Outcome != "scanned" {
		t.Fatal("a command split into arguments was not scanned", report)
	}
}

func TestSkippedProbeLeavesReasonsWithoutValues(t *testing.T) {
	s, r, p := trialFixture(t)
	e := event("PreToolUse", p, "skipped-session", "")
	e.Tool = "Read"
	e.Input = map[string]any{"file_path": filepath.Join(t.TempDir(), "LYN 创建准入.md")}
	if out := handle(t, r, e); len(out) != 0 {
		t.Fatal("a path outside the project woke a cue", out)
	}
	report, err := s.HookDiagnostics(testCtx, "skipped-session")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range report["recent_events"].([]map[string]string) {
		if row["result"] != "probe_skipped" {
			continue
		}
		found = true
		if strings.Contains(row["detail"], "LYN 创建准入") || !strings.Contains(row["detail"], `"status":"path_outside_project"`) || !strings.Contains(row["detail"], `"outcome":"no_eligible_text"`) {
			t.Fatal("the trace must explain the skip and keep the value out", row["detail"])
		}
	}
	if !found {
		t.Fatal("a skipped call left no trace", report["recent_events"])
	}
}

func TestInspectionShowsArrayElementsWithoutValues(t *testing.T) {
	raw, _ := json.Marshal(describeToolFields(map[string]any{
		"questions": []any{map[string]any{"question": "PRIVATE_Q", "options": []any{map[string]any{"label": "PRIVATE_L"}}}},
		"tags":      []any{"PRIVATE_T"},
		"name":      "PRIVATE_N",
	}))
	for _, path := range []string{`"path":"questions"`, `"path":"questions.*"`, `"path":"questions.*.question"`, `"path":"questions.*.options.*.label"`, `"path":"tags.*"`, `"path":"name"`} {
		if !strings.Contains(string(raw), path) {
			t.Fatal("missing", path, string(raw))
		}
	}
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("a value was reported", string(raw))
	}
}

func TestProbeSampleNeedsItsActualDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "event.json")
	os.WriteFile(file, []byte(`{"tool_name":"Read","tool_input":{"file_path":"AGENTS.md"}}`), 0600)
	if _, err := readProbeEvent(probeCheckOptions{File: file}); err == nil || !strings.Contains(err.Error(), "cwd") {
		t.Fatal("the probe guessed a directory", err)
	}
}

func TestSurfaceListsEffectiveRulesWithTheirSource(t *testing.T) {
	config := probeConfig{Tools: defaultToolRules(), Projects: map[string][]probeRule{"project:x": {{Tool: "Bash", Fields: mapped("text", "command")}}}}
	sources := map[string]string{}
	for _, item := range config.surface("project:x") {
		sources[item["tool"].(string)] = item["source"].(string)
	}
	if sources["Bash"] != "project" || sources["Read"] != "tools" || sources["mcp:*:mysql_query"] != "tools" || sources["Write"] != "tools" {
		t.Fatal(sources)
	}
	config.Tools, config.toolsImplicit = nil, true
	for _, item := range config.surface("project:x") {
		if item["tool"] == "Read" && item["source"] != "default" {
			t.Fatal("an untouched file must say its rules are the defaults", item)
		}
	}
}

// makeLink creates a directory link: a junction on Windows, a symbolic link
// elsewhere. It reports whether the system allowed it.
func makeLink(t *testing.T, link, target string) bool {
	t.Helper()
	t.Cleanup(func() { os.Remove(link) })
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run() == nil
	}
	return os.Symlink(target, link) == nil
}

func TestLinksAreFollowedAndMustStayInsideTheProject(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	inside := filepath.Join(root, "real")
	for _, dir := range []string{filepath.Join(root, "mod"), filepath.Join(inside, "nested"), filepath.Join(outside, "secret")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range [][2]string{
		{filepath.Join(root, "mod", "inside"), inside},
		{filepath.Join(root, "mod", "outside"), outside},
		{filepath.Join(inside, "hop"), filepath.Join(root, "mod")},
		{filepath.Join(inside, "loop"), filepath.Join(root, "mod", "inside")},
	} {
		if !makeLink(t, pair[0], pair[1]) {
			t.Skip("the system does not allow directory links here")
		}
	}
	for _, tc := range []struct{ name, path, want string }{
		{"a link to a directory inside the project", filepath.Join("mod", "inside", "nested", "new.go"), ""},
		{"two links that both stay inside", filepath.Join("mod", "inside", "hop", "inside", "nested", "x.go"), ""},
		{"a link to a directory outside the project", filepath.Join("mod", "outside", "secret", "x.go"), "path_outside_project"},
		{"a link inside that leads through another link outside", filepath.Join("mod", "inside", "hop", "outside", "secret", "x.go"), "path_outside_project"},
		{"a link chain that never ends", filepath.Join("mod", "inside", strings.Repeat("loop"+string(filepath.Separator), 40)+"x.go"), "path_unresolved"},
		{"a plain path beside the links", filepath.Join("mod", "plain.go"), ""},
	} {
		if got := pathVerdict(root, root, filepath.Join(root, filepath.FromSlash(tc.path))); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
