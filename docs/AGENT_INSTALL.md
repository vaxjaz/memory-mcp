# Install memory-mcp

When an existing library predates schema 8, complete the [cue migration](#upgrade-to-the-cue-schema) before switching its service.

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

Version 0.6.3 development builds use database schema 8 and integration protocol 9. Existing libraries must complete this step **before** setup or serve. New empty libraries start at schema 8.

1. Run the read-only preview.

       memory-mcp migrate --data-dir <original-library>

   Schema 7 cue groups already have the new simple expression shape. The migration rebuilds the cue index and retains IDs, cue matches, evidence and delivery history. Review current_entries_needing_cues. The list contains existing experience and agreement IDs that have no cues. Schema 6 libraries keep their record IDs, evidence, cue IDs and delivery history. The migration replaces each cue's old typed entrances with matches groups containing literal strings and explicit operations. From schema 5 or earlier it also removes old selector fields and moves old object prefixes to object_id_prefixes.
2. Stop every writer sharing the library. Back up the complete data directory and client configuration, then run migration with a new SQLite backup destination.

       memory-mcp migrate --data-dir <original-library> --apply --backup <new-backup.db>

3. Run setup with the existing service name, data directory and project binding. The listed records remain readable as historical evidence. To activate one, verify its sources, create appropriate cues, and resubmit the same kind of record with that entry's ID as previous_id. For new writes, copy the **Define when a cue matches** example from `memory-mcp guide`.

## Register a custom tool

Run `memory-mcp guide` or call `memory_bootstrap` for the examples shipped by the actual binary. The bootstrap lists `tool_field_mapping`, `cue_match_expressions` and `tool_result_facts`.

1. Run `doctor --probe EVENT.json` against a representative event using the same host, config, data directory and project. Inspect `observed_fields` and the registration template if available. If you lack a sample, register just the tool name, run setup, restart and use the tool once. Read `tool_field_shape` in `doctor --session SESSION_ID` for the actual argument and result field names and types.
2. Put the registration in `tool-probes.json` under the existing project scope. For example, `{"tool":"Write","fields":{"text":"content","path":"file_path"}}` maps the native input fields for the cue. Use `mcp_tool` for a bare MCP name across servers.
3. Run setup after adding the selector and restart the host. Check `probe_config_valid` and `probe_matcher_mismatches` in doctor.
4. Save your cue with its comparison rules. Run `doctor --probe EVENT.json --expect RECORD_ID --expect-cue CUE_ID` for a matching sample and a near miss. Check real dispatch separately with `doctor --session SESSION_ID`.

### Convert tool registrations from dev.9 or earlier

Back up the existing `tool-probes.json` with the library. Rewrite it before running setup. Database migration preserves cue records and cannot infer which cue should own the conditions previously shared by a tool.

| Old configuration | New home |
| --- | --- |
| `text_fields: ["content"]` and `path_field: "file_path"` | `fields: {"text":"content","path":"file_path"}` on the tool registration. |
| `extensions: [".sql"]`, `when` and `branches` | Add `all` expressions to interested cues; for a filename use `{"field":"path","op":"suffix","value":".sql"}`. Separate branches are alternatives in `matches`. Set `case_sensitive: true` for old case-sensitive guards you want to preserve. Add an explicit path comparison if the cue should apply only to one repository. |
| `mode: "append"` or `"replace"` | A project entry defines the effective mapping. Include every native field the cue needs. Remove the entry to use the preset. |
| `context_facts` and result `when` | Add `observe` under the producing tool. Put the result comparisons and consumer key in the cue's `fact.match` and `fact.key`. Recreate a short-lived result fact by running the producer again. |

Invalid legacy keys are reported by doctor and rejected by setup. Tool probes pause while the invalid file is present. The per-field metadata and cue checks show where to fix a mismatch.

Rollback: stop all new writers and restore the old program, configuration and complete pre-upgrade library together.
