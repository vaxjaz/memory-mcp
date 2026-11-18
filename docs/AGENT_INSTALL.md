# Install memory-mcp 0.6.2

When the user requests installation/upgrade, identify the actual host and execution OS, service name, original data_dir, real project directory and stable scope from configuration. This document does not grant permission by itself. Never guess from the model name or create a replacement empty library.

1. Download the 0.6.2 platform archive or the agent-upgrade ZIP from GitHub Releases. Verify SHA256SUMS and the SOURCE_COMMIT marker against build-info. The released program reports version 0.6.2 and schema_version 5. These checks do not open the database.
2. Stop all old writers to the original library. Make a complete independent backup of the data directory including WAL and of the host config/hooks/launcher. Keep the old executable for rollback. All DB-opening commands, including doctor/setup, come after this backup.
3. Install the new executable alongside the old one. Run:

       memory-mcp setup --host <actual-host> --config <actual-config> --data-dir <original-library> --project <actual-project> --scope <stable-project-scope> --server-name <actual-service-name>

Setup replaces only this service's managed handlers and arguments, including previous mode/capture/Stop definitions. It preserves other plugins, environment settings, project files, all historical entries and bindings. No AGENTS/CLAUDE changes. The new executable rejects old workflow flags rather than silently reactivating them.

4. Use doctor to verify configuration_consistent, binding_verified, wakeup_enabled, experience_hooks_installed and experience_index. All library-sharing clients must use this version. Historical plaintext stays searchable; do not batch-promote it or insert synthetic trial cases into the original library.
5. Restart/reconnect the real host's MCP service, review updated hooks when the host requests trust, and verify actual bootstrap version and the exact seven-tool catalog. Configuration and separate stdio checks do not prove native hook delivery. Do not fabricate trust hashes or event receipts.
6. For ZCode, fully restart after setup regenerates matchers. Check a known, specific experience entrance in an isolated SQL query using mysql_query, or in an eligible Bash/Grep/Read operation. A fresh session should pause on the first relevant entrance, observe the lookup's successful native Post result, then let the original operation retry. The doctor hook log reports lookup_result_observed or lookup_result_missing without storing SQL or result text.
7. Test actual SessionStart/UserPromptSubmit/PreToolUse and the exact configured lookup Pre/PostToolUse plus narrowly selected business PostToolUse. Ordinary inputs emit no reminder, no-match probes emit nothing, specific cue entrances (or supported legacy anchor/condition hits) request memory_experience_lookup with host-generated queries. Its native Pre/Post success observer credits only returned bodies; another cue can append one bounded fact. Repeated cues and ordinary turns remain silent. A delivery receipt does not certify applicability.

--disable-wakeup disables selective pauses for this project; no older mode is activated. --enable-wakeup restores them. Preserve the policy on ordinary reinstall. Unbound projects remain silent. Existing bound projects not previously opted in are not silently enrolled by upgrading a different project.

memory_experience_record saves verified failure features; memory_reference_record registers compact pointers to explicitly confirmed agreements. Corrections and autonomous discoveries need current evidence. The host builds independent queries and judges the returned conditions; MCP does not translate tasks. memory_cleanup remains separate confirmed maintenance.

Version 0.6.2 uses schema 5 and integration protocol 6. Existing schema-5 history and delivery records carry forward. Upgrades from schemas 3/4 rebuild derived state, including context-scoped delivery records and cue entrances, while preserving source history. The pre-upgrade backup MUST precede every database-opening command. Older schema-3/4 executables cannot open the upgraded library. Index migration preserves original IDs, bodies, profiles and history without promoting legacy prose into trigger rules.

## Customize tool probes

Available starting with the 0.6.3 development build. Create a UTF-8 file named tool-probes.json in the **original memory data directory**. Use the same project scope as setup. The example adds two ZCode file-tool probes for SQL and one MCP tool whose server name may differ by installation.

```json
{
  "projects": {
    "project:example/team-repo": [
      {
        "tool": "Write",
        "path_field": "file_path",
        "text_fields": ["content"],
        "extensions": [".sql"]
      },
      {
        "tool": "Edit",
        "path_field": "file_path",
        "text_fields": ["new_string"],
        "extensions": [".sql"]
      },
      {
        "mcp_tool": "warehouse_query",
        "text_fields": ["arguments.statement"]
      }
    ]
  }
}
```

- Use tool for an exact, case-sensitive name in the client's tool event. Use mcp_tool for a bare MCP tool name such as warehouse_query. It matches names of the form mcp__<server>__warehouse_query across server names. An exact rule takes priority over an overlapping MCP rule.
- The default mode, append, adds the selected fields to the built-in probe. Use "mode": "replace" to use only your rule for the selected tool in this project. For example, a replace rule for Grep with "text_fields": ["pattern"] scans only pattern. Removing a rule restores the built-in behavior.
- Field names follow the client's actual tool_input payload. Dots access nested objects. Only the specified strings are scanned. A path_field selects and checks a project file path. Relative paths use the event working directory within the bound project. When extensions is present, it limits the probe to those file suffixes.
- After adding, renaming or removing a tool selector, rerun setup with the original host, MCP service name, project, scope and data directory, then restart the client. Field-only edits apply to subsequent events for already dispatched tools. Use doctor to check probe_config_valid, custom_probe_rules and probe_matcher_mismatches. An invalid file is rejected by setup. Running hooks keep their built-in probes when an edited file is invalid.
- The same rule format works with the ZCode, Codex, Qoder CLI and Qoder IDE adapters. Tool names and payload fields can vary among hosts. Check the native event shape when reusing a rule. The probes use the existing experience entries and delivery rules. Adding a tool or field alone does not create a reason to block.

Rollback: stop all new writers and restore the old program/configuration and the complete pre-upgrade library together. Merely pointing the old binary at a post-upgrade library is not a tested rollback.
