package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

const probeConfigName = "tool-probes.json"

type probeConfig struct {
	Projects map[string][]probeRule     `json:"projects"`
	Facts    map[string][]probeFactRule `json:"context_facts,omitempty"`
}

type probeRule struct {
	Tool       string          `json:"tool,omitempty"`
	MCPTool    string          `json:"mcp_tool,omitempty"`
	Mode       string          `json:"mode,omitempty"`
	PathField  string          `json:"path_field,omitempty"`
	TextFields []string        `json:"text_fields,omitempty"`
	Extensions []string        `json:"extensions,omitempty"`
	When       *probeCondition `json:"when,omitempty"`
	Branches   []probeBranch   `json:"branches,omitempty"`
}

type probeBranch struct {
	ID         string          `json:"id"`
	When       *probeCondition `json:"when,omitempty"`
	PathField  string          `json:"path_field,omitempty"`
	TextFields []string        `json:"text_fields,omitempty"`
	Extensions []string        `json:"extensions,omitempty"`
}

var (
	probeToolName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	probeFieldPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*){0,5}$`)
	probeExtension = regexp.MustCompile(`^\.[A-Za-z0-9]{1,16}$`)
)

func loadProbeConfig(dir string) (probeConfig, error) {
	path := filepath.Join(dir, probeConfigName)
	var config probeConfig
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	defer file.Close()
	const maxConfigBytes = 64 * 1024
	raw, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return config, err
	}
	if len(raw) > maxConfigBytes {
		return config, fmt.Errorf("%s exceeds %d bytes", probeConfigName, maxConfigBytes)
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if err := textguard.JSON(raw); err != nil {
		return config, fmt.Errorf("%s: %w", probeConfigName, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		return config, fmt.Errorf("%s: %w", probeConfigName, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return config, fmt.Errorf("%s: expected exactly one JSON object", probeConfigName)
	}
	if config.Projects == nil {
		return config, fmt.Errorf("%s: projects must be an object", probeConfigName)
	}
	if err := config.validateFacts(); err != nil {
		return config, fmt.Errorf("%s: %w", probeConfigName, err)
	}
	total := 0
	for scope, rules := range config.Projects {
		if !strings.HasPrefix(scope, "project:") || strings.TrimSpace(scope) != scope || strings.TrimSpace(strings.TrimPrefix(scope, "project:")) == "" {
			return config, fmt.Errorf("%s: expected project:<identifier>", probeConfigName)
		}
		total += len(rules)
		if total > 128 {
			return config, fmt.Errorf("%s: at most 128 rules across projects", probeConfigName)
		}
		seen := make(map[[2]string]bool, len(rules))
		for i, rule := range rules {
			key := [2]string{rule.Tool, rule.MCPTool}
			if seen[key] {
				return config, fmt.Errorf("%s: %s rule %d repeats a selector", probeConfigName, scope, i+1)
			}
			seen[key] = true
			if err := rule.validate(); err != nil {
				return config, fmt.Errorf("%s: %s rule %d: %w", probeConfigName, scope, i+1, err)
			}
			for _, branch := range rule.branches() {
				if err := branch.When.validate(false, config.Facts[scope], 0); err != nil {
					return config, fmt.Errorf("%s: %s rule %d branch %q: %w", probeConfigName, scope, i+1, branch.ID, err)
				}
			}
		}
	}
	return config, nil
}

func (r probeRule) validate() error {
	if err := validateProbeSelector(r.Tool, r.MCPTool); err != nil {
		return err
	}
	if r.Mode != "" && r.Mode != "append" && r.Mode != "replace" {
		return errors.New("mode must be append or replace")
	}
	if len(r.Branches) > 8 || r.Branches != nil && len(r.Branches) == 0 {
		return errors.New("branches requires 1-8 entries")
	}
	if len(r.Branches) > 0 && (r.PathField != "" || len(r.TextFields) > 0 || len(r.Extensions) > 0 || r.When != nil) {
		return errors.New("branches owns the scan fields and when; omit them at tool level")
	}
	seen := map[string]bool{}
	for _, branch := range r.branches() {
		if !probeRuleID.MatchString(branch.ID) || seen[branch.ID] {
			return errors.New("branch id must be a unique name")
		}
		seen[branch.ID] = true
		if err := branch.validateFields(); err != nil {
			return fmt.Errorf("branch %q: %w", branch.ID, err)
		}
	}
	return nil
}

func (r probeRule) branches() []probeBranch {
	if len(r.Branches) != 0 {
		return r.Branches
	}
	return []probeBranch{{ID: "default", When: r.When, PathField: r.PathField, TextFields: r.TextFields, Extensions: r.Extensions}}
}

func validateProbeSelector(tool, mcpTool string) error {
	if (tool == "") == (mcpTool == "") {
		return errors.New("specify exactly one of tool or mcp_tool")
	}
	name := tool
	if mcpTool != "" {
		name = mcpTool
	}
	if !probeToolName.MatchString(name) || (mcpTool != "" && strings.HasPrefix(name, "mcp__")) {
		return errors.New("tool selectors must use literal native tool names or bare MCP tool names")
	}
	if isMemoryProbeName(name) {
		return errors.New("memory tools cannot trigger a custom probe")
	}
	return nil
}

func (r probeBranch) validateFields() error {
	if r.PathField == "" && len(r.TextFields) == 0 {
		return errors.New("specify path_field or text_fields")
	}
	if r.PathField != "" && !probeFieldPath.MatchString(r.PathField) {
		return errors.New("invalid path_field")
	}
	if len(r.TextFields) > 8 || len(r.Extensions) > 8 {
		return errors.New("at most eight text fields and eight extensions")
	}
	fields := map[string]bool{r.PathField: true}
	for _, field := range r.TextFields {
		if !probeFieldPath.MatchString(field) || fields[field] {
			return errors.New("invalid or duplicate text field")
		}
		fields[field] = true
	}
	for _, ext := range r.Extensions {
		if r.PathField == "" || !probeExtension.MatchString(ext) {
			return errors.New("extensions require path_field and literal suffixes such as .sql")
		}
	}
	return nil
}

func isMemoryProbeName(tool string) bool {
	tool = strings.ToLower(tool)
	if strings.HasPrefix(tool, "mcp__") {
		tool = tool[strings.LastIndex(tool, "__")+2:]
	}
	return strings.HasPrefix(tool, "memory_")
}

func (r probeRule) pattern() string {
	if r.Tool != "" {
		return "^" + regexp.QuoteMeta(r.Tool) + "$"
	}
	return "^mcp__.+__" + regexp.QuoteMeta(r.MCPTool) + "$"
}

func (c probeConfig) matcher(server string) string {
	patterns := []string{experienceToolMatcher, businessToolMatcher, lookupToolMatcher(server)}
	custom := c.factPatterns()
	for _, rules := range c.Projects {
		for _, rule := range rules {
			custom[rule.pattern()] = true
		}
	}
	return probeMatchers(patterns, custom)
}

func (c probeConfig) postMatcher(server string) string {
	return probeMatchers([]string{businessToolMatcher, lookupToolMatcher(server)}, c.factPatterns())
}

func (c probeConfig) failureMatcher(server string) string {
	custom := c.factPatterns()
	if len(custom) == 0 {
		return lookupToolMatcher(server)
	}
	return probeMatchers([]string{lookupToolMatcher(server)}, custom)
}

func (c probeConfig) factPatterns() map[string]bool {
	custom := map[string]bool{}
	for _, rules := range c.Facts {
		for _, rule := range rules {
			custom[probeRule{Tool: rule.Tool, MCPTool: rule.MCPTool}.pattern()] = true
		}
	}
	return custom
}

func probeMatchers(patterns []string, custom map[string]bool) string {
	extra := make([]string, 0, len(custom))
	for pattern := range custom {
		extra = append(extra, pattern)
	}
	sort.Strings(extra)
	return "(" + strings.Join(append(patterns, extra...), ")|(") + ")"
}

func (c probeConfig) ruleFor(scope, tool string) (probeRule, bool) {
	if isMemoryProbeName(tool) {
		return probeRule{}, false
	}
	var suffixRule probeRule
	for _, rule := range c.Projects[scope] {
		if rule.Tool != "" && rule.Tool == tool {
			return rule, true
		}
		if rule.MCPTool != "" {
			suffix := "__" + rule.MCPTool
			if strings.HasPrefix(tool, "mcp__") && strings.HasSuffix(tool, suffix) && len(tool) > len("mcp__")+len(suffix) {
				// A longer suffix is more specific if a tool name contains "__".
				if len(rule.MCPTool) > len(suffixRule.MCPTool) {
					suffixRule = rule
				}
			}
		}
	}
	return suffixRule, suffixRule.MCPTool != ""
}

func probeValue(input map[string]any, field string) any {
	var value any = input
	for _, key := range strings.Split(field, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func probePath(root, cwd, path string) (string, string) {
	if path == "" {
		return "", "path_field_unavailable"
	}
	if len(path) > 4096 {
		return "", "path_too_long"
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		if filepath.VolumeName(path) != "" {
			return "", "path_not_resolved"
		}
		path = filepath.Join(cwd, path)
	}
	if !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(path)) {
		return "", "path_outside_project"
	}
	// Write may target a file that does not exist yet. Resolve its nearest
	// existing ancestor so a symlink cannot turn an in-project path into an
	// out-of-project probe.
	ancestor := path
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) || ancestor == root {
			return "", "path_not_resolved"
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", "path_not_resolved"
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", "path_not_resolved"
	}
	suffix, err := filepath.Rel(ancestor, path)
	if err != nil {
		return "", "path_not_resolved"
	}
	relative, err := filepath.Rel(root, filepath.Join(resolved, suffix))
	if err != nil || !filepath.IsLocal(relative) {
		return "", "path_outside_project"
	}
	if relative == "." {
		return "", "path_is_project_root"
	}
	// EvalSymlinks does not identify all Windows reparse points as links.
	// Root.Stat checks traversal from the project directory's actual handle.
	known, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(known) {
		return "", "path_outside_project"
	}
	project, err := os.OpenRoot(root)
	if err != nil {
		return "", "path_not_resolved"
	}
	defer project.Close()
	if _, err := project.Stat(known); err != nil {
		return "", "path_not_accessible_from_project"
	}
	return filepath.ToSlash(relative), ""
}

const customProbeTextLimit = 32 * 1024

type probeInputField struct {
	Field  string `json:"field"`
	Status string `json:"status"`
	Bytes  int    `json:"bytes"`
}

type probeInputReport struct {
	Tool       string            `json:"tool"`
	Branch     string            `json:"branch"`
	Outcome    string            `json:"outcome"`
	PathField  string            `json:"path_field,omitempty"`
	TextFields []probeInputField `json:"text_fields,omitempty"`
}

func (r probeBranch) clues(e Event, root string) ([]string, *probeInputReport) {
	var texts []string
	report := &probeInputReport{Tool: e.Tool, Branch: r.ID, PathField: r.PathField}
	total := 0
	for _, field := range r.TextFields {
		input := probeValue(e.Input, field)
		value, isString := input.(string)
		status := "present"
		switch {
		case input == nil:
			status = "missing"
		case !isString:
			status = "not_string"
		case value == "":
			status = "empty"
		default:
			total += len(value)
			texts = append(texts, value)
		}
		report.TextFields = append(report.TextFields, probeInputField{Field: field, Status: status, Bytes: len(value)})
	}

	if r.PathField != "" {
		value, ok := probeValue(e.Input, r.PathField).(string)
		if !ok {
			report.Outcome = "path_field_unavailable"
			return nil, report
		}
		path, rejected := probePath(root, e.CWD, value)
		if rejected != "" {
			report.Outcome = rejected
			return nil, report
		}
		if len(r.Extensions) > 0 {
			matches := false
			for _, ext := range r.Extensions {
				matches = matches || strings.EqualFold(filepath.Ext(path), ext)
			}
			if !matches {
				report.Outcome = "extension_mismatch"
				return nil, report
			}
		}
		texts = append([]string{path}, texts...)
	}
	if total > customProbeTextLimit {
		report.Outcome = "text_budget_exceeded"
		return nil, report
	}
	for _, field := range report.TextFields {
		if field.Status != "present" {
			report.Outcome = "text_fields_unavailable"
			return texts, report
		}
	}
	report.Outcome = "scanned"
	return texts, report
}
