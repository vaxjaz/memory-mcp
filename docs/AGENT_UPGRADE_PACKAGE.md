# Upgrade memory-mcp

This offline bundle contains platform archives, the exact source, `SOURCE_COMMIT` and inner checksums. Verify the outer archive against the release checksum when available. Select the platform archive and use `build-info` to verify its version, schema and full source commit.

The agent should execute the requested upgrade using `docs/AGENT_INSTALL.md` in the extracted platform archive. Identify the real client, service, original library and verified scope. Stop all old writers, save a complete pre-upgrade backup and keep the old executable. Preview the schema with `migrate` and automatically apply the supported conversion with `--apply --backup` when required. Convert legacy tool registrations, refresh setup with the original library and bindings, and reconnect the actual client.

Before activating the migrated cues, audit the existing records with the original scope and follow the full inventory cursor. Trace each rule through the project's existing implementation. Verify its behavior with tests, an isolated replay or the relevant historical code revision. Use business sources to establish domain intent and scope. **Semantic distillation of stored records is prohibited.** The old text locates a case. The evidence determines its revised rule and trigger. For a first installation, follow the default historical-code initialization in the same guide.

For duplicate consolidation or archival, show the exact affected IDs and retained evidence in a cleanup preview. Apply it only after fresh user approval. Summarize the result in a few lines with the installed version, migration counts, concrete verified trigger examples and outstanding decisions. If verification fails, restore the stopped service's old executable, configuration and full backup together.
