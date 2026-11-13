package hooks

import (
	"bytes"
	"os"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func configuredMode(args []string, hook bool) string {
	for i, arg := range args {
		if strings.HasPrefix(arg, "--mode=") {
			mode, err := memory.IntegrationMode(strings.TrimPrefix(arg, "--mode="))
			if err != nil {
				return "invalid"
			}
			return mode
		}
		if arg == "--mode" && i+1 < len(args) {
			mode, err := memory.IntegrationMode(args[i+1])
			if err != nil {
				return "invalid"
			}
			return mode
		}
	}
	if hook {
		for _, arg := range args {
			if arg == "--context-check" || arg == "--capture-user-text" || arg == "--review-on-stop" {
				return memory.ModeStrict
			}
		}
	}
	return memory.ModeAssist
}

// Configuration checks never claim native delivery or model compliance.
func inspectManagedModes(report, c map[string]any, services []map[string]any, host, config, serverName string) {
	launchers := []map[string]any{}
	launcherModes := map[string]string{}
	mismatches := []map[string]any{}
	if host != "zcode" {
		for _, service := range services {
			name, _ := service["server"].(string)
			binary, _ := service["command"].(string)
			data, _ := service["data_dir"].(string)
			o := SetupOptions{Host: host, Config: config, Binary: binary, DataDir: data}
			path := launcherPath(o, name)
			actual, readErr := os.ReadFile(path)
			o.ExperienceHooks = bytes.Contains(actual, []byte("--experience-hooks"))
			matchedMode := ""
			matchedCapture := false
			for _, mode := range []string{memory.ModeAssist, memory.ModeStrict} {
				for _, capture := range []bool{false, true} {
					for _, review := range []bool{false, true} {
						if mode == memory.ModeAssist && review || mode == memory.ModeStrict && !capture {
							continue
						}
						o.Mode = mode
						o.Capture = capture
						o.Review = review
						if readErr == nil && bytes.Equal(actual, launcher(o, managedHookArgs(o, name))) {
							matchedMode = mode
							matchedCapture = capture
						}
					}
				}
			}
			if matchedMode != "" && service["matches_opened_data_dir"] == true {
				launcherModes[name] = matchedMode
			}
			launchers = append(launchers, map[string]any{"server": name, "path": path, "exists": readErr == nil, "matches_managed_definition": matchedMode != "", "mode": matchedMode, "raw_capture": matchedCapture, "experience_hooks": o.ExperienceHooks, "uses_opened_data_dir": service["matches_opened_data_dir"], "note": "file/configuration verification only; not proof of native event delivery"})
		}
	}
	handlers := 0
	softHandlers := 0
	experienceHandlers := 0
	if h, ok := c["hooks"].(map[string]any); ok {
		if events, ok := h["events"].(map[string]any); ok {
			for event, v := range events {
				groups, _ := v.([]any)
				for _, v := range groups {
					group, _ := v.(map[string]any)
					hooks, _ := group["hooks"].([]any)
					for _, v := range hooks {
						handler, _ := v.(map[string]any)
						for _, service := range services {
							name, _ := service["server"].(string)
							if serverName != "" && name != serverName {
								continue
							}
							mode := ""
							if host == "zcode" && managed(handler, name) {
								mode = configuredMode(stringArgs(handler["args"]), true)
							}
							if host != "zcode" && handler["command"] == nativeCommand(SetupOptions{Host: host, Config: config}, name) {
								mode = launcherModes[name]
							}
							if mode == "" {
								continue
							}
							if mode == memory.ModeStrict {
								handlers++
							} else if mode == memory.ModeAssist {
								softHandlers++
								if event == "PreToolUse" {
									experienceHandlers++
								}
							}
							if mode != service["mode"] {
								mismatches = append(mismatches, map[string]any{"server": name, "event": event, "service_mode": service["mode"], "hook_mode": mode})
							}
						}
					}
				}
			}
		}
	}
	report["managed_launchers"] = launchers
	report["context_check_handlers"] = handlers
	report["assist_handlers"] = softHandlers
	report["experience_hooks_installed"] = experienceHandlers > 0
	report["mode_mismatches"] = mismatches
	report["mode_configuration_consistent"] = len(mismatches) == 0
	report["mode_scope"] = "configured service and all its bound projects; no per-turn switch prompt"
}
