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

// One mapped string may be this long, and one event may expose this much text
// in total. Beyond the limit the value is left out and reported, never trimmed.
const customProbeTextLimit = 32 * 1024
const probeEventTextLimit = 64 * 1024
const probeElementLimit = 64

// probeConfig is the whole tool-probes.json. Tools apply to every project;
// a rule in Projects replaces the rule for the same tool in one project.
type probeConfig struct {
	Tools    []probeRule            `json:"tools,omitempty"`
	Projects map[string][]probeRule `json:"projects"`
	// toolsImplicit holds while the file has no "tools" section: an untouched
	// file keeps the shipped defaults until setup writes them out.
	toolsImplicit bool
}

type probeRule struct {
	Tool    string                 `json:"tool,omitempty"`
	MCPTool string                 `json:"mcp_tool,omitempty"`
	Enabled *bool                  `json:"enabled,omitempty"`
	Fields  map[string]fieldSource `json:"fields,omitempty"`
	Observe []probeObservation     `json:"observe,omitempty"`
	// Scan limits a shell-like rule to the listed command names.
	Scan []string `json:"scan,omitempty"`
	// Discover names a built-in discovery strategy; "query" finds query
	// statements by their known argument names.
	Discover string `json:"discover,omitempty"`
}

// fieldSource says where a mapped value comes from: one input path, or several
// candidates of which the first present one is used.
type fieldSource struct {
	Paths []string
}

func (f *fieldSource) UnmarshalJSON(raw []byte) error {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		f.Paths = []string{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil || len(many) == 0 {
		return errors.New("map a field to one input path or to a list of candidate paths")
	}
	f.Paths = many
	return nil
}

func (f fieldSource) MarshalJSON() ([]byte, error) {
	if len(f.Paths) == 1 {
		return json.Marshal(f.Paths[0])
	}
	return json.Marshal(f.Paths)
}

var (
	probeToolName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	probeFieldPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.([A-Za-z_][A-Za-z0-9_]*|\d+|\*)){0,7}$`)
	// Observation keys and result fields name one value, so "*" is not allowed.
	probeObjectPath  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*){0,5}$`)
	probeCommandName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)
	probePathHint    = `use dotted object keys, an array index or * for every element, for example arguments.statement or questions.*.question`
)

func loadProbeConfig(dir string) (probeConfig, error) {
	var config probeConfig
	file, err := os.Open(filepath.Join(dir, probeConfigName))
	if errors.Is(err, os.ErrNotExist) {
		config.toolsImplicit = true
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
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &shape); err != nil {
		return config, fmt.Errorf("%s: expected one JSON object with tools and projects", probeConfigName)
	}
	_, hasTools := shape["tools"]
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		return config, fmt.Errorf("%s: %w. Use tools for rules that apply to every project and projects.<scope> for one project; map input names under fields and put match conditions in the cue; see memory-mcp guide", probeConfigName, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return config, fmt.Errorf("%s: expected exactly one JSON object", probeConfigName)
	}
	if config.Projects == nil {
		config.Projects = map[string][]probeRule{}
	}
	config.toolsImplicit = !hasTools
	total := 0
	seenTools := map[[2]string]bool{}
	for idx, rule := range config.Tools {
		if err := rule.validate(map[string]bool{}, false); err != nil {
			return config, fmt.Errorf("%s: tools rule %d: %w", probeConfigName, idx+1, err)
		}
		key := [2]string{rule.Tool, rule.MCPTool}
		if seenTools[key] {
			return config, fmt.Errorf("%s: tools rule %d repeats a selector", probeConfigName, idx+1)
		}
		seenTools[key] = true
	}
	total = len(config.Tools)
	for scope, rules := range config.Projects {
		if !strings.HasPrefix(scope, "project:") || strings.TrimSpace(scope) != scope || len(scope) <= len("project:") {
			return config, fmt.Errorf("%s: expected project:<identifier>", probeConfigName)
		}
		total += len(rules)
		if total > 128 {
			return config, fmt.Errorf("%s: at most 128 rules in total", probeConfigName)
		}
		seen := map[[2]string]bool{}
		observations := map[string]bool{}
		for idx, rule := range rules {
			if err := rule.validate(observations, true); err != nil {
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

// inspectionOnly rules record field shapes and never scan text.
func (r probeRule) inspectionOnly() bool {
	return r.Discover == "" && len(r.Fields) == 0 && len(r.Observe) == 0
}

func (r probeRule) business() bool { return r.Discover == "query" }

// validate checks one rule. Observations belong to a project rule: they are
// looked up per project, so a global one would never be found.
func (r probeRule) validate(observations map[string]bool, project bool) error {
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
	if r.Discover != "" && r.Discover != "query" {
		return errors.New(`discover is "query" or omitted`)
	}
	if r.Discover != "" && (len(r.Fields) > 0 || len(r.Scan) > 0) {
		return errors.New("discover provides its own fields; omit fields and scan")
	}
	for _, command := range r.Scan {
		if !probeCommandName.MatchString(command) {
			return fmt.Errorf("scan lists command names such as grep or head, got %q", command)
		}
	}
	if len(r.Scan) > 0 {
		if _, ok := r.Fields["text"]; !ok {
			return errors.New("scan needs a field named text that holds the command")
		}
	}
	for alias, source := range r.Fields {
		if !matching.FieldName.MatchString(alias) || alias == "tool" || alias == "mcp_tool" {
			return fmt.Errorf("invalid field mapping %q: use a name made of letters, digits or underscores other than tool and mcp_tool", alias)
		}
		seen := map[string]bool{}
		for _, path := range source.Paths {
			if !probeFieldPath.MatchString(path) {
				return fmt.Errorf("invalid field mapping %q: path %q; %s", alias, path, probePathHint)
			}
			if seen[path] {
				return fmt.Errorf("invalid field mapping %q: path %q is listed twice", alias, path)
			}
			seen[path] = true
		}
	}
	if !project && len(r.Observe) > 0 {
		return errors.New("observe is looked up per project; put this rule under projects.<scope>")
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

// globalRules are the rules that apply to every project.
func (c probeConfig) globalRules() []probeRule {
	if c.Tools != nil {
		return c.Tools
	}
	return defaultToolRules()
}

// ruleFor selects the one effective rule for a tool in a project: a project
// rule replaces the global rule for the same tool.
func (c probeConfig) ruleFor(scope, tool string) (probeRule, bool) {
	if isMemoryProbeName(tool) {
		return probeRule{}, false
	}
	if rule, ok := selectTool(c.Projects[scope], tool); ok {
		return rule, true
	}
	return selectTool(c.globalRules(), tool)
}

// ruleSource names where the rule in effect for a tool comes from, with the
// words probe_surface uses, so a report never attributes a call to a rule
// that a project rule had replaced.
func (c probeConfig) ruleSource(scope, tool string) string {
	if _, ok := selectTool(c.Projects[scope], tool); ok {
		return "project"
	}
	if c.toolsImplicit {
		return "default"
	}
	return "tools"
}

func (c probeConfig) routePatterns(observeOnly bool) map[string]bool {
	patterns := map[string]bool{}
	for _, r := range c.globalRules() {
		if r.active() && (!observeOnly || r.business()) {
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

func (c probeConfig) observationCount(scope string) int {
	count := 0
	for _, r := range c.Projects[scope] {
		count += len(r.Observe)
	}
	return count
}

func probeMatchers(patterns []string, custom map[string]bool) string {
	extra := make([]string, 0, len(custom))
	for pattern := range custom {
		extra = append(extra, pattern)
	}
	sort.Strings(extra)
	return "(" + strings.Join(append(patterns, extra...), ")|(") + ")"
}

// surface lists the effective rules of one project, each with its source, so
// an author can see which tools are scanned and which names a cue may use.
func (c probeConfig) surface(scope string) []map[string]any {
	source := "tools"
	if c.toolsImplicit {
		source = "default"
	}
	overridden := map[[2]string]bool{}
	for _, r := range c.Projects[scope] {
		overridden[[2]string{r.Tool, r.MCPTool}] = true
	}
	var out []map[string]any
	for _, group := range []struct {
		rules  []probeRule
		source string
		skip   func(probeRule) bool
	}{{c.globalRules(), source, func(r probeRule) bool { return overridden[[2]string{r.Tool, r.MCPTool}] }},
		{c.Projects[scope], "project", func(probeRule) bool { return false }}} {
		for _, r := range group.rules {
			if group.skip(r) {
				continue
			}
			out = append(out, describeRule(r, group.source))
		}
	}
	return out
}

func describeRule(r probeRule, source string) map[string]any {
	name := r.Tool
	if r.MCPTool != "" {
		name = "mcp:*:" + r.MCPTool
	}
	item := map[string]any{"tool": name, "source": source, "enabled": r.active()}
	if len(r.Fields) > 0 {
		fields := map[string]any{}
		for alias, s := range r.Fields {
			fields[alias] = s.Paths
		}
		item["fields"] = fields
	}
	if len(r.Scan) > 0 {
		item["scan"] = r.Scan
	}
	if r.business() {
		item["discover"] = r.Discover
	}
	if len(r.Observe) > 0 {
		item["observations"] = len(r.Observe)
	}
	return item
}
