package hooks

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Tool integration, never business vocabulary or a prescribed workflow. Unknown
// adapters remain uncovered rather than treating every tool as a semantic probe.
const businessToolMatcher = "^mcp__.+__(get_ticket|read_ticket|search_tickets|get_order|query_order|query_orders|search_orders|query|read_query|execute_query|execute_sql|run_sql|query_database)$"

var businessTools = regexp.MustCompile(businessToolMatcher)

func isBusinessTool(name string) bool {
	lower := strings.ToLower(name)
	return !strings.Contains(lower, "memory_") && !strings.Contains(lower, "decision_memory") && businessTools.MatchString(name)
}

func businessClues(e Event, output bool) string {
	if !isBusinessTool(e.Tool) {
		return ""
	}
	if output {
		records := businessObservationRecords(e)
		if len(records) == 1 {
			return records[0]
		}
		return "" // Never flatten several records into a fabricated association.
	}
	keys := map[string]bool{"query": true, "sql": true, "statement": true, "order_id": true, "order_no": true, "order_code": true, "ticket_id": true, "ticket_no": true, "document_id": true, "id": true, "number": true, "operation": true, "action": true, "type": true, "params": true, "parameters": true, "arguments": true}
	var parts []string
	size := 0
	oversized := false
	var walk func(any, bool, int)
	walk = func(value any, allowed bool, depth int) {
		if depth > 6 {
			oversized = true
			return
		}
		switch v := value.(type) {
		case string:
			if allowed {
				size += len(v)
				if size > 32768 {
					oversized = true
					return
				}
				parts = append(parts, v)
			}
		case map[string]any:
			for k, child := range v {
				if keys[strings.ToLower(k)] {
					walk(child, true, depth+1)
				}
			}
		case []any:
			if allowed {
				for _, child := range v {
					walk(child, true, depth+1)
				}
			}
		}
	}
	walk(e.Input, false, 0)
	if oversized || len(parts) == 0 {
		return ""
	}
	return e.Tool + " " + strings.Join(parts, " ")
}

// Preserve object boundaries through structured rows and MCP JSON text blocks.
// Parent/sibling fields are not inherited by a record. Unstructured text is one
// candidate record; Store rejects ambiguous multiple-object co-occurrence.
func businessObservationRecords(e Event) []string {
	if !isBusinessTool(e.Tool) || e.IsError || e.Error != nil {
		return nil
	}
	if result, ok := e.Response.(map[string]any); ok && result["isError"] == true {
		return nil
	}
	keys := map[string]bool{"query": true, "sql": true, "statement": true, "order_id": true, "order_no": true, "order_code": true, "ticket_id": true, "ticket_no": true, "document_id": true, "id": true, "number": true, "operation": true, "action": true, "type": true, "params": true, "parameters": true, "arguments": true, "text": true, "body": true, "description": true, "summary": true, "content": true, "result": true, "rows": true, "data": true, "items": true, "records": true, "ticket": true, "order": true}
	var records []string
	size := 0
	invalid := false
	add := func(parts []string) {
		if len(parts) > 0 {
			records = append(records, e.Tool+" "+strings.Join(parts, " "))
		}
		if len(records) > 128 {
			invalid = true
		}
	}
	var walk func(any, int)
	var text func(string, int) (string, bool)
	text = func(s string, depth int) (string, bool) {
		size += len(s)
		if size > 32768 {
			invalid = true
			return "", false
		}
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var structured any
			if json.Unmarshal([]byte(trimmed), &structured) == nil {
				walk(structured, depth+1)
				return "", false
			}
		}
		return s, true
	}
	walk = func(value any, depth int) {
		if invalid {
			return
		}
		if depth > 6 {
			invalid = true
			return
		}
		switch v := value.(type) {
		case string:
			if s, ok := text(v, depth); ok {
				add([]string{s})
			}
		case []any:
			for _, child := range v {
				walk(child, depth+1)
			}
		case map[string]any:
			// A native MCP structured result is the record source. Do not also
			// infer object relations from a potentially mixed narrative text copy.
			if structured, ok := v["structuredContent"]; ok {
				switch structured.(type) {
				case map[string]any, []any:
					walk(structured, depth+1)
					return
				}
			}
			var names []string
			for k := range v {
				if keys[strings.ToLower(k)] {
					names = append(names, k)
				}
			}
			sort.Strings(names)
			var parts []string
			for _, k := range names {
				if s, ok := v[k].(string); ok {
					if s, keep := text(s, depth); keep {
						parts = append(parts, s)
					}
				} else {
					walk(v[k], depth+1)
				}
			}
			add(parts)
		}
	}
	walk(e.Response, 0)
	if invalid {
		return nil
	}
	return records
}
