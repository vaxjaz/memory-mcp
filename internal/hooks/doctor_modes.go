package hooks

import (
	"bytes"
	"os"
	"strings"
)

func configuredWorkflow(args []string) string {
	for _, a := range args {
		if a == "--mode" || strings.HasPrefix(a, "--mode=") || a == "--capture-user-text" || a == "--review-on-stop" || a == "--context-check" {
			return "obsolete_configuration"
		}
	}
	return "verified_experience"
}
func inspectManagedWorkflow(report, c map[string]any, services []map[string]any, host, config, serverName string) {
	launchers := []map[string]any{}
	valid := map[string]bool{}
	consistent := true
	for _, service := range services {
		name, _ := service["server"].(string)
		binary, _ := service["command"].(string)
		data, _ := service["data_dir"].(string)
		if serverName != "" && serverName != name {
			continue
		}
		if service["workflow"] != "verified_experience" {
			consistent = false
		}
		if host == "zcode" {
			valid[name] = true
			continue
		}
		o := SetupOptions{Host: host, Config: config, Binary: binary, DataDir: data}
		path := launcherPath(o, name)
		actual, err := os.ReadFile(path)
		args := managedHookArgs(o, name)
		matched := err == nil && (bytes.Equal(actual, launcher(o, args)) ||
			bytes.Equal(actual, launcher(o, append(args, "--"+legacyExperienceHooksFlag))))
		valid[name] = matched && service["matches_opened_data_dir"] == true
		if !valid[name] {
			consistent = false
		}
		launchers = append(launchers, map[string]any{"server": name, "path": path, "exists": err == nil, "matches_managed_definition": matched, "uses_opened_data_dir": service["matches_opened_data_dir"], "note": "configuration only; native execution needs separate verification"})
	}
	count := 0
	obsolete := 0
	if h, ok := c["hooks"].(map[string]any); ok {
		if events, ok := h["events"].(map[string]any); ok {
			for event, v := range events {
				groups, _ := v.([]any)
				for _, v := range groups {
					g, _ := v.(map[string]any)
					handlers, _ := g["hooks"].([]any)
					for _, v := range handlers {
						handler, _ := v.(map[string]any)
						for _, service := range services {
							name, _ := service["server"].(string)
							if serverName != "" && name != serverName {
								continue
							}
							own := host == "zcode" && managed(handler, name) || host != "zcode" && handler["command"] == nativeCommand(SetupOptions{Host: host, Config: config}, name)
							if !own {
								continue
							}
							if event == "Stop" || host == "zcode" && configuredWorkflow(stringArgs(handler["args"])) != "verified_experience" {
								obsolete++
								consistent = false
							}
							if event == "PreToolUse" && valid[name] {
								count++
							}
						}
					}
				}
			}
		}
	}
	report["workflow"] = "verified_experience"
	report["managed_launchers"] = launchers
	report["obsolete_handlers"] = obsolete
	report["experience_hooks_installed"] = count > 0
	report["configuration_consistent"] = consistent
}
