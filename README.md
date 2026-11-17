# memory-mcp 0.4.0-experience.1

Local experience memory for coding agents: synchronously record verified failure features, actively query them for a concrete uncertainty, then cross-check current code/runtime evidence.

There is one workflow. No selectable assistant/strict modes, per-turn reminders, raw prompt capture, checkpoint/review tools or Stop continuation are shipped.

Six MCP tools: memory_bootstrap, memory_experience_record, memory_experience_lookup, memory_search, memory_read, memory_cleanup. New knowledge is written only through the experience interface. Historical material remains read-only searchable. Maintenance consent is a separate audit record, hidden from business retrieval.

Install/upgrade: docs/AGENT_INSTALL.md. Standalone multi-platform bundle: docs/AGENT_UPGRADE_PACKAGE.md. Mechanism and known costs: docs/EXPERIENCE_TRIAL.md. Real-host acceptance: docs/HOST_TRIAL.md.

    memory-mcp setup --host <actual-host> --config <actual-config> --data-dir <original-library> --project <actual-project> --scope <stable-project-scope>

The requested project's wakeup defaults on when first configured. Explicit --disable-wakeup silences selective interruption without reactivating another workflow; --enable-wakeup re-enables it. Existing policies and other bindings are preserved. Do not mix old and new executables on one library. Back up the entire original library and configuration with writers stopped before opening it with the new version.
