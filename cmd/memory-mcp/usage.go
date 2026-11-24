package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/hooks"
)

// commandDocs is the reference of the commands implemented in this package.
// setup, hook, doctor and adapters are documented where they run; help prints
// both lists as one.
var commandDocs = []hooks.CommandDoc{
	{
		Name:  "serve",
		Usage: "memory-mcp serve [--data-dir DIR]",
		Summary: "Run the MCP server over stdin/stdout. This is the command the client's MCP configuration launches; " +
			"stdout carries the protocol, so do not run it in a terminal.",
		Flags: [][2]string{
			{"--data-dir DIR", "Library directory. Default: $MEMORY_MCP_DATA_DIR, then the platform default directory."},
		},
	},
	{
		Name:    "status",
		Usage:   "memory-mcp status [--data-dir DIR]",
		Summary: "Print the version, library format version, data directory and record counts, as JSON.",
		Flags: [][2]string{
			{"--data-dir DIR", "Library directory. Default: $MEMORY_MCP_DATA_DIR, then the platform default directory."},
		},
	},
	{
		Name:    "export",
		Usage:   "memory-mcp export --out FILE [--data-dir DIR]",
		Summary: "Write the whole library as plaintext JSON.",
		Flags: [][2]string{
			{"--out FILE", "New output file. An existing file is never overwritten."},
			{"--data-dir DIR", "Library directory. Default: $MEMORY_MCP_DATA_DIR, then the platform default directory."},
		},
	},
	{
		Name:    "backup",
		Usage:   "memory-mcp backup --out FILE [--data-dir DIR]",
		Summary: "Write a consistent SQLite snapshot of the library.",
		Flags: [][2]string{
			{"--out FILE", "New output file. An existing file is never overwritten."},
			{"--data-dir DIR", "Library directory. Default: $MEMORY_MCP_DATA_DIR, then the platform default directory."},
		},
	},
	{
		Name:  "forget",
		Usage: "memory-mcp forget --id ID --yes [--data-dir DIR]",
		Summary: "Permanently delete one record with all its revisions and dependent interpretations. " +
			"Existing exports, host transcripts, backups and snapshots are unaffected.",
		Flags: [][2]string{
			{"--id ID", "Full ID of the record."},
			{"--yes", "Confirm the deletion."},
			{"--data-dir DIR", "Library directory. Default: $MEMORY_MCP_DATA_DIR, then the platform default directory."},
		},
	},
	{
		Name:  "migrate",
		Usage: "memory-mcp migrate [--data-dir DIR]\n         memory-mcp migrate --apply --backup FILE [--data-dir DIR]",
		Summary: "Without --apply: a read-only report on the library's format version and on the records that need attention " +
			"(no cue, qualifiers only in legacy fields, cue text carrying facts beyond the body, missing evidence). " +
			"With --apply: upgrade the stored format to the current one after saving the original library as --backup FILE. " +
			"Stored shapes change; record contents are never reworded. Read docs/AGENT_INSTALL.md before upgrading a library.",
		Flags: [][2]string{
			{"--data-dir DIR", "Library directory. Default: $MEMORY_MCP_DATA_DIR, then the platform default directory."},
			{"--apply", "Upgrade the stored format. Nothing changes when the library is already current."},
			{"--backup FILE", "New file that receives the original library. Required with --apply."},
		},
	},
	{
		Name:    "diagnose",
		Usage:   "memory-mcp diagnose --data-dir DIR [--entry-id ID] [--scope project:ID] [--probe-ids ID,ID] [--out FILE]",
		Summary: "Read-only diagnostics of the metadata and links of a library. Opens it without upgrading and repairs nothing.",
		Flags: [][2]string{
			{"--data-dir DIR", "Existing library directory. Required; no path is guessed."},
			{"--entry-id ID", "Audit the forward links of this record."},
			{"--scope project:ID", "Expected scope, reported for comparison."},
			{"--probe-ids ID,ID", "Hook or source IDs to test for existence."},
			{"--out FILE", "Write the JSON report to a new file instead of stdout."},
		},
	},
	{
		Name:    "version",
		Usage:   "memory-mcp version",
		Summary: "Print the version.",
	},
	{
		Name:  "build-info",
		Usage: "memory-mcp build-info",
		Summary: "Print the version, the library format version and the exact source commit of the packaged build, as JSON. " +
			"A source build reports its commit as unknown.",
	},
	{
		Name:    "guide",
		Usage:   "memory-mcp guide",
		Summary: "Print the embedded guide: the workflow, the cue matching manual and the tool-probes.json format. The same text is returned by memory_bootstrap.",
	},
	{
		Name:    "config",
		Usage:   "memory-mcp config",
		Summary: "Print an MCP configuration fragment that launches this executable. It is generic: the data directory and server name that setup used are not included.",
	},
}

// usage is the full help of one top-level command, or "" for an unknown one.
func usage(cmd string) string {
	for _, d := range commandDocs {
		if d.Name == cmd {
			return d.String()
		}
	}
	return hooks.Usage(cmd)
}

// utilityOptions are the options of serve, status, export, backup, forget and
// migrate.
type utilityOptions struct {
	dir, out, id, migrationBackup *string
	yes, applyMigration           *bool
}

// utilityFlags binds the options of one of these commands; a flag that belongs
// to another one is rejected, so --help lists exactly what the command takes.
func utilityFlags(cmd string) (*flag.FlagSet, *utilityOptions) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage(cmd)) }
	o := &utilityOptions{dir: fs.String("data-dir", "", "local data directory"), out: new(string), id: new(string),
		migrationBackup: new(string), yes: new(bool), applyMigration: new(bool)}
	switch cmd {
	case "migrate":
		o.applyMigration = fs.Bool("apply", false, "upgrade the inspected library and retain a backup")
		o.migrationBackup = fs.String("backup", "", "new backup file for the original SQLite library")
	case "export", "backup":
		o.out = fs.String("out", "", "new output file (will not overwrite)")
	case "forget":
		o.id = fs.String("id", "", "entry ID; removes lineage and dependent interpretations")
		o.yes = fs.Bool("yes", false, "confirm permanent local deletion")
	}
	return fs, o
}

// allCommands is the whole reference in the order help lists it: the server,
// the installation commands, then the utilities.
func allCommands() []hooks.CommandDoc {
	all := make([]hooks.CommandDoc, 0, len(commandDocs)+len(hooks.Commands()))
	all = append(all, commandDocs[0])
	all = append(all, hooks.Commands()...)
	return append(all, commandDocs[1:]...)
}

func help() string {
	var b strings.Builder
	b.WriteString("memory-mcp: local verified-experience memory for an AI coding client.\n\nCommands:\n")
	width := 0
	for _, d := range allCommands() {
		if n := len(d.Name); n > width {
			width = n
		}
	}
	for _, d := range allCommands() {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, d.Name, d.Summary)
	}
	b.WriteString(`
Run "memory-mcp <command> --help" for the parameters of one command.
MEMORY_MCP_DATA_DIR overrides the platform default data directory.
"memory-mcp guide" prints the embedded guide: the workflow, the cue matching manual and the tool-probes.json format.
"memory-mcp doctor" checks an installation and replays a sample tool call; it changes nothing.
Read docs/AGENT_INSTALL.md to select the actual supported host; do not infer it from the model name.
No model, account, network service or runtime dependency is required.
`)
	return b.String()
}
