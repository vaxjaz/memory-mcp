# 0.5.0 host-generated recall

## Writes

memory_experience_record retains verified failure features, not conclusions or manuals: actual anchors/signals, challenged inference, counterexample, impact, applicability, boundary and evidence. Essential fields <=240 characters; feature JSON <=3000 bytes. Optional anchor_prefixes explicitly register an evidence-grounded ASCII numbering convention (3-40 characters), never a regex guessed from one sample.

memory_reference_record accepts compact already-confirmed requirement, applicability/boundary, authority, confirmation_reference and evidence, with recognition features. No fabricated counterexample. Changing an agreement needs its authority; indexing an already authorized source does not create a new business rule.

Both writes are synchronous and idempotent. The entry, profile and inverted index commit together. indexed=true follows commit; validation/locators do not certify semantic truth. Local source hashes are rechecked; external sources are marked not revalidated.

## Queries

memory_experience_lookup requires explicit scope/project (or real challenge), task and questions. The host constructs queries: 1-4 distinct perspectives, each with terms and match=all/any. Up to 16 terms per explicit query, combined <=4096 bytes. Compatibility keywords remains a single OR query. MCP performs no translation/model call.

The index covers titles and complete recorded features, including counterexamples and exceptions, and preserved historical evidence. Han bigrams support short Chinese terms; ASCII identifiers stay whole. No implicit synonyms, negation interpretation or identifier-prefix inference.

Field-weighted BM25-style lexical scoring ranks each query; reciprocal-rank fusion combines independent rankings. Exact registered anchors are prioritized and duplicate identical queries do not inflate scores. matched_queries/matched_terms explain lexical hits only. The host evaluates current applicability.

Default two, maximum three complete items within one JSON text body <=8 KiB. next_cursor makes remaining items explicit; changed queries/corpus invalidate cursors. More than 2000 lexical candidates requires refinement instead of an incomplete silent result. Legacy excerpts are navigation, not confirmed conventions or automatic triggers.

## Hooks

PreToolUse observes existing code tools and narrowly named business MCP tools: ticket/order reads and database-query suffixes. A specific anchor/prefix plus independent actual condition must match. Domain names alone do not interrupt.

Selected business PostToolUse only observes bounded source fields. It stores matched IDs and hashes of identifiers with explicitly registered prefixes, never raw text. A subsequent call must contain the same identifier. New input, reported compaction, changed revision and explicit different agent identity cannot reuse the observation. The source read itself is not denied and no unsolicited material is injected.

The existing challenge has one native-input interruption budget. Required candidates must appear in actual generated responses before completion; bounded pages accumulate server-side ID receipts without a model-authored complete statement. This proves response creation, not transport delivery or reading. Errors/two-minute expiry fail open without pretending completion. Pure text, unknown tools/wrappers, absent stable identifiers and unreported pruning remain gaps. A receipt does not prove semantic consumption or current context visibility.

## Costs and schema

No extra model is run by MCP and ordinary inputs have no reminder. Local matching, SQLite, process startup, tool round trips and returned evidence all have costs. Business pre/post can require two launches. Earlier Windows PowerShell measurements on this machine were around 0.8 seconds per launch; do not enable every tool or claim zero cost.

Schema 4 builds derived postings while preserving original entries, revisions and IDs. Migration happens once. Stop old writers and back up the entire library/configuration BEFORE opening it with the new version. Schema-3 rollback requires restoring that backup; mixed versions are unsupported.

Executable replays validate mechanisms, not actual native delivery or model quality. Measure those separately.
