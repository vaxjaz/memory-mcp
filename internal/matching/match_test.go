package matching

import (
	"encoding/json"
	"strings"
	"testing"
)

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

// A field name may stand for several values, such as the elements of an
// array. A comparison that names the field is satisfied when any value under
// that name satisfies it; a name that carries one value behaves as before.
func TestNamedFieldComparesEveryValue(t *testing.T) {
	load := func(text string) *Expression {
		var expression Expression
		if err := json.Unmarshal([]byte(text), &expression); err != nil {
			t.Fatal(err)
		}
		return &expression
	}
	many := &Input{Fields: map[string]any{"text": "first", "tool": "ask"}, Texts: []NamedText{
		{Field: "text", Value: "first"}, {Field: "text", Value: "Is entAsk right?"}, {Field: "path", Value: "notes/entAsk.md"},
	}}
	one := &Input{Fields: map[string]any{"text": "first"}, Texts: []NamedText{{Field: "text", Value: "first"}}}
	for _, tc := range []struct {
		name   string
		text   string
		input  *Input
		want   Truth
		onPath bool
	}{
		{"an entrance in the second element", `{"field":"text","op":"contains","entrances":["entAsk"]}`, many, True, false},
		{"an entrance under another name is not looked at", `{"field":"text","op":"contains","entrances":["entAsk"]}`, one, False, false},
		{"without a name every value is searched", `{"op":"contains","entrances":["entAsk"]}`, many, True, false},
		{"a value guard met by the second element", `{"field":"text","op":"token","value":"entask"}`, many, True, false},
		{"a value guard met by no element", `{"field":"text","op":"token","value":"missing"}`, many, False, false},
		{"a name that carries nothing", `{"field":"absent","op":"contains","entrances":["entAsk"]}`, many, Unknown, false},
	} {
		truth, hit := load(tc.text).compare(tc.input)
		// Only a comparison with entrances reports which value matched.
		wantHit := tc.want == True && strings.Contains(tc.text, "entrances")
		if truth != tc.want || (hit != nil) != wantHit || hit != nil && (hit.Field == "path") != tc.onPath {
			t.Fatalf("%s: got %v %v, want %v (hit on path: %v)", tc.name, truth, hit, tc.want, tc.onPath)
		}
	}
}
