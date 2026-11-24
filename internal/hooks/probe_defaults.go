package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// toolRulesSeed is what setup did, or with --dry-run would do, to the rules
// that apply to every project.
type toolRulesSeed struct {
	Created bool     `json:"created"`
	Added   []string `json:"added"`
	Backup  string   `json:"backup,omitempty"`
}

func ruleName(rule probeRule) string {
	if rule.MCPTool != "" {
		return "mcp:" + rule.MCPTool
	}
	return rule.Tool
}

// seedToolRules creates tool-probes.json on a first setup and tops up its
// "tools" section with default tools that are missing. An existing entry is
// never changed; a default that was removed is listed again, so a tool is
// switched off with "enabled": false rather than by deleting it. The file is
// backed up before it is rewritten.
func seedToolRules(dir string, dryRun bool) (toolRulesSeed, error) {
	seed := toolRulesSeed{Added: []string{}}
	path := filepath.Join(dir, probeConfigName)
	raw, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return seed, err
	}
	defaults := defaultToolRules()
	if missing {
		seed.Created = true
		for _, rule := range defaults {
			seed.Added = append(seed.Added, ruleName(rule))
		}
		if dryRun {
			return seed, nil
		}
		return seed, writeFileAtomic(path, renderProbeConfig(defaults, map[string]json.RawMessage{"projects": []byte("{}")}))
	}
	// The caller validates the file first; here it is only reshaped.
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &shape); err != nil {
		return seed, err
	}
	var tools []probeRule
	listed, hasTools := shape["tools"]
	if hasTools {
		if err := json.Unmarshal(listed, &tools); err != nil {
			return seed, err
		}
	}
	present := map[string]bool{}
	for _, rule := range tools {
		present[ruleName(rule)] = true
	}
	for _, rule := range defaults {
		if name := ruleName(rule); !present[name] {
			tools = append(tools, rule)
			seed.Added = append(seed.Added, name)
		}
	}
	if len(seed.Added) == 0 || dryRun {
		return seed, nil
	}
	if seed.Backup, err = backupFile(path, raw); err != nil {
		return seed, err
	}
	delete(shape, "tools")
	return seed, writeFileAtomic(path, renderProbeConfig(tools, shape))
}
