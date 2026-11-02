package hooks

import (
	"context"
	"strings"
	"testing"
)

func TestInstallAndHookRequireAnExplicitActualHost(t *testing.T) {
	for _, command := range []string{"setup", "hook"} {
		err := Run(context.Background(), []string{command})
		if err == nil || !strings.Contains(err.Error(), "explicit --host required") {
			t.Fatalf("%s defaulted a host: %v", command, err)
		}
	}
	err := Run(context.Background(), []string{"setup", "--host", "codex"})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("unsupported adapter was guessed: %v", err)
	}
}
