package hooks

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// Presets and project registrations use one selector, route generator and
// input adapter. Preset checks only describe the tool protocol.
var defaultProbeRules = func() []probeRule {
	var rules []probeRule
	for _, name := range []string{"Grep", "grep", "RG", "rg", "Glob", "glob", "Read", "read", "read_file", "grep_code", "search_file", "search_files", "code_search"} {
		rules = append(rules, probeRule{Tool: name, preset: "search"})
	}
	for _, name := range []string{"Bash", "bash", "exec_command", "run_command", "run_in_terminal", "terminal"} {
		rules = append(rules, probeRule{Tool: name, preset: "shell"})
	}
	for _, name := range []string{"read_file", "search_files", "grep", "glob", "code_search"} {
		rules = append(rules, probeRule{MCPTool: name, preset: "search"})
	}
	for _, name := range []string{"get_ticket", "read_ticket", "search_tickets", "get_order", "query_order", "query_orders", "search_orders", "query", "mysql_query", "read_query", "execute_query", "execute_sql", "run_sql", "query_database"} {
		rules = append(rules, probeRule{MCPTool: name, preset: "business"})
	}
	rules = append(rules, probeRule{Tool: "mysql_query", preset: "business"})
	return rules
}()

var toolTemplates = map[string]probeRule{
	"Write": {Tool: "Write", Fields: map[string]string{"text": "content", "path": "file_path"}},
	"Edit":  {Tool: "Edit", Fields: map[string]string{"text": "new_string", "path": "file_path"}},
}

type probeInputField struct {
	Field     string `json:"field"`
	InputPath string `json:"input_path"`
	Status    string `json:"status"`
	Bytes     int    `json:"bytes"`
}

type probeInputReport struct {
	Tool    string            `json:"tool"`
	Source  string            `json:"source"`
	Outcome string            `json:"outcome"`
	Fields  []probeInputField `json:"fields,omitempty"`
}

var searchFields = []string{"pattern", "query", "regex", "search_pattern", "path", "file_path", "filePath", "command", "cmd", "script"}

func (r probeRule) extract(e Event) (*matching.Input, probeInputReport) {
	input := &matching.Input{Fields: map[string]any{"tool": e.Tool}}
	report := probeInputReport{Tool: e.Tool, Source: "project", Outcome: "scanned"}
	if r.preset != "" {
		report.Source = "preset"
	}
	if !r.active() {
		report.Outcome = "tool_disabled"
		return input, report
	}
	if r.inspectionOnly() {
		report.Outcome = "inspection_only"
		return input, report
	}
	total := 0
	used := map[[2]string]bool{}
	put := func(alias, path string, value any) {
		field := probeInputField{Field: alias, InputPath: path, Status: "present"}
		switch v := value.(type) {
		case string:
			field.Bytes = len(v)
			if v == "" {
				field.Status = "empty"
			} else {
				input.Texts = append(input.Texts, matching.NamedText{Field: alias, Value: v})
				key := [2]string{path, v}
				if !used[key] {
					total += len(v)
					used[key] = true
				}
				if _, exists := input.Fields[alias]; !exists {
					input.Fields[alias] = v
				}
			}
		case bool, float64:
			input.Fields[alias] = v
		case nil:
			field.Status = "missing"
		default:
			field.Status = "unsupported_type"
		}
		report.Fields = append(report.Fields, field)
	}
	switch r.preset {
	case "search", "shell":
		for _, key := range searchFields {
			value, ok := e.Input[key].(string)
			if !ok || value == "" {
				continue
			}
			if !eligibleSearchField(e.CWD, key, value) {
				report.Outcome = "input_not_eligible"
				return &matching.Input{}, report
			}
			alias := key
			switch {
			case r.preset == "shell" && (key == "command" || key == "cmd" || key == "script"):
				alias = "text"
			case r.preset == "search" && key == "pattern":
				alias = "text"
			case key == "file_path" || key == "filePath":
				alias = "path"
			}
			if _, exists := input.Fields[alias]; exists {
				alias = key
			}
			put(alias, key, value)
		}
		if r.preset == "shell" {
			command, _ := input.Fields["text"].(string)
			if !isSearchCommand(command) {
				report.Outcome = "input_not_eligible"
				return &matching.Input{}, report
			}
		}
	case "business":
		for _, value := range businessInputFields(e) {
			put(value.Field, value.InputPath, value.Value)
		}
	default:
		names := make([]string, 0, len(r.Fields))
		for name := range r.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, alias := range names {
			path := r.Fields[alias]
			put(alias, path, probeValue(e.Input, path))
		}
	}
	if r.preset != "" && input.Fields["text"] == nil && len(input.Texts) > 0 {
		first := input.Texts[0]
		put("text", first.Field, first.Value)
	}
	if total > customProbeTextLimit {
		report.Outcome = "text_budget_exceeded"
		return &matching.Input{}, report
	}
	if len(input.Texts) == 0 {
		report.Outcome = "text_fields_unavailable"
	}
	return input, report
}

func eligibleSearchField(cwd, key, value string) bool {
	if len(value) > 16384 {
		return false
	}
	normal := strings.ToLower(strings.ReplaceAll(value, "\\", "/"))
	for _, marker := range []string{"agents.md", "skill.md", "/.agents/", "/.codex/"} {
		if strings.Contains(normal, marker) {
			return false
		}
	}
	if (key == "path" || key == "file_path" || key == "filePath") && filepath.IsAbs(value) {
		root, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			return false
		}
		_, reason := probePath(root, cwd, value)
		return reason == ""
	}
	return true
}

func isSearchCommand(command string) bool {
	command = " " + strings.ToLower(command)
	for _, marker := range []string{" rg ", " rg\n", " grep ", "select-string", "get-content", " cat ", "readfile", "read_file", " sed "} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}

type businessField struct {
	Field, InputPath, Value string
}

func businessInputFields(e Event) []businessField {
	keys := map[string]bool{"query": true, "sql": true, "statement": true, "order_id": true, "order_no": true, "order_code": true,
		"ticket_id": true, "ticket_no": true, "document_id": true, "id": true, "number": true, "operation": true, "action": true,
		"type": true, "params": true, "parameters": true, "arguments": true}
	var fields []businessField
	var walk func(map[string]any, string, int)
	walk = func(object map[string]any, prefix string, depth int) {
		if depth > 6 || len(fields) >= 64 {
			return
		}
		names := make([]string, 0, len(object))
		for name := range object {
			if keys[strings.ToLower(name)] {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if len(fields) >= 64 {
				break
			}
			path := prefix + name
			switch value := object[name].(type) {
			case map[string]any:
				walk(value, path+".", depth+1)
			case string:
				fields = append(fields, businessField{name, path, value})
			case []any:
				for _, child := range value {
					if len(fields) >= 64 {
						break
					}
					if str, ok := child.(string); ok {
						fields = append(fields, businessField{name, path, str})
					}
					if obj, ok := child.(map[string]any); ok {
						walk(obj, path+".", depth+1)
					}
				}
			}
		}
	}
	walk(e.Input, "", 0)
	// The primary text for a query is its SQL, even when identifier fields exist.
	for _, key := range []string{"sql", "query", "statement"} {
		for _, field := range fields {
			if strings.EqualFold(field.Field, key) && field.Value != "" {
				return append([]businessField{{"text", field.InputPath, field.Value}}, fields...)
			}
		}
	}
	return fields
}

type observedToolField struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Bytes *int   `json:"bytes,omitempty"`
}

func describeToolFields(input map[string]any) []observedToolField {
	out := make([]observedToolField, 0)
	var walk func(map[string]any, string, int)
	walk = func(object map[string]any, prefix string, depth int) {
		if depth >= 6 {
			return
		}
		names := make([]string, 0, len(object))
		for name := range object {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if len(out) >= 32 {
				return
			}
			path := prefix + name
			if len(path) > 128 || !probeFieldPath.MatchString(path) {
				continue
			}
			field := observedToolField{Path: path}
			switch v := object[name].(type) {
			case string:
				n := len(v)
				field.Type, field.Bytes = "string", &n
			case bool:
				field.Type = "boolean"
			case float64:
				field.Type = "number"
			case []any:
				field.Type = "array"
			case map[string]any:
				field.Type = "object"
			default:
				field.Type = "null"
			}
			out = append(out, field)
			if child, ok := object[name].(map[string]any); ok {
				walk(child, path+".", depth+1)
			}
		}
	}
	walk(input, "", 0)
	return out
}
