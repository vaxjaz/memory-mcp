package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func asObject(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func listOf(value any) []any {
	result, _ := value.([]any)
	return result
}

func selectorOf(rule map[string]any) string {
	if name, ok := rule["mcp_tool"].(string); ok {
		return "mcp:" + name
	}
	return rule["tool"].(string)
}

// guideExamples returns every fenced ```json block of the embedded guide as
// the JSON values it holds. A block may show more than one value.
func guideExamples(t *testing.T) [][]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "server", "guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	var blocks [][]any
	for _, block := range strings.Split(string(raw), "```json\n")[1:] {
		decoder := json.NewDecoder(strings.NewReader(strings.SplitN(block, "```", 2)[0]))
		var values []any
		for decoder.More() {
			var value any
			if err := decoder.Decode(&value); err != nil {
				t.Fatalf("a guide example is not valid JSON: %v\n%s", err, block)
			}
			values = append(values, value)
		}
		blocks = append(blocks, values)
	}
	return blocks
}

// Every example in the embedded guide must do what its text says it does.
// Rule examples load as one configuration; each cue example is a valid write
// that the project's rules could fire; each printed event sample reaches the
// cue printed beside it and the printed near misses reach nothing. An example
// that stops working fails here instead of misleading every agent that reads it.
func TestGuideExamplesLoadStoreAndFire(t *testing.T) {
	// A library of its own, bound to the scope the guide's examples name.
	root := t.TempDir()
	store, err := memory.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	const scope = "project:example/team-repo"
	if err := store.HookBind(testCtx, project, scope); err != nil {
		t.Fatal(err)
	}

	// Unrelated bodies, so that the write-time comparison stays out of the way.
	facts := map[string]string{
		"settlement_code":            "The settlement column is configured per environment.",
		"settlement_code_write_gate": "A warehouse closure runs every night at two.",
		"credential_self_fetch":      "The service account is issued by the operations owner.",
		"shown_expression":           "Shipping labels carry the carrier's reference code.",
		"ask_example":                "Archived tickets are purged after ninety days.",
	}
	// Later examples replace an earlier rule for the same tool, as editing the file would.
	var tools []any
	projects := map[string][]any{}
	put := func(rules []any, rule any) []any {
		for i, existing := range rules {
			if selectorOf(asObject(existing)) == selectorOf(asObject(rule)) {
				rules[i] = rule
				return rules
			}
		}
		return append(rules, rule)
	}
	// The array mapping is printed for another, placeholder project.
	var arrayRule map[string]any
	var cues []memory.ExperienceCue
	var samples []map[string]any
	for _, block := range guideExamples(t) {
		for _, value := range block {
			document := asObject(value)
			switch {
			case document["tools"] != nil || document["projects"] != nil:
				for _, rule := range listOf(document["tools"]) {
					tools = put(tools, rule)
				}
				for name, rules := range asObject(document["projects"]) {
					for _, rule := range listOf(rules) {
						if arrayRule == nil {
							for _, path := range asObject(asObject(rule)["fields"]) {
								paths := listOf(path)
								if text, ok := path.(string); ok {
									paths = []any{text}
								}
								for _, item := range paths {
									if strings.Contains(item.(string), ".*.") {
										arrayRule = asObject(rule)
									}
								}
							}
						}
						projects[name] = put(projects[name], rule)
					}
				}
			case document["cue_id"] != nil:
				raw, _ := json.Marshal(document)
				var cue memory.ExperienceCue
				if err := json.Unmarshal(raw, &cue); err != nil {
					t.Fatal(err)
				}
				cues = append(cues, cue)
			case document["tool_name"] != nil:
				samples = append(samples, document)
			case document["when"] != nil || document["op"] != nil:
				// A bare expression is shown as one match of a cue.
				raw, _ := json.Marshal(document)
				cues = append(cues, memory.ExperienceCue{ID: "shown_expression", Label: "shown", EvidenceRefs: []string{"verified-source"},
					Matches: []memory.CueMatch{jsonMatch(t, raw)}})
			}
		}
	}
	if len(tools) == 0 || len(projects[scope]) == 0 || len(cues) == 0 || len(samples) == 0 {
		t.Fatal("the guide lost a kind of example", len(tools), len(projects[scope]), len(cues), len(samples))
	}
	if arrayRule == nil {
		t.Fatal("the guide no longer shows how to map an array")
	}
	projects[scope] = put(projects[scope], arrayRule)
	config, _ := json.Marshal(map[string]any{"tools": tools, "projects": projects})
	if err := os.WriteFile(filepath.Join(store.Dir, probeConfigName), config, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProbeConfig(store.Dir); err != nil {
		t.Fatal("a rule example of the guide does not load", err)
	}

	// The array mapping is printed as a rule; give it the cue that uses it.
	cues = append(cues, memory.ExperienceCue{ID: "ask_example", Label: "ask", EvidenceRefs: []string{"verified-ask"},
		Matches: []memory.CueMatch{jsonMatch(t, []byte(`{"when":{"field":"mcp_tool","op":"eq","value":"ask"},"field":"text","op":"contains","entrances":["entGuideAsk"]}`))}})
	for i, cue := range cues {
		reference := cue.EvidenceRefs[0]
		if err := RejectUnreachableCues(store.Dir, scope, "experience", []memory.ExperienceCue{cue}); err != nil {
			t.Fatalf("cue example %q could never fire under the guide's own rules: %v", cue.ID, err)
		}
		write := memory.ExperienceWrite{Scope: scope, Project: project, Title: "guide example " + cue.ID, RequestID: fmt.Sprintf("guide-%d-%s", i, cue.ID),
			Admission: &memory.ExperienceAdmission{FutureDecision: "Check the guide example before relying on it.", EvidenceRef: reference},
			Experience: memory.Experience{Fact: facts[cue.ID], Evidence: []memory.ExperienceEvidence{{Kind: "business", Reference: reference, Observation: "isolated guide example"}},
				Cues: []memory.ExperienceCue{cue}}}
		receipt, err := store.RecordExperience(testCtx, write)
		if err == nil && receipt.Status == "needs_comparison" {
			// Two printed cues share an entrance on purpose; each is its own fact.
			write.Resolution = &memory.WriteResolution{Action: "create", TargetID: receipt.Candidates[0].ID, Difference: "a separate guide example with a fact of its own",
				EvidenceRef: reference, ComparisonToken: receipt.ComparisonToken}
			receipt, err = store.RecordExperience(testCtx, write)
		}
		if err != nil || receipt.ID == "" {
			t.Fatalf("cue example %q is not a valid write: %v %v", cue.ID, receipt, err)
		}
	}

	runtime := Runtime{Host: "zcode", Store: store}
	fired := func(tool string, input map[string]any) map[string]bool {
		selection, err := runtime.configuredInput(testCtx, Event{Name: "PreToolUse", CWD: project, Session: "guide", Tool: tool, Input: input}, scope)
		if err != nil {
			t.Fatal(err)
		}
		matches, err := store.MatchToolInput(testCtx, scope, selection.Input)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, match := range matches {
			found[match.CueID] = true
		}
		return found
	}
	reached := map[string]bool{}
	for _, sample := range samples {
		found := fired(sample["tool_name"].(string), asObject(sample["tool_input"]))
		if !found["settlement_code_write_gate"] && !found["settlement_code"] {
			t.Fatalf("the guide's sample %v reaches no printed cue: %v", sample["tool_input"], found)
		}
		for cue := range found {
			reached[cue] = true
		}
	}
	if !reached["settlement_code"] || !reached["settlement_code_write_gate"] {
		t.Fatal("the Write samples do not reach both cues printed beside them", reached)
	}
	for tool, input := range map[string]map[string]any{
		"mcp__api-tester__login": {"username": "G003027"},
		"mcp__asker__ask":        {"questions": []any{map[string]any{"question": "first"}, map[string]any{"question": "Is entGuideAsk right?"}}},
	} {
		if found := fired(tool, input); !found["credential_self_fetch"] && !found["ask_example"] {
			t.Fatalf("the guide's %s example does not fire for %v: %v", tool, input, found)
		}
	}
	// The near misses the guide names: the same column in a SELECT, a .txt file, or the filename alone.
	for _, input := range []map[string]any{
		{"file_path": "example.sql", "content": "select work_order_feedback_FIX_THIRD from demo"},
		{"file_path": "example.txt", "content": "update demo SET work_order_feedback_FIX_THIRD=1"},
		{"file_path": "work_order_feedback_fix_third.sql", "content": "ordinary text"},
	} {
		if found := fired("Write", input); len(found) != 0 {
			t.Fatalf("a near miss the guide names fired: %v %v", input, found)
		}
	}
}

func jsonMatch(t *testing.T, raw []byte) memory.CueMatch {
	t.Helper()
	var match memory.CueMatch
	if err := json.Unmarshal(raw, &match); err != nil {
		t.Fatal(err)
	}
	return match
}
