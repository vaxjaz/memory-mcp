package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func Run(ctx context.Context, args []string) error {
	cmd := args[0]
	if cmd == "adapters" {
		return json.NewEncoder(os.Stdout).Encode(Adapters())
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	host := fs.String("host", "", "explicit protocol adapter; run adapters for supported hosts")
	dir := fs.String("data-dir", "", "memory directory")
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	config := fs.String("config", "", "actual user-level host configuration; defaults by adapter")
	project := fs.String("project", "", "project directory to bind without modifying it")
	scope := fs.String("scope", "", "project:<stable identifier>")
	serverName := fs.String("server-name", "", "explicit MCP server name (use a distinct name and data directory for isolated acceptance)")
	capture := fs.Bool("capture-user-text", false, "enable raw prompt and supplied answer capture in bound projects")
	review := fs.Bool("review-on-stop", false, "request one host-model business-memory review at turn end; adds a model continuation")
	contextCheck := fs.Bool("context-check", false, "context-aware retrieval and before-action check; setup enables this in managed hooks")
	reviews := fs.Bool("reviews", false, "doctor: include paginated current needs_review entries for --scope")
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
	// Preserve the original read-only doctor default for existing ZCode scripts.
	// Installation and event processing always require an explicit actual host.
	if *host == "" && cmd == "doctor" {
		*host = "zcode"
	}
	adapter := Adapter{}
	if *host != "" {
		adapter, err = AdapterFor(*host)
		if err != nil {
			return err
		}
	}
	if *config == "" && *host != "" {
		*config = adapter.DefaultConfig(home)
	}
	if cmd == "setup" {
		exe, er := os.Executable()
		if er != nil {
			return er
		}
		r, er := Setup(ctx, SetupOptions{Host: *host, Config: *config, DataDir: *dir, Project: *project, Scope: *scope, Binary: exe, ServerName: *serverName, Capture: *capture, Review: *review, DryRun: *dry})
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
		c := map[string]any{}
		if *host != "" {
			var readErr error
			c, _, _, readErr = readAdapterConfig(adapter, *config)
			if readErr != nil {
				return readErr
			}
		}
		r["host"] = *host
		r["hook_trust_required"] = adapter.RequiresTrust
		r["hooks_config"] = adapter.DefaultHooks(*config)
		r["protocol_validation"] = adapter.Validation
		r["native_event_verified"] = false
		r["native_event_verification"] = "requires matching these receipts to an actual client session; manual replay also creates receipts"
		receipts, readErr := store.HookAdapterDiagnostics(ctx, *host)
		if readErr != nil {
			return readErr
		}
		r["adapter_receipts"] = receipts
		_, statErr := os.Stat(*config)
		r["config_exists"] = statErr == nil
		services := []map[string]any{}
		if mc, ok := c["mcp"].(map[string]any); ok {
			if servers, ok := mc["servers"].(map[string]any); ok {
				for name, v := range servers {
					if ent, ok := v.(map[string]any); ok {
						command, _ := ent["command"].(string)
						if isMemoryCommand(command) {
							data := configData(ent)
							a, _ := canonicalForDoctor(data)
							b, _ := canonicalForDoctor(store.Dir)
							services = append(services, map[string]any{"server": name, "command": command, "data_dir": data, "matches_opened_data_dir": a != "" && a == b})
						}
					}
				}
			}
		}
		r["memory_services"] = services
		contextHandlers := 0
		if h, ok := c["hooks"].(map[string]any); ok {
			if events, ok := h["events"].(map[string]any); ok {
				for _, v := range events {
					if groups, ok := v.([]any); ok {
						for _, v := range groups {
							if group, ok := v.(map[string]any); ok {
								if handlers, ok := group["hooks"].([]any); ok {
									for _, v := range handlers {
										if handler, ok := v.(map[string]any); ok {
											for _, arg := range stringArgs(handler["args"]) {
												if arg == "--context-check" {
													contextHandlers++
												}
											}
											if *host != "" && *host != "zcode" {
												for _, service := range services {
													name, _ := service["server"].(string)
													if (*serverName == "" || *serverName == name) && handler["command"] == nativeCommand(SetupOptions{Host: *host, Config: *config}, name) {
														contextHandlers++
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		r["context_check_handlers"] = contextHandlers
		bindings, er := store.HookBindings(ctx)
		if er != nil {
			return er
		}
		r["bindings"] = bindings
		if *project != "" {
			resolved, er := store.HookResolve(ctx, *project)
			r["project"] = *project
			r["resolved_scope"] = resolved
			r["binding_verified"] = er == nil && (*scope == "" || resolved == *scope)
			if er != nil {
				r["binding_error"] = er.Error()
			}
			if *scope != "" && resolved != *scope {
				r["binding_error"] = "resolved scope does not match expected scope"
			}
		}
		if *reviews {
			if *scope == "" {
				return errors.New("doctor --reviews requires --scope")
			}
			yes := true
			p, er := store.SearchPage(ctx, memory.SearchInput{Scope: *scope, NeedsReview: &yes, Limit: 30})
			if er != nil {
				return er
			}
			r["review_queue"] = p
		}
		if *scope != "" {
			m, er := store.Maintenance(ctx, *scope)
			if er != nil {
				return er
			}
			r["maintenance"] = m
		}
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
	e, err := adapter.Decode(raw)
	if err != nil {
		return fail(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, er := (Runtime{Host: *host, Store: store, Capture: *capture, Review: *review, ServerName: *serverName, ContextCheck: *contextCheck}).Handle(deadline, e)
	if er != nil {
		_ = store.HookLog(ctx, e.Name, "", e.Session, "failed", er.Error())
		return fail(er)
	}
	response, err := adapter.Encode(e, r)
	if err != nil {
		return fail(err)
	}
	if len(response.Stdout) > 0 {
		if _, err = os.Stdout.Write(response.Stdout); err != nil {
			return err
		}
	}
	if response.Stderr != "" {
		fmt.Fprintln(os.Stderr, response.Stderr)
	}
	if response.ExitCode != 0 {
		return &HookExitError{Code: response.ExitCode}
	}
	return nil
}

func canonicalForDoctor(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		a = strings.ToLower(a)
	}
	return filepath.Clean(a), nil
}
