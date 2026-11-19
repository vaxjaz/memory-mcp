package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/memory"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

type probeCheckOptions struct {
	Adapter                                                         Adapter
	File, Config, Dir, Project, Scope, Server, Session, Expect, Cue string
}

type probeCheckMatch struct {
	memory.ExperienceMatch
	Source string `json:"source"`
}

func runProbeCheck(ctx context.Context, o probeCheckOptions) error {
	e, err := readProbeEvent(o)
	if err != nil {
		return err
	}
	store, err := memory.OpenReadOnly(o.Dir)
	if err != nil {
		return err
	}
	defer store.Close()
	report, err := checkProbe(ctx, store, o, e)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func readProbeEvent(o probeCheckOptions) (Event, error) {
	var e Event
	input := os.Stdin
	if o.File != "-" {
		file, err := os.Open(o.File)
		if err != nil {
			return e, err
		}
		defer file.Close()
		input = file
	}
	const limit = 1024 * 1024
	raw, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return e, err
	}
	if len(raw) > limit {
		return e, errors.New("probe sample exceeds 1 MiB")
	}
	if err := textguard.JSON(raw); err != nil {
		return e, err
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return e, fmt.Errorf("probe needs an event object with tool_name and tool_input: %w", err)
	}
	e.normalize()
	if e.Name != "" && e.Name != "PreToolUse" {
		return e, errors.New("probe check accepts PreToolUse samples only")
	}
	if e.Tool == "" || e.Input == nil {
		return e, errors.New("probe needs tool_name and a tool_input object")
	}
	if e.CWD == "" {
		e.CWD = o.Project
		if e.CWD == "" {
			e.CWD, err = os.Getwd()
			if err != nil {
				return e, err
			}
		}
	}
	if e.Session == "" {
		e.Session = o.Session
	}
	e.Name = "PreToolUse"
	e.normalizeAgentSession()
	return e, nil
}

func checkProbe(ctx context.Context, store *memory.Store, o probeCheckOptions, e Event) (map[string]any, error) {
	report := map[string]any{"tool": e.Tool, "read_only": true, "native_event_verified": false, "observed_fields": describeToolFields(e.Input)}
	if template, ok := toolTemplates[e.Tool]; ok {
		report["registration_template"] = template
	}
	result := func(outcome, next string) (map[string]any, error) {
		report["outcome"] = outcome
		if next != "" {
			report["next"] = next
		}
		return report, nil
	}
	version, err := store.LibrarySchema(ctx)
	if err != nil {
		return nil, err
	}
	if version != memory.SchemaVersion {
		report["schema_version"] = version
		report["supported_schema"] = memory.SchemaVersion
		if version < memory.SchemaVersion {
			return result("schema_upgrade_required", "Run memory-mcp migrate to inspect the existing library before trying the probe check.")
		}
		return result("schema_too_new", "Use the program version that supports this library schema.")
	}
	scope, err := store.HookResolve(ctx, e.CWD)
	if err != nil || o.Scope != "" && o.Scope != scope {
		return result("project_binding_mismatch", "Use the sample's actual cwd and the matching library/scope from the existing setup.")
	}
	report["scope"] = scope
	var expectedEntry memory.Entry
	if o.Expect != "" {
		entry, err := store.Read(ctx, o.Expect)
		if err != nil {
			return nil, err
		}
		expectedEntry = entry.Entry
		preview := memory.PreviewTriggers(entry.Entry)
		report["expected"] = map[string]any{"id": o.Expect, "cue_id": o.Cue, "trigger_preview": preview}
		if entry.Entry.Scope != scope || preview.RecordStatus != "eligible" {
			return result("expected_record_ineligible", "Read the record in the bound scope and check that you are testing its current, active revision.")
		}
	}
	enabled, err := store.ExperienceTrialEnabled(ctx, scope)
	if err != nil {
		return nil, err
	}
	report["wakeup_enabled"] = enabled
	if !enabled {
		return result("wakeup_disabled", "Check the existing project wakeup policy before testing a trigger.")
	}
	probes, err := loadProbeConfig(store.Dir)
	if err != nil {
		report["config_error"] = err.Error()
		return result("invalid_probe_config", "Correct the reported tool-probes.json field and run the same check again.")
	}
	rule, registered := probes.ruleFor(scope, e.Tool)
	if !registered {
		return result("tool_not_registered", "Map a verified input path in observed_fields to a named field, then run setup to register the tool.")
	}
	if !rule.active() {
		return result("tool_disabled", "Check the active project tool registration.")
	}
	input, extraction := rule.extract(e)
	report["tool_input"] = extraction
	c, _, _, err := readAdapterConfig(o.Adapter, o.Config)
	if err != nil {
		return nil, err
	}
	routing := map[string]any{}
	inspectManagedWorkflow(routing, c, configuredMemoryServices(c, store.Dir), o.Adapter.Name, o.Config, o.Server, probes, e.Tool)
	report["tool_routed"] = routing["tool_routed"]
	if routing["tool_routed"] != true {
		return result("tool_not_routed", "Check the exact event tool name and service config. After adding a selector, run setup and refresh the client.")
	}
	if rule.inspectionOnly() {
		return result("inspection_only", "Map an observed field in the tool registration. A selector without fields or observations reports only field metadata.")
	}
	r := Runtime{Host: o.Adapter.Name, Store: store, ServerName: o.Server}
	r.attachFacts(ctx, e, scope, probes, input)
	if o.Expect != "" {
		checks, err := memory.ExplainCue(expectedEntry, o.Cue, input)
		if err != nil {
			return nil, err
		}
		report["cue_checks"] = checks
		if len(checks) == 0 {
			return result("cue_not_found", "Use a cue ID in the selected current record, or omit --expect-cue to check the whole record.")
		}
	}
	if len(input.Texts) == 0 {
		return result("no_scanned_fields", "Check tool_input.fields and its outcome. Map a present string from observed_fields, or use a supported operation for this preset.")
	}
	probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	direct, matches, err := r.probeMatches(probe, e, scope, input, rule)
	if err != nil {
		return nil, err
	}
	sources := map[[2]string]bool{}
	for _, item := range direct {
		sources[[2]string{item.ID, item.CueID}] = true
	}
	candidates := make([]probeCheckMatch, 0, len(matches))
	for _, item := range matches {
		source := "context"
		if sources[[2]string{item.ID, item.CueID}] {
			source = "direct"
		}
		candidates = append(candidates, probeCheckMatch{ExperienceMatch: item, Source: source})
	}
	report["candidates"] = candidates
	expected := func(items []memory.ExperienceMatch) bool {
		for _, item := range items {
			if o.Expect == "" || o.Expect == item.ID && (o.Cue == "" || o.Cue == item.CueID) {
				return true
			}
		}
		return false
	}
	if expected(direct) {
		return result("direct_match", "")
	}
	if expected(matches) {
		return result("context_match", "The candidate came from a stored same-input business observation.")
	}
	return result("cue_missed", "Check cue_checks for a failed or unknown when prerequisite or a missed entrance, and tool_input for the field mapping. Use a full identifier with token, or use contains for a deliberate substring.")
}
