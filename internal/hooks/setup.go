package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

const ManagedID = "decision-memory-hooks-v1"

type SetupOptions struct {
	Config, DataDir, Project, Scope, Binary string
	ServerName                              string
	Capture, Review, DryRun                 bool
}

func object(m map[string]any, key string) (map[string]any, error) {
	if v, ok := m[key]; ok {
		r, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be a JSON object; refusing to replace existing content", key)
		}
		return r, nil
	}
	r := map[string]any{}
	m[key] = r
	return r, nil
}
func readConfig(p string) (map[string]any, []byte, error) {
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var c map[string]any
	if err = json.Unmarshal(raw, &c); err != nil {
		return nil, nil, err
	}
	if c == nil {
		return nil, nil, errors.New("config must be a JSON object")
	}
	return c, raw, nil
}
func stringArgs(value any) []string {
	out := []string{}
	if a, ok := value.([]any); ok {
		for _, v := range a {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func managed(h map[string]any, name string) bool {
	a := stringArgs(h["args"])
	for i := 0; i+1 < len(a); i++ {
		if a[i] == "--managed-id" && (a[i+1] == ManagedID+"|"+name || a[i+1] == ManagedID && name == "decision-memory") {
			return true
		}
	}
	// Supersede only the exact probe previously distributed by this project.
	return name == "decision-memory" && len(a) > 1 && filepath.Base(a[0]) == "zcode-hook-probe.mjs" && a[1] == "hook"
}
func configData(entry map[string]any) string {
	a := stringArgs(entry["args"])
	for i := 0; i < len(a); i++ {
		if a[i] == "--data-dir" && i+1 < len(a) {
			return a[i+1]
		}
		if strings.HasPrefix(a[i], "--data-dir=") {
			return strings.TrimPrefix(a[i], "--data-dir=")
		}
	}
	if env, ok := entry["env"].(map[string]any); ok {
		if d, ok := env["MEMORY_MCP_DATA_DIR"].(string); ok {
			return d
		}
	}
	return ""
}
func writeFileAtomic(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".memory-mcp-config-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
func backupFile(p string, raw []byte) (string, error) {
	if raw == nil {
		return "", nil
	}
	dest := p + ".memory-mcp-backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(raw)
	if e := f.Close(); err == nil {
		err = e
	}
	return dest, err
}
func Setup(ctx context.Context, o SetupOptions) (map[string]any, error) {
	if !o.Capture {
		return nil, errors.New("setup requires --capture-user-text to explicitly enable raw input/answer collection for bound projects; read docs/ZCODE_HOOKS.md first")
	}
	if o.Project == "" || o.Scope == "" {
		return nil, errors.New("setup requires --project DIR --scope project:<identifier>")
	}
	if err := memory.Validate(memory.Draft{Kind: "record", Scope: o.Scope, Title: "scope check", Body: "scope check", Origin: "host_quote"}); err != nil || o.Scope == "global" {
		return nil, errors.New("valid project scope required; global is not inferred")
	}
	var err error
	o.Config, err = filepath.Abs(o.Config)
	if err != nil {
		return nil, err
	}
	o.Project, err = filepath.Abs(o.Project)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(o.Project)
	if err != nil || !info.IsDir() {
		return nil, errors.New("project must be an existing directory")
	}
	rel, e := filepath.Rel(o.Project, o.Config)
	if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return nil, errors.New("user configuration must be outside the project; project files are never modified")
	}
	c, raw, err := readConfig(o.Config)
	if err != nil {
		return nil, err
	}
	mcp, err := object(c, "mcp")
	if err != nil {
		return nil, err
	}
	servers, err := object(mcp, "servers")
	if err != nil {
		return nil, err
	}
	// Native MCP config suppresses .agents fallback. Preserve those effective servers.
	if len(servers) == 0 {
		home, er := os.UserHomeDir()
		if er != nil {
			return nil, er
		}
		fallback := filepath.Join(home, ".agents", "mcp.json")
		if b, er := os.ReadFile(fallback); er == nil {
			var f map[string]any
			if er = json.Unmarshal(b, &f); er != nil {
				return nil, fmt.Errorf("invalid .agents MCP fallback: %w", er)
			}
			if old, ok := f["mcpServers"].(map[string]any); ok {
				for k, v := range old {
					servers[k] = v
				}
			}
		} else if !os.IsNotExist(er) {
			return nil, er
		}
	}
	name := "decision-memory"
	if o.ServerName != "" {
		name = o.ServerName
		if len(name) > 80 || strings.ContainsAny(name, " /\\|\n\r") {
			return nil, errors.New("invalid MCP server name")
		}
	}
	if _, ok := servers[name]; !ok && o.ServerName == "" {
		matches := []string{}
		for k, v := range servers {
			if ent, ok := v.(map[string]any); ok {
				cmd, _ := ent["command"].(string)
				base := strings.TrimSuffix(strings.ToLower(filepath.Base(cmd)), ".exe")
				if base == "memory-mcp" {
					matches = append(matches, k)
				}
			}
		}
		if len(matches) > 1 {
			return nil, errors.New("multiple existing memory-mcp servers; select and reconcile them before setup")
		}
		if len(matches) == 1 {
			name = matches[0]
		}
	}
	entry := map[string]any{}
	if old, ok := servers[name]; ok {
		var valid bool
		entry, valid = old.(map[string]any)
		if !valid {
			return nil, errors.New("existing memory MCP config is not an object")
		}
	}
	oldDir := configData(entry)
	if entry["url"] != nil {
		return nil, errors.New("existing memory server uses a remote URL; refusing to overwrite its transport")
	}
	if oldDir != "" {
		if !filepath.IsAbs(oldDir) {
			return nil, errors.New("existing memory data directory is relative; resolve it explicitly before setup")
		}
		if o.DataDir != "" {
			a, _ := filepath.Abs(o.DataDir)
			b, _ := filepath.Abs(oldDir)
			if !strings.EqualFold(a, b) {
				return nil, errors.New("--data-dir conflicts with existing MCP data; refusing to redirect or abandon it")
			}
		}
		o.DataDir = oldDir
	}
	if o.DataDir == "" {
		o.DataDir, err = memory.DefaultDir()
		if err != nil {
			return nil, err
		}
	}
	o.DataDir, err = filepath.Abs(o.DataDir)
	if err != nil {
		return nil, err
	}
	o.Binary, err = filepath.Abs(o.Binary)
	if err != nil {
		return nil, err
	}
	h, err := object(c, "hooks")
	if err != nil {
		return nil, err
	}
	events, err := object(h, "events")
	if err != nil {
		return nil, err
	}
	if v, ok := h["enabled"]; ok {
		if _, valid := v.(bool); !valid {
			return nil, errors.New("hooks.enabled must be a boolean; refusing to replace existing content")
		}
	}
	if enabled, ok := h["enabled"].(bool); ok && !enabled && len(events) > 0 {
		return nil, errors.New("user hooks are globally disabled; setup will not enable unrelated hooks; enable/reconcile them in ZCode first")
	}
	// Remove only our entries; retain matchers and handlers owned by others.
	for event, v := range events {
		groups, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("hooks.events.%s must be an array", event)
		}
		kept := []any{}
		for _, v := range groups {
			g, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("invalid existing hook group")
			}
			handlers, ok := g["hooks"].([]any)
			if !ok {
				return nil, errors.New("invalid existing hook handlers")
			}
			rest := []any{}
			for _, v := range handlers {
				handler, ok := v.(map[string]any)
				if !ok {
					return nil, errors.New("invalid existing hook handler")
				}
				if !managed(handler, name) {
					rest = append(rest, handler)
				}
			}
			if len(rest) > 0 {
				g["hooks"] = rest
				kept = append(kept, g)
			}
		}
		events[event] = kept
	}
	args := []any{"hook", "--host", "zcode", "--data-dir", o.DataDir, "--managed-id", ManagedID + "|" + name, "--server-name", name, "--capture-user-text"}
	if o.Review {
		args = append(args, "--review-on-stop")
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "Stop"} {
		group := map[string]any{"hooks": []any{map[string]any{"type": "process", "command": o.Binary, "args": args, "enabled": true, "timeoutMs": 10000}}}
		if event == "PreToolUse" {
			group["matcher"] = "Write|Edit|apply_patch"
		}
		existing, _ := events[event].([]any)
		events[event] = append(existing, group)
	}
	h["enabled"] = true
	entry["command"] = o.Binary
	entry["args"] = []string{"serve", "--data-dir", o.DataDir}
	entry["enable"] = true
	servers[name] = entry
	report := map[string]any{"config": o.Config, "data_dir": o.DataDir, "project": o.Project, "scope": o.Scope, "mcp_server": name, "review_on_stop": o.Review, "raw_capture": true, "host_event_verified": false, "restart_required": true, "dry_run": o.DryRun}
	if o.DryRun {
		return report, nil
	}
	if err = os.MkdirAll(filepath.Dir(o.Config), 0700); err != nil {
		return nil, err
	}
	backup, err := backupFile(o.Config, raw)
	if err != nil {
		return nil, err
	}
	report["config_backup"] = backup
	store, err := memory.Open(o.DataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	backups := filepath.Join(o.DataDir, "backups")
	if err = os.MkdirAll(backups, 0700); err != nil {
		return nil, err
	}
	snapshot := filepath.Join(backups, "before-hook-setup-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".db")
	if err = store.Backup(ctx, snapshot); err != nil {
		return nil, err
	}
	report["database_backup"] = snapshot
	if err = store.HookBind(ctx, o.Project, o.Scope); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if err = writeFileAtomic(o.Config, b); err != nil {
		return nil, err
	}
	return report, nil
}
