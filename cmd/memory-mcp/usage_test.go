package main

import (
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/hooks"
)

// A reference that lists a flag the command rejects, or omits one it takes,
// misleads whoever reads --help.
func TestReferenceListsExactlyTheFlagsOfItsCommand(t *testing.T) {
	sets := map[string]*flag.FlagSet{}
	for _, cmd := range []string{"serve", "status", "export", "backup", "forget", "migrate"} {
		sets[cmd], _ = utilityFlags(cmd)
	}
	sets["diagnose"], _, _, _, _, _ = diagnoseFlags()
	for cmd, fs := range sets {
		accepted := map[string]bool{}
		fs.VisitAll(func(f *flag.Flag) { accepted[f.Name] = true })
		if listed := hooks.FlagsIn(usage(cmd)); !reflect.DeepEqual(listed, accepted) {
			t.Errorf("%s: --help lists %v, the command accepts %v", cmd, listed, accepted)
		}
	}
}

func TestUsageLineNamesEveryListedFlag(t *testing.T) {
	for _, d := range allCommands() {
		listed := map[string]bool{}
		for _, f := range d.Flags {
			for name := range hooks.FlagsIn(f[0]) {
				listed[name] = true
			}
		}
		if named := hooks.FlagsIn(d.Usage); !reflect.DeepEqual(named, listed) {
			t.Errorf("%s: the usage line names %v, the reference lists %v", d.Name, named, listed)
		}
	}
}

// Help is the list of everything the program can be asked to do: no command
// missing, none twice, and each name answers --help.
func TestHelpListsEveryCommandOnceAndEachAnswers(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	stdout := os.Stdout
	os.Stdout = devNull
	defer func() { os.Stdout = stdout }()

	seen := map[string]bool{}
	for _, d := range allCommands() {
		if seen[d.Name] {
			t.Errorf("%s is listed twice", d.Name)
		}
		seen[d.Name] = true
		if !strings.HasPrefix(d.Usage, "memory-mcp "+d.Name+" ") && d.Usage != "memory-mcp "+d.Name {
			t.Errorf("%s: usage does not start with the command: %q", d.Name, d.Usage)
		}
		if d.Summary == "" {
			t.Errorf("%s has no summary", d.Name)
		}
		if err := run([]string{d.Name, "--help"}); err != nil {
			t.Errorf("%s --help: %v", d.Name, err)
		}
	}
	for _, missing := range []string{"serve", "setup", "doctor", "migrate", "backup", "export", "forget", "diagnose", "guide"} {
		if !seen[missing] {
			t.Errorf("%s is missing from help", missing)
		}
	}
}
