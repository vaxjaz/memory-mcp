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

type probeCheckStep struct {
	Stage  string `json:"stage"`
	Detail any    `json:"detail"`
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
	report := map[string]any{"tool": e.Tool, "read_only": true, "native_event_verified": false}
	result := func(outcome, next string) (map[string]any, error) {
		report["outcome"] = outcome
		if next != "" {
			report["next"] = next
		}
		return report, nil
	}
	scope, err := store.HookResolve(ctx, e.CWD)
	if err != nil || o.Scope != "" && o.Scope != scope {
		return result("project_binding_mismatch", "Use the sample's actual cwd and the matching library/scope from the existing setup.")
	}
	report["scope"] = scope
	if o.Expect != "" {
		entry, err := store.Read(ctx, o.Expect)
		if err != nil {
			return nil, err
		}
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
	r := Runtime{Host: o.Adapter.Name, Store: store, ServerName: o.Server}
	var steps []probeCheckStep
	clues, _, err := r.evaluateProbe(ctx, e, scope, probes, func(stage string, detail any) {
		steps = append(steps, probeCheckStep{Stage: stage, Detail: detail})
	})
	report["steps"] = steps
	if err != nil {
		return nil, err
	}
	if len(clues) == 0 {
		next := probeInputAdvice(steps)
		if next == "" {
			next = "Check the branch outcome and actual event input keys. For a built-in search probe, use an eligible read/search action."
		}
		return result("no_scanned_fields", next)
	}
	probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	direct, matches, err := r.probeMatches(probe, e, scope, clues)
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
		return result("context_match", "This cue was matched by a stored business observation in this session.")
	}
	if next := probeInputAdvice(steps); next != "" {
		return result("cue_missed", next)
	}
	return result("cue_missed", "Check the saved entrance against the scanned field. ASCII code/config entrances use whole identifier boundaries. Register the actual full identifier and rerun this check.")
}

func probeInputAdvice(steps []probeCheckStep) string {
	for _, step := range steps {
		report, ok := step.Detail.(*probeInputReport)
		if !ok {
			continue
		}
		switch report.Outcome {
		case "path_outside_project":
			return "Check the bound directory. For a separate repository, use a when suffix condition on input.file_path and scan content."
		case "extension_mismatch":
			return "Compare the actual target suffix with the branch extensions."
		case "path_field_unavailable":
			return "Use the actual event field containing the target path."
		case "path_not_resolved", "path_not_accessible_from_project", "path_is_project_root", "path_too_long":
			return "Check that the target resolves to a file under the bound directory, including link and junction boundaries."
		case "text_budget_exceeded", "combined_budget_exceeded":
			return "Use fewer or smaller configured text fields. This probe exceeded the 32 KiB text budget."
		case "text_fields_unavailable":
			return "Use the actual event's string field and check the missing, empty or non-string status."
		}
	}
	return ""
}
