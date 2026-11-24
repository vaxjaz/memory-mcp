package hooks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/matching"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

// UnreachableCue names a cue that no active rule of the project can carry,
// where in the cue the impossible demand sits, and what to do about it.
type UnreachableCue struct {
	Cue   string `json:"cue_id"`
	Where string `json:"where"`
	Why   string `json:"why"`
	Next  string `json:"next"`
}

type demand struct {
	field, value, path string
}

// demands is what one match expression requires of a single tool rule.
// Everything listed must hold for one and the same rule.
type demands struct {
	path   string // where the expression sits inside the cue
	tools  []demand
	fields []demand
	facts  []demand
	// results are fields an observation must expose for a fact's comparison;
	// value is the name of the fact.
	results []demand
	scanned bool // the expression has no condition at all and relies on any rule that scans text
	// vague is set when a condition sits under "any" or "not", or compares a
	// tool name with another operator than eq: nothing can be proven, so the
	// cue is accepted.
	vague bool
}

func (d *demands) walk(e *matching.Expression, path string) {
	if e == nil {
		return
	}
	d.walk(e.When, path+".when")
	if e.Fact != nil {
		d.facts = append(d.facts, demand{"", e.Fact.Name, path + ".fact"})
		// The key names input values, which the tool carrying this cue must expose.
		for _, key := range e.Fact.Key {
			d.fields = append(d.fields, demand{key, "", path + ".fact.key"})
		}
		// The comparison names result values, which the observation itself exposes.
		d.walkResult(e.Fact.Match, e.Fact.Name, path+".fact.match")
	}
	if e.Field != "" {
		item := demand{e.Field, fmt.Sprint(e.Value), path}
		switch {
		case (e.Field == "tool" || e.Field == "mcp_tool") && e.Op != "eq":
			d.vague = true
		case e.Field == "tool" || e.Field == "mcp_tool":
			d.tools = append(d.tools, item)
		default:
			d.fields = append(d.fields, item)
		}
	}
	for i, child := range e.All {
		d.walk(child, fmt.Sprintf("%s.all[%d]", path, i))
	}
	for _, child := range e.Any {
		if conditions(child) {
			d.vague = true
		}
	}
	if conditions(e.Not) {
		d.vague = true
	}
}

// walkResult collects the result fields a fact's comparison depends on.
func (d *demands) walkResult(e *matching.Expression, fact, path string) {
	if e == nil {
		return
	}
	d.walkResult(e.When, fact, path+".when")
	if e.Field != "" {
		d.results = append(d.results, demand{e.Field, fact, path})
	}
	for i, child := range e.All {
		d.walkResult(child, fact, fmt.Sprintf("%s.all[%d]", path, i))
	}
	for _, child := range e.Any {
		if conditions(child) {
			d.vague = true
		}
	}
	if conditions(e.Not) {
		d.vague = true
	}
}

// conditions reports whether an expression or anything below it compares a
// field or reads a result fact. Entrances alone are not conditions.
func conditions(e *matching.Expression) bool {
	if e == nil {
		return false
	}
	return e.Field != "" || e.Fact != nil || conditions(e.When) || conditions(e.Not) || e.Fact != nil && conditions(e.Fact.Match) ||
		some(func(child *matching.Expression) bool { return conditions(child) }, e.All) ||
		some(func(child *matching.Expression) bool { return conditions(child) }, e.Any)
}

func some(keep func(*matching.Expression) bool, items []*matching.Expression) bool {
	for _, item := range items {
		if keep(item) {
			return true
		}
	}
	return false
}

// effective lists the rules that apply in one project: its own rules and the
// global rules it does not replace.
func (c probeConfig) effective(scope string) []probeRule {
	replaced := map[string]bool{}
	for _, r := range c.Projects[scope] {
		replaced[selector(r)] = true
	}
	var rules []probeRule
	for _, r := range c.globalRules() {
		if !replaced[selector(r)] {
			rules = append(rules, r)
		}
	}
	return append(rules, c.Projects[scope]...)
}

func selector(r probeRule) string {
	if r.MCPTool != "" {
		return "mcp:" + r.MCPTool
	}
	return r.Tool
}

// unreachable explains why a cue can never fire in a project, or returns nil
// when some active rule could carry it or when that cannot be decided.
func (c probeConfig) unreachable(scope string, cue memory.ExperienceCue) *UnreachableCue {
	var first *UnreachableCue
	for k, expression := range cue.Matches {
		d := demands{path: fmt.Sprintf(".matches[%d]", k)}
		d.walk(&expression, d.path)
		if !d.vague && len(d.tools) == 0 && len(d.fields) == 0 && len(d.facts) == 0 && len(d.results) == 0 {
			d.scanned = true
		}
		if issue := c.uncarried(scope, d); issue != nil {
			if first == nil {
				first = issue
			}
			continue
		}
		return nil // one expression that can fire is enough
	}
	if first == nil {
		return nil
	}
	first.Cue = cue.ID
	return first
}

const reachabilityNext = "Correct the cue, or register the tool in tool-probes.json and run setup."

func (c probeConfig) uncarried(scope string, d demands) *UnreachableCue {
	if d.vague {
		return nil
	}
	rules := c.effective(scope)
	issue := &UnreachableCue{Next: reachabilityNext}
	// Narrow the rules by each demand in turn; whichever demand leaves none explains the cue.
	narrow := func(keep func(probeRule) bool) bool {
		var kept []probeRule
		for _, r := range rules {
			if keep(r) {
				kept = append(kept, r)
			}
		}
		rules = kept
		return len(kept) > 0
	}
	for _, tool := range d.tools {
		if narrow(func(r probeRule) bool {
			return tool.field == "tool" && r.Tool == tool.value || tool.field == "mcp_tool" && r.MCPTool == tool.value
		}) {
			continue
		}
		issue.Where, issue.Why = tool.path, fmt.Sprintf("no rule in tool-probes.json registers a tool named %q", tool.value)
		for _, r := range c.effective(scope) {
			if tool.field == "tool" && r.MCPTool == tool.value {
				issue.Why += fmt.Sprintf("; %q is registered as an MCP tool, so name it with {\"field\":\"mcp_tool\",\"op\":\"eq\",\"value\":%q}", tool.value, tool.value)
			} else if tool.field == "mcp_tool" && r.Tool == tool.value {
				issue.Why += fmt.Sprintf("; %q is registered as a native tool, so name it with {\"field\":\"tool\",\"op\":\"eq\",\"value\":%q}", tool.value, tool.value)
			}
		}
		return issue
	}
	for _, field := range d.fields {
		if narrow(func(r probeRule) bool { return exposes(r, field.field) }) {
			continue
		}
		issue.Where = field.path
		issue.Why = fmt.Sprintf("no rule %s exposes a field named %q", under(d.tools), field.field)
		// Name only rules that are switched on; a disabled one is no source yet.
		var elsewhere []string
		for _, r := range c.effective(scope) {
			if r.active() && exposes(r, field.field) {
				elsewhere = append(elsewhere, ruleName(r))
			}
		}
		if len(elsewhere) > 0 {
			sort.Strings(elsewhere)
			issue.Why += fmt.Sprintf("; it is available from %s", strings.Join(elsewhere, ", "))
		}
		return issue
	}
	observed := map[string]probeObservation{}
	for _, r := range c.Projects[scope] {
		for _, o := range r.Observe {
			observed[o.Name] = o
		}
	}
	for _, fact := range d.facts {
		if _, ok := observed[fact.value]; !ok {
			issue.Where, issue.Why = fact.path, fmt.Sprintf("no rule of this project observes a result named %q", fact.value)
			return issue
		}
	}
	for _, result := range d.results {
		if _, ok := observed[result.value].Fields[result.field]; !ok {
			issue.Where = result.path
			issue.Why = fmt.Sprintf("the observation %q exposes the results %s, not %q", result.value, quotedKeys(observed[result.value].Fields), result.field)
			return issue
		}
	}
	// A bare entrance is looked for in every text a rule hands over, a path included.
	var active []probeRule
	for _, r := range rules {
		if r.active() && (!d.scanned || scansText(r)) {
			active = append(active, r)
		}
	}
	if len(active) > 0 {
		return nil
	}
	issue.Where = d.path
	var names []string
	for _, r := range rules {
		names = append(names, ruleName(r))
	}
	sort.Strings(names)
	switch {
	case d.scanned:
		issue.Why = "no active rule scans any text in this project, so a bare entrance could never be looked for"
	case len(rules) == 0:
		issue.Why = "no active rule is left for this cue"
	default:
		issue.Why = fmt.Sprintf("%s is listed with \"enabled\": false, so this cue could never fire", strings.Join(names, ", "))
		issue.Next = "Set it to true in tool-probes.json, run setup, then refresh the client."
	}
	return issue
}

func quotedKeys(fields map[string]string) string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, fmt.Sprintf("%q", name))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// exposes reports whether a rule hands a named value to cues.
func exposes(r probeRule, name string) bool {
	if name == "tool" || name == "mcp_tool" {
		return true
	}
	_, ok := r.Fields[name]
	return ok || r.business() && name == "text"
}

// scansText reports whether a rule hands over any text an entrance could appear in.
func scansText(r probeRule) bool {
	return r.business() || len(r.Fields) > 0
}

func under(tools []demand) string {
	if len(tools) == 0 {
		return "in this project"
	}
	return fmt.Sprintf("for %s", strings.Join(toolNames(tools), " or "))
}

func toolNames(tools []demand) []string {
	var names []string
	for _, tool := range tools {
		if tool.field == "mcp_tool" {
			names = append(names, "mcp:...:"+tool.value)
		} else {
			names = append(names, tool.value)
		}
	}
	return names
}

// RejectUnreachableCues refuses a write that carries a cue no active rule of
// the project could ever fire. A cue is refused only when that is certain;
// anything that cannot be decided is accepted. An unreadable tool-probes.json
// proves nothing, so the write is left to the other validations.
func RejectUnreachableCues(dir, scope, kind string, cues []memory.ExperienceCue) error {
	config, err := loadProbeConfig(dir)
	if err != nil {
		return nil
	}
	var issues []string
	for i, cue := range cues {
		if issue := config.unreachable(scope, cue); issue != nil {
			at := fmt.Sprintf("%s.cues[%d]", kind, i)
			if issue.Where != "" {
				at += issue.Where
			}
			issues = append(issues, fmt.Sprintf("%s: %s. %s", at, issue.Why, issue.Next))
		}
	}
	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("invalid record (%d issues, no write performed):\n- %s", len(issues), strings.Join(issues, "\n- "))
}

// UnreachableCues lists the stored cues of a project that could never fire,
// so an upgrade can audit what earlier writes left behind.
func UnreachableCues(ctx context.Context, store *memory.Store, scope string) ([]UnreachableCue, error) {
	config, err := loadProbeConfig(store.Dir)
	if err != nil {
		// doctor already reports the unreadable file; nothing can be proven from it.
		return []UnreachableCue{}, nil
	}
	entries, err := store.Experiences(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := []UnreachableCue{}
	for _, entry := range entries {
		var cues []memory.ExperienceCue
		switch {
		case entry.Profile != nil && entry.Profile.Experience != nil:
			cues = entry.Profile.Experience.Cues
		case entry.Profile != nil && entry.Profile.Agreement != nil:
			cues = entry.Profile.Agreement.Cues
		}
		for _, cue := range cues {
			if issue := config.unreachable(scope, cue); issue != nil {
				out = append(out, UnreachableCue{Cue: issue.Cue, Where: issue.Where, Why: issue.Why, Next: issue.Next})
			}
		}
	}
	return out, nil
}
