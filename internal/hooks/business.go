package hooks

import (
	"regexp"
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
	keys := map[string]bool{"query": true, "sql": true, "statement": true, "order_id": true, "order_no": true, "order_code": true, "ticket_id": true, "ticket_no": true, "document_id": true, "id": true, "number": true, "operation": true, "action": true, "type": true, "params": true, "parameters": true, "arguments": true}
	if output {
		for _, k := range []string{"text", "body", "description", "summary", "content", "result", "rows", "data", "items", "records", "ticket", "order"} {
			keys[k] = true
		}
	}
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
	if output {
		walk(e.Response, true, 0)
	} else {
		walk(e.Input, false, 0)
	}
	if oversized || len(parts) == 0 {
		return ""
	}
	return e.Tool + " " + strings.Join(parts, " ")
}
