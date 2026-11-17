# memory-mcp 0.6.0-delivery.1

Local verified-experience memory. The host understands the current task, constructs queries and judges applicability. MCP stores compact counterexamples and performs local retrieval; it never calls another model.

Seven tools: memory_bootstrap, memory_experience_record, memory_reference_record, memory_experience_lookup, memory_search, memory_read, memory_cleanup.

Specific observed entrances map to stable, evidenced associations (cues). An undelivered experience requests one lookup through a real challenge. Only the actual successful native result credits the returned bodies and its trigger cues. Later entrances for the same cue are silent; a different undelivered association emits one bounded historical sentence while allowing the operation. Another experience still requires its own body. New turns do not reset delivery. There is no global lookup permit, cooldown, model-authored acknowledgement, per-turn experience reminder, raw transcript capture, async write or Stop continuation.

Lookup returns one JSON text copy of complete facts, conditions, exceptions and evidence within 8 KiB, with pagination. Index/entrance metadata stays out of the visible experience DTO. Voluntary lookup/read remains available. Delivery records describe system output, never understanding, applicability, authorization or an obligation to change code. Finding no current problem is a valid result.

Schema 5 replaces obsolete generated-response/cooldown state with context-scoped delivery records. Stop all old writers and independently back up the COMPLETE library/configuration before any new binary opens it. Existing history, IDs, bindings and disabled policies survive. Rollback restores the old executable/configuration AND pre-upgrade library. Never mix versions.

Install: [AGENT_INSTALL](docs/AGENT_INSTALL.md). Protocol: [EXPERIENCE_TRIAL](docs/EXPERIENCE_TRIAL.md). Measured acceptance and limits: [DELIVERY_ACCEPTANCE](docs/DELIVERY_ACCEPTANCE.md). Upgrade bundle: [AGENT_UPGRADE_PACKAGE](docs/AGENT_UPGRADE_PACKAGE.md).
