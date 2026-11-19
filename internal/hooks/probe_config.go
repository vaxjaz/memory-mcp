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

	"github.com/vaxjaz/memory-mcp/internal/matching"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

const probeConfigName = "tool-probes.json"
const customProbeTextLimit = 32 * 1024

type probeConfig struct {
	Projects map[string][]probeRule `json:"projects"`
}

type probeRule struct {
	Tool    string             `json:"tool,omitempty"`
	MCPTool string             `json:"mcp_tool,omitempty"`
	Enabled *bool              `json:"enabled,omitempty"`
	Fields  map[string]string  `json:"fields,omitempty"`
	Observe []probeObservation `json:"observe,omitempty"`
	preset  string
}

var (
	probeToolName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	probeFieldPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*){0,5}$`)
)

func loadProbeConfig(dir string) (probeConfig, error) {
	var config probeConfig
	file, err := os.Open(filepath.Join(dir, probeConfigName))
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	defer file.Close()
	const limit = 64 * 1024
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return config, err
	}
	if len(raw) > limit {
		return config, fmt.Errorf("%s exceeds %d bytes", probeConfigName, limit)
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if err := textguard.JSON(raw); err != nil {
		return config, fmt.Errorf("%s: %w", probeConfigName, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		return config, fmt.Errorf("%s: %w. Use fields to map input names and put match conditions in the cue; see memory-mcp guide", probeConfigName, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return config, fmt.Errorf("%s: expected exactly one JSON object", probeConfigName)
	}
	if config.Projects == nil {
		return config, fmt.Errorf("%s: projects must be an object", probeConfigName)
	}
	total := 0
	for scope, rules := range config.Projects {
		if !strings.HasPrefix(scope, "project:") || strings.TrimSpace(scope) != scope || len(scope) <= len("project:") {
			return config, fmt.Errorf("%s: expected project:<identifier>", probeConfigName)
		}
		total += len(rules)
		if total > 128 {
			return config, fmt.Errorf("%s: at most 128 rules across projects", probeConfigName)
		}
		seen := map[[2]string]bool{}
		observations := map[string]bool{}
		for idx, rule := range rules {
			if err := rule.validate(observations); err != nil {
				return config, fmt.Errorf("%s: %s tool %d: %w", probeConfigName, scope, idx+1, err)
			}
			key := [2]string{rule.Tool, rule.MCPTool}
			if seen[key] {
				return config, fmt.Errorf("%s: %s tool %d repeats a selector", probeConfigName, scope, idx+1)
			}
			seen[key] = true
		}
	}
	return config, nil
}

func (r probeRule) active() bool { return r.Enabled == nil || *r.Enabled }
func (r probeRule) inspectionOnly() bool {
	return r.preset == "" && len(r.Fields) == 0 && len(r.Observe) == 0
}

func (r probeRule) validate(observations map[string]bool) error {
	if (r.Tool == "") == (r.MCPTool == "") {
		return errors.New("specify exactly one of tool or mcp_tool")
	}
	name := r.Tool
	if r.MCPTool != "" {
		name = r.MCPTool
	}
	if !probeToolName.MatchString(name) || (r.MCPTool != "" && strings.HasPrefix(name, "mcp__")) || isMemoryProbeName(name) {
		return errors.New("use a native tool name or a bare non-memory MCP tool name")
	}
	if len(r.Fields) > 8 || len(r.Observe) > 8 {
		return errors.New("at most eight fields and eight observations")
	}
	for name, path := range r.Fields {
		if !matching.FieldName.MatchString(name) || name == "tool" || !probeFieldPath.MatchString(path) {
			return fmt.Errorf("invalid field mapping %q", name)
		}
	}
	for _, o := range r.Observe {
		if err := o.validate(); err != nil {
			return err
		}
		if observations[o.Name] {
			return fmt.Errorf("duplicate observation name %q", o.Name)
		}
		observations[o.Name] = true
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

func selectTool(rules []probeRule, tool string) (probeRule, bool) {
	var suffix probeRule
	for _, rule := range rules {
		if rule.Tool != "" && rule.Tool == tool {
			return rule, true
		}
		if rule.MCPTool != "" {
			ending := "__" + rule.MCPTool
			if strings.HasPrefix(tool, "mcp__") && strings.HasSuffix(tool, ending) &&
				len(tool) > len("mcp__")+len(ending) && len(rule.MCPTool) > len(suffix.MCPTool) {
				suffix = rule
			}
		}
	}
	return suffix, suffix.MCPTool != ""
}

// One effective adapter. An explicit project entry takes priority over a preset.
func (c probeConfig) ruleFor(scope, tool string) (probeRule, bool) {
	if isMemoryProbeName(tool) {
		return probeRule{}, false
	}
	if rule, ok := selectTool(c.Projects[scope], tool); ok {
		return rule, true
	}
	return selectTool(defaultProbeRules, tool)
}

func (c probeConfig) routePatterns(observeOnly bool) map[string]bool {
	patterns := map[string]bool{}
	for _, r := range defaultProbeRules {
		if r.active() && (!observeOnly || r.preset == "business") {
			patterns[r.pattern()] = true
		}
	}
	for _, rules := range c.Projects {
		for _, r := range rules {
			if r.active() && (!observeOnly || len(r.Observe) > 0 || r.inspectionOnly()) {
				patterns[r.pattern()] = true
			}
		}
	}
	return patterns
}
func (c probeConfig) matcher(server string) string {
	return probeMatchers([]string{lookupToolMatcher(server)}, c.routePatterns(false))
}
func (c probeConfig) postMatcher(server string) string {
	return probeMatchers([]string{lookupToolMatcher(server)}, c.routePatterns(true))
}
func (c probeConfig) failureMatcher(server string) string {
	names := map[string]bool{}
	for _, rules := range c.Projects {
		for _, r := range rules {
			if r.active() && len(r.Observe) > 0 {
				names[r.pattern()] = true
			}
		}
	}
	if len(names) == 0 {
		return lookupToolMatcher(server)
	}
	return probeMatchers([]string{lookupToolMatcher(server)}, names)
}

func probeMatchers(patterns []string, custom map[string]bool) string {
	extra := make([]string, 0, len(custom))
	for pattern := range custom {
		extra = append(extra, pattern)
	}
	sort.Strings(extra)
	return "(" + strings.Join(append(patterns, extra...), ")|(") + ")"
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

func (c probeConfig) observationCount(scope string) int {
	count := 0
	for _, r := range c.Projects[scope] {
		count += len(r.Observe)
	}
	return count
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
