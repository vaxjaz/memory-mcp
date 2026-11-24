# Install or upgrade memory-mcp

When the user asks for installation or upgrade, carry out the steps for the actual client. Run `memory-mcp adapters` if needed. Resolve the operating system, client configuration, MCP service name, data directory, project path, scope and wakeup policy from the active configuration. Use the latest verified package in the user's chosen channel. Select the platform for the OS and CPU that run the MCP service. A package supplied for testing selects its candidate version.

Every tool, parameter, command and command flag is listed in [API.md](API.md); the acceptance suite checks that file against the program. `memory-mcp <command> --help` lists the parameters of that command alone, and a parameter that belongs to another command is an error. [CAPABILITY_MATRIX.md](CAPABILITY_MATRIX.md) states what is supported and how each item was verified.

The embedded guide's **Function model** section names the runtime functions: `W(x)` write judgment, `R(x, M)` recall, `F(j, E, m)` hot fusion and `P` verification budget. W and R run inside the service; F and P run in the host. 0.6.4 changes W (comparison before a write, refusal of cues that could never fire) and R (one file of tool rules, path ownership, array parameters).

## Select the path and verify the package

- **First install for this project** means no existing binding or library data for the project. If the client already has a memory service, use its library and add the verified project binding.
- **Upgrade** means the project already has a binding or library data. Reuse its service name, directory, scope and wakeup policy.
- Verify the selected platform archive against `SHA256SUMS`. Run `memory-mcp build-info` and compare the version and full source commit with the package name and `SOURCE_COMMIT`. Read the bundled `docs/AGENT_GUIDE.md` or run `memory-mcp guide` for that executable's schema and feature details.

## First install

1. Install the binary, bind the real project with the setup command below, and verify the live MCP version and binding as described under **Bind and verify the installation**. Setup creates `tool-probes.json` in the data directory with the default tool rules, so what will be scanned is visible and editable from the start.
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

   The migration converts stored shapes and rebuilds derived indexes. It never rewrites a record's content. Keep original IDs, revision chains, evidence and delivery history. `current_entries_needing_cues` lists records that remain historical evidence until verified and revised.

   Which libraries need the apply step:

   | Library written by | Schema | With the 0.6.4 executable |
   | --- | --- | --- |
   | 0.6.4 (dev.1 and later) | 10 | Opens directly |
   | 0.6.3 release, 0.6.4-dev.11 to dev.13 | 9 | `--apply --backup` |
   | 0.6.3-dev.10 | 8 | `--apply --backup` |
   | 0.6.2, and 0.6.3 development builds before dev.10 | 7 and below | `--apply --backup`; run the preview first and keep the rollback baseline. Their tool registration format was retired, see the end of **Register a custom tool** |

   The apply step walks the migration chain from the detected schema and always requires a fresh backup file. Three behavioral notes for upgrading agents: (1) experience bodies are two mutually exclusive forms — a sourced business `fact` with its qualifiers inside the sentence, or `pitfall` plus `counterexample`; `impact`/`applicability`/`boundary` remain optional legacy columns and a fact-form body that still fills them is rejected. (2) Authored cue text is rejected at the write boundary; a revision that omits it inherits the unchanged historical text only for a pure alias update. (3) The write path compares against same-project history: a `needs_comparison` response writes nothing and carries a prefilled `resolution_template` — fill `action` and `difference`, then resend the same draft and request_id with it. Committed request retries replay their original action; a changed action or target conflicts.

   A host-verified revision is the only path from a legacy shape to a current one.

   | Legacy record shape | Current form | How it converts |
   | --- | --- | --- |
   | Free body with separate `impact`/`applicability`/`boundary` columns | `fact` sentence carrying its object/environment/time qualifiers, or `pitfall` + `counterexample` | Columns stay readable as legacy. Verify the original evidence, then revise with the qualifiers written into the sentence and the columns left empty. |
   | Cue with authored `text` | Cue without authored text; reminders quote this version's body | Authored text is rejected at the write boundary. A pure alias revision inherits the unchanged historical text; a material revision carries no cue text. |
   | Record without cues | Historical evidence listed in `current_entries_needing_cues` | Rebuild a cue only from facts verified in project sources; never distill the stored text into one. |

3. Convert legacy configuration to the current format before setup. All tool rules now live in one `tool-probes.json`: `tools` for every project, `projects` for one project. `setup` writes the default rules into `tools` and later adds any default rule that is missing; it never changes a rule that is already there. The tables under **Register a custom tool** cover the older formats. Rebuild each legacy trigger condition from verified project code and native events. If a condition's target cue or scope is unclear, keep the backup and report the exact rule that needs more evidence before activating the affected triggers. Use an isolated client profile to discover an unknown native field when needed.
4. Recheck every existing experience in the upgraded scope, including records that already have cues. Use the new executable to audit the upgraded library through MCP before the client resumes its hooks. Keep the backup intact. Enumerate the scope with `memory_search`, an empty query and `include_inactive: true`. Follow every `next_cursor` and inspect the revision chain whenever relying on an older version. For each existing rule, the host agent must go back to the project code and trace the real entry point, condition and outcome. Check tests, an isolated replay or the relevant historical code revision as evidence. Use business assets to verify the business rule and scope alongside the code. **Semantic distillation of stored text is prohibited.** Use a stored record as a locator. Rebuild or revise a rule only from facts verified in project sources. Keep unresolved records intact and report the blocking ID or source. Complete the source audit before reactivating the affected wakeup policy. A successful database migration completes the format step only.
5. Finish routine migration and configuration without asking for a second approval. For duplicate consolidation or archival, show the exact affected IDs, retained evidence, proposed changes and reason using `memory_cleanup` preview. Obtain fresh user approval of that plan before `confirm` and `apply`.
6. Run setup again for every bound project, from the new executable. The client launches the hooks by the path that setup wrote, so replacing the executable alone leaves the hooks on the old program. Setup also completes `tool-probes.json`. Then run `doctor` as described under **Bind and verify the installation**: `split_executables` must be empty and `configuration_consistent` true. Each client sharing this library must run the selected version.
7. Audit the cues that can no longer fire. With `doctor --scope "<scope>" --project "<project>"`, read `unreachable_cues`. Each listed cue names a tool, a field or an observed result that no active rule of this project provides. For each one, go back to the project's code and real events: register the tool in `tool-probes.json`, or switch on the rule the entry names, then run setup again; or revise the cue from verified facts. Report the cues left unresolved with their IDs.
8. Validate the new service and its triggers as described below. If verification fails, restore the original executable, configuration and full pre-upgrade library together with writers stopped.

## Bind and verify the installation

```text
memory-mcp setup --host "<client>" --config "<client config>" --data-dir "<library>" --project "<project>" --scope "<scope>" --server-name "<service>"
```

Run `doctor` with the same options. Restart or reconnect the real client, complete its native trust review if prompted, and call `memory_bootstrap` on the actual service to confirm its version and capabilities. See the [adapter notes](adapters/README.md) for reload details.

What to read in the `doctor` report:

| Field | Means | When it is not right |
| --- | --- | --- |
| `configuration_consistent`, `split_executables` | The hooks and the MCP service launch the same program | `split_executables` names each hook still on another program: run setup again from the new executable |
| `probe_config_valid`, `probe_config_error` | `tool-probes.json` parses and every rule is well formed | Fix the named position; no rule applies while the file is invalid |
| `probe_tools_implicit`, `probe_config_missing` | The file exists and lists the default rules in `tools` | Run setup to create or complete it |
| `probe_surface` | The rule in effect for each tool of this scope | A tool the project needs is missing or disabled: add or switch on its rule, then run setup |
| `probe_matcher_mismatches` | The matchers in the client equal the file | The file changed after the last setup: run setup and restart the client |
| `unreachable_cues` | Stored cues that no active rule could ever fire | See upgrade step 7 |
| `wakeup_enabled` | The wakeup policy of this project | Preserve an explicitly disabled policy |
| `bindings`, `binding_verified` | Which scope the project directory resolves to | The directory resolves to another scope or to none |

Use `doctor --probe EVENT.json --expect RECORD_ID --expect-cue CUE_ID` to check a matching action and a similar action that should pass. The sample carries `cwd`; without it, pass `--project`. For example, if an experience applies to writing SQL and the project has switched on its Write rule, replay a `Write` with a `.sql` path and the verified identifier in `content`, then try `Grep` with the same identifier. Write and Edit are listed with `"enabled": false` by default: every routed tool starts a hook process on each call, so a project switches on the writing tools it needs. Validate actual client dispatch with a safe native action and `doctor --session SESSION_ID`; a call that reached the hook but was not scanned appears there as `probe_skipped` with its reason. For a challenge, `doctor --challenge FULL_ID --data-dir LIBRARY` distinguishes a pending receipt from confirmed delivery.

Keep the user report short. Give the installed version, the count of initialized or migrated records, and one concrete trigger example for each new or changed cue that was verified. Each example names the operation, matched field and keyword or prerequisite. Report only actionable unresolved items and any cleanup awaiting approval.

## Register a custom tool

`tool-probes.json` sits in the data directory and is the single source of tool rules. `tools` applies to every project; `projects` holds the rules of one project by scope and overrides `tools` by tool name. A full worked manual — cue anatomy, field sources, operator reference, complete examples and a silent-miss checklist — is the "Cue matching manual" section of the embedded guide, visible at runtime through `memory_bootstrap` and `memory-mcp guide` and shipped as `docs/AGENT_GUIDE.md` in every archive. [PROBE_SCAN_CONTRACT.md](PROBE_SCAN_CONTRACT.md) lists every outcome of a scan decision.

1. Inspect a sample with `doctor --probe EVENT.json`. If no sample is available, register the tool selector alone, for example `{"tool":"Write"}` or `{"mcp_tool":"warehouse_query"}`, rerun setup, restart, use the tool once, and read `tool_field_shape` in `doctor --session SESSION_ID`.
2. Map the observed native paths. For example, `{"tool":"Write","fields":{"text":"content","path":"file_path"}}`. Use `mcp_tool` for an MCP tool's bare name across servers.
3. Write a path into arrays with an index or `*`: `questions.*.question` takes the `question` of every element, `questions.0.question` only the first.
4. For a shell tool, list in `scan` the command names whose command line is read; the default rule reads the common search and file-reading commands.
5. After the file changes, run setup and restart the client. Check `probe_config_valid`, `probe_surface` and `probe_matcher_mismatches`. Store the verified comparison and optional `when` in the cue.

### Convert tool registrations from dev.9 or earlier

| Old configuration | Current format |
| --- | --- |
| `text_fields: ["content"]` and `path_field: "file_path"` | `fields: {"text":"content","path":"file_path"}` on the tool rule. |
| `extensions`, `when`, `branches` | Put the verified prerequisite in the relevant cue's `when`. Use `all` to combine conditions or separate alternatives in `matches`. Preserve explicit casing and scope. |
| `mode: "append"` or `"replace"` | Write the project's rule under `projects` with every verified native field it needs. Removing it falls back to the rule in `tools`. |
| `context_facts` and result `when` | Register the producer's result fields under `observe` in a project rule. The cue compares the fact with `fact.match` and correlates it with `fact.key`. Replay the producer to refresh the short-lived result. |
| A whole array parameter, rejected as unsupported | Map the element's field with `*` or an index, as in step 3 above. |

Use `doctor` to validate the converted file. Fix any reported legacy key or mapping before activating the updated service. A `*.pre-schema8` probe file beside the data directory is not converted: `doctor` lists it as `legacy_probe_config_files`; re-register that project's custom tools with the current format, check each with `doctor --probe`, then remove the leftover file.

## Utility commands

All of them take `--data-dir "<library>"` when the library is not the default one; `diagnose` requires it.

| Command | Does |
| --- | --- |
| `status` | Version, library format version, data directory and record counts |
| `backup --out FILE` | A consistent SQLite snapshot to a new file |
| `export --out FILE` | The whole library as plaintext JSON to a new file |
| `forget --id ID --yes` | Permanently deletes one record with its revisions; exports, host transcripts and backups are unaffected |
| `diagnose [--entry-id ID] [--scope SCOPE] [--probe-ids ID,ID] [--out FILE]` | Read-only metadata and link diagnostics; repairs nothing |
| `adapters` | The supported clients with their configuration locations and validation status |
| `guide` | The embedded guide, the same text as `memory_bootstrap` |
| `config` | An MCP configuration fragment that launches this executable |
| `build-info` | Version, library format version and the exact source commit of the build |
