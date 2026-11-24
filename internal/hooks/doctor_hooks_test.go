package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func doctorReport(t *testing.T, config, data string) map[string]any {
	t.Helper()
	var report map[string]any
	if err := json.Unmarshal(captureDoctorStdout(t, []string{"doctor", "--host", "zcode", "--config", config, "--data-dir", data}), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

// A partial upgrade moves the MCP service to a new executable and leaves the
// hooks launching the old one. Two programs then read one library, so doctor
// must say so, and setup with the current executable must repair it.
func TestDoctorReportsHooksThatLaunchAnotherProgram(t *testing.T) {
	root := t.TempDir()
	data, config, project := filepath.Join(root, "data"), filepath.Join(root, "config.json"), filepath.Join(root, "project")
	os.MkdirAll(project, 0700)
	os.WriteFile(config, []byte("{}"), 0600)
	old, current := filepath.Join(root, "one", "memory-mcp.exe"), filepath.Join(root, "two", "memory-mcp.exe")
	options := SetupOptions{Host: "zcode", Config: config, DataDir: data, Project: project, Scope: "project:x", Binary: old}
	if _, err := Setup(testCtx, options); err != nil {
		t.Fatal(err)
	}
	if report := doctorReport(t, config, data); report["configuration_consistent"] != true {
		t.Fatal("a fresh setup must be consistent", report["configuration_consistent"])
	}

	c, _, err := readConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	c["mcp"].(map[string]any)["servers"].(map[string]any)["decision-memory"].(map[string]any)["command"] = current
	moved, _ := json.Marshal(c)
	os.WriteFile(config, moved, 0600)
	report := doctorReport(t, config, data)
	split, _ := report["split_executables"].([]any)
	if report["configuration_consistent"] != false || len(split) == 0 {
		t.Fatal("hooks launching the old program were not reported", report["configuration_consistent"], report["split_executables"])
	}
	first := split[0].(map[string]any)
	if first["hook_command"] != old || first["service_command"] != current || first["next"] == "" {
		t.Fatal(first)
	}

	options.Binary = current
	if _, err = Setup(testCtx, options); err != nil {
		t.Fatal(err)
	}
	if report := doctorReport(t, config, data); report["configuration_consistent"] != true || report["split_executables"] != nil {
		t.Fatal("setup with the current executable must bring the hooks back in step", report["configuration_consistent"])
	}
}
