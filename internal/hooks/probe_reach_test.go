package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

// expression builds one match expression from JSON, the way a cue is written.
func expression(t *testing.T, text string) memory.CueMatch {
	t.Helper()
	var match memory.CueMatch
	if err := json.Unmarshal([]byte(text), &match); err != nil {
		t.Fatal(err)
	}
	return match
}

func writeProbeDocument(t *testing.T, document string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, probeConfigName), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// configured writes tool-probes.json and returns its loaded form.
func configured(t *testing.T, document string) probeConfig {
	t.Helper()
	config, err := loadProbeConfig(writeProbeDocument(t, document))
	if err != nil {
		t.Fatal(err)
	}
	return config
}

// A cue is refused only when it is certain that no active rule of the project
// could ever carry it. The reason names the condition and the way out.
func TestCueIsRefusedOnlyWhenNoActiveRuleCanCarryIt(t *testing.T) {
	config := configured(t, `{"tools":[
		{"tool":"Read","fields":{"path":["file_path","path"]}},
		{"tool":"Bash","fields":{"text":"command"},"scan":["grep"]},
		{"tool":"Write","enabled":false,"fields":{"text":"content","path":"file_path"}},
		{"tool":"Edit","enabled":false,"fields":{"text":"new_string","path":"file_path"}}],
		"projects":{"project:x":[
		{"tool":"Write","fields":{"text":"content","path":"file_path"}},
		{"mcp_tool":"login","fields":{"text":"username","username":"username"},"observe":[{"name":"signed_in","key":["input.username"],"fields":{"ok":"ok"}}]}]}}`)
	const entrance = `,"op":"token","entrances":["x"]}`
	for _, tc := range []struct {
		name, match, refuse, hint string
	}{
		{"a bare MCP name written as a native tool", `{"when":{"field":"tool","op":"eq","value":"login"}` + entrance, `no rule in tool-probes.json registers a tool named "login"`, `"field":"mcp_tool"`},
		{"an MCP tool nobody registered", `{"when":{"field":"mcp_tool","op":"eq","value":"unknown"}` + entrance, `registers a tool named "unknown"`, ""},
		{"a registered MCP tool by bare name", `{"when":{"field":"mcp_tool","op":"eq","value":"login"}` + entrance, "", ""},
		{"a native tool registered for the project", `{"when":{"all":[{"field":"tool","op":"eq","value":"Write"},{"field":"path","op":"suffix","value":".md"}]}` + entrance, "", ""},
		{"a field no rule exposes", `{"field":"nope","op":"exists"` + entrance, `exposes a field named "nope"`, ""},
		{"a field only other tools expose", `{"when":{"field":"tool","op":"eq","value":"Read"},"field":"text","op":"contains","entrances":["x"]}`, `no rule for Read exposes a field named "text"`, "it is available from Bash, Write, mcp:login"},
		{"a result this project observes", `{"fact":{"name":"signed_in","key":["username"],"match":{"field":"ok","op":"eq","value":true}}` + entrance, "", ""},
		{"a result field the observation does not expose", `{"fact":{"name":"signed_in","key":["username"],"match":{"field":"role","op":"eq","value":"admin"}}` + entrance, `the observation "signed_in" exposes the results "ok", not "role"`, ""},
		{"a field exposed under a candidate path", `{"when":{"field":"tool","op":"eq","value":"Write"},"field":"path","op":"suffix","value":".md"` + entrance, "", ""},
		{"a condition that cannot be decided", `{"any":[{"field":"text","op":"token","value":"A"},{"field":"nope","op":"exists"}]` + entrance, "", ""},
		{"a tool compared with another operator", `{"when":{"field":"tool","op":"prefix","value":"log"}` + entrance, "", ""},
		{"a result nobody observes", `{"fact":{"name":"signed_out","key":["username"],"match":{"field":"ok","op":"eq","value":true}}` + entrance, `observes a result named "signed_out"`, ""},
		{"a result keyed by a field its tool does not expose", `{"fact":{"name":"signed_in","key":["missing"],"match":{"field":"ok","op":"eq","value":true}}` + entrance, `exposes a field named "missing"`, ""},
		{"a bare entrance", `{"op":"token","entrances":["x"]}`, "", ""},
	} {
		cue := memory.ExperienceCue{ID: "c", Matches: []memory.CueMatch{expression(t, tc.match)}}
		issue := config.unreachable("project:x", cue)
		if (issue != nil) != (tc.refuse != "") || issue != nil && !strings.Contains(issue.Why, tc.refuse) {
			t.Fatalf("%s: got %+v, want a refusal containing %q", tc.name, issue, tc.refuse)
		}
		if issue != nil && tc.hint != "" && !strings.Contains(issue.Why, tc.hint) {
			t.Fatalf("%s: hint %q missing from %q", tc.name, tc.hint, issue.Why)
		}
	}

	disabled := memory.ExperienceCue{ID: "c", Matches: []memory.CueMatch{expression(t,
		`{"when":{"all":[{"field":"tool","op":"eq","value":"Edit"},{"field":"path","op":"suffix","value":".md"}]}`+entrance)}}
	issue := config.unreachable("project:x", disabled)
	if issue == nil || !strings.Contains(issue.Why, `"enabled": false`) || !strings.Contains(issue.Next, "true") {
		t.Fatal("a listed but disabled tool was not explained", issue)
	}
	if issue = config.unreachable("project:x", memory.ExperienceCue{ID: "c", Matches: []memory.CueMatch{
		expression(t, `{"when":{"field":"tool","op":"eq","value":"Edit"}`+entrance),
		expression(t, `{"op":"token","entrances":["x"]}`),
	}}); issue != nil {
		t.Fatal("one expression that can fire is enough", issue)
	}
	if issue := config.unreachable("project:y", memory.ExperienceCue{ID: "c", Matches: []memory.CueMatch{
		expression(t, `{"when":{"field":"tool","op":"eq","value":"Write"},"field":"path","op":"suffix","value":".md"`+entrance),
	}}); issue == nil || !strings.Contains(issue.Why, `"enabled": false`) {
		t.Fatal("another project must be judged by its own rules", issue)
	}
}

func TestCueRefusalNamesTheConditionAndLeavesUndecidableCasesAlone(t *testing.T) {
	dir := writeProbeDocument(t, `{"tools":[],"projects":{}}`)
	scope := "project:x"
	bare := memory.ExperienceCue{ID: "settlement", Matches: []memory.CueMatch{expression(t, `{"op":"token","entrances":["fix_third"]}`)}}
	named := memory.ExperienceCue{ID: "account", Matches: []memory.CueMatch{expression(t,
		`{"when":{"field":"tool","op":"eq","value":"login"},"op":"token","entrances":["acct"]}`)}}
	err := RejectUnreachableCues(dir, scope, "experience", []memory.ExperienceCue{bare, named})
	for _, want := range []string{"invalid record (2 issues, no write performed)", "experience.cues[0].matches[0]: no active rule scans any text",
		"experience.cues[1].matches[0].when: no rule in tool-probes.json registers a tool named \"login\""} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must contain %q", want)
		}
	}
	// An unreadable file proves nothing: the write is left to the other validations.
	broken := t.TempDir()
	os.WriteFile(filepath.Join(broken, probeConfigName), []byte("{"), 0600)
	if err := RejectUnreachableCues(broken, scope, "experience", []memory.ExperienceCue{named}); err != nil {
		t.Fatal("a broken configuration must not be read as a refusal", err)
	}
	// A fresh installation that has not listed its tools yet still scans the defaults.
	if err := RejectUnreachableCues(t.TempDir(), scope, "experience", []memory.ExperienceCue{bare}); err != nil {
		t.Fatal(err)
	}
}

// A cue stored before this check existed, or under another configuration,
// stays readable; doctor lists it so an upgrade can find it.
func TestStoredCuesThatCouldNeverFireAreListedNotChanged(t *testing.T) {
	s, _, p := trialFixture(t)
	if _, err := s.RecordExperience(testCtx, memory.ExperienceWrite{Scope: "project:demo", Project: p, Title: "bare MCP name", RequestID: "bare",
		Admission: &memory.ExperienceAdmission{FutureDecision: "Check the account before asking.", EvidenceRef: "owner note"},
		Experience: memory.Experience{Fact: "The acceptance account is issued by its owner.", Evidence: []memory.ExperienceEvidence{{Kind: "runtime", Reference: "owner note", Observation: "isolated"}},
			Cues: []memory.ExperienceCue{{ID: "ask", Label: "ask", EvidenceRefs: []string{"owner note"},
				Matches: []memory.CueMatch{expression(t, `{"when":{"field":"tool","op":"eq","value":"ask"},"op":"token","entrances":["askOwner"]}`)}}}}}); err != nil {
		t.Fatal(err)
	}
	listed, err := UnreachableCues(testCtx, s, "project:demo")
	if err != nil || len(listed) != 1 || listed[0].Cue != "ask" || !strings.Contains(listed[0].Why, `"ask"`) {
		t.Fatal(listed, err)
	}
	if other, err := UnreachableCues(testCtx, s, "project:elsewhere"); err != nil || len(other) != 0 {
		t.Fatal("a cue was reported for another project", other, err)
	}
}
