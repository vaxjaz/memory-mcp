# 0.6.0 first-body and incremental-cue protocol

## Writes and identity

Experiences contain pitfall, counterexample, impact, applicability, boundary, 1-3 evidence locators and 1-12 cues. Each cue has stable cue_id, label (<=24 Unicode characters), a complete historical sentence (<=160), 1-3 evidence_refs present in the body and 1-12 directly evidenced entrances (code_symbol/file/config/business_signal). Pure business cases require no source file. Multiple aliases for one association share one cue. Do not turn dependencies into associations. The host verifies meaning; length and locators cannot certify semantic truth.

Visible experience facts stay <=3000 UTF-8 bytes, without truncation. Every cue fact is included in the first body; a material new risk cannot hide in a short reminder. Visible DTO omits recognition arrays, entrances, cue metadata and file hashes. Evidence and concrete source-drift indicators remain available. Titles are identity/navigation, not substitutes for a counterexample.

Body, cues, index and revisions commit atomically. Alias-only updates, code hash or line/commit relocation preserve content identity and historical evidence. Material facts/conditions/boundaries/cue changes create a previous_id revision; independent mechanisms create separate experiences. Agreements retain their separately authorized source/authority. Historical structured experiences without cues remain eligible through legacy_primary, with a first body and no invented extra associations. Legacy prose stays searchable without promotion.

## Delivery and hooks

consumer = host + stable bound project + scope + native session/agent + context generation. Body keys are consumer + root/version; cue keys additionally include cue_id. Turns are not context generations. Explicit compact/clear starts lazy re-delivery on the next related entrance; resume/continue does not. Explicit agent identity is isolated; no ID is invented from task text.

No hit: empty output. Unseen body: short challenge, no duplicate body. Host constructs task/questions/queries. The challenge's actual candidates cannot disappear merely because query wording misses. Lookup returns complete pages in one JSON text <=8192 bytes, with receipt_id, has_more/next_cursor. The generated receipt contains only identities/digest and does not credit delivery.

Only this configured service's exact lookup name (including native hyphen/underscore spelling) gets a narrow nonblocking Pre observer and Post observer. Pre binds native tool_use_id to its consumer/context; Post parses the documented complete success result, checks isError/transport state, receipt/digest/project/challenge/version and credits only returned items. Late results stay with their original context. Ordinary lookup credits bodies but invents no trigger cues. read remains unrestricted and has no forced receipt. Post emits no repeated context.

A delivered body's unseen cue allows the operation with a fixed fact, grouped by experience. Aliases merge. Total cue text <=1024 UTF-8 bytes; full sentences are never sliced. Overflow is explicitly marked and undisplayed cues remain pending. SQLite serializable transactions claim output once among concurrent processes. stdout failure releases reservations; process-start identity permits recovery after a dead emitter. The small crash window after stdout and before commit may conservatively replay; this is not an end-to-end exactly-once guarantee. A receipt proves observed output only, not model retention or understanding.

Two minutes is only a technical stuck-challenge fail-open deadline, never a reminder interval. Failure does not credit a body/cue or schedule compensation. There is no global turn permit, time/count-based reminding, model acknowledgement, skip/complete requirement or Stop continuation. Another experience requires its own first body. A current revision cannot be released by an old receipt. Active voluntary lookup/read is never suppressed.

## Costs and migration

MCP calls no model. Hook process startup, SQLite, tool round trips and appended context cost time/tokens; there is no overwriteable memory slot. Narrow business observations preserve only identifier hashes and candidate IDs, and are invalidated independently on new inputs/context changes. Unknown tools, missing native IDs, absent hooks and unreported pruning remain gaps.

Schema 5 adds derived entrances, delivery ledgers and call/response association, replacing old generated-receipt/challenge/cooldown tables. Original entries/profiles, revisions, IDs, evidence, bindings, policies and maintenance survive. Stop ALL sharing writers and independently back up the complete data directory and client configuration BEFORE any new DB-opening command. Restore pre-upgrade library/config/binary together for rollback. Never point an old executable at the upgraded library.

See DELIVERY_ACCEPTANCE.md for actual measurements and unverified native/model boundaries. Deterministic replay is not evidence that an autonomous model used a counterexample.

## Business observation corrections in 0.6.1

Observed candidates restore the fixed cue label/text from the exact current, active version/cue; missing, retired or revised cues are not promoted. Direct and same-object observed candidates are always combined and deduplicated by version/cue, so a delivered direct match cannot hide another experience or association.

Selected business output is split into bounded records before matching: arrays/rows and MCP content JSON text retain row boundaries; native structuredContent is preferred over its narrative copy. No parent/sibling action is inherited. At most 128 records and 32 KiB of allowed fields are probed. Each record needs one unambiguous identifier for the experience's explicitly registered prefixes; multiple positive records may retain several object hashes for the same cue. Mixed free text containing several recognized objects is not assigned by co-occurrence. Nested formats without observable object/action ownership remain uncovered. No arbitrary tool scanning, source text logging or extra model is added.

Normal input boundaries expire only temporary business observations. Native turn IDs take precedence; when absent, a hashed event ID identifies a new input and its replays. Seen identities prevent late replay from deleting a later input's observation. When both IDs are absent, each prompt conservatively expires temporary observations; replay cannot be distinguished. A tool result with neither an input identity nor an equivalent pre-bound native association cannot be proven to belong to an earlier input after it arrives late. No prompt text/hash is used to fabricate identity. Body/cue delivery and context generation remain unchanged.

Schema remains 5: an additive derived experience_input_events table records only input identity hashes. Upgrading an existing schema-5 library preserves its body/cue deliveries, history, bindings and policies; pre-upgrade complete backup and all-client version consistency remain required.
