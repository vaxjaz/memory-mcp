package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vaxjaz/memory-mcp/internal/hooks"
	"github.com/vaxjaz/memory-mcp/internal/memory"
	"github.com/vaxjaz/memory-mcp/internal/server"
)

// version may be supplied by the release linker; source builds use VERSION.
var version string
var buildCommit string

//go:embed VERSION
var sourceVersion string

func main() {
	if err := run(os.Args[1:]); err != nil {
		var hookExit *hooks.HookExitError
		if errors.As(err, &hookExit) {
			os.Exit(hookExit.Code)
		}
		fmt.Fprintln(os.Stderr, "memory-mcp:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	buildVersion := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if buildVersion == "" {
		buildVersion = strings.TrimPrefix(strings.TrimSpace(sourceVersion), "v")
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Print(`memory-mcp: local decision memory, powered by your host AI.

Usage:
  memory-mcp serve [--data-dir DIR]           MCP over stdin/stdout
  memory-mcp status [--data-dir DIR]          Show path and record count
  memory-mcp export --out FILE [--data-dir DIR]
  memory-mcp backup --out FILE [--data-dir DIR]  Consistent standalone SQLite snapshot
  memory-mcp forget --id ID --yes [--data-dir DIR]
  memory-mcp setup --host HOST --project DIR --scope project:ID --capture-user-text
  memory-mcp hook --host HOST [--data-dir DIR]    User-level event handler (not an MCP server)
  memory-mcp doctor [--data-dir DIR]             Event/recall/review receipts, no chat bodies
  memory-mcp adapters                           List implemented host protocols and validation limits
  memory-mcp diagnose --data-dir DIR [--entry-id ID] [--probe-ids ID,ID] [--out FILE]
                                             Read-only metadata/link diagnostics; no migration or repair
  memory-mcp guide                          Print host integration guide
  memory-mcp config                         Print generic MCP JSON configuration
  memory-mcp version                        Print version
  memory-mcp build-info                     Print version and exact packaged source commit

MEMORY_MCP_DATA_DIR overrides the platform default data directory.
Read docs/HOOK_SETUP.md to select a supported host adapter; do not infer the client from the model name.
No model, account, network service, or runtime dependencies required.
Export is plaintext JSON. Forget also removes revisions and dependents;
it cannot erase exports, host transcripts, backups, or storage snapshots.
`)
		return nil
	}
	if args[0] == "version" {
		fmt.Println(buildVersion)
		return nil
	}
	if args[0] == "build-info" {
		commit := buildCommit
		if commit == "" {
			commit = "unknown (unpackaged source build)"
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"version": buildVersion, "source_commit": commit, "schema_version": memory.SchemaVersion})
	}
	if args[0] == "guide" {
		fmt.Print(server.Guide)
		return nil
	}
	if args[0] == "config" {
		path, err := os.Executable()
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]any{"mcpServers": map[string]any{"decision-memory": map[string]any{"command": path, "args": []string{"serve"}}}})
	}
	cmd := args[0]
	if cmd == "diagnose" {
		return diagnose(args[1:], buildVersion, buildCommit)
	}
	if cmd == "setup" || cmd == "hook" || cmd == "doctor" || cmd == "adapters" {
		return hooks.Run(context.Background(), args)
	}
	switch cmd {
	case "serve", "status", "export", "backup", "forget":
	default:
		return fmt.Errorf("unknown command %q; run memory-mcp help", cmd)
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	dir := fs.String("data-dir", "", "local data directory")
	var out, id string
	var yes bool
	if cmd == "export" || cmd == "backup" {
		fs.StringVar(&out, "out", "", "new output file (will not overwrite)")
	}
	if cmd == "forget" {
		fs.StringVar(&id, "id", "", "entry ID; removes lineage and dependent interpretations")
		fs.BoolVar(&yes, "yes", false, "confirm permanent local deletion")
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if (cmd == "export" || cmd == "backup") && out == "" {
		return fmt.Errorf("%s requires --out FILE", cmd)
	}
	if cmd == "forget" && (id == "" || !yes) {
		return errors.New("forget requires --id ID --yes; removes all revisions and dependent interpretations")
	}
	if *dir == "" {
		p, err := memory.DefaultDir()
		if err != nil {
			return err
		}
		*dir = p
	}
	store, err := memory.Open(*dir)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	switch cmd {
	case "serve":
		// stdout belongs exclusively to the MCP transport; diagnostics use stderr.
		return server.New(store, buildVersion).Run(ctx, &mcp.StdioTransport{})
	case "status":
		stats, err := store.Stats(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"version": buildVersion, "schema_version": memory.SchemaVersion, "data_dir": store.Dir, "entries_including_history": stats.Entries, "current_entries": stats.Current, "needs_review": stats.NeedsReview})
	case "export":
		snapshot, err := store.Export(ctx)
		if err != nil {
			return err
		}
		if err = memory.WriteExport(out, snapshot); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Exported to", out)
		return nil
	case "backup":
		if err = store.Backup(ctx, out); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Backed up to", out)
		return nil
	case "forget":
		n, err := store.Forget(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Deleted %d entries including revisions and dependent interpretations. Existing exports, host transcripts, and backups are unaffected.\n", n)
		return nil
	}
	return nil
}
