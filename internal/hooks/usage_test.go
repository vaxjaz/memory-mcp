package hooks

import (
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"
)

// hiddenFlags are accepted for installed launchers and shown to nobody.
var hiddenFlags = map[string]bool{"managed-id": true, legacyExperienceHooksFlag: true}

func acceptedFlags(fs *flag.FlagSet) map[string]bool {
	accepted := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		if !hiddenFlags[f.Name] {
			accepted[f.Name] = true
		}
	})
	return accepted
}

// A reference that lists a flag the command rejects, or omits one it takes,
// misleads whoever reads --help.
func TestReferenceListsExactlyTheFlagsOfItsCommand(t *testing.T) {
	for _, cmd := range []string{"setup", "hook", "doctor"} {
		fs, _ := newFlags(cmd)
		listed := map[string]bool{}
		for _, line := range strings.Split(Usage(cmd), "\n") {
			if strings.HasPrefix(line, "  --") {
				listed[strings.TrimPrefix(strings.Fields(line)[0], "--")] = true
			}
		}
		if accepted := acceptedFlags(fs); !reflect.DeepEqual(listed, accepted) {
			t.Errorf("%s: --help lists %v, the command accepts %v", cmd, listed, accepted)
		}
	}
}

func TestUsageLineNamesEveryListedFlag(t *testing.T) {
	for _, d := range commands {
		listed := map[string]bool{}
		for _, f := range d.Flags {
			listed[flagsOf(f[0])] = true
		}
		if named := FlagsIn(d.Usage); !reflect.DeepEqual(named, listed) {
			t.Errorf("%s: the usage line names %v, the reference lists %v", d.Name, named, listed)
		}
	}
}

func flagsOf(parameter string) string {
	return strings.TrimPrefix(strings.Fields(parameter)[0], "--")
}

// A parameter of another command is an error, not something silently ignored.
func TestACommandRejectsTheFlagOfAnother(t *testing.T) {
	for cmd, foreign := range map[string]string{"setup": "--session", "doctor": "--dry-run", "hook": "--project"} {
		fs, _ := newFlags(cmd)
		fs.SetOutput(io.Discard)
		if err := fs.Parse([]string{foreign, "x"}); err == nil || !strings.Contains(err.Error(), "not defined") {
			t.Errorf("%s accepted %s: %v", cmd, foreign, err)
		}
	}
}
