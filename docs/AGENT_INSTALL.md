# Install 0.6.1-delivery.1

When the user requests installation/upgrade, identify the actual host and execution OS, service name, original data_dir, real project directory and stable scope from configuration. This document does not grant permission by itself. Never guess from the model name or create a replacement empty library.

1. Verify package checksum, SOURCE_COMMIT, version and build-info. These commands do not open the database.
2. Stop all old writers to the original library. Make a complete independent backup of the data directory including WAL and of the host config/hooks/launcher. Keep the old executable for rollback. All DB-opening commands, including doctor/setup, come after this backup.
3. Install the new executable alongside the old one. Run:

       memory-mcp setup --host <actual-host> --config <actual-config> --data-dir <original-library> --project <actual-project> --scope <stable-project-scope>

Setup replaces only this service's managed handlers and arguments, including previous mode/capture/Stop definitions. It preserves other plugins, environment settings, project files, all historical entries and bindings. No AGENTS/CLAUDE changes. The new executable rejects old workflow flags rather than silently reactivating them.

4. Use doctor to verify configuration_consistent, binding_verified, wakeup_enabled, experience_hooks_installed and experience_index. All library-sharing clients must use this version. Historical plaintext stays searchable; do not batch-promote it or insert synthetic trial cases into the original library.
5. Restart/reconnect the real host's MCP service, review updated hooks when the host requests trust, and verify actual bootstrap version and the exact seven-tool catalog. Configuration and separate stdio checks do not prove native hook delivery. Do not fabricate trust hashes or event receipts.
6. Test actual SessionStart/UserPromptSubmit/PreToolUse and the exact configured lookup Pre/PostToolUse plus narrowly selected business PostToolUse. Ordinary inputs emit no reminder, no-match probes emit nothing, specific cue entrances (or supported legacy anchor/condition hits) request memory_experience_lookup with host-generated queries. Its native Pre/Post success observer credits only returned bodies; another cue can append one bounded fact. Repeated cues and ordinary turns remain silent. A delivery receipt does not certify applicability.

--disable-wakeup disables selective pauses for this project; no older mode is activated. --enable-wakeup restores them. Preserve the policy on ordinary reinstall. Unbound projects remain silent. Existing bound projects not previously opted in are not silently enrolled by upgrading a different project.

memory_experience_record saves verified failure features; memory_reference_record registers compact pointers to explicitly confirmed agreements. Corrections and autonomous discoveries need current evidence. The host builds independent queries and judges the returned conditions; MCP does not translate tasks. memory_cleanup remains separate confirmed maintenance.

Schema 5 adds context-scoped delivery state and atomically updated cue entrances; obsolete generated receipts/cooldowns are discarded without rewriting source history. The pre-upgrade backup MUST precede every database-opening command. Older schema-3/4 executables cannot open the upgraded library. Index migration preserves original IDs, bodies, profiles and history without promoting legacy prose into trigger rules.

Rollback: stop all new writers and restore the old program/configuration and the complete pre-upgrade library together. Merely pointing the old binary at a post-upgrade library is not a tested rollback.
