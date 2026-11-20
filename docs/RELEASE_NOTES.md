# memory-mcp 0.6.4-dev.2

- External-review fixes over dev.1. The experience write boundary now rejects authored cue text (field-position diagnostics) and rejects fact-form bodies that still fill `impact`/`applicability`/`boundary`; stored legacy records keep reading and delivering their cue text through the compatibility path.
- New-association reminders append the stored qualifiers beside the counterexample, keeping the same assertion boundary as the first body delivery. The budget fallback names `memory_read` with the exact version ID, which the tests actually read back.
- Committed `request_id` retries return their receipt before validation runs (same payload) or conflict (different payload), so payloads committed by older binaries keep retrying cleanly across the schema upgrade.
- SessionStart carries a short hot-fusion prompt, so hosts that never call `memory_bootstrap` still receive it; verified on a real ZCode session.

# memory-mcp 0.6.4-dev.1

- Experience bodies take one of two mutually exclusive forms: a sourced business `fact` with its object/environment/time qualifiers, or `pitfall` + `counterexample`. The legacy `impact`/`applicability`/`boundary` columns become optional and are preserved verbatim on old-client submissions; nothing is dropped and reported as success.
- Experience cues no longer carry authored text. Post-body new-association reminders quote the fact or counterexample from this version's body with its full qualifiers; when the reminder budget cannot hold them, a locator entry keeps access to the complete fact instead of a trimmed assertion. Agreement cues keep requiring their confirmed text.
- Lookups project a `form` field and keep every legacy qualifier and cue-text fact losslessly. The embedded guide adds the standing recall/hot-fusion prompt for hosts.
- Schema 10: older binaries refuse to open the new library instead of silently dropping the fact form. `migrate` inventories legacy narrative qualifiers, cue facts beyond the body, and records missing evidence; `migrate --apply --backup FILE` upgrades with a rollback baseline.

This dev release uses integration protocol 12 and schema 10. Schema 9 libraries require the packaged migration with a fresh backup before opening.

# memory-mcp 0.6.3

- Configure built-in and project tools through one registration path. Bind keyword matching, optional `when` prerequisites and verified earlier tool results to the cue.
- Require a reusable future decision and an exact supporting evidence reference for every experience write or revision.
- Request lookup once per challenge. Covered retries can proceed while confirmation is pending. Inspect the receipt stage with `doctor --challenge FULL_ID`.
- Continue historical search and cleanup inventory through legacy IDs and timestamps using the returned cursor.
- Include the installation, upgrade and agent guides in the offline bundle and each platform archive. Agents initialize from verified project history and recheck stored rules against project code before activation. Duplicate consolidation and archival use a user-approved cleanup preview.

This release uses integration protocol 11 and schema 9. Back up the original library and run the packaged migration steps before opening an older schema. Libraries already on schema 9 can open directly. Keep the existing scope, library and wakeup policy during setup.
