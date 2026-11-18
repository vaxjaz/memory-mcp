# Upgrade to memory-mcp 0.6.2

The offline agent-upgrade ZIP contains five platform packages, the exact source archive, SOURCE_COMMIT and checksums for the enclosed archives. The release page provides a separate SHA256SUMS file that also covers the outer ZIP. The released executable reports version 0.6.2, schema 5, and the full source commit in build-info.

Verify the actual host, service name, original data_dir, bindings and disabled policy. STOP old writers and back up the COMPLETE original library/configuration/launchers BEFORE any new executable opens the library. Schema 5 is not readable by schema-3/4 programs; rollback restores the backup and old executable/configuration.

Extract the actual platform archive and follow docs/AGENT_INSTALL.md. Reuse original data and scope; do not create a replacement library or automatically bind historical projects. Run setup with the existing service name to refresh the managed matchers, then restart or reconnect the host and review hook trust as required by that host.

Expect seven tools and integration protocol 6. SessionStart and UserPromptSubmit establish the session and input boundaries. PreToolUse probes eligible operations. The configured lookup has exact-name Pre/Post observers and ZCode also registers its failure event for that lookup. A successful native Post confirms delivery. A missing result releases only the bound challenge without crediting delivery.

Verify version and source commit, preserved history and policy, and multi-query recall. For ZCode, check that an eligible mysql_query can trigger a challenge and that an actual lookup records lookup_result_observed before the original query is retried. Separate executable replay from real native events and model behavior. Never seed synthetic cases into the original library or invent business agreements. Installation consent is not business-rule approval. Keep all pre-upgrade backups.
