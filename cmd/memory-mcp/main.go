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
		fmt.Print(help())
		return nil
	}
	// An explicit help request shows one command's reference on stdout.
	for _, a := range args[1:] {
		if a == "--help" || a == "-h" {
			if text := usage(args[0]); text != "" {
				fmt.Print(text)
				return nil
			}
		}
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
		fs := flag.NewFlagSet("guide", flag.ContinueOnError)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
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
	case "serve", "status", "export", "backup", "forget", "migrate":
	default:
		return fmt.Errorf("unknown command %q; run memory-mcp help", cmd)
	}
	fs, o := utilityFlags(cmd)
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if (cmd == "export" || cmd == "backup") && *o.out == "" {
		return fmt.Errorf("%s requires --out FILE", cmd)
	}
	if cmd == "forget" && (*o.id == "" || !*o.yes) {
		return errors.New("forget requires --id ID --yes; removes all revisions and dependent interpretations")
	}
	if *o.dir == "" {
		p, err := memory.DefaultDir()
		if err != nil {
			return err
		}
		*o.dir = p
	}
	if cmd == "migrate" {
		ctx := context.Background()
		var report memory.MigrationReport
		var err error
		if *o.applyMigration {
			report, err = memory.ApplyCueMigration(ctx, *o.dir, *o.migrationBackup)
		} else {
			if *o.migrationBackup != "" {
				return errors.New("--backup is only used with --apply")
			}
			report, err = memory.InspectCueMigration(ctx, *o.dir)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	store, err := memory.Open(*o.dir)
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
		if err = memory.WriteExport(*o.out, snapshot); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Exported to", *o.out)
		return nil
	case "backup":
		if err = store.Backup(ctx, *o.out); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Backed up to", *o.out)
		return nil
	case "forget":
		n, err := store.Forget(ctx, *o.id)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Deleted %d entries including revisions and dependent interpretations. Existing exports, host transcripts, and backups are unaffected.\n", n)
		return nil
	}
	return nil
}
