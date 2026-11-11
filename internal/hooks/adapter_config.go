package hooks

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"unicode/utf16"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

type adapterExtra struct {
	Root, Hooks map[string]any
	HooksRaw    []byte
}

func readAdapterConfig(a Adapter, path string) (map[string]any, []byte, adapterExtra, error) {
	x := adapterExtra{}
	if a.Name == "zcode" {
		c, b, e := readConfig(path)
		return c, b, x, e
	}
	var root map[string]any
	var raw []byte
	var err error
	if a.Name == "codex" {
		raw, err = os.ReadFile(path)
		if os.IsNotExist(err) {
			err = nil
		}
		if err != nil {
			return nil, nil, x, err
		}
		root = map[string]any{}
		if len(raw) > 0 {
			if err = toml.Unmarshal(raw, &root); err != nil {
				return nil, nil, x, err
			}
		}
		x.Hooks, x.HooksRaw, err = readConfig(a.DefaultHooks(path))
		if err != nil {
			return nil, nil, x, err
		}
	} else {
		root, raw, err = readConfig(path)
		if err != nil {
			return nil, nil, x, err
		}
		x.Hooks = root
	}
	x.Root = root
	key := "mcpServers"
	if a.Name == "codex" {
		key = "mcp_servers"
	}
	servers, err := object(root, key)
	if err != nil {
		return nil, nil, x, err
	}
	events, err := object(x.Hooks, "hooks")
	if err != nil {
		return nil, nil, x, err
	}
	// Reuse the common installer checks on a canonical, in-memory representation.
	c := map[string]any{"mcp": map[string]any{"servers": servers}, "hooks": map[string]any{"events": events}}
	return c, raw, x, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func psQuote(s string) string    { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func launcherPath(o SetupOptions, name string) string {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".ps1"
	}
	return filepath.Join(filepath.Dir(o.Config), "memory-mcp-hooks", o.Host+"-"+name+ext)
}
func nativeCommand(o SetupOptions, name string) string {
	p := launcherPath(o, name)
	if runtime.GOOS == "windows" {
		// Do not interpolate user paths into cmd.exe syntax (% and quotes have
		// shell meaning even in several quoted contexts). Only base64 crosses it.
		script := "$ProgressPreference='SilentlyContinue'; & " + psQuote(p) + "; exit $LASTEXITCODE"
		units := utf16.Encode([]rune(script))
		raw := make([]byte, len(units)*2)
		for i, u := range units {
			binary.LittleEndian.PutUint16(raw[i*2:], u)
		}
		return "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -InputFormat Text -OutputFormat Text -EncodedCommand " + base64.StdEncoding.EncodeToString(raw)
	}
	return "sh " + shellQuote(p)
}
func launcher(o SetupOptions, args []any) []byte {
	all := []string{o.Binary}
	for _, v := range args {
		all = append(all, v.(string))
	}
	quoted := make([]string, len(all))
	if runtime.GOOS == "windows" {
		for i, v := range all {
			quoted[i] = psQuote(v)
		}
		// UTF-8 input is inherited by the native executable. Windows PowerShell 5
		// otherwise defaults to legacy encodings when it owns a text pipeline.
		return []byte("\xef\xbb\xbf$ProgressPreference='SilentlyContinue'\r\n[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)\r\n[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)\r\n& " + strings.Join(quoted, " ") + "\r\nexit $LASTEXITCODE\r\n")
	}
	for i, v := range all {
		quoted[i] = shellQuote(v)
	}
	return []byte("#!/bin/sh\nexec " + strings.Join(quoted, " ") + "\n")
}

type configPlan struct {
	Path          string
	Before, After []byte
}

func jsonBytes(v any) ([]byte, error) {
	b, e := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), e
}
func adapterFiles(a Adapter, o SetupOptions, c map[string]any, raw []byte, x adapterExtra, name string, args []any) ([]configPlan, error) {
	if a.Name == "zcode" {
		b, e := jsonBytes(c)
		return []configPlan{{o.Config, raw, b}}, e
	}
	servers := c["mcp"].(map[string]any)["servers"].(map[string]any)
	events := c["hooks"].(map[string]any)["events"]
	x.Hooks["hooks"] = events
	plans := []configPlan{}
	if a.Name == "codex" {
		b, e := replaceTOMLServer(raw, name, servers[name])
		if e != nil {
			return nil, e
		}
		h, e := jsonBytes(x.Hooks)
		if e != nil {
			return nil, e
		}
		plans = append(plans, configPlan{o.Config, raw, b}, configPlan{a.DefaultHooks(o.Config), x.HooksRaw, h})
	} else {
		x.Root["mcpServers"] = servers
		b, e := jsonBytes(x.Root)
		if e != nil {
			return nil, e
		}
		plans = append(plans, configPlan{o.Config, raw, b})
	}
	path := launcherPath(o, name)
	before, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	plans = append(plans, configPlan{path, before, launcher(o, args)})
	for _, p := range plans {
		rel, e := filepath.Rel(o.Project, p.Path)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return nil, errors.New("all managed configuration and launchers must be outside project")
		}
	}
	return plans, nil
}

// TOML is edited by AST table boundaries, never line regexes (a header-looking
// line can occur inside a multiline string). Unowned bytes/comments are retained.
func replaceTOMLServer(raw []byte, name string, entry any) ([]byte, error) {
	before := map[string]any{}
	if len(raw) > 0 {
		if e := toml.Unmarshal(raw, &before); e != nil {
			return nil, e
		}
	}
	type section struct {
		start int
		owned bool
	}
	sections := []section{}
	var p unstable.Parser
	p.Reset(raw)
	for p.NextExpression() {
		n := p.Expression()
		if n.Kind != unstable.Table && n.Kind != unstable.ArrayTable {
			continue
		}
		keys := []string{}
		start := -1
		it := n.Key()
		for it.Next() {
			k := it.Node()
			if start < 0 {
				start = int(k.Raw.Offset)
			}
			keys = append(keys, string(k.Data))
		}
		if start < 0 {
			return nil, errors.New("unrecognized TOML header")
		}
		for start > 0 && raw[start-1] != '\n' {
			start--
		}
		sections = append(sections, section{start, len(keys) >= 2 && keys[0] == "mcp_servers" && keys[1] == name})
	}
	if e := p.Error(); e != nil {
		return nil, e
	}
	var out bytes.Buffer
	cursor := 0
	for i, s := range sections {
		if !s.owned {
			continue
		}
		end := len(raw)
		if i+1 < len(sections) {
			end = sections[i+1].start
		}
		out.Write(raw[cursor:s.start])
		cursor = end
	}
	out.Write(raw[cursor:])
	if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		out.WriteByte('\n')
	}
	chunk, e := toml.Marshal(map[string]any{"mcp_servers": map[string]any{name: entry}})
	if e != nil {
		return nil, e
	}
	// Marshal can generate a bare parent header; it is not ours to redefine.
	chunk = bytes.Replace(chunk, []byte("[mcp_servers]\n"), nil, 1)
	out.Write(chunk)
	after := map[string]any{}
	if e = toml.Unmarshal(out.Bytes(), &after); e != nil {
		return nil, fmt.Errorf("unsupported existing TOML layout; no files changed: %w", e)
	}
	strip := func(m map[string]any) {
		if v, ok := m["mcp_servers"].(map[string]any); ok {
			delete(v, name)
			if len(v) == 0 {
				delete(m, "mcp_servers")
			}
		}
	}
	strip(before)
	strip(after)
	if !reflect.DeepEqual(before, after) {
		return nil, errors.New("TOML edit would change unrelated settings; no files changed")
	}
	return out.Bytes(), nil
}

func applyConfigPlans(plans []configPlan) ([]string, error) {
	backups := []string{}
	done := []configPlan{}
	rollback := func(cause error) ([]string, error) {
		for i := len(done) - 1; i >= 0; i-- {
			p := done[i]
			current, e := os.ReadFile(p.Path)
			if e != nil || !bytes.Equal(current, p.After) {
				cause = errors.Join(cause, fmt.Errorf("rollback conflict at %s; restore backup", p.Path))
				continue
			}
			if p.Before == nil {
				e = os.Remove(p.Path)
			} else {
				e = writeFileAtomic(p.Path, p.Before)
			}
			cause = errors.Join(cause, e)
		}
		return backups, cause
	}
	for _, p := range plans {
		current, e := os.ReadFile(p.Path)
		if os.IsNotExist(e) {
			current = nil
			e = nil
		}
		if e != nil {
			return rollback(e)
		}
		if !bytes.Equal(current, p.Before) {
			return rollback(fmt.Errorf("configuration changed concurrently: %s", p.Path))
		}
		if bytes.Equal(p.Before, p.After) {
			continue
		}
		if e = os.MkdirAll(filepath.Dir(p.Path), 0700); e != nil {
			return rollback(e)
		}
		backup, e := backupFile(p.Path, p.Before)
		if e != nil {
			return rollback(e)
		}
		if backup != "" {
			backups = append(backups, backup)
		}
		if e = writeFileAtomic(p.Path, p.After); e != nil {
			return rollback(e)
		}
		done = append(done, p)
	}
	return backups, nil
}
