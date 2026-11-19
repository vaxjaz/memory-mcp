# Install memory-mcp

When an existing library predates schema 6, complete the [cue migration](#upgrade-to-the-cue-schema) before switching its service.

When the user requests installation/upgrade, identify the actual host and execution OS, service name, original data_dir, real project directory and stable scope from configuration. This document does not grant permission by itself. Never guess from the model name or create a replacement empty library.

1. Download the platform archive or the agent-upgrade ZIP from GitHub Releases. Verify SHA256SUMS and the SOURCE_COMMIT marker against build-info. Check that the reported version matches the archive and the reported source commit matches SOURCE_COMMIT. These checks do not open the database.
2. Stop all old writers to the original library. Make a complete independent backup of the data directory including WAL and of the host config/hooks/launcher. Keep the old executable for rollback. All DB-opening commands, including doctor/setup, come after this backup.
3. Install the new executable alongside the old one. Run:

       memory-mcp setup --host <actual-host> --config <actual-config> --data-dir <original-library> --project <actual-project> --scope <stable-project-scope> --server-name <actual-service-name>

Setup replaces only this service's managed handlers and arguments, including previous mode/capture/Stop definitions. It preserves other plugins, environment settings, project files, all historical entries and bindings. No AGENTS/CLAUDE changes. The new executable rejects old workflow flags rather than silently reactivating them.

4. Use doctor to verify configuration_consistent, binding_verified, wakeup_enabled, experience_hooks_installed and experience_index. All library-sharing clients must use this version. Historical plaintext stays searchable; do not batch-promote it or insert synthetic trial cases into the original library.
5. Restart/reconnect the real host's MCP service, review updated hooks when the host requests trust, and verify actual bootstrap version and the exact seven-tool catalog. Configuration and separate stdio checks do not prove native hook delivery. Do not fabricate trust hashes or event receipts.
6. For ZCode, fully restart after setup regenerates matchers. Check a known, specific experience entrance in an isolated SQL query using mysql_query, or in an eligible Bash/Grep/Read operation. A fresh session should pause on the first relevant entrance, observe the lookup's successful native Post result, then let the original operation retry. The doctor hook log reports lookup_result_observed or lookup_result_missing without storing SQL or result text.
7. Test actual SessionStart/UserPromptSubmit/PreToolUse and the exact configured lookup Pre/PostToolUse plus narrowly selected business PostToolUse. Ordinary inputs emit no reminder, no-match probes emit nothing, specific cue entrances request memory_experience_lookup with host-generated queries. Its native Pre/Post success observer credits only returned bodies; another cue can append one bounded fact. Repeated cues and ordinary turns remain silent. A delivery receipt does not certify applicability.

--disable-wakeup disables selective pauses for this project; no older mode is activated. --enable-wakeup restores them. Preserve the policy on ordinary reinstall. Unbound projects remain silent. Existing bound projects not previously opted in are not silently enrolled by upgrading a different project.

memory_experience_record saves verified failure features; memory_reference_record registers compact pointers to explicitly confirmed agreements. Corrections and autonomous discoveries need current evidence. The host builds independent queries and judges the returned conditions; MCP does not translate tasks. memory_cleanup remains separate confirmed maintenance.

## Upgrade to the cue schema

Version 0.6.3 development builds use database schema 6 and integration protocol 7. Existing libraries must complete this step **before** setup or serve. New empty libraries start at schema 6.

1. Run the read-only preview.

       memory-mcp migrate --data-dir <original-library>

   Review current_entries_needing_cues. The list contains existing experience and agreement IDs that have no cues. Existing cues are converted without changing their stored facts. Legacy selector fields are removed, and existing object identifier prefixes move to object_id_prefixes.
2. Stop every writer sharing the library. Back up the complete data directory and client configuration, then run migration with a new SQLite backup destination.

       memory-mcp migrate --data-dir <original-library> --apply --backup <new-backup.db>

3. Run setup with the existing service name, data directory and project binding. The listed records remain readable as historical evidence. To activate one, verify its sources, create appropriate cues, and resubmit the same kind of record with that entry's ID as previous_id.

## Customize tool probes

Available starting with the 0.6.3 development build. Create a UTF-8 file named tool-probes.json in the **original memory data directory**. Use the same project scope as setup. The example adds two ZCode file-tool probes for SQL and one MCP tool whose server name may differ by installation.

Use `memory-mcp guide` or `memory_bootstrap` to get a minimal example from the current executable. The bootstrap feature is `custom_tool_probes`.

The Write and Edit examples below target files inside the **bound project directory**. For targets in a separate documentation repository, see the path-suffix condition in the guide under **Writes to a separate repository**.

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

Check `doctor` before replaying a hook. If a probe allows the action, run `doctor --session <event-session-id>` using the same host, config, data directory, project and scope. In `recent_events`, `probe_config_invalid` records fallback to built-in probes. `custom_probe_no_clues` and `custom_probe_no_match` indicate where a configured probe stopped. An `experience_probe` with `outcome: "silent"` in its detail indicates a match with no new delivery. An empty list means there is no recorded event for that session.

When `custom_probe_input` is present, look at its `outcome` and the selected fields' statuses and UTF-8 lengths. A project path rejection, an extension mismatch and the 32 KiB budget are distinct outcomes. The diagnostic contains no input values.

For conditional probes and cross-call facts, run `memory-mcp guide` and see **Conditions and previous results**. Validate the sample against the actual tool events with `doctor`. Producer selectors need a setup refresh so the client sends both the PreToolUse and PostToolUse events. Preserve case-specific cues and use `doctor --session` to see whether a branch passed, failed or has unknown context.

Rollback: stop all new writers and restore the old program/configuration and the complete pre-upgrade library together. Merely pointing the old binary at a post-upgrade library is not a tested rollback.
