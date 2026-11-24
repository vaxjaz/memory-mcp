package hooks

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
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
func configuredMemoryServices(c map[string]any, dir string) []map[string]any {
	services := []map[string]any{}
	mcp, _ := c["mcp"].(map[string]any)
	servers, _ := mcp["servers"].(map[string]any)
	for name, v := range servers {
		entry, _ := v.(map[string]any)
		command, _ := entry["command"].(string)
		if isMemoryCommand(command) {
			data := configData(entry)
			a, _ := canonicalForDoctor(data)
			b, _ := canonicalForDoctor(dir)
			services = append(services, map[string]any{"workflow": configuredWorkflow(stringArgs(entry["args"])),
				"server": name, "command": command, "data_dir": data, "matches_opened_data_dir": a != "" && a == b,
				"enabled": entry["enable"] != false && entry["enabled"] != false})
		}
	}
	return services
}

func inspectManagedWorkflow(report, c map[string]any, services []map[string]any, host, config, serverName string, probes probeConfig, tool string) {
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
	mismatchedMatchers := 0
	routed := false
	var splitExecutables []map[string]any
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
							// On ZCode the hooks name the program directly. A service moved
							// to a new executable while its hooks still launch the old one
							// is a silent split: two programs read one library.
							if hookBinary, serviceBinary := handler["command"], service["command"]; host == "zcode" && hookBinary != nil && !sameExecutable(fmt.Sprint(hookBinary), fmt.Sprint(serviceBinary)) {
								splitExecutables = append(splitExecutables, map[string]any{"server": name, "event": event,
									"hook_command": hookBinary, "service_command": serviceBinary,
									"next": "The hooks and the service run different programs. Run setup with the current executable, then fully restart the client."})
							}
							if event == "Stop" || host == "zcode" && configuredWorkflow(stringArgs(handler["args"])) != "verified_experience" {
								obsolete++
								consistent = false
							}
							if !valid[name] {
								continue
							}
							var expected string
							switch event {
							case "PreToolUse":
								count++
								expected = probes.matcher(name)
								if tool != "" && h["enabled"] != false && handler["enabled"] != false &&
									service["enabled"] == true && service["matches_opened_data_dir"] == true {
									pattern, _ := g["matcher"].(string)
									// Installed configuration is inspectable; client dispatch still needs a native event.
									if pattern == "" || matcherAccepts(pattern, tool) {
										routed = true
									}
								}
							case "PostToolUse":
								expected = probes.postMatcher(name)
							case "PostToolUseFailure":
								expected = probes.failureMatcher(name)
							}
							if expected != "" && service["matches_opened_data_dir"] == true && g["matcher"] != expected {
								mismatchedMatchers++
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
	report["probe_matcher_mismatches"] = mismatchedMatchers
	if len(splitExecutables) > 0 {
		report["split_executables"] = splitExecutables
	}
	report["configuration_consistent"] = consistent && mismatchedMatchers == 0 && len(splitExecutables) == 0
	if tool != "" {
		report["tool_routed"] = routed
	}
}

// sameExecutable reports whether two configured commands name the same file.
func sameExecutable(a, b string) bool {
	x, err := canonicalForDoctor(a)
	if err != nil {
		return false
	}
	y, err := canonicalForDoctor(b)
	return err == nil && x != "" && x == y
}

func matcherAccepts(pattern, tool string) bool {
	re, err := regexp.Compile(pattern)
	return err == nil && re.MatchString(tool)
}
