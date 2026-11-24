package hooks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/matching"
)

// defaultScanCommands are the commands whose text the default shell rule
// reads: reading files and searching. A command is scanned when one of these
// names appears in it as a whole word. The list lives in tool-probes.json, so
// a project that wants every command scanned removes "scan" from its own rule.
var defaultScanCommands = []string{
	"rg", "grep", "egrep", "fgrep", "ag", "ack", "findstr", "select-string",
	"cat", "get-content", "type", "head", "tail", "less", "more", "bat", "sed", "awk", "nl",
	"find", "fd", "ls", "dir", "get-childitem", "tree", "locate", "readfile",
}

var businessToolNames = []string{"get_ticket", "read_ticket", "search_tickets", "get_order", "query_order", "query_orders", "search_orders", "query",
	"mysql_query", "read_query", "execute_query", "execute_sql", "run_sql", "query_database"}

// defaultToolRules seeds the "tools" section of tool-probes.json on first
// setup and tops it up on upgrade. While a file has no "tools" section they
// apply as they are. Writing and editing tools are listed but stay disabled:
// every routed tool starts a hook process for each call, so a project turns on
// the ones it needs.
func defaultToolRules() []probeRule {
	candidates := func(paths ...string) fieldSource { return fieldSource{paths} }
	search := candidates("pattern", "query", "regex", "search_pattern")
	where := candidates("path", "file_path", "filePath")
	file := candidates("file_path", "path", "filePath")
	command := candidates("command", "cmd", "script")
	var rules []probeRule
	for _, name := range []string{"Bash", "Grep", "Read", "Write", "Edit"} {
		switch name {
		case "Bash":
			rules = append(rules, probeRule{Tool: name, Fields: map[string]fieldSource{"text": command}, Scan: defaultScanCommands})
		case "Grep":
			rules = append(rules, probeRule{Tool: name, Fields: map[string]fieldSource{"text": search, "path": where}})
		case "Read":
			rules = append(rules, probeRule{Tool: name, Fields: map[string]fieldSource{"path": file}})
		case "Write":
			rules = append(rules, disabled(probeRule{Tool: name, Fields: map[string]fieldSource{"text": candidates("content"), "path": candidates("file_path")}}))
		case "Edit":
			rules = append(rules, disabled(probeRule{Tool: name, Fields: map[string]fieldSource{"text": candidates("new_string"), "path": candidates("file_path")}}))
		}
	}
	for _, name := range []string{"grep", "RG", "rg", "Glob", "glob", "grep_code", "search_file", "search_files", "code_search"} {
		rules = append(rules, probeRule{Tool: name, Fields: map[string]fieldSource{"text": search, "path": where}})
	}
	for _, name := range []string{"read", "read_file"} {
		rules = append(rules, probeRule{Tool: name, Fields: map[string]fieldSource{"path": file}})
	}
	for _, name := range []string{"bash", "exec_command", "run_command", "run_in_terminal", "terminal"} {
		rules = append(rules, probeRule{Tool: name, Fields: map[string]fieldSource{"text": command}, Scan: defaultScanCommands})
	}
	for _, name := range []string{"read_file", "search_files", "grep", "glob", "code_search"} {
		rules = append(rules, probeRule{MCPTool: name, Fields: map[string]fieldSource{"text": search, "path": where}})
	}
	for _, name := range businessToolNames {
		rules = append(rules, probeRule{MCPTool: name, Discover: "query"})
	}
	return append(rules, probeRule{Tool: "mysql_query", Discover: "query"})
}

func disabled(r probeRule) probeRule {
	off := false
	r.Enabled = &off
	return r
}

// toolTemplates are the mappings doctor suggests for the writing tools.
var toolTemplates = map[string]probeRule{
	"Write": {Tool: "Write", Fields: map[string]fieldSource{"text": {[]string{"content"}}, "path": {[]string{"file_path"}}}},
	"Edit":  {Tool: "Edit", Fields: map[string]fieldSource{"text": {[]string{"new_string"}}, "path": {[]string{"file_path"}}}},
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

// bareMCPName is the name of an MCP tool without its server, the same name a
// rule selects with mcp_tool. It is empty for a native tool.
func bareMCPName(tool string) string {
	if strings.HasPrefix(tool, "mcp__") {
		if at := strings.LastIndex(tool, "__"); at > len("mcp__") {
			return tool[at+2:]
		}
	}
	return ""
}

// extract builds the matching input for one event. root is the bound project
// directory. Every field is judged on its own and reports why it was left
// out; one ineligible field never removes another.
func (r probeRule) extract(e Event, root string) (*matching.Input, probeInputReport) {
	input := &matching.Input{Fields: map[string]any{"tool": e.Tool, "mcp_tool": bareMCPName(e.Tool)}}
	// Source says where the rule came from; the caller fills it, being the one
	// that knows whether a project rule replaced the general one.
	report := probeInputReport{Tool: e.Tool, Outcome: "scanned"}
	skipped := func(outcome string) (*matching.Input, probeInputReport) {
		report.Outcome = outcome
		return &matching.Input{}, report
	}
	if !r.active() {
		return skipped("tool_disabled")
	}
	if r.inspectionOnly() {
		return skipped("inspection_only")
	}
	total := 0
	if r.business() {
		for _, value := range businessInputFields(e) {
			if len(value.Value) > customProbeTextLimit {
				report.Fields = append(report.Fields, probeInputField{Field: value.Field, InputPath: value.InputPath, Status: "field_too_large", Bytes: len(value.Value)})
				continue
			}
			input.Texts = append(input.Texts, matching.NamedText{Field: value.Field, Value: value.Value})
			if _, exists := input.Fields[value.Field]; !exists {
				input.Fields[value.Field] = value.Value
			}
			report.Fields = append(report.Fields, probeInputField{Field: value.Field, InputPath: value.InputPath, Status: "present", Bytes: len(value.Value)})
		}
	} else {
		if len(r.Scan) > 0 {
			if command, present := probeFieldValues(e.Input, r.Fields["text"]); !present || !scansAny(command.Items, r.Scan) {
				return skipped("operation_not_scanned")
			} else {
				for _, raw := range command.Items {
					if text, ok := raw.(string); ok && instructionReference(text) {
						return skipped("instruction_file")
					}
				}
			}
		}
		aliases := make([]string, 0, len(r.Fields))
		for alias := range r.Fields {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			found, present := probeFieldValues(e.Input, r.Fields[alias])
			if !present {
				report.Fields = append(report.Fields, probeInputField{Field: alias, InputPath: r.Fields[alias].Paths[0], Status: "missing"})
				continue
			}
			values := found.Items
			if len(values) == 0 {
				report.Fields = append(report.Fields, probeInputField{Field: alias, InputPath: found.Path, Status: "empty"})
				continue
			}
			for i, raw := range values {
				at := found.Path
				if len(values) > 1 {
					at = fmt.Sprintf("%s[%d]", found.Path, i)
				}
				text, isText := raw.(string)
				status := ""
				switch {
				case isText:
					status = textStatus(alias, text, root, e.CWD)
					if status == "" && total+len(text) > probeEventTextLimit {
						status = "text_budget_exceeded"
					}
				default:
					status = "unsupported_type"
					if len(values) == 1 {
						switch raw.(type) {
						case bool, float64:
							input.Fields[alias] = raw
							status = ""
						}
					}
				}
				field := probeInputField{Field: alias, InputPath: at, Status: "present"}
				if isText {
					field.Bytes = len(text)
				}
				if status != "" {
					field.Status = status
					report.Fields = append(report.Fields, field)
					continue
				}
				report.Fields = append(report.Fields, field)
				if isText {
					total += len(text)
					input.Texts = append(input.Texts, matching.NamedText{Field: alias, Value: text})
				}
				if _, exists := input.Fields[alias]; !exists {
					input.Fields[alias] = raw
				}
			}
			if found.Truncated {
				report.Fields = append(report.Fields, probeInputField{Field: alias, InputPath: fmt.Sprintf("%s[%d]", found.Path, probeElementLimit), Status: "element_limit"})
			}
		}
	}
	if len(input.Texts) == 0 {
		return skipped("no_eligible_text")
	}
	return input, report
}

// textStatus reports why a string is left out, or "" when it takes part.
// Only a field named path is judged as a location; the same text under
// another name is ordinary content.
func textStatus(alias, value, root, cwd string) string {
	switch {
	case value == "":
		return "empty"
	case len(value) > customProbeTextLimit:
		return "field_too_large"
	case alias != "path":
		return ""
	case instructionPath(value):
		return "instruction_file"
	}
	return pathVerdict(root, cwd, value)
}

// scansCommand reports whether a command text contains name as a whole word.
func scansCommand(command, name string) bool {
	lower := strings.ToLower(command)
	word := func(index int) bool {
		if index < 0 || index >= len(lower) {
			return false
		}
		b := lower[index]
		return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '-'
	}
	for start := 0; ; {
		at := strings.Index(lower[start:], name)
		if at < 0 {
			return false
		}
		at += start
		if !word(at-1) && !word(at+len(name)) {
			return true
		}
		start = at + 1
	}
}

func scansAny(values []any, names []string) bool {
	for _, raw := range values {
		text, ok := raw.(string)
		if !ok {
			continue
		}
		for _, name := range names {
			if scansCommand(text, strings.ToLower(name)) {
				return true
			}
		}
	}
	return false
}

// fieldValues are the values one mapped field names.
type fieldValues struct {
	Path      string
	Items     []any
	Truncated bool
}

// probeFieldValues follows the first candidate path that is present and
// returns every value it names, at most probeElementLimit.
func probeFieldValues(input map[string]any, source fieldSource) (fieldValues, bool) {
	for _, path := range source.Paths {
		if items, truncated, present := probeValues(input, path); present {
			return fieldValues{Path: path, Items: items, Truncated: truncated}, true
		}
	}
	return fieldValues{}, false
}

// probeValues resolves a dotted path: object keys, array indexes and "*"
// for every element. A path that ends at an array takes its elements.
func probeValues(input map[string]any, path string) (values []any, truncated, present bool) {
	if path == "" || !probeFieldPath.MatchString(path) {
		return nil, false, false
	}
	leaves := []any{input}
	for _, segment := range strings.Split(path, ".") {
		var next []any
		for _, leaf := range leaves {
			switch node := leaf.(type) {
			case map[string]any:
				if value, ok := node[segment]; ok {
					next = append(next, value)
				}
			case []any:
				if segment == "*" {
					next = append(next, node...)
				} else if index := arrayIndex(segment); index >= 0 && index < len(node) {
					next = append(next, node[index])
				}
			}
		}
		if len(next) == 0 {
			return nil, false, false
		}
		leaves = next
	}
	for _, leaf := range leaves {
		if items, ok := leaf.([]any); ok {
			values = append(values, items...)
		} else {
			values = append(values, leaf)
		}
	}
	if len(values) > probeElementLimit {
		return values[:probeElementLimit], true, true
	}
	return values, false, true
}

func arrayIndex(segment string) int {
	index := 0
	for _, c := range segment {
		if c < '0' || c > '9' || index > 1e6 {
			return -1
		}
		index = index*10 + int(c-'0')
	}
	return index
}

// probeValue resolves a path that must name one value, such as an observation key.
func probeValue(input map[string]any, field string) any {
	var value any = input
	for _, segment := range strings.Split(field, ".") {
		switch node := value.(type) {
		case map[string]any:
			value = node[segment]
		case []any:
			index := arrayIndex(segment)
			if index < 0 || index >= len(node) {
				return nil
			}
			value = node[index]
		default:
			return nil
		}
	}
	return value
}

type businessField struct {
	Field, InputPath, Value string
}

// businessInputFields finds the query statement of a database-like tool by its
// known argument names, at any nesting up to a bounded depth.
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

// describeToolFields lists the paths and types of a sample input without
// their values. Array elements are shown with the "*" a mapping uses.
func describeToolFields(input map[string]any) []observedToolField {
	out := make([]observedToolField, 0)
	var walk func(object map[string]any, prefix string, depth int)
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
			switch child := object[name].(type) {
			case map[string]any:
				walk(child, path+".", depth+1)
			case []any:
				if len(child) > 0 {
					walk(map[string]any{"*": child[0]}, path+".", depth+1)
				}
			}
		}
	}
	walk(input, "", 0)
	return out
}
