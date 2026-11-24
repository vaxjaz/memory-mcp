package hooks

import (
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// CommandDoc is one entry of the command reference. "memory-mcp help" prints
// the usage line of every entry; "memory-mcp <command> --help" prints one
// whole entry, so a command never shows the parameters of another.
type CommandDoc struct {
	Name    string
	Usage   string
	Summary string
	Flags   [][2]string // flag with its placeholder, then what it means
	Notes   []string
}

// String renders the full help of the command.
func (d CommandDoc) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage:\n  %s\n\n%s\n", d.Usage, d.Summary)
	if len(d.Flags) > 0 {
		b.WriteString("\nParameters:\n")
		width := 0
		for _, f := range d.Flags {
			if n := len(f[0]); n > width {
				width = n
			}
		}
		for _, f := range d.Flags {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, f[0], f[1])
		}
	}
	for i, note := range d.Notes {
		if i == 0 {
			b.WriteString("\nNotes:\n")
		}
		fmt.Fprintf(&b, "  - %s\n", note)
	}
	return b.String()
}

const dataDirFlag = "Library directory. Default: $MEMORY_MCP_DATA_DIR, then the platform default directory."

var commands = []CommandDoc{
	{
		Name:  "setup",
		Usage: "memory-mcp setup --host HOST --project DIR --scope project:ID [--data-dir DIR] [--config FILE] [--server-name NAME] [--enable-wakeup | --disable-wakeup] [--dry-run]",
		Summary: "Bind one project and install this program into one client: the MCP entry and the hooks in the client's user configuration, " +
			"the binding of the project directory to its scope, the default tool rules in tool-probes.json and the matchers kept in the client.",
		Flags: [][2]string{
			{"--host NAME", "zcode | codex | qoder | qoder-ide: the client actually in use, not the model name."},
			{"--project DIR", "Existing project directory. It is bound and never modified."},
			{"--scope project:ID", "Stable identifier of this project, for example project:team-repo."},
			{"--data-dir DIR", dataDirFlag},
			{"--config FILE", "The client's user-level configuration file. Default: the adapter's own location."},
			{"--server-name NAME", "Name of the MCP server entry. Default: decision-memory."},
			{"--enable-wakeup", "Turn selective wakeup on for this project."},
			{"--disable-wakeup", "Turn selective wakeup off for this project."},
			{"--dry-run", "Show the targets without writing any file or opening the library."},
		},
		Notes: []string{
			"Run it again after replacing the executable, so the hooks and the server run the same program; it also adds any default tool rule missing from tool-probes.json.",
			"Without --enable-wakeup or --disable-wakeup the project keeps its current wakeup setting.",
			"The library is copied into <data-dir>/backups/ before the first change. Restart the client afterwards.",
		},
	},
	{
		Name:  "hook",
		Usage: "memory-mcp hook --host HOST [--data-dir DIR] [--server-name NAME] [--config FILE]",
		Summary: "Hook handler the client's configuration launches for each event. Reads one event from stdin and answers on stdout. " +
			"Not an MCP server, and not meant to be run by hand: \"doctor --probe\" replays a sample.",
		Flags: [][2]string{
			{"--host NAME", "zcode | codex | qoder | qoder-ide."},
			{"--data-dir DIR", dataDirFlag},
			{"--server-name NAME", "MCP server entry whose wakeup state applies."},
			{"--config FILE", "The client's user-level configuration file. Default: the adapter's own location."},
		},
	},
	{
		Name: "doctor",
		Usage: "memory-mcp doctor [--host HOST] [--config FILE] [--data-dir DIR] [--project DIR] [--scope project:ID] [--server-name NAME]\n" +
			"                    [--probe EVENT.json [--expect RECORD_ID [--expect-cue CUE_ID]]] [--session ID] [--challenge ID]\n" +
			"                    [--reviews] [--check-text] [--out FILE]",
		Summary: "Check an installation. Every mode is read-only. Reports whether the hooks and the server run the same program, whether tool-probes.json is valid, " +
			"the tool rules in effect for a scope, the cues no rule could ever fire, the project bindings and the recent hook events.",
		Flags: [][2]string{
			{"--host NAME", "zcode | codex | qoder | qoder-ide. Default: zcode; pass it for any other client."},
			{"--config FILE", "The client's user-level configuration file. Default: the adapter's own location."},
			{"--data-dir DIR", dataDirFlag},
			{"--project DIR", "Project directory whose binding is verified."},
			{"--scope project:ID", "Adds this project's records, tool rules, unreachable cues and reminder state."},
			{"--server-name NAME", "MCP server entry to check."},
			{"--probe EVENT.json", "Replay one sample tool call and show which cue would fire, or the field and reason it does not. \"-\" reads stdin."},
			{"--expect RECORD_ID", "With --probe: check whether this record ID matches."},
			{"--expect-cue CUE_ID", "With --expect: check this cue ID."},
			{"--session ID", "Limit the events to this session, including the calls that were not scanned."},
			{"--challenge ID", "Delivery status of one challenge ID."},
			{"--reviews", "With --scope: also list the records whose sources changed."},
			{"--check-text", "Also check the stored text for encoding damage. Changes nothing."},
			{"--out FILE", "Write the full JSON report to a new file and print only its path."},
		},
		Notes: []string{
			"A --probe sample without cwd requires --project; the directory of the terminal is never assumed.",
		},
	},
	{
		Name:    "adapters",
		Usage:   "memory-mcp adapters",
		Summary: "List the supported clients with their configuration location and validation status, as JSON.",
	},
}

// Commands returns the reference of the commands this package runs.
func Commands() []CommandDoc { return append([]CommandDoc(nil), commands...) }

// Usage is the full help of one of these commands, or "" for any other name.
func Usage(cmd string) string {
	for _, d := range commands {
		if d.Name == cmd {
			return d.String()
		}
	}
	return ""
}

// FlagsIn returns the long flags a usage text names.
func FlagsIn(text string) map[string]bool {
	found := map[string]bool{}
	for _, name := range flagToken.FindAllString(text, -1) {
		found[strings.TrimPrefix(name, "--")] = true
	}
	return found
}

var flagToken = regexp.MustCompile(`--([a-z][a-z-]*)`)

// writeUsage prints the full help of a command.
func writeUsage(w io.Writer, cmd string) { fmt.Fprint(w, Usage(cmd)) }

// flags are the options of setup, hook and doctor.
type flags struct {
	host, dir, config, project, scope, serverName                           *string
	enableWakeup, disableWakeup, reviews, dry, checkText                    *bool
	session, probeFile, expectedRecord, expectedCue, challengeID, doctorOut *string
}

// newFlags binds the options of one command. A flag that belongs to another
// command is rejected instead of being accepted and ignored, so the reference
// printed by --help is exactly what the command takes.
func newFlags(cmd string) (*flag.FlagSet, *flags) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.Usage = func() { writeUsage(fs.Output(), cmd) }
	// Every option exists for every command; only the command's own are bound.
	o := &flags{
		host: new(string), dir: new(string), config: new(string), project: new(string), scope: new(string), serverName: new(string),
		enableWakeup: new(bool), disableWakeup: new(bool), reviews: new(bool), dry: new(bool), checkText: new(bool),
		session: new(string), probeFile: new(string), expectedRecord: new(string), expectedCue: new(string), challengeID: new(string), doctorOut: new(string),
	}
	o.host = fs.String("host", "", "explicit protocol adapter; run adapters for supported hosts")
	o.dir = fs.String("data-dir", "", "memory directory")
	o.config = fs.String("config", "", "actual user-level host configuration; defaults by adapter")
	o.serverName = fs.String("server-name", "", "explicit MCP server name")
	switch cmd {
	case "setup":
		o.project = fs.String("project", "", "project directory to bind without modifying it")
		o.scope = fs.String("scope", "", "project:<stable identifier>")
		o.enableWakeup = fs.Bool("enable-wakeup", false, "enable selective experience wakeup for this project")
		o.disableWakeup = fs.Bool("disable-wakeup", false, "disable selective experience wakeup without activating an alternative workflow")
		o.dry = fs.Bool("dry-run", false, "show setup targets without writing config or opening the database")
	case "doctor":
		o.project = fs.String("project", "", "project directory to bind without modifying it")
		o.scope = fs.String("scope", "", "project:<stable identifier>")
		o.reviews = fs.Bool("reviews", false, "doctor: include paginated current needs_review entries for --scope")
		o.checkText = fs.Bool("check-text", false, "doctor: scan stored text for invalid UTF-8/U+FFFD without changing it")
		o.session = fs.String("session", "", "limit recent hook events to this exact session ID")
		o.challengeID = fs.String("challenge", "", "read-only delivery status for a full challenge ID")
		o.probeFile = fs.String("probe", "", "read-only probe check from event JSON file, or - for stdin")
		o.expectedRecord = fs.String("expect", "", "with --probe, check whether this full record ID directly matches")
		o.expectedCue = fs.String("expect-cue", "", "with --expect, check this specific cue ID")
		o.doctorOut = fs.String("out", "", "write the full JSON report to a new file and print only its path")
	case "hook":
		_ = fs.Bool(legacyExperienceHooksFlag, false, "accepted for older installed launchers")
	}
	_ = fs.String("managed-id", "", "installer ownership marker")
	return fs, o
}
