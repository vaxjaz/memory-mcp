# Verified experience and incremental delivery

Follow current user instructions and project rules. The host reconstructs the full visible task, builds grounded queries and judges applicability. MCP performs lexical retrieval, not semantic interpretation, translation or another model call.

After a correction or autonomous discovery is verified, synchronously memory_experience_record the challenged inference, actual counterexample, impact, conditions, exceptions and compact evidence. Include stable cues: complete historical facts with evidence_refs and directly supported entrances. Several file/symbol/config/business aliases for the same association share one cue_id. Pure business evidence/entrances are valid. Do not expand dependency graphs or invent incidents. Validation checks mechanics, not truth. Ordinary facts, progress and finding no problem do not qualify.

A new risk, changed condition or boundary is a material previous_id revision (or an independent new experience), not a short-only alias. Alias/line/hash relocation preserves the existing version and delivery records. Body, cues and index commit atomically. request_id retries the same submission; indexed is only a storage receipt.

For a revision, use the full current ID and send the complete revised body. Copy each cue reference exactly from the submitted evidence[].reference values. If evidence is removed, review the cues that cited it in the same submission. Resolve the listed field errors together before resubmitting.

Use memory_reference_record for compact pointers to existing explicitly confirmed agreements. Include authority, confirmation locator, scope, exceptions and evidence-backed cues. Indexing creates no authority. Changes of meaning/scope require the relevant authorization.

After a successful new write or revision, including an entrance-only update, tell the user in one sentence how to encounter the recorded cue in normal work. Use one or two entrances from trigger_preview and the bound scope. Mention the tool, field or conditions if verified in the active host. A preview reports eligibility and the stored wakeup policy. Actual delivery still depends on the host dispatching a matching probe and the current delivery state. If the record is ineligible or wakeup is disabled, say so. For additional entrances, read the saved revision by ID. Report an exact submission retry once.

Use cues[].entrances for distinctive trigger phrases and identifiers. Optional object_id_prefixes belongs to the same-input business-result association only. A newer client can inspect an older library with memory-mcp migrate. When the migration report lists a record without cues, read its evidence, verify applicability, then submit a new cue with previous_id to activate the revised record.

Lookup task/questions come from the whole current task and concrete uncertainty. Supply 1-4 independent grounded queries, terms with match=all (default) or any. keywords remains compatible. Similar words and source locations do not establish applicability. Check current evidence; no hit does not prove no risk. Up to three complete items (default two) fit one JSON text body <=8 KiB. Follow next_cursor or refine queries; changed queries/corpus require restarting. Legacy excerpts are navigation; read freely for full historical source.

Only specific observable index hits can intervene. If the body is undelivered, use the actual issued challenge_id to look up candidates; the original operation is not automatically rerun. Response generation alone is not delivery. Native Pre/Post tool_use_id associates the actual result with its original consumer/context, including late results. Successful results credit only actual complete returned items and that challenge's trigger cues. Errors fail open without credit. No acknowledgement/skip/complete tool exists.

After body delivery, another entrance for the same cue is silent. A new association of that body permits the operation with one fixed, evidenced short historical fact. Do not interpret this as an instruction to investigate or modify a named class. Ordinary turns preserve delivery; an explicit context reset starts lazy first delivery when a related entrance is seen, without reinjecting the library. Separate native agent identities are independent. Missing call IDs/hooks and unreported pruning are coverage gaps. Output is appended context, not a replaceable memory slot; accumulated context still has costs.

You may judge an experience inapplicable, already repaired or consistent with correct current code, and continue without any refusal registration, forced investigation or write-back. Voluntary lookup/read is always available. A receipt never proves model understanding, current applicability, completed investigation or permission.

Selected business results preserve record boundaries (including MCP JSON text and structuredContent) before matching. Direct and observed candidates are combined; ambiguous multi-object prose creates no inferred association. Only identifier hashes and matched candidate IDs are retained. No raw prompt, command or result is persisted. No per-turn experience reminder, time/count reminder, extra model, async write or Stop continuation. Existing explicit maintenance stays separate: preview a reversible plan and obtain actual fresh approval for that shown plan before confirm/apply; undo needs fresh approval. Preserve history and backups.

## Custom tool probes

Put tool-probes.json in the running service's data_dir. Replace the example scope with the exact bound scope. Each project maps to an array of rules. This Write example selects files inside the bound project directory.

```json
{
  "projects": {
    "project:example/team-repo": [
      {"tool": "Write", "path_field": "file_path", "text_fields": ["content"], "extensions": [".sql"]},
      {"mcp_tool": "warehouse_query", "text_fields": ["arguments.statement"]}
    ]
  }
}
```

Use exactly one selector per rule. tool is the exact native event name. mcp_tool is the bare name matched across mcp__<server>__<tool>. Field names come from the actual tool_input. Dots select nested string fields. mode defaults to append. Use replace to scan only the configured fields for that tool.

Run doctor with the active --host, --config, --data-dir, --project and --scope before replaying a hook. Check probe_config_valid and custom_probe_rules. A selector change requires setup and a client restart; probe_matcher_mismatches checks the on-disk dispatch configuration. A valid configuration alone cannot confirm native dispatch.

Use doctor --session <event-session-id> with the same options to inspect recent_events for that session. probe_config_invalid means the file was rejected and the hook fell back to built-ins. custom_probe_no_clues means the rule supplied no usable input; custom_probe_no_match means no cue matched. experience_probe with outcome silent is a matched probe with no new delivery. Look for the recorded outcome before repeating with a fresh session.

custom_probe_input explains path_field rejections, extension mismatch, unavailable text fields and the 32 KiB text budget. It reports field names, statuses and UTF-8 byte lengths without path or text values. A field can be present even when a path or extension check rejected the branch.

### Writes to a separate repository

To match a Write from the bound session based on the target's filename and proposed content, use a field condition for the suffix. This also works when the target is in a separate documentation repository. Only the proposed content is scanned for cues. The condition compares the tool argument as text.

```json
{
  "tool": "Write",
  "mode": "replace",
  "when": {"field": "input.file_path", "op": "suffix", "value": ".sql"},
  "text_fields": ["content"]
}
```

The suffix comparison is case-sensitive. Add an any condition for other suffix spelling or combine a prefix condition to target a particular repository.

### Conditions and previous results

A rule accepts when plus scan fields, or branches with separate id, when and scan fields. Branches that pass contribute their own fields. Custom fields are matched separately and cue hits are deduplicated. With append, the built-in scan runs independently. Use replace to let the custom conditions control that tool's whole probe.

The following example uses fixture field names. Confirm the fields of the actual PreToolUse and PostToolUse payloads before adapting it.

```json
{
  "context_facts": {
    "project:example/team-repo": [{
      "name": "authenticated", "mcp_tool": "login",
      "key": ["input.service", "input.connection_id"],
      "when": {"field": "result.ok", "op": "eq", "value": true},
      "ttl_seconds": 120
    }]
  },
  "projects": {
    "project:example/team-repo": [{
      "mcp_tool": "warehouse_query", "mode": "replace",
      "branches": [
        {"id": "change", "text_fields": ["sql"], "when": {"all": [
          {"fact": "authenticated", "key": ["input.service", "input.connection_id"]},
          {"field": "input.sql", "op": "token", "value": "UPDATE"}
        ]}},
        {"id": "read", "text_fields": ["sql"], "when":
          {"field": "input.sql", "op": "token", "value": "SELECT"}}
      ]
    }]
  }
}
```

Use input.<field> to read tool input and result.<field> in an observer to read the result object. The latter can be a direct object, MCP structuredContent or a single JSON-object text block. all, any and not can nest. Operators are exists, eq, prefix, suffix and token. eq compares a string, boolean or number without coercion. prefix and suffix are case-sensitive. token uses case-insensitive whole ASCII identifier matching. A comparison with a missing value or missing fact is unknown. Negating unknown keeps it unknown. Only true branches scan.

An observer needs matching native call IDs and an observed input boundary. Its key is an ordered tuple of up to four scalars; large IDs belong in strings. Facts are scoped by host, project, session/agent and context generation. Default lifetime is input; session retains an observed fact across inputs up to its TTL. TTL defaults to 120 seconds and is capped at 600. A late result cannot become a new input's fact. Only a hash of the key and the predicate outcome are stored; the original result is not saved.

After adding a producer or consumer tool, run setup and restart to install the native matchers. doctor shows probe_context_fact_rules and checks Pre/Post dispatch configuration. doctor --session shows custom_probe_condition with branch outcomes passed, not_met or unknown and shows whether a result could be associated with its call. A result observer affects later calls.
