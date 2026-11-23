# Install or upgrade memory-mcp

When the user asks for installation or upgrade, carry out the steps for the actual client. Run `memory-mcp adapters` if needed. Resolve the operating system, client configuration, MCP service name, data directory, project path, scope and wakeup policy from the active configuration. Use the latest verified package in the user's chosen channel. Select the platform for the OS and CPU that run the MCP service. A package supplied for testing selects its candidate version.

The embedded guide's **Function model** section names the runtime functions: `W(x)` write judgment, `R(x, M)` recall, `F(j, E, m)` hot fusion and `P` verification budget. W and R run inside the service; F and P run in the host. The write-path changes in 0.6.4 are W's implementation.

## Select the path and verify the package

- **First install for this project** means no existing binding or library data for the project. If the client already has a memory service, use its library and add the verified project binding.
- **Upgrade** means the project already has a binding or library data. Reuse its service name, directory, scope and wakeup policy.
- Verify the selected platform archive against `SHA256SUMS`. Run `memory-mcp build-info` and compare the version and full source commit with the package name and `SOURCE_COMMIT`. Read the bundled `docs/AGENT_GUIDE.md` or run `memory-mcp guide` for that executable's schema and feature details.

## First install

1. Install the binary, bind the real project with the setup command below, and verify the live MCP version and binding as described under **Bind and verify the installation**.
2. By default, initialize verified pitfalls from the project's code and business history. Read the relevant implementation and tests. Use fix commits, regression reports, existing project assets and confirmed business rules to locate candidates. Reproduce the observed behavior with an existing test or an isolated fixture, or trace the code path that proves the behavior and its limits.
3. Record only a case that has a concrete counterexample and changes a future decision. Submit the case, exact evidence references, cue and required `admission` through the new service. Use one cue per distinct verified fact. If the project has no verified candidate, report zero initialized cases.
4. Check every new trigger with a positive sample and a nearby miss. Use the host's actual tool name and argument fields. Leave the real library free of synthetic acceptance fixtures.

## Upgrade to the cue schema

1. Stop all processes writing to the existing library. Back up the full data directory, including any WAL files, and the client configuration, managed hooks and launcher. Keep the original executable. Install the new executable alongside it.
2. Run the read-only migration preview. When `from_schema` is below `target_schema`, run the apply command automatically after the backup. When the schemas already agree, continue to configuration and record verification.

   ```text
   memory-mcp migrate --data-dir "<existing library>"
   memory-mcp migrate --data-dir "<existing library>" --apply --backup "<new SQLite backup file>"
   ```

   The migration converts supported historical data shapes and rebuilds derived indexes. Keep original IDs, revision chains, evidence and delivery history. `current_entries_needing_cues` lists records that remain historical evidence until verified and revised. 0.6.4 uses schema 10: schema 9 libraries (0.6.3 and dev.11–dev.13) convert automatically with the `--apply --backup` flow, and schema 10 libraries open directly. Libraries on older schemas (schema 8 and below, 0.5.x and earlier) use the same path: the apply step walks the migration chain from the detected schema with the same fresh-backup requirement, so run the read-only preview first and keep the rollback baseline. Experience writes and revisions require `admission.future_decision` and `admission.evidence_ref`. Three behavioral notes for upgrading agents: (1) experience bodies are two mutually exclusive forms — a sourced business `fact` with its qualifiers inside the sentence, or `pitfall` plus `counterexample`; `impact`/`applicability`/`boundary` remain optional legacy columns and a fact-form body that still fills them is rejected. (2) Authored cue text is rejected at the write boundary; a revision that omits it inherits the unchanged historical text only for a pure alias update. (3) The write path compares against same-project history: a `needs_comparison` response writes nothing and carries a prefilled `resolution_template` — fill `action` and `difference`, then resend the same draft and request_id with it. Committed request retries replay their original action; a changed action or target conflicts.

   The migration never rewrites stored bodies; a host-verified revision is the only path from a legacy shape to a current one.

   | Legacy record shape | Current form | How it converts |
   | --- | --- | --- |
   | Free body with separate `impact`/`applicability`/`boundary` columns | `fact` sentence carrying its object/environment/time qualifiers, or `pitfall` + `counterexample` | Columns stay readable as legacy. Verify the original evidence, then revise with the qualifiers written into the sentence and the columns left empty. |
   | Cue with authored `text` | Cue without authored text; reminders quote this version's body | Authored text is rejected at the write boundary. A pure alias revision inherits the unchanged historical text; a material revision carries no cue text. |
   | Record without cues | Historical evidence listed in `current_entries_needing_cues` | Rebuild a cue only from facts verified in project sources; never distill the stored text into one. |

3. Convert legacy configuration to the current format before setup. The tool registration table below covers field mappings and selectors. Rebuild each legacy trigger condition from verified project code and native events. If a condition's target cue or scope is unclear, keep the backup and report the exact rule that needs more evidence before activating the affected triggers. Use an isolated client profile to discover an unknown native field when needed.
4. Recheck every existing experience in the upgraded scope, including records that already have cues. Use the new executable to audit the upgraded library through MCP before the client resumes its hooks. Keep the backup intact. Enumerate the scope with `memory_search`, an empty query and `include_inactive: true`. Follow every `next_cursor` and inspect the revision chain whenever relying on an older version. For each existing rule, the host agent must go back to the project code and trace the real entry point, condition and outcome. Check tests, an isolated replay or the relevant historical code revision as evidence. Use business assets to verify the business rule and scope alongside the code. **Semantic distillation of stored text is prohibited.** Use a stored record as a locator. Rebuild or revise a rule only from facts verified in project sources. Keep unresolved records intact and report the blocking ID or source. Complete the source audit before reactivating the affected wakeup policy. A successful database migration completes the format step only.
5. Finish routine migration and configuration without asking for a second approval. For duplicate consolidation or archival, show the exact affected IDs, retained evidence, proposed changes and reason using `memory_cleanup` preview. Obtain fresh user approval of that plan before `confirm` and `apply`.
6. Refresh setup and validate the new service and its triggers. If verification fails, restore the original executable, configuration and full pre-upgrade library together with writers stopped.

## Bind and verify the installation

```text
memory-mcp setup --host "<client>" --config "<client config>" --data-dir "<library>" --project "<project>" --scope "<scope>" --server-name "<service>"
```

Run `doctor` with the same options. Check configuration, binding, wakeup policy, hook routes and index. Preserve explicitly disabled wakeup policies. Restart or reconnect the real client, complete its native trust review if prompted, and call `memory_bootstrap` on the actual service to confirm its version and capabilities. Update every client sharing this library to use the selected version. See the [adapter notes](adapters/README.md) for reload details.

Use `doctor --probe EVENT.json --expect RECORD_ID --expect-cue CUE_ID` to check a normal matching action and a similar action that should pass. For example, if an experience applies to writing SQL, replay a `Write` with a `.sql` path and the verified identifier in `content`, then try `Grep` with the same identifier. Validate actual client dispatch with a safe native action and `doctor --session SESSION_ID`. For a challenge, `doctor --challenge FULL_ID --data-dir LIBRARY` distinguishes a pending receipt from confirmed delivery.

Keep the user report short. Give the installed version, the count of initialized or migrated records, and one concrete trigger example for each new or changed cue that was verified. Each example names the operation, matched field and keyword or prerequisite. Report only actionable unresolved items and any cleanup awaiting approval.

## Register a custom tool

Use `memory-mcp guide` or `memory_bootstrap` for current examples. Put `tool-probes.json` under the active library and use the verified project scope. A full worked manual — cue anatomy, field sources, operator reference, complete examples and a silent-miss checklist — is the "Cue matching manual" section of the embedded guide, visible at runtime through `memory_bootstrap` and `memory-mcp guide` and shipped as `docs/AGENT_GUIDE.md` in every archive.

1. Inspect a sample with `doctor --probe EVENT.json`. If no sample is available, register the tool selector alone, rerun setup, restart, use the tool once, and read `tool_field_shape` in `doctor --session SESSION_ID`.
2. Map the observed native paths. For example, `{"tool":"Write","fields":{"text":"content","path":"file_path"}}`. Use `mcp_tool` for a bare MCP tool name across servers.
3. After a selector changes, run setup and restart the client. Check `probe_config_valid` and `probe_matcher_mismatches`. Store the verified comparison and optional `when` in the cue.

### Convert tool registrations from dev.9 or earlier

| Old configuration | Current format |
| --- | --- |
| `text_fields: ["content"]` and `path_field: "file_path"` | `fields: {"text":"content","path":"file_path"}` on the tool registration. |
| `extensions`, `when`, `branches` | Put the verified prerequisite in the relevant cue's `when`. Use `all` to combine conditions or separate alternatives in `matches`. Preserve explicit casing and scope. |
| `mode: "append"` or `"replace"` | Use one effective project mapping containing every verified native field needed. Remove the project entry to restore its preset. |
| `context_facts` and result `when` | Register the producer's result fields under `observe`. The cue compares the fact with `fact.match` and correlates it with `fact.key`. Replay the producer to refresh the short-lived result. |

Use `doctor` to validate the converted file. Fix any reported legacy key or mapping before activating the updated service.
