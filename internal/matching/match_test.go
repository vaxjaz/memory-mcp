package matching

import "testing"

func TestSharedComparison(t *testing.T) {
	sensitive := true
	insensitive := false
	for _, tc := range []struct {
		name             string
		rule             Rule
		actual, expected any
		want             Truth
	}{
		{"default word boundary", Rule{Op: "token"}, "UPDATE x", "update", True},
		{"longer SQL identifier", Rule{Op: "token"}, "SET work_order_feedback_fix_second=1", "fix_second", False},
		{"case-sensitive substring", Rule{Op: "contains", CaseSensitive: &sensitive}, "work_order_feedback_fix_Second", "fix_Second", True},
		{"different case", Rule{Op: "contains", CaseSensitive: &sensitive}, "work_order_feedback_fix_second", "fix_Second", False},
		{"suffix with folded case", Rule{Op: "suffix", CaseSensitive: &insensitive}, "EXPORT.SQL", ".sql", True},
		{"old path normalization", Rule{Op: "contains", Normalize: "path"}, `C:\repo\src\Handler.java`, "src/handler.java", True},
		{"old whitespace behavior", Rule{Op: "token", Normalize: "whitespace"}, "run Show   Columns now", "SHOW COLUMNS", True},
		{"whitespace keeps original boundary", Rule{Op: "token", Normalize: "whitespace"}, "run NOTSHOW COLUMNS now", "SHOW COLUMNS", False},
		{"missing text", Rule{Op: "contains"}, nil, "ready", Unknown},
		{"typed equality", Rule{Op: "eq"}, true, true, True},
		{"no coercion", Rule{Op: "eq"}, "true", true, Unknown},
		{"missing existence", Rule{Op: "exists"}, nil, nil, False},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rule.Compare(tc.actual, tc.expected, false); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
