package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vaxjaz/memory-mcp/internal/memory"
	"github.com/vaxjaz/memory-mcp/internal/server"
)

var version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "memory-mcp:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Print(`memory-mcp: local decision memory, powered by your host AI.

Usage:
  memory-mcp serve [--data-dir DIR]           MCP over stdin/stdout
  memory-mcp status [--data-dir DIR]          Show path and record count
  memory-mcp export --out FILE [--data-dir DIR]
  memory-mcp forget --id ID --yes [--data-dir DIR]
  memory-mcp guide                          Print host integration guide
  memory-mcp config                         Print generic MCP JSON configuration
  memory-mcp version                        Print version

MEMORY_MCP_DATA_DIR overrides the platform default data directory.
No model, account, network service, or runtime dependencies required.
Export is plaintext JSON. Forget also removes revisions and dependents;
it cannot erase exports, host transcripts, backups, or storage snapshots.
`)
		return nil
	}
	if args[0] == "version" {
		fmt.Println(version)
		return nil
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
	switch cmd {
	case "serve", "status", "export", "forget":
	default:
		return fmt.Errorf("unknown command %q; run memory-mcp help", cmd)
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	dir := fs.String("data-dir", "", "local data directory")
	var out, id string
	var yes bool
	if cmd == "export" {
		fs.StringVar(&out, "out", "", "new JSON export file (will not overwrite)")
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
	if cmd == "export" && out == "" {
		return errors.New("export requires --out FILE")
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
		return server.New(store, version).Run(ctx, &mcp.StdioTransport{})
	case "status":
		snapshot, err := store.Export(ctx)
		if err != nil {
			return err
		}
		current, review := 0, 0
		for _, e := range snapshot.Entries {
			if e.Current {
				current++
				if e.NeedsReview {
					review++
				}
			}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"version": version, "schema_version": memory.SchemaVersion, "data_dir": store.Dir, "entries_including_history": len(snapshot.Entries), "current_entries": current, "needs_review": review})
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
