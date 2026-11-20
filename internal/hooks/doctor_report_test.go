package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A remote review reported doctor stdout breaking inside the probe-receipt
// section around nested Chinese quotes (\"提交推送\"). The report is a single
// json.Encoder pass, so it cannot emit invalid JSON by itself; these tests pin
// that contract end-to-end through Run (stdout capture), including large
// outputs and the --out file mode for hosts that truncate tool output.

func captureDoctorStdout(t *testing.T, args []string) []byte {
	t.Helper()
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	type done struct {
		out []byte
		err error
	}
	read := make(chan done, 1)
	go func() {
		out, err := io.ReadAll(r)
		read <- done{out, err}
	}()
	runErr := Run(context.Background(), args)
	os.Stdout = saved
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	finished := <-read
	if err = finished.err; err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr, string(finished.out))
	}
	return finished.out
}

func TestDoctorReportIsValidJSONWithNestedQuotesAndLargeDetails(t *testing.T) {
	s, _, _ := fixture(t)
	// Twenty receipts whose details carry nested Chinese quotes and a large
	// payload, shaped like the reported break sample.
	for i := 0; i < 20; i++ {
		detail := fmt.Sprintf(`{"tool":"Write","note":"执行 \"提交推送\" 前核对 staging 第%d步","pad":"%s"}`, i, strings.Repeat("回执", 700))
		if err := s.HookLog(testCtx, "PreToolUse", "project:demo", "doctor-json", "experience_probe", detail); err != nil {
			t.Fatal(err)
		}
	}
	out := captureDoctorStdout(t, []string{"doctor", "--data-dir", s.Dir, "--session", "doctor-json"})
	if len(out) < 64*1024 {
		t.Fatal("expected a large report to exercise host truncation limits", len(out))
	}
	var report map[string]any
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("doctor stdout is not valid JSON: %v near %q", err, clipAround(string(out), err))
	}
	events := report["recent_events"].([]any)
	if len(events) != 20 {
		t.Fatal("recent events missing", len(events))
	}
	first := events[0].(map[string]any)["detail"].(string)
	if !strings.Contains(first, `\"提交推送\"`) && !strings.Contains(first, `"提交推送"`) {
		t.Fatal("nested-quoted detail lost")
	}
}

func TestDoctorReportOutFileMode(t *testing.T) {
	s, _, _ := fixture(t)
	out := filepath.Join(t.TempDir(), "doctor-report.json")
	stdout := captureDoctorStdout(t, []string{"doctor", "--data-dir", s.Dir, "--out", out})
	if strings.HasPrefix(strings.TrimSpace(string(stdout)), "{") {
		t.Fatal("--out still prints the full report to stdout")
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal("file report is not valid JSON", err)
	}
	if report["data_dir"] == nil {
		t.Fatal("file report incomplete")
	}
}

func TestDoctorFlagsPreSchema8ProbeLeftovers(t *testing.T) {
	s, _, _ := fixture(t)
	if err := os.WriteFile(filepath.Join(s.Dir, "tool-probes.json"+legacyProbeSuffix), []byte(`{"projects":{"project:demo":[{"tool":"Write","path_field":"file_path"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out := captureDoctorStdout(t, []string{"doctor", "--data-dir", s.Dir})
	var report map[string]any
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatal(err)
	}
	files, ok := report["legacy_probe_config_files"].([]any)
	if !ok || len(files) != 1 || files[0].(string) != "tool-probes.json"+legacyProbeSuffix {
		t.Fatal("pre-schema8 leftover not flagged", report["legacy_probe_config_files"])
	}
	if next, _ := report["legacy_probe_config_next"].(string); !strings.Contains(next, "Re-register") {
		t.Fatal("leftover guidance missing")
	}
}

func clipAround(text string, err error) string {
	pos := 0
	if se, ok := err.(*json.SyntaxError); ok {
		pos = int(se.Offset)
	}
	if pos > len(text) {
		pos = len(text)
	}
	start := pos - 120
	if start < 0 {
		start = 0
	}
	end := pos + 120
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}
