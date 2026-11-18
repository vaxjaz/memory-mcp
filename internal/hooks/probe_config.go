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
	Projects map[string][]probeRule `json:"projects"`
}

type probeRule struct {
	Tool       string   `json:"tool,omitempty"`
	MCPTool    string   `json:"mcp_tool,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	PathField  string   `json:"path_field,omitempty"`
	TextFields []string `json:"text_fields,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
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
		}
	}
	return config, nil
}

func (r probeRule) validate() error {
	if (r.Tool == "") == (r.MCPTool == "") {
		return errors.New("specify exactly one of tool or mcp_tool")
	}
	name := r.Tool
	if r.MCPTool != "" {
		name = r.MCPTool
	}
	if !probeToolName.MatchString(name) || (r.MCPTool != "" && strings.HasPrefix(name, "mcp__")) {
		return errors.New("tool selectors must use literal native tool names or bare MCP tool names")
	}
	if isMemoryProbeName(name) {
		return errors.New("memory tools cannot trigger a custom probe")
	}
	if r.Mode != "" && r.Mode != "append" && r.Mode != "replace" {
		return errors.New("mode must be append or replace")
	}
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
	custom := map[string]bool{}
	for _, rules := range c.Projects {
		for _, rule := range rules {
			custom[rule.pattern()] = true
		}
	}
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

func probeField(input map[string]any, field string) string {
	var value any = input
	for _, key := range strings.Split(field, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[key]
	}
	text, _ := value.(string)
	return text
}

func probePath(root, cwd, path string) string {
	if path == "" || len(path) > 4096 {
		return ""
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		if filepath.VolumeName(path) != "" {
			return ""
		}
		path = filepath.Join(cwd, path)
	}
	if !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(path)) {
		return ""
	}
	// Write may target a file that does not exist yet. Resolve its nearest
	// existing ancestor so a symlink cannot turn an in-project path into an
	// out-of-project probe.
	ancestor := path
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) || ancestor == root {
			return ""
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return ""
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return ""
	}
	suffix, err := filepath.Rel(ancestor, path)
	if err != nil {
		return ""
	}
	relative, err := filepath.Rel(root, filepath.Join(resolved, suffix))
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return ""
	}
	return filepath.ToSlash(relative)
}

func (r probeRule) clues(e Event, root string) string {
	var parts []string
	if r.PathField != "" {
		path := probePath(root, e.CWD, probeField(e.Input, r.PathField))
		if path == "" {
			return ""
		}
		if len(r.Extensions) > 0 {
			matches := false
			for _, ext := range r.Extensions {
				matches = matches || strings.EqualFold(filepath.Ext(path), ext)
			}
			if !matches {
				return ""
			}
		}
		parts = append(parts, path)
	}
	size := 0
	for _, field := range r.TextFields {
		value := probeField(e.Input, field)
		size += len(value)
		if size > 32768 {
			return ""
		}
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}
