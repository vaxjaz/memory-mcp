# memory-mcp

Stable release 0.6.2. The 0.6.3 development build uses one cue-based recognition model and adds [project-specific tool probes](docs/AGENT_INSTALL.md#register-a-custom-tool).

Local verified-experience memory. The host understands the current task, constructs queries and judges applicability. MCP stores compact counterexamples and performs local retrieval; it never calls another model.

Seven tools: memory_bootstrap, memory_experience_record, memory_reference_record, memory_experience_lookup, memory_search, memory_read, memory_cleanup.

Specific observed entrances map to stable, evidenced associations (cues). An undelivered experience requests one lookup through a real challenge. Only the actual successful native result credits the returned bodies and its trigger cues. Later entrances for the same cue are silent; a different undelivered association emits one bounded historical sentence while allowing the operation. Another experience still requires its own body. New turns do not reset delivery. There is no global lookup permit, cooldown, model-authored acknowledgement, per-turn experience reminder, raw transcript capture, async write or Stop continuation.

Lookup returns one JSON text copy of complete facts, conditions, exceptions and evidence within 8 KiB, with pagination. Index/entrance metadata stays out of the visible experience DTO. Voluntary lookup/read remains available. Delivery records describe system output, never understanding, applicability, authorization or an obligation to change code. Finding no current problem is a valid result.

The 0.6.3 development build uses integration protocol 11 and schema 9. An existing library requires [explicit migration](docs/AGENT_INSTALL.md#upgrade-to-the-cue-schema) before setup. Selected SQL tools, including ZCode's mysql_query, probe bounded sql/query/statement fields. Lookup delivery diagnostics distinguish generated, observed and missing results.

Upgrades from older schemas rebuild derived indexes and workflow state. Stop all old writers and independently back up the COMPLETE library/configuration before any new binary opens it. Existing history, IDs, bindings and disabled policies survive. Rollback restores the old executable/configuration AND pre-upgrade library. Never mix versions.

Start with the [release downloads](https://github.com/vaxjaz/memory-mcp/releases/tag/v0.6.2) and the [installation guide](docs/AGENT_INSTALL.md). See the [upgrade bundle contents](docs/AGENT_UPGRADE_PACKAGE.md), [experience protocol](docs/EXPERIENCE_TRIAL.md), and [acceptance results](docs/DELIVERY_ACCEPTANCE.md).
