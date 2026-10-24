# Public client contracts

`runtime.json` is the single source for `protocolVersion` and browser capacity limits.
The Go daemon embeds it through this package; desktop generates `web/contracts/` from the same pinned revision.
`api.ts` and `events.ts` are the TypeScript/Zod client validation definitions for the REST and SSE protocol.
Server structs and lifecycle semantics remain owned by core; verify both sides when changing a wire field.

Protocol 45 adds eyes `wink` / `sleepy`, mouths `grin` / `mew`, headwear
`crown` / `bow`, arms `mitten` / `claw`, eyewear `glasses` / `sunglasses`,
and stickers `hearts` / `lightning`. It also adds assistant body shapes `pudding`, `hexagon`, and `cat` alongside
`classic`, `round`, and `square`. Shape and palette remain independent. Schema
advances to 55 to replace saved `christmas` red selections with `apricot`; other selections are preserved. Update desktop and daemon together
so new values can be validated and rendered. The same expansion includes the
independent devil set: body `devil`, eyes `mischief`, mouth `fangs`, headwear
`horns`, arms `wings`, eyewear `domino`, stickers `flames`, and palette `crimson`.
The Santa set adds body `plump`, eyes `jolly`, mouth `laugh`, headwear and arms
`santa`, eyewear `halfmoon`, and sticker `beard`; it defaults to the soft peach `apricot` palette, with fixed red hat and mittens.

Protocol 44 expands assistant memories with `scope` (`personal` / `project`),
`source` (`user_settings` / `conversation`), a positive `revision`, and optional
`projectID`, `sourceSessionID`, `sourceTurnID`. Schema 54 preserves existing
preferences, adds provenance and revision columns, and builds a local term index.
`builtin_assistant_memory` supports search/save/update/delete only in the fixed
assistant conversation; execution verifies that association independently of the
model's tool list. Writes record the actual source turn, and updates/deletes use
memory revisions. Conversation changes to `communication_preference` also advance
the profile revision so stale forms cannot overwrite them. No memory management
UI or new memory REST endpoints are added in this increment.

The model judges which information is useful to retain; no trigger phrase or
fixed category is required. Current identity and communication preference are
always included. Other personal memories are selected using the latest user text
and a Chinese/English term index, capped at six facts / 4000 content characters;
project memories require the matching project scope. This is lexical retrieval,
not vector similarity. The model can search further using the scoped tool.
Updates replace indexed content; deletes remove it from future memory retrieval,
without deleting the original conversation. Memory does not grant tool permission.

Protocol 43 allows an empty assistant name to select the localized display default
(小布 in Chinese, Puddy in English). New profiles store an empty name; existing
names remain unchanged until explicitly cleared. Schema remains 53. The profile
and its fixed conversation remain intact when clearing the name. Clients render
the default according to the current UI locale, without rewriting it on locale changes.

Protocol 42 separates assistant body shape from palette selection: body supports
`classic`, `round`, `square`; palette adds `blue`, `coral`, `yellow`, `violet`,
`emerald`. Both fields already exist in appearance JSON and persist independently.
Schema remains 53; existing shapes, palettes and defaults are preserved. Clients
and daemon must update together to validate and render these new values.

Protocol 41 extends assistant parts: eyes add `curved` and `dots`; mouth adds
`smile` and `surprised`; headwear adds `hat` and `beacon`; arms add `short` and
`orb`; faceSticker adds `freckles` and `stars`. These are stable IDs in the existing
appearance JSON. Schema remains 53, with no data migration or default changes.
Older renderers cannot display the new values and must update with the daemon.

Protocol 40 adds mutually exclusive `eyewear` selections: `none`, `classic` (the
original green frame, glass and reflection), and `goggles`. The body no longer
contains a mask. Schema 53 maps the old `none` choice to `classic` to preserve
its appearance and advances the profile revision, so stale forms cannot overwrite
the migrated choice. Goggles remain selected; future `none` renders a bare face.

Protocol 39 adds authenticated `POST /assistant/open` alongside `GET/PUT /assistant`. Schema 52 stores one local
assistant (`id: personal`, enforced by the database) and explicit personal
memories independently of sessions. GET returns `{assistant: null, memories: []}`
until first opened; reading never creates an identity. POST accepts `locale`
(`zh-CN`, `zh-TW`, or `en`) and an optional provider/model pair. It creates missing
defaults (小布 or Puddy) and binds one conversation atomically when a model is
available. Reopening preserves the existing identity, selections and conversation.
The snapshot includes `sessionID` once bound; this conversation is excluded from
ordinary root listings. Archive/delete returns `409 assistant_conversation_protected`.
Schema 52 upgrades the earlier schema 51 `palette` column to appearance JSON while
preserving data, and also accepts the canonical schema 51 appearance layout. PUT requires
`expectedRevision` (0 if the identity does not exist), name, userName, appearance and
communicationPreference. Names are trimmed and limited to 80 characters without
line breaks; the preference is trimmed and limited to 2000 characters. A stale
changed form returns `409 assistant_conflict`. An identical current profile and
preference returns the current snapshot without another revision, making a retry
after a lost response safe. Profile and preference commit atomically.

Appearance persists as JSON with required `body`, `eyes`, and `mouth` asset IDs
(default `classic`). Body supports `classic`, `round`, `square`, `pudding`, `hexagon`, `cat`, `devil`, `plump`. Palette supports
`original`, `lavender`, `mint`, `blue`, `coral`, `yellow`, `violet`, `emerald`, `crimson`, `apricot`. Eyes support
`classic`, `curved`, `dots`, `wink`, `sleepy`, `mischief`, `jolly`; mouth supports `classic`, `smile`, `surprised`, `grin`, `mew`, `fangs`, `laugh`.
Decorative slots carry explicit IDs or `none`: headwear supports `antenna`, `hat`,
`beacon`, `crown`, `bow`, `horns`, `santa`; arms support `classic`, `short`, `orb`, `mitten`, `claw`, `wings`, `santa`;
faceSticker supports `blush`, `freckles`, `stars`, `hearts`, `lightning`, `flames`, `beard`;
eyewear supports `classic`, `goggles`, `glasses`, `sunglasses`, `domino`, `halfmoon`.
Missing/unknown selections return `400 invalid_assistant`.
The optional communication preference uses memory ID `communication_preference`,
scope `personal` and source `user_settings`; an empty value deletes it. This
increment provides profile persistence and one current assistant conversation.
Cross-session scheduling remains deferred; the assistant always
uses the same conversation, with no new-chat or conversation-switching entry. The
Desktop remote gateway does not expose the assistant endpoint.

Semantic rename (`builtin_code_rename`) returns `ok: false` with
`reason: "rename_conflict"` when its saved-file snapshots or returned LSP
versions no longer match. Clients should explain that changes were preserved
and ask the user/model to inspect current code before a fresh request; do not
retry the same coordinates automatically. The first rename response discovers
targets only. Core then captures and synchronizes those files, recomputes the
edits, validates versions and contents, and keeps the original snapshots
through patch preparation and the patch writer's stale-content checks. A newly
introduced target during recomputation is a conflict, not an unchecked write.
This adds a tool-result reason within the existing opaque result body; REST/SSE
schemas, handshake version and database schema are unchanged. It does not
serialize unrelated external filesystem writers across OS rename syscalls.

Protocol 37 requires `{ expectedRevision }` for turn file undo/redo
(`POST /sessions/:id/turns/:turnID/file-changes/undo` and `/redo`). Read it from
`ConversationTurn.fileChangeRevision` alongside `fileChangeState`. A successful
response returns `{ state, revision }`. Revisions are positive safe integers,
strictly increasing for each turn; treat them as opaque comparison values.
Missing or invalid revisions return `400 turn_file_changes_invalid_revision`.
An obsolete revision, incompatible state, running turn or changed file returns
`409 turn_file_changes_conflict` without replaying the files. The version check
runs inside the replay lock before any filesystem changes and again in the
state update's compare-and-swap. An old undo cannot undo a later redo cycle.

Clients must reconcile uncertain responses by GETting that specific turn and
updating its cached state/revision. If this read also fails, retain the attempt
and offer a read-only status retry, including across component remounts. Do not
automatically resend a write or interpret every 409 as success. After a
successful read, a new explicit action uses the newly reviewed revision. Late
GET/list/SSE/action responses must not replace a newer file-change revision.
This is state reconciliation, not a durable success receipt: a GET shows the
current cycle, not proof of which caller performed an earlier write.

The existing `turn_file_change_states.updated_at` provides the revision, updated
to `max(previous + 1, now)` even within one millisecond or after clock rollback;
schema remains 49 and existing turn snapshots require no migration. Existing
file snapshot checks, rollback and authorization remain in force. Filesystem
changes and the database state are not a crash-atomic transaction; this does not
add recovery for daemon termination between those steps or serialize external
filesystem writers. The new handshake prevents older clients from sending
unguarded replay requests.

Protocol 36 adds optional `clientRequestID` and `expectedRevision` to Git commit
and branch creation (`POST /sessions/:id/project/git/commit` and `/branches`).
Available Git status responses include an opaque `revision` covering HEAD,
branch, index status and changed worktree contents. A keyed first execution
requires that reviewed revision; a mismatch is `409 git_state_conflict` without
executing. IDs contain 1–200 ASCII letters, digits, underscores or hyphens.
The scope is repository, session and operation. Missing/invalid revisions on
keyed requests return `400 git_invalid_request`; requests without a key retain
independent execution.

Successful receipts persist in local Git metadata under `pudding/requests` and
are not tracked project files. An identical retry checks the receipt before the
revision and returns current repository status without committing, staging,
creating a branch or switching HEAD again. Changed parameters with the same ID
return `409 git_request_conflict`. Clients retain the exact ID, message/name,
stage policy and original revision after uncertain responses; a deliberately
new operation needs a new ID. A replay does not revive a subsequently deleted
branch or override a later branch selection.

Git mutation and receipt-file publication are separate filesystem operations.
If the daemon stops or recording fails after Git changed, a missing receipt is
not guessed from an equal message/branch name: the original revision guard
rejects a retry after that change. Clients must show that the repository needs
review and require a fresh explicit action. A receipt I/O failure is
`git_receipt_failed`. This contract does not serialize external Git clients or
restore receipts removed by a user, and adds no database schema migration.

Protocol 35 adds optional `clientRequestID` to `POST /projects` and
`POST /projects/:id/merge`. Keys contain 1–200 ASCII letters, digits, underscores
or hyphens; creation and merge use separate namespaces. Schema v49 stores each
normalized request fingerprint and result ID atomically with the project changes
and session migration. Repeating the same operation returns the project's current
metadata, including after daemon restart; it never reapplies the original name,
roots or session migration. Reusing a key with different parameters returns
`409 project_request_conflict`; a deleted result returns `404` and is not revived.
Receipts survive deletion. Requests without a key retain independent execution.

Merge may also carry `expectedTargetUpdatedAt` and `expectedSourceUpdatedAt` from
the originally reviewed projects. The first execution checks these against stored
millisecond timestamps and returns `409 project_merge_conflict` on a mismatch.
An acknowledged receipt is checked first, so an uncertain successful merge can
still be recovered after its source is deleted or its target changes. Clients
must keep both the request ID and original payload across retries, and allocate
a new identity for a deliberately new operation. Failed transactions consume no key.

Protocol 34 extends `POST /studio/items` creation identity to Widgets. Optional
`initialFiles` is a map of source paths to UTF-8 text, allowed only with Widget
kind and `clientRequestID`. The initial draft obeys Widget source limits (no
fixture writes), but need not yet be a buildable package. All files publish in
one directory rename. The creation receipt fingerprints metadata and the initial
source hash; replay returns current metadata and never replaces an existing draft
or later committed source. An initialization failure retains the item identity
for retry instead of deleting an uncertain result. Key reuse with different
parameters is `400`; archived/tombstoned items are not revived, and permanent
purge ends the key's lifetime. Widget keys use a
separate namespace from Page/Sheet. The existing save receipt stores the creation
fingerprint without adding a source revision; schema remains 48. Omitted keys
retain the legacy independent creation API (without `initialFiles`).

Clients creating a downloaded original's edit copy must also retain the existing
`POST /studio/items/:id/draft` `clientRequestID` across retries. That operation
returns the same current copy and draft, preserving independent edits and data.
The Desktop authoring tool requires a stable logical `clientRequestID` for new
widgets and first edit copies, namespaces it by its trusted session, and asks for
a new ID only for a deliberately new creation. Resuming a returned copy ID needs
no new creation identity. This also covers lost MCP results after HTTP succeeded;
clients must not rotate IDs merely because their HTTP work finished.

Protocol 33 adds optional `clientRequestID` to `POST /studio/items` for Page and
Sheet creation (including their legacy kind aliases). Keys contain 1–200 ASCII
letters, digits, underscores or hyphens. The same key and normalized creation
parameters return the existing item's **current** metadata without adding an
item or revision, including after daemon restart; later edits are preserved.
Reusing a key with different kind, body, name/naming policy, appearance or source
session returns `400`. Page and Sheet share the key namespace. The deterministic
item ID and initial save fingerprint commit atomically with content and history;
failed transactions do not consume the key. Archived or tombstoned content returns
`404` rather than being revived; permanent purge ends the key's lifetime. Omitted
keys retain independent creation, and Widget creation does not accept this field.
Clients must retain the exact unacknowledged payload (including generated Sheet
row/column IDs and appearance), then rotate the key after acknowledgement. No
database migration is needed; schema remains 48.

Protocol 32 introduces declared plugin connection selection (`user` by default,
`single_active`; `automatic` is reserved and currently rejected). Standard
Connections own Web Search & Fetch credentials; `/plugins/:id/connection-bindings`
reads/patches `{bindings:{active:connectionID}}`. Search and fetch share that one
selection. Legacy `/tools/web` and `web` identities remain compatibility facades.
`GET/PUT /sessions/:id/plugins/:pluginID/connection` provides a user-only chat
selection; a model tool cannot choose an account by supplying its ID. Returned
candidates omit credentials. REST/GraphQL/MCP execution and MCP discovery enforce
the selection. Schema v48 adds `session_plugin_connections` with session-owned
cascade deletion and an account/instance fingerprint; existing chats start with
no implicit account selection. See [Plugins](../docs/plugins.md#connection-selection).

Widget preview distribution adds optional `preview` (boolean) and `channel` (up to 64 characters) to source packages and registry releases. `preview: true` is the authoritative classification, matching plugins; neither the version suffix nor `channel` alone changes it. Widget installation persists `origin.preview` in the existing origin JSON, so an installed preview remains identifiable without its catalog. A subsequent stable installation clears the flag. Settings exposes independent `show_plugin_preview_versions` and `show_widget_preview_versions` booleans, both defaulting to false; settings.yaml stores them under `plugins.show_preview_versions` and `widgets.show_preview_versions`. No database schema change is required. The obsolete frontend key `show_app_preview_versions` remains invalid.

Protocol 28 adds Studio document range leases at `GET/POST /studio/items/:itemID/edit-operations` and atomic `replacement` writes carrying `operationID` and `leaseToken`. Ranges are zero-based UTF-8 bytes `[from,to)`; begin requires the current content hash. Active leases last two minutes, may renew up to fifteen minutes, and survive daemon restarts. All working-content writes reject overlap with `409 document_locked`, including restore and undo; disjoint edits remap leases. GET omits tokens and target text. Authenticated users can cancel a lease; only its owner/token can commit or renew. Each action and commit is idempotent by client request ID. Successful commit stores both revision and terminal operation in the same transaction. Schema v40 stores leases and action receipts. Project filesystem writes are outside this Studio protocol.

Protocol 27 adds persistent plugin catalog sources at `GET/POST /plugins/sources` and `DELETE /plugins/sources/:sourceID`. Plugin installs optionally assert `pluginID` and `pluginVersion`; an existing installation cannot be replaced by a different catalog. Desktop requires these routes.

Protocol 26 adds optional `clientRequestID` to `POST /sessions`. Repeating a creation request returns the same current session without resetting its model, title, project or history; the first successful creation wins. The key is encoded in the existing session primary key, so concurrent retries and daemon restarts require no schema change or in-memory deduplication. Keys contain 1–200 ASCII letters, digits, underscores or hyphens. Use a new key for a new conversation and reuse it after an uncertain response. Archived sessions are not revived by replay; permanent deletion ends the key's lifetime. The existing session-scoped `clientMessageID` separately deduplicates submission. Clients relying on this behavior require protocol 26.

Protocol 2 adds the external `-ui-dir` startup contract and removes mobile pairing/device-token authentication.
Clients reject incompatible daemon handshakes rather than attaching to an older embedded-UI runtime.
The generated client files must not be edited or committed in desktop.

Protocol 3 adds scheduled task definitions, run history, and daemon scheduling. Desktop requires these routes; the handshake rejects older daemons rather than presenting a management page backed by missing endpoints.

Protocol 4 adds independent canvas resources, immutable source revisions, App bindings/query grants, action runs and cross-App links. `canvas.json` owns runtime limits and `canvas.ts` the client schemas. Old daemons are rejected at handshake. Installed App endpoints must explicitly set `canvas_writes: true` to enable confirmed canvas actions; the default is disabled.

Protocol 7 removes canvas query grants and their routes. Canvas queries reuse installed App connections; ambiguous accounts require an explicit source binding. Automatic reads are limited to REST GET without body and GraphQL queries. The consolidated schema v25 migration omits the old grants data.

Protocol 8 renames canvases to Studio items and Apps to plugins. Items live under `/studio/items` with a `kind` (`doc`, `table`, `widget`; this release creates widgets); session views are `/sessions/{id}/studio/mounts`. `studio.ts` owns the item and version schemas, `widget.ts` the widget package, manifest, build, bridge, query, action and link schemas, and `widget.json` the widget runtime limits. Widget manifests are `widget.json` with plugin sources (`pluginID`), and generated source imports `@pudding/widget`. Plugin routes are `/plugins`, `/plugin-skills`, `/plugin-assets`, `/plugin-connections` and `/plugin-oauth`; sessions report `loadedPluginIDs`. Installed endpoints opt into confirmed widget writes with `widget_writes: true`. Old daemons are rejected at handshake; no `/canvases` or `/apps` routes remain.

Protocol 9 requires an explicit `kind: doc | widget` when creating Studio content and adds native Markdown content, image assets and version authorship. `studio.json` owns document limits and autosave timing, while `studio.ts` adds body/hash/write schemas. Document storage is included in the combined v26→v30 schema upgrade. The Chat-mode Core `studio` plugin provides document tools for foreground, scheduled and child-session turns; see [Studio](../docs/studio.md).

Protocol 10 enables `kind: table` creation and adds structured table content, typed columns and atomic by-ID operations at `/studio/items/{id}/operations`. `studio.json` owns table limits; `studio.ts` defines body/write schemas. Schema v30 records stable row/column identities alongside native content. The Chat-mode `studio` plugin includes table create/read/update tools; see [Studio](../docs/studio.md).

Protocol 11 adds user-only selective AI undo through `undoRevision` on document/table writes and optional table `rowID`. Core derives the inverse from immutable versions, preserves disjoint later edits and rejects overlap without writing. Undo appends a version and keeps the existing request-id idempotency contract; no separate schema migration is needed for undo. See [Studio](../docs/studio.md#selective-ai-undo).

Protocol 12 adds Studio archive/restore, archived listing and 30-day cleanup for documents, tables and widgets. Deletion now permanently removes content, versions and managed files; clients use archive for ordinary removal. Schema v30 stores the archive timestamp. See [Studio archives](../docs/studio.md#archives).

Protocol 13 adds `GET /tools/command-shell`, which reports whether the Windows command tool can find PowerShell 7 (`installed`, `missing`, or `not_required` on other platforms). The lookup also reads the persisted machine and user PATH, so an installation made while the daemon runs is found without a restart. Desktop uses it to guide installation; old daemons are rejected at handshake.

Protocol 14 adds owner-approved browser remote access over certificate-free LAN Direct HTTP and relay HTTPS. LAN origins use concrete IPv4 addresses; relay origins require HTTPS. `remote*` schemas in `api.ts` define loopback control routes under `/remote`, a secret-free model list, and resource-scoped authorization invalidations. Schema v31 stores a stable desktop identity, one-use five-minute pairing codes and poll hashes, and 30-day device credential hashes. Electron remains the only network gateway; daemon startup-token authentication and session business APIs stay unchanged. See [Remote access](../docs/remote-access.md).

Protocol 14 is the first unreleased browser-remote protocol. The LAN HTTP refinement changes origin validation without changing request/response shapes or schema v31; it retains protocol 14 and does not provide HTTPS LAN compatibility.

Protocol 15 replaces requested/approved pairing with locally preauthorized QR codes. `POST /remote/pairings/request` now atomically consumes a five-minute code and returns `{token,device}` once; clients use the automatic `Browser device` label. Pairing snapshots no longer carry status/deviceName, cancellation is idempotent, and poll/approve/deny routes and DTOs are removed. Schema v32 invalidates only old short-lived pairing rows while preserving desktop identity, paired devices and canonical history. Its new fingerprint does not alter released v31. See [Remote access](../docs/remote-access.md).

Protocol 25 adds subtask archives scoped to a parent conversation. Child snapshots include `archivedAt`, `canArchive`, and `pendingUserInputs`. `POST /sessions/{id}/children/archive` archives eligible completed children; `POST /sessions/{id}/children/{childID}/archive` and `/restore` act on one owned child. A busy single child returns `409 subtask_archive_busy`; batch archive skips busy children. Responses use the same `{children}` snapshot as GET. Schema v39 stores presentation-only subtask archives separately from session retention. Archives remain readable and restorable within their parent, do not appear in the global session archive, and are deleted with the parent group. New work automatically restores the child. Dispatch admission allows at most three new children per user request; retries, system continuations and form replies share the request budget, and archiving does not refund it.

Protocol 29 adds Studio `document` items with versioned Tiptap JSON, page geometry, immutable owned image assets and an optional original DOCX. `/studio/items/{id}/document-content` supports reads and CAS/idempotent writes or revision restoration; `/document-assets/{hash}` reads owned binaries. Creation accepts a structured `body` plus base64 `blobs`, committed together. Schema v41 adds `studio_rich_blobs`, renames Studio item kinds to `note` and `document` (`CHECK(kind IN ('note','table','widget','document'))`), and migrates existing rows without rewriting Markdown content or changing hashes. Document body v2 adds `headerFooter` with default/first/even paragraph documents, live `pageNumber`/`totalPages` fields, edge distances and variant flags. Body v1 remains readable and restorable byte-for-byte; it is not rewritten on open. Automatic page boundaries are derived UI state, never stored. DOCX conversion remains local in Desktop.


Protocol 30 adds the independent DOCX create/read/edit tools. Rich writes accept an atomic `operations` batch or a user-only `undoRevision`, mutually exclusive with full body writes and restoration. Operations use stable body block IDs and require `expectedHash`; immutable revisions carry server-authenticated session/turn authorship. AI creation and write retries return the original committed revision without resetting the current head. Selective undo applies an inverse only where the AI result is still present, retaining later unrelated edits and rejecting overlaps. The editor's default attributes/mark ordering/text-run normalization are equivalent for undo comparisons only; stored content is not normalized on read. No schema migration or Markdown API change is required.

Protocol 31 renames standalone table identities to sheets. API item/mount/revision kinds are `sheet`; contracts export `Sheet*` and `sheet*`, and limits use `maxSheet*`. The generic `/content` read and `/operations` write URLs retain their existing authorization and semantics. Schema v42 migrates `kind=table` to `sheet` and renames `studio_table_ids` to `studio_sheet_ids` without rewriting content, version hashes, request identities or messages. Legacy table tool names/payloads and API creation kind are accepted at explicit compatibility boundaries; see [Sheets](../docs/sheets.md).

The note streaming extension is additive: range `begin`/commit accept optional `ranges`/`replacements` for a single atomic multi-range revision, and operation snapshots include precise ranges with targets redacted. Existing scalar requests and saved operations remain compatible, with no schema migration. A transient `turn.tool.argsText` snapshot replaces accumulated arguments on subscription/reconnect for in-progress note editing calls; following `argsDelta` fields append normally. The hub bounds these snapshots to 32 calls / 256 KiB per call / two minutes and removes them on tool execution or turn termination. Incomplete arguments never enter canonical document content.

The Page block extension is additive to protocol 31. Content reads add optional `structure` (`page-blocks-v1` schema), while preserving Markdown body/hash/revision fields. Page writes add optional `blockHints`; edit begin adds optional `blocks` containing stable `blockID`, `version` and `placement` (`replace`, `before`, `after`). Block-based begins validate target versions rather than the whole-body hash; legacy source ranges retain hash validation. Lease/commit/SSE shapes are unchanged. Schema 45 adds explicit content formats to current snapshots and revisions and converts current Pages without rewriting history; see [Page storage](../docs/studio.md#page-block-storage-schema-45).

Page naming is an additive protocol extension: Studio items may include `nameMode` (`auto`/`manual`) and `nameFallback`. Creation may supply these fields; rename accepts optional `nameMode: auto` to resume content-derived naming. Old rename requests pin the supplied name. See `docs/studio.md` for schema 46 migration and history semantics.
