package hooks

import (
	"context"
	"encoding/json"
	"github.com/vaxjaz/memory-mcp/internal/memory"
	"os"
	"path/filepath"
	"testing"
)

var testCtx = context.Background()

func fixture(t *testing.T) (*memory.Store, Runtime, string) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "project")
	os.Mkdir(p, 0700)
	s, err := memory.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.HookBind(testCtx, p, "project:demo"); err != nil {
		t.Fatal(err)
	}
	return s, Runtime{Store: s}, p
}
func handle(t *testing.T, r Runtime, e Event) map[string]any {
	t.Helper()
	out, err := r.Handle(testCtx, e)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func event(name, cwd, session, prompt string) Event {
	return Event{Name: name, CWD: cwd, Session: session, Prompt: prompt}
}
func text(out map[string]any) string { b, _ := json.Marshal(out); return string(b) }
