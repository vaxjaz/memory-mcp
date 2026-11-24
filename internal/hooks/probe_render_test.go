package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// The file a project edits: every rule that fits stays on one line of its own,
// and what is read back equals what was written.
func TestRenderedProbeFileListsOneRulePerLine(t *testing.T) {
	project := json.RawMessage(`{"project:team/repo":[{"tool":"Bash","fields":{"text":"command"}},{"mcp_tool":"ask","fields":{"text":"questions.*.question"}},{"tool":"a<b>&c","fields":{"text":"pattern"}}]}`)
	rendered := renderProbeConfig(defaultToolRules(), map[string]json.RawMessage{"projects": project})
	var back struct {
		Tools    []probeRule            `json:"tools"`
		Projects map[string][]probeRule `json:"projects"`
	}
	if err := json.Unmarshal(rendered, &back); err != nil {
		t.Fatal(err)
	}
	defaults := defaultToolRules()
	if len(back.Tools) != len(defaults) {
		t.Fatalf("tools read back: %d of %d", len(back.Tools), len(defaults))
	}
	own := back.Projects["project:team/repo"]
	if len(own) != 3 || own[0].Fields["text"].Paths[0] != "command" || own[1].MCPTool != "ask" || own[1].Fields["text"].Paths[0] != "questions.*.question" || own[2].Tool != "a<b>&c" {
		t.Fatalf("project rules read back: %+v", own)
	}
	if bytes.Contains(rendered, []byte("\r")) || !bytes.HasSuffix(rendered, []byte("\n")) {
		t.Fatal("the file must use LF line endings and end with a newline")
	}
	if !bytes.Contains(rendered, []byte(`"a<b>&c"`)) {
		t.Fatal("a name was escaped on the way out")
	}
	lines := strings.Split(strings.TrimSuffix(string(rendered), "\n"), "\n")
	onLine := 0
	for i, rule := range defaults {
		parsed, err := parseJSON(compactJSON(rule))
		if err != nil {
			t.Fatal(err)
		}
		line := parsed.line()
		if 4+len(line) > layoutWidth {
			continue // only a rule longer than a line is broken at its braces
		}
		want := "    " + line
		if i < len(defaults)-1 {
			want += ","
		}
		if !contains(lines, want) {
			t.Errorf("rule %d is not on one line of its own: %s", i, line)
		}
		onLine++
	}
	if onLine < 35 {
		t.Errorf("only %d of %d rules fit on one line", onLine, len(defaults))
	}
	if len(lines) > 100 {
		t.Errorf("the file is %d lines", len(lines))
	}
	for _, line := range lines {
		// A line may exceed the width only when it holds one indivisible value.
		if len(line) > layoutWidth && strings.Count(line, "\"") > 2 {
			t.Errorf("line of %d characters wraps nothing: %s", len(line), line)
		}
	}
}

func TestLayoutBreaksALongListOfScalarsAndKeepsShortValuesInline(t *testing.T) {
	names := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		names = append(names, "command_"+strings.Repeat("x", i%5))
	}
	value, err := parseJSON(compactJSON(map[string]any{"scan": names, "fields": map[string]string{"text": "command"}, "enabled": false}))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	out.WriteString("{\n  \"rule\": ")
	value.write(&out, "  ", 9)
	out.WriteString("\n}\n")
	text := out.String()
	back, err := parseJSON([]byte(text))
	if err != nil || len(back.items) != 1 {
		t.Fatalf("not read back: %v\n%s", err, text)
	}
	for i, key := range back.items[0].keys {
		switch key {
		case "scan":
			if len(back.items[0].items[i].items) != 30 {
				t.Fatalf("scan read back with %d names", len(back.items[0].items[i].items))
			}
		case "enabled":
			if string(back.items[0].items[i].raw) != "false" {
				t.Fatalf("enabled read back as %s", back.items[0].items[i].raw)
			}
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if len(line) > layoutWidth {
			t.Errorf("line of %d characters: %s", len(line), line)
		}
	}
	if !strings.Contains(text, "\"enabled\": false,") || !strings.Contains(text, "\"fields\": {\"text\": \"command\"},") {
		t.Errorf("a short value was broken: %s", text)
	}
}
