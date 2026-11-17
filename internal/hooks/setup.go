package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

const ManagedID = "decision-memory-hooks-v1"

func managedHookArgs(o SetupOptions, name string) []any {
	args := []any{"hook", "--host", o.Host, "--data-dir", o.DataDir, "--managed-id", ManagedID + "|" + name, "--server-name", name}
	if o.ExperienceHooks {
		args = append(args, "--experience-hooks")
	}
	return args
}

type SetupOptions struct {
	EnableWakeup, DisableWakeup, ExperienceHooks bool
	Host                                         string
	Config, DataDir, Project, Scope, Binary      string
	ServerName                                   string
	DryRun                                       bool
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
	if err = textguard.JSON(raw); err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&c); err != nil {
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

func isMemoryCommand(command string) bool {
	if strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe") == "memory-mcp" {
		return true
	}
	actual, err := os.Executable()
	if err != nil {
		return false
	}
	a, err := canonicalForDoctor(command)
	if err != nil {
		return false
	}
	b, err := canonicalForDoctor(actual)
	return err == nil && a != "" && a == b
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
	if o.EnableWakeup && o.DisableWakeup {
		return nil, errors.New("choose enable or disable wakeup, not both")
	}
	if o.Host == "" {
		o.Host = "zcode"
	}
	adapter, err := AdapterFor(o.Host)
	if err != nil {
		return nil, err
	}

	if o.Project == "" || o.Scope == "" {
		return nil, errors.New("setup requires --project DIR --scope project:<identifier>")
	}
	if err := memory.Validate(memory.Draft{Kind: "record", Scope: o.Scope, Title: "scope check", Body: "scope check", Origin: "host_quote"}); err != nil || o.Scope == "global" {
		return nil, errors.New("valid project scope required; global is not inferred")
	}
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
	c, raw, extra, err := readAdapterConfig(adapter, o.Config)
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
	if len(servers) == 0 && o.Host == "zcode" {
		home, er := os.UserHomeDir()
		if er != nil {
			return nil, er
		}
		fallback := filepath.Join(home, ".agents", "mcp.json")
		if b, er := os.ReadFile(fallback); er == nil {
			var f map[string]any
			if er = textguard.JSON(b); er != nil {
				return nil, fmt.Errorf("invalid .agents MCP text encoding: %w", er)
			}
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
				if isMemoryCommand(cmd) {
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
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\|\r\n\x00") {
		return nil, errors.New("unsafe existing MCP server name; reconcile before setup")
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
				if o.Host == "qoder" || o.Host == "qoder-ide" {
					other := o
					if o.Host == "qoder" {
						other.Host = "qoder-ide"
					} else {
						other.Host = "qoder"
					}
					if handler["command"] == nativeCommand(other, name) {
						return nil, errors.New("Qoder CLI/IDE share this configuration and another memory hook profile is installed; reconcile it or use separate user configurations to avoid duplicate hook execution")
					}
				}
				if !(o.Host == "zcode" && managed(handler, name) || o.Host != "zcode" && handler["command"] == nativeCommand(o, name)) {
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
	var store *memory.Store
	var databaseBackup string
	desiredTrial := !o.DisableWakeup
	if !o.DryRun {
		store, err = memory.Open(o.DataDir)
		if err != nil {
			return nil, err
		}
		defer store.Close()
		backups := filepath.Join(o.DataDir, "backups")
		if err = os.MkdirAll(backups, 0700); err != nil {
			return nil, err
		}
		databaseBackup = filepath.Join(backups, "before-hook-setup-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".db")
		if err = store.Backup(ctx, databaseBackup); err != nil {
			return nil, err
		}
		if !o.EnableWakeup && !o.DisableWakeup {
			found, er := store.ExperiencePolicyExists(ctx, o.Scope)
			if er != nil {
				return nil, er
			}
			if found {
				desiredTrial, err = store.ExperienceTrialEnabled(ctx, o.Scope)
				if err != nil {
					return nil, err
				}
			}
		}
		other, er := store.OtherExperienceTrialsEnabled(ctx, o.Scope)
		if er != nil {
			return nil, er
		}
		o.ExperienceHooks = other || desiredTrial
	} else {
		o.ExperienceHooks = desiredTrial
	}
	args := managedHookArgs(o, name)
	installedEvents := []string{"SessionStart", "UserPromptSubmit"}
	if o.ExperienceHooks {
		installedEvents = append(installedEvents, "PreToolUse", "PostToolUse")
	}
	for _, event := range installedEvents {
		group := map[string]any{"hooks": []any{map[string]any{"type": "process", "command": o.Binary, "args": args, "enabled": true, "timeoutMs": 10000}}}
		if o.Host != "zcode" {
			group["hooks"] = []any{map[string]any{"type": "command", "command": nativeCommand(o, name), "timeout": 10}}
		}
		if event == "PreToolUse" {
			group["matcher"] = experienceToolMatcher
		}
		if event == "PostToolUse" {
			group["matcher"] = businessToolMatcher
		}
		existing, _ := events[event].([]any)
		events[event] = append(existing, group)
	}
	h["enabled"] = true
	entry["command"] = o.Binary
	entry["args"] = []string{"serve", "--data-dir", o.DataDir}
	if o.Host == "zcode" {
		entry["enable"] = true
	} else if o.Host == "codex" {
		entry["enabled"] = true
	}
	servers[name] = entry
	report := map[string]any{"config": o.Config, "workflow": "verified_experience", "data_dir": o.DataDir, "project": o.Project, "scope": o.Scope, "mcp_server": name, "host_event_verified": false, "restart_required": true, "dry_run": o.DryRun, "wakeup_enabled": desiredTrial, "experience_hooks_installed": o.ExperienceHooks}
	report["host"] = o.Host
	report["hook_trust_required"] = adapter.RequiresTrust
	report["hooks_config"] = adapter.DefaultHooks(o.Config)
	report["historical_projects_auto_bound"] = false
	report["verification_command"] = fmt.Sprintf("doctor --host %s --config %q --data-dir %q --project %q --scope %q", o.Host, o.Config, o.DataDir, o.Project, o.Scope)
	report["verification_argv"] = []string{o.Binary, "doctor", "--host", o.Host, "--config", o.Config, "--data-dir", o.DataDir, "--project", o.Project, "--scope", o.Scope, "--server-name", name}
	plans, err := adapterFiles(adapter, o, c, raw, extra, name, args)
	if err != nil {
		return nil, err
	}
	if o.DryRun {
		return report, nil
	}
	if err = os.MkdirAll(filepath.Dir(o.Config), 0700); err != nil {
		return nil, err
	}
	report["database_backup"] = databaseBackup
	if err = store.HookBind(ctx, o.Project, o.Scope); err != nil {
		return nil, err
	}
	resolved, er := store.HookResolve(ctx, o.Project)
	if er != nil || resolved != o.Scope {
		return nil, errors.New("project binding verification failed")
	}
	report["binding_verified"] = true
	backupsWritten, err := applyConfigPlans(plans)
	if err != nil {
		return nil, err
	}
	report["config_backups"] = backupsWritten
	if err = store.ConfigureExperienceTrial(ctx, o.Scope, desiredTrial); err != nil {
		return nil, err
	}
	for _, backup := range backupsWritten {
		if strings.HasPrefix(backup, o.Config+".memory-mcp-backup-") {
			report["config_backup"] = backup
		}
	}
	return report, nil
}
