# memory-mcp 0.5.0-host-recall.1

Local agent memory. The host understands tasks, extracts verified experience, builds independent queries and judges applicability. MCP synchronously saves compact material and executes local full-text retrieval; it never calls a model or translates tasks.

Seven tools: memory_bootstrap, memory_experience_record, memory_reference_record, memory_experience_lookup, memory_search, memory_read, memory_cleanup.

Experiences preserve challenged inference, actual counterexample, impact, conditions, exceptions and evidence. Agreement references preserve an explicitly confirmed source, authority and scope without inventing failures. Historical prose remains searchable without becoming an intervention rule.

Lookup accepts host-generated queries (terms plus all/any), searches complete recorded features, combines rankings and reports matched queries. Complete items fit a single 8 KiB JSON text response; next_cursor exposes remaining results. No whole-library injection, per-turn reminders, raw transcript capture, prepare/complete ritual, async writing or Stop continuation.

Selective hooks retain the bounded query challenge. Code/business parameters can trigger on specific features. Selected business results only record identifier hashes; a subsequent operation must concern the same identifier. Unknown tools, missing identifiers and pure-text outputs remain uncovered. Retrieval does not prove consumption or authorization.

Install: docs/AGENT_INSTALL.md. Package: docs/AGENT_UPGRADE_PACKAGE.md. Protocol/costs: docs/EXPERIENCE_TRIAL.md. Acceptance: docs/HOST_TRIAL.md.

Schema 4 adds a transactional inverted index. Stop old writers and back up the COMPLETE original library/configuration BEFORE any new executable opens it. Schema-3 programs reject this database: rollback restores the pre-upgrade backup. Never mix versions. Index migration does not rewrite source entries or history.
