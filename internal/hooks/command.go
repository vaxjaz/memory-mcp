package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/vaxjaz/memory-mcp/internal/textguard"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func Run(ctx context.Context, args []string) error {
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	host := fs.String("host", "", "explicit protocol adapter; see docs/HOOK_SETUP.md (currently zcode is implemented)")
	dir := fs.String("data-dir", "", "memory directory")
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	config := fs.String("config", filepath.Join(home, ".zcode", "cli", "config.json"), "user-level ZCode configuration")
	project := fs.String("project", "", "project directory to bind without modifying it")
	scope := fs.String("scope", "", "project:<stable identifier>")
	serverName := fs.String("server-name", "", "explicit MCP server name (use a distinct name and data directory for isolated acceptance)")
	capture := fs.Bool("capture-user-text", false, "enable raw prompt and supplied answer capture in bound projects")
	review := fs.Bool("review-on-stop", false, "request one host-model business-memory review at turn end; adds a model continuation")
	dry := fs.Bool("dry-run", false, "show setup targets without writing config or opening the database")
	checkText := fs.Bool("check-text", false, "doctor: scan stored text for invalid UTF-8/U+FFFD without changing it")
	_ = fs.String("managed-id", "", "installer ownership marker")
	if err = fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *host == "" && cmd != "doctor" {
		return errors.New("explicit --host required; identify the actual client and read docs/HOOK_SETUP.md")
	}
	if *host != "" && *host != "zcode" {
		return fmt.Errorf("host %q is not implemented; no guessed adapter is installed", *host)
	}
	if cmd == "setup" {
		exe, er := os.Executable()
		if er != nil {
			return er
		}
		r, er := Setup(ctx, SetupOptions{Config: *config, DataDir: *dir, Project: *project, Scope: *scope, Binary: exe, ServerName: *serverName, Capture: *capture, Review: *review, DryRun: *dry})
		if er != nil {
			return er
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	// Hooks fail open, including database/permission errors; diagnostics go to stderr.
	fail := func(e error) error {
		if cmd == "hook" {
			fmt.Fprintln(os.Stderr, "memory-mcp hook:", e)
			fmt.Fprintln(os.Stdout, "{}")
			return nil
		}
		return e
	}
	if *dir == "" {
		*dir, err = memory.DefaultDir()
		if err != nil {
			return fail(err)
		}
	}
	store, err := memory.Open(*dir)
	if err != nil {
		return fail(err)
	}
	defer store.Close()
	if cmd == "doctor" {
		r, er := store.HookDiagnostics(ctx)
		if er != nil {
			return er
		}
		r["data_dir"] = store.Dir
		r["config"] = *config
		r["host_runtime_connected"] = "not determined by configuration files"
		c, _, er := readConfig(*config)
		if er != nil {
			return er
		}
		r["config_exists"] = len(c) > 0
		if *checkText {
			audit, er := store.AuditText(ctx)
			if er != nil {
				return er
			}
			r["text_integrity"] = audit
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1024*1024+1))
	if err != nil {
		return fail(err)
	}
	if len(raw) > 1024*1024 {
		return fail(errors.New("hook input exceeds 1 MiB"))
	}
	var e Event
	if err = textguard.JSON(raw); err != nil {
		return fail(err)
	}
	if err = json.Unmarshal(raw, &e); err != nil {
		return fail(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, er := (Runtime{Store: store, Capture: *capture, Review: *review, ServerName: *serverName}).Handle(deadline, e)
	if er != nil {
		_ = store.HookLog(ctx, e.Name, "", e.Session, "failed", er.Error())
		return fail(er)
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}
