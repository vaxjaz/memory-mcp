# Verified experience and incremental delivery

Follow current user instructions and project rules. The host reconstructs the full visible task, builds grounded queries and judges applicability. MCP performs lexical retrieval, not semantic interpretation, translation or another model call.

After a correction, fix the current task first. Skip experience writing when rereading the original wording resolves the error, such as a missed qualifier, mistaken referent or misunderstood scope. To record an evidenced counterexample or a newly verified boundary, identify which specific future judgment it changes when a similar situation recurs and cite the evidence for that change.

For every memory_experience_record call, put `admission` beside `experience`. Send `{"admission":{"future_decision":"Check each independent entry point before treating the local validation as complete.","evidence_ref":"trace:second-entry-point"}}` as part of the submission. Copy the exact `evidence_ref` from the actual `experience.evidence[].reference` in that same submission. New records and previous_id revisions use the same check. Missing or invalid admission rejects the write and leaves the library unchanged. Admission text is used for validation and stays outside recall and the stored experience body. A submitted reference is evidence to verify, and passing the check does not establish the host's interpretation.

Save the challenged inference, actual counterexample, impact, conditions, exceptions and compact evidence synchronously. A cue supplies a stable historical fact with evidence_refs and directly supported entrances. Several file/symbol/config/business aliases for the same association share one cue_id. Pure business evidence and entrances are valid. Skip ordinary recoverable facts, progress and finding no problem. Do not expand dependency graphs or invent incidents.

A new risk, changed condition or boundary is a material previous_id revision (or an independent new experience), not a short-only alias. Alias/line/hash relocation preserves the existing version and delivery records. Body, cues and index commit atomically. request_id retries the same record payload; indexed is only a storage receipt.

For a revision, use the full current ID and send the complete revised body. Copy each cue reference exactly from the submitted evidence[].reference values. If evidence is removed, review the cues that cited it in the same submission. Resolve the listed field errors together before resubmitting.

Use memory_reference_record for compact pointers to existing explicitly confirmed agreements. Include authority, confirmation locator, scope, exceptions and evidence-backed cues. Indexing creates no authority. Changes of meaning/scope require the relevant authorization.

After a successful new write or revision, including an entrance-only update, tell the user in one sentence how to encounter the recorded cue in normal work. Use one or two entrances from trigger_preview and the bound scope. Mention the tool, field or conditions if verified in the active host. A preview reports eligibility and the stored wakeup policy. If an entrance is marked conditional, describe the full saved expression or check it with doctor before stating how to trigger it. Actual delivery still depends on the host dispatching a matching probe and the current delivery state. If the record is ineligible or wakeup is disabled, say so. For additional entrances, read the saved revision by ID. Report an exact submission retry once.

To verify a direct trigger before calling it ready, use doctor --probe EVENT.json --expect RECORD_ID or an actual hook record that lists that cue. A result of a lookup alone verifies retrieval, separate from a direct trigger. The probe check is read-only and reports where to correct a miss. See **Check a trigger before using it** below.

Use literal entrances in cue match expressions for distinctive trigger phrases and identifiers. Optional object_id_prefixes belongs to the same-input business-result association only. Run memory-mcp migrate to inspect an older library. When the report lists a record without cues, verify its evidence and submit a revised cue with previous_id.

Lookup task/questions come from the whole current task and concrete uncertainty. Supply 1-4 independent grounded queries, terms with match=all (default) or any. keywords remains compatible. Similar words and source locations do not establish applicability. Check current evidence; no hit does not prove no risk. Up to three complete items (default two) fit one JSON text body <=8 KiB. Follow next_cursor or refine queries; changed queries/corpus require restarting. Legacy excerpts are navigation; read freely for full historical source. To enumerate historical records in a scope, call memory_search with an empty query and follow next_cursor until it ends. Keep the same filters on each page. Use inventory.next_cursor with memory_cleanup inspect and inventory.review_next_cursor with memory_search and needs_review=true.

Only specific observable index hits can intervene. If the body is undelivered, use the actual issued challenge_id to look up candidates; the original operation is not automatically rerun. For a challenge ID, the hook requests lookup at most once. Repeated or parallel hits already covered by that challenge proceed while the receipt is pending. Continue a partial result using the same ID and next_cursor. Use `memory-mcp doctor --challenge ID --data-dir LIBRARY` to distinguish query_requested, lookup_in_progress, response_pending_observation, partial_delivery, delivered, stale_context and technical failure or expiry. Status inspection is read-only. Only a verified native receipt credits delivery. Native Pre/Post tool_use_id associates the actual result with its original consumer/context, including late results. Successful results credit only actual complete returned items and that challenge's trigger cues. Errors fail open without credit. No acknowledgement/skip/complete tool exists.

After body delivery, another entrance for the same cue is silent. A new association of that body permits the operation with one fixed, evidenced short historical fact. Do not interpret this as an instruction to investigate or modify a named class. Ordinary turns preserve delivery; an explicit context reset starts lazy first delivery when a related entrance is seen, without reinjecting the library. Separate native agent identities are independent. Missing call IDs/hooks and unreported pruning are coverage gaps. Output is appended context, not a replaceable memory slot; accumulated context still has costs.

You may judge an experience inapplicable, already repaired or consistent with correct current code, and continue without any refusal registration, forced investigation or write-back. Voluntary lookup/read is always available. A receipt never proves model understanding, current applicability, completed investigation or permission.

Selected business results preserve record boundaries (including MCP JSON text and structuredContent) before matching. Direct and observed candidates are combined; ambiguous multi-object prose creates no inferred association. Only identifier hashes and matched candidate IDs are retained. No raw prompt, command or result is persisted. No per-turn experience reminder, time/count reminder, extra model, async write or Stop continuation. Existing explicit maintenance stays separate: preview a reversible plan and obtain actual fresh approval for that shown plan before confirm/apply; undo needs fresh approval. Preserve history and backups.

## Register the tool and name its fields

Put `tool-probes.json` in the running service's `data_dir`. Use the bound scope from `doctor`. First check a representative event with `doctor --probe EVENT.json`. `observed_fields` lists paths, types and string lengths from that sample. For Write and Edit, `registration_template` shows the suggested mapping. Compare the template to the fields your client actually sent.

If you do not have an event sample, register only the selector, for example `{"tool":"Write"}` or `{"mcp_tool":"warehouse_query"}`. Run setup, restart, then use the tool once in that project and run `doctor --session SESSION_ID`. The `tool_field_shape` rows show field paths and types for observed PreToolUse and successful PostToolUse events. A selector with no fields or observations is for inspection and does not challenge the operation. Copy a confirmed path into `fields`. Save the match in your cue.

```json
{
  "projects": {
    "project:example/team-repo": [
      {"tool": "Write", "fields": {"path": "file_path", "text": "content"}},
      {"mcp_tool": "warehouse_query", "fields": {"text": "arguments.statement"}}
    ]
  }
}
```

In `fields` the left side names a value for cues. The right side is a path inside `tool_input`, with dots for nested objects. The example makes `tool_input.content` available to a cue as `text`. Use `tool` for an exact native tool name, including a full MCP name when targeting one server. Use `mcp_tool` for a bare name across MCP servers. A project entry takes precedence over a preset. An exact tool name takes precedence over a bare MCP name within the project. Set `enabled: false` to skip that tool in the project.

Bash search commands (including rg), Grep, Read and recognized MCP query tools such as mysql_query already have presets. Their primary command, pattern or SQL is available as `text`. Read paths are available as `path`. Use `tool_input.fields` to confirm what the tool actually exported. The Bash preset covers search operations. A project can explicitly map Bash's command field if other shell operations should participate. An invalid tool configuration yields `probe_config_invalid`; tool probes pause until the file is corrected.

Run setup and restart the client after changing tool selectors. `doctor` reports `probe_config_valid`, `custom_probe_rules` and `probe_matcher_mismatches`. Adjusting a cue's match or changing a field mapping for an already routed tool applies to subsequent calls. A valid configuration confirms the on-disk settings. Native events confirm what the client sends.

## Define when a cue matches

The following cue matches when Write targets a .sql file and its proposed content has both the UPDATE token and a column containing fix_third. The same column in a SELECT, a .txt file, or the filename alone does not trigger this cue. Replace the evidence reference with an exact reference in your submission.

```json
{
  "cue_id": "settlement_code",
  "label": "Settlement field",
  "text": "The two isolated environments returned different values for the same field.",
  "evidence_refs": ["verified-source"],
  "matches": [{
    "when": {"all": [
      {"field": "tool", "op": "eq", "value": "Write"},
      {"field": "path", "op": "suffix", "value": ".sql"},
      {"field": "text", "op": "token", "value": "UPDATE"}
    ]},
    "field": "text",
    "op": "contains",
    "entrances": ["fix_second", "fix_third"]
  }]
}
```

| Match property | Behavior |
| --- | --- |
| `matches` | Any matching expression is enough for this cue. A simple expression such as `{"op":"token","entrances":["findCustomer"]}` works with preset and registered tools. |
| `when` | Optional prerequisite on one match expression. Check the native tool name in `tool`, mapped inputs such as `path`, or an earlier result with `fact`. Use `all`, `any` or `not` for combinations. If it is false or unknown, this expression does not match. |
| `entrances` | Literal trigger phrases or identifiers. Any string in a leaf can match. A leaf with no `field` searches the exposed strings individually. Each successful alternative needs an entrance. |
| `value` | A comparison guard on a named field. Use it for an operation, path, switch, or result flag. |
| `all`, `any`, `not` | Require every condition, at least one condition, or a false child. Missing fields or unavailable facts are unknown, and negating unknown leaves it unknown. |
| `token`, `contains` | `token` uses ASCII identifier boundaries, including underscores. `contains` finds a substring within one field. Use it for a partial identifier such as `fix_third`. |
| `eq`, `prefix`, `suffix`, `exists` | Compare the whole value, its beginning, its end, or its presence. `eq` also accepts boolean and number values and preserves their types. `exists` takes no value. |
| `case_sensitive`, `normalize` | Entrances retain original spelling. String comparisons default to case-insensitive. Set `case_sensitive: true` on a comparison to require exact casing. `normalize: "path"` unifies slash styles, and `"whitespace"` ignores Unicode whitespace. |

For SQL queries, `text` can already be mapped by the mysql_query preset or by the warehouse_query example. To make the cue work for both file writing and queries, add a second expression to `matches` with an exact tool guard for the query and the desired SQL predicates. Use `value` inside `when` and put `entrances` beside it. Leave `when` out when an entrance alone should match. A path comparison interprets the event's filename as text, including a target in a separate documentation repository.

### Previous tool results

A tool can expose result fields through an observation. In this example, login exposes two fields for comparison with a cue. The ordered producer key uses `tool_input.connection_id`. The consumer names that same input value `connection`.

```json
{
  "projects": {
    "project:example/team-repo": [
      {"mcp_tool": "login", "observe": [{
        "name": "authenticated",
        "key": ["input.connection_id"],
        "fields": {"ok": "ok", "state": "status"},
        "ttl_seconds": 120
      }]},
      {"mcp_tool": "warehouse_query", "fields": {
        "text": "arguments.statement", "connection": "connection_id"
      }}
    ]
  }
}
```

In the query cue's `matches`, place the following expression alongside any other trigger expressions. The authentication comparison belongs to this cue.

```json
{
  "when": {"all": [
    {"field": "text", "op": "token", "value": "UPDATE"},
    {"fact": {"name": "authenticated", "key": ["connection"], "match": {
      "all": [
        {"field": "ok", "op": "eq", "value": true},
        {"field": "state", "op": "eq", "value": "READY"}
      ]
    }}}
  ]},
  "field": "text",
  "op": "contains",
  "entrances": ["fix_third"]
}
```

Observation field paths are relative to the result object, which can be a direct object, MCP structuredContent or a single JSON-object text block. A producer key has 1–4 ordered scalar paths prefixed with `input.` or `result.`. Use the same value types and order on the consumer. The hook needs the same native call ID on PreToolUse and PostToolUse, plus the session and input boundary. Default lifetime is the current input. Set `lifetime: "session"` to retain a fact across inputs within its TTL. TTL defaults to 120 seconds and is capped at 600. Only the comparison outcome and hashes are stored. Rerun the producer after changing the result predicate.

### Check a trigger before using it

Save a sample with the client's `tool_name` and `tool_input`. Use `--project` if the sample omits `cwd`. For example, save the following as EVENT.json.

```json
{"tool_name":"Write","tool_input":{"file_path":"example.sql","content":"update demo SET work_order_feedback_fix_third = 1;"}}
```

```text
memory-mcp doctor --host zcode --config CONFIG --data-dir LIBRARY --project PROJECT --probe EVENT.json --expect RECORD_ID --expect-cue settlement_code
```

Read `outcome` and `next`. For `tool_not_registered`, use `observed_fields` to map the actual paths. For `tool_not_routed`, rerun setup and refresh the client. For `no_scanned_fields` or missing named inputs, check `tool_input.fields` for missing, empty, unsupported or oversized values. For `cue_missed`, check `cue_checks`. A check with `stage: "when"` reports whether the prerequisite passed, failed, or lacked an input or fact. Its `conditions` give the tested fields. After it passes, the next check reports whether the entrance matched. Try the expected matching sample and a similar sample that should pass without a challenge. Include `--session SESSION_ID` or the native event identity if testing a saved result fact. The probe check executes no tool and writes no hook event, fact, challenge or receipt. It does not verify native dispatch.

After a real operation, use `doctor --session SESSION_ID` to inspect `recent_events`. An `experience_probe` with `candidate_ids` and a `silent` outcome means the cue matched and had no new delivery. A successful lookup can also retrieve a cue absent from the trigger candidates. Inspect the candidates before concluding that a cue triggered.
