# Studio item contract

Protocol 12 serves Studio items under `/studio/items`. Creation requires an explicit `kind`: `note` for Notes (native GFM Markdown), `document` for Documents (structured page-layout content), `table` for typed tabular data, or `widget` for LLM-authored React source packages built by Desktop. Shared schemas live in `contracts/studio.ts` (items, documents, tables and versions) and `contracts/widget.ts` (widget packages, manifests, builds, bridge, queries, actions and links). Limits live in `contracts/studio.json` and `contracts/widget.json`. Desktop product behavior and acceptance records live in its documentation.

An item has an integer optimistic `revision`, a `headRevision` and optional `sourceSessionID`. Widgets additionally use `activeRevision` source hashes, source-slot `bindings` and `bindingVersion`. Note, document and table heads refer to immutable snapshot IDs; their content hash is separate. Deleting the origin session clears the provenance link without deleting the item. Widget sources are atomically installed under `<home>/studio/<id>/revisions/<hash>` before SQLite records the reference; failed saves cannot overwrite the active source. Session views are mounts that carry no content copies.

Every widget revision references an immutable package of `widget.json` plus `src/*.tsx`. Manifest sources name an installed plugin endpoint as `{pluginID, endpoint}`, and generated code imports `@pudding/widget`. New activation requests require a matching build receipt. Revision reads return `kind`, `revision`, `package` and `manifest`.

## Routes

All routes require the loopback startup token.

- `GET/POST /studio/items`, `GET/PATCH/DELETE /studio/items/{id}`, `PATCH /studio/items/{id}/appearance`. Creation accepts `kind`, `name`, optional `sourceSessionID`, and an optional `body` (Markdown string for a note, structured JSON object for a table or document); Documents also accept optional `blobs`. Rename requires `name` and `expectedRevision`.
- `GET /studio/items/{id}/content` reads a note or table. `PUT` writes notes; `POST /studio/items/{id}/operations` writes tables. `POST /studio/items/{id}/assets` stores a raw raster upload; `GET /studio/items/{id}/assets/{filename}` reads it with the startup token.
- `GET/PUT /studio/items/{id}/document-content` reads/writes structured Documents; `GET /studio/items/{id}/document-assets/{hash}` reads retained document assets and original DOCX blobs.
- `POST/GET /studio/items/{id}/draft` starts or reads the persistent widget working copy, initialized from the current source head. `GET/PUT /studio/items/{id}/draft/file` reads or edits one file; writes require the previous `draftHash`, and `null` deletes. An incomplete draft is allowed. `POST /studio/items/{id}/draft/commit` validates the complete package, publishes one immutable revision and advances the source head only when it still matches the draft's base. Draft conflicts return `currentDraftHash`; source conflicts return `currentRevision` and `currentHeadRevision`. Drafts live under `<home>/studio/<id>/draft`.
- `GET /studio/items/{id}/revisions`, `GET /studio/items/{id}/revisions/{hash}`.
- `POST /studio/items/{id}/build-receipts`, `POST /studio/items/{id}/activate`, `PUT /studio/items/{id}/bindings`.
- `POST /studio/items/{id}/queries/{operationID}`.
- `POST /studio/items/{id}/actions/{operationID}/prepare`, `POST /studio/items/{id}/action-runs/{actionID}/execute`, `GET /studio/items/{id}/actions`.
- `GET/PUT /studio/items/{id}/data` reads/writes widget-owned persistent JSON data. See below.
- `GET/POST /studio/items/{id}/links`, `DELETE /studio/items/{id}/links/{linkID}`.
- `POST /sessions/{id}/studio/items/{itemID}/open` opens or reuses a session mount; `GET /sessions/{id}/studio/mounts` lists them and `DELETE /sessions/{id}/studio/mounts/{mountID}` removes one mount. Global deletion removes the item from all views. Library favorites reference the item as `studio:<id>`.

All native content and widget operations validate the item kind. Notes, Documents and tables do not have builds, activation, plugin bindings or widget drafts.

## Archives

Protocol 12 / schema v30 adds `archivedAt` to all three item kinds. `GET /studio/items` lists active items; `?scope=archived` lists archived items. `POST /studio/items/{id}/archive` and `/restore` require `expectedRevision`; changing state increments the metadata revision without changing content history or `updatedAt`. Repeating the same state is idempotent and never extends retention. Archived items are unavailable to normal reads/writes, tools, active lists and session mounts. Restore preserves content, versions, drafts, bindings, mounts and favorites.

The daemon checks at startup and hourly, permanently deleting items archived for at least 30 days. `DELETE /studio/items/{id}` with `expectedRevision` is immediate permanent deletion. Cleanup first records an inaccessible tombstone, removes `<home>/studio/<id>` (including assets and widget files), then deletes the database row with cascading dependent history and references. Filesystem failures leave the tombstone for the next cleanup; restoring or changing the item after a cleanup scan invalidates its revision and prevents stale deletion. Desktop normal removal uses archive without confirmation after saving local drafts; only permanent deletion asks for confirmation.

## Native notes

The working Markdown and its SHA-256 content hash live in SQLite `studio_item_content`. Each write atomically updates that body, the item head and an immutable `studio_item_revisions` snapshot with an author (`user`, or trusted session ID + turn ID). Storing the body with its versions keeps a note write in one transaction; there is no second filesystem copy of the working Markdown. Revision IDs include the parent and request ID, so restoring identical content creates a new version. Revision lists omit bodies; individual revision reads return `{kind, revision}` with `revision.body`. Deleting the origin session does not remove content or historical authorship.

Writes require `clientRequestID` and exactly one of `body`, `edits`, `restoreRevision`, `undoRevision` or lease-bound `replacement`. Full replacement and restore require `expectedHash`; stale content returns 409 `{error: "content_conflict", currentHash}`. Anchor edits resolve every `old` string against the latest body, require a unique non-overlapping match and apply atomically, so unrelated human changes remain intact. The same request ID and payload return the original write result without applying it again; reuse with different content is rejected.

Desktop seals each human editing pause after 1 second; every persisted human save is already a version before a later AI edit or restore. `preserveOnly: true` archives an unsaved human body without replacing the working content, then returns the current body for the load-latest conflict action. AI tools cannot use this option. Saving the local body after a conflict explicitly compares against a fresh hash; both authors' versions remain available.

`contracts/studio.json` bounds a note at 2 MiB UTF-8, a raster asset at 10 MiB and one edit call at 100 anchors. Tool reads accept Unicode character offsets and limits (default 16000, at most 16384 characters / 64 KiB) and return `nextOffset` when needed. PNG, JPEG, GIF and WebP uploads are detected from their bytes and atomically stored at `<home>/studio/<id>/content/assets/<sha256>.<ext>`. Markdown uses relative `assets/...` paths. Desktop fetches these authenticated assets as blobs, suppresses remote image requests and never executes raw HTML. Export returns the Markdown source unchanged.

The optional Core `artifact` plugin exposes only `builtin_artifact_list` and `builtin_artifact_open`. The independent `note-authoring` plugin exposes `builtin_note_create`, `builtin_note_read` and `builtin_note_edit`; `sheet-authoring` owns `builtin_sheet_create/read/edit`; `document-authoring` owns `builtin_document_create/read/edit`. Each plugin loads its own tools and Skill in Chat mode. A known content ID does not require loading `artifact` or sibling plugins. Tools execute in Core with explicit session and turn scope, without Desktop or per-write approval; scheduled turns and child sessions use the same path. Creation opens a mount and returns `itemID`, `mountID`, `contentHash` and `revisionID`. Content is read explicitly through tools, not injected into system instructions. There is no AI deletion tool. Widget source editing remains a Code-mode capability of `widget-authoring`.

### Tool names and legacy doc retirement

Core files, types and functions follow `note` / `document`; shared persistence uses `content`. See [Content naming](content-naming.md) for source paths, protocol exceptions and historical-data regression fixtures.

Studio Notes is the lightweight Markdown editor; Studio Documents is the structured page-layout editor with DOCX import/export. They keep separate creation/import entries and independently loaded plugins. Plugin and Skill names are Note Authoring and Document Authoring; creation entries and content labels stay Notes and Documents. Chinese UI names are localized; model-facing instructions remain English.

| Product | Plugin ID | Canonical tools |
| --- | --- | --- |
| Notes | `note-authoring` | `builtin_note_create/read/edit` |
| Documents | `document-authoring` | `builtin_document_create/read/edit` |
| Content Library | `artifact` | `builtin_artifact_list/open` |

Release audit: the latest stable Desktop [v0.5.0](https://github.com/teatak/pudding/releases/tag/v0.5.0), published 2026-10-08, pins Core `1571c7bacc4aa7f9e2dd7b9e75329d0ef0f2011e` and shipped only `builtin_doc_create/read/edit` for Markdown. `builtin_rich_document_*` and the intermediate Markdown `builtin_document_*` names were not released; they have no compatibility aliases. `builtin_document_*` now exclusively edits structured Documents, never Markdown.

`internal/tool/page_compat.go` maps exactly those three released `builtin_doc_*` names to `builtin_note_*`. New tool catalogs and model instructions expose only canonical names. Legacy calls retain the same plugin loading, enablement and capability checks; stored call/result identities are preserved. History is translated when building model context (including provider continuations), without rewriting original records. Desktop `web/src/lib/noteToolCompat.ts` covers tool labels, events and editing state.

Retain this compatibility for six **calendar months after the first stable Desktop release containing this rename**, not after the discussion or commit date. Fill in this record and both compatibility-file comments at release time:

| Field | Record |
| --- | --- |
| First stable release | Pending first stable release of this change |
| Release date | Pending first stable release of this change |
| Remove-after date | Release date plus six calendar months; clamp to month end if needed |
| Removal release | First maintenance release on or after the remove-after date |

At retirement remove the exact legacy map and its execution, capability, plugin ownership, history and Desktop display/event/editing callers, plus compatibility-only tests. Retain canonical `note_*`/`document_*` tests and unrelated tool compatibility. Do not add a runtime clock gate or rewrite saved history. After retirement, old names are no longer executable or replayable as current tools; old history remains stored and uses generic UI fallback.

Plugin IDs now use `note-authoring` and `document-authoring` with [legacy identity compatibility](plugin-ids.md). Item IDs, Markdown source, structured body/versions and `.md`/`.docx` formats remain unchanged; stored kinds are `note` and `document`, and mentions use `@note/<id>` (while accepting historical `@doc/<id>` and `doc`/`rich-document` references). New notes use a notebook icon; documents use a page icon. Desktop presents the old default note icon as a notebook without changing stored data or other custom icons.

### Note range leases (protocol 28 / schema 40)

`GET /studio/items/{id}/edit-operations` returns active/recent terminal operations and a content hash, without lease tokens or target text. `POST` supports `begin`, `renew` and `cancel`; authenticated user cancellation may omit a token. Model callers use the same actions through `builtin_note_edit`, with session/turn ownership determined by Core.

`begin` accepts `expectedHash` and either a unique `target` or a zero-based, half-open UTF-8 byte range `from`/`to` (not the Unicode offsets used by `builtin_note_read`). Bounds must be Unicode boundaries, nonempty and at most 64 KiB. Up to eight non-overlapping leases may coexist. A lease lasts two minutes, renewable up to fifteen minutes from creation. State survives restart and expiration is checked on access.

Commit uses `PUT .../content` with `operationID`, `leaseToken`, `replacement` and a request ID, or the tool's `action: commit`. Owner, token, expiration and unchanged target are checked in the same short SQLite transaction that writes the new revision and marks the operation committed. Replayed commits return their original result without moving the head. Cancel/renew cannot resurrect terminal operations.

A batch `begin` may instead supply `ranges: [{target, from?, to?}, ...]` (at most eight, disjoint and in document order, total target text at most 64 KiB). Commit supplies `replacements: [text, ...]` in matching order; an empty string deletes that passage. The entire batch shares one lease, cancellation and immutable revision. Selective `undoRevision` restores the whole batch while preserving disjoint later writes. Existing single-range requests and stored operations remain readable without a migration; `from/to` on a batch cover its outer bounds for older clients, while `ranges` carry precise locks. Public snapshots redact every target and the lease token. Ended model turns cannot begin or commit leases; reading active operations retires their abandoned reservations.

The editor can preview incremental `turn.tool` `streaming_args` for a lease-bound commit. Tools should emit `item_id`, `action`, `operation_id`, `lease_token` before `replacement/replacements`; providers without argument deltas still commit correctly. Incomplete arguments are presentation only: Core never writes partial text. Cancelling a reservation rejects a later commit even if model generation continues. Tool aliases retain this behavior.

All working-body writes, including legacy anchor edits, replacement, restore and selective undo, check active ranges. Unrelated changes rebase leases; touching a protected range or its boundary returns `409 document_locked`. `preserveOnly` does not change the working body. These leases currently apply to Studio Notes; they do not lock structured Documents, project files or external filesystem writers.

## Native tables

`kind: sheet` stores `{columns: [{id, name, type, options?}], rows: [{id, cells: {columnID: value}}]}` in `studio_item_content`. Supported types are text, finite number, date (`YYYY-MM-DD`), select (declared options), checkbox (boolean) and link (HTTP/HTTPS). Missing cells and `null` are blank. IDs are 1–100 ASCII letters, digits, underscores or hyphens; order is independent of identity. Deleted IDs cannot be reused, even after an add/delete pair in one batch. Restoring a historical version revives the same identities.

`POST /studio/items/{id}/operations` accepts `clientRequestID` and exactly one of `operations`, `restoreRevision`, `undoRevision` or lease-bound `replacement`. Operations are `set_cell`, `add_row`, `delete_row`, `move_row`, `add_column`, `update_column`, `delete_column` and `move_column`. Cell operations name `rowID` and `columnID`; `expected` optionally compares an old value, with null meaning blank. Without it, the latest write to that cell wins. Moves use optional `beforeID`; omission appends. Column updates contain the complete column with its unchanged ID. Type conversions and their cell updates are one atomic batch. Missing targets, invalid types and limit violations reject the entire batch. Unrelated human and AI edits survive because writes apply to the current body by ID, without whole-body replacement.

Every successful write atomically updates content/hash/head, immutable version, author and request fingerprint. Retrying the same ID/payload returns the original result without replaying it over newer edits. Reuse with different operations is rejected. Restore requires `expectedHash` and appends a new version; it never rewinds the history. Optional expected hashes produce `content_conflict`; stale cell expectations produce `cell_conflict` with row/column IDs.

The independent Chat-mode `sheet-authoring` plugin exposes `builtin_sheet_create`, `builtin_sheet_read` and `builtin_sheet_edit`. Creation opens a mount. Reading accepts optional row/column IDs, then applies offset/limit to selected rows in canonical order; responses provide `nextOffset`, identity metadata and JSON or CSV. Writes run entirely in Core with trusted session/turn authorship, without Desktop or per-write approval. `@sheet/<id>(name)` references the item.

Limits come from `contracts/studio.json`: 10,000 rows, 100 columns, 10 MiB serialized JSON, 64 KiB per string cell, 1,000 operations per batch and 200 rows / 1 MiB per tool read. Column names and select options are at most 200 UTF-8 bytes; at most 100 distinct nonempty select options. Oversized read rows require a narrower column selection. There are no formulas, filter views or merged cells.

## Selective AI undo

Protocol 11 adds `undoRevision` to the existing note and table write requests; table writes may also specify `rowID`. This is a user-only operation, not an AI tool. The selected immutable version must belong to this item, have session authorship and a parent, and be in the current head's ancestry. The before/after pair comes from canonical snapshots, never from Desktop state. AI creations have no parent and cannot be undone by deleting content.

Within one transaction, Core maps the inverse onto current content and appends a user-authored version. Notes use exact character changes: later disjoint edits are retained, overlapping edits (including ambiguous insertion boundaries) reject the operation. Tables compare stable IDs and individual cells/column fields. A row undo only applies to changed cells in a row present before and after, with unchanged columns in the selected change. Row/column additions, deletions and order changes use whole-change undo; later structural changes that make the order ambiguous cause a conflict. Restoring a deleted row/column revives its original identity.

Conflicts return `content_conflict` and do not write any content or version. Repeating an identical request ID/payload returns its saved result. A new request against an already reverted change is checked against current content and may conflict; this also applies to whole-change undo after one of its rows was reverted. No force or fuzzy merge mode is provided. This uses the native content and immutable version tables in schema v30.

Desktop derives change review from the same version pair. Keeping a change is temporary local review progress; undo uses these APIs. Selection rewriting and filling a column prepare an editable session-scoped Chat draft using existing Studio tools, and do not send a turn automatically.

## Queries and actions

Queries validate the exact saved operation, parameter schema and current installed plugin connection. A unique usable connection is selected automatically; multiple usable connections require an explicit binding. No per-query authorization is stored. Automatic queries require both a `read` effect hint and read-only transport semantics (REST GET without body, or a GraphQL query). A generated `effectHint` alone never grants write access. Only declared parameters are substituted; no server-side generated code is evaluated. REST and GraphQL share `internal/pluginexec` with session tools, whose session/mode gates remain intact. Widget requests cannot override connection-owned query/body fields.

Actions additionally require the installed endpoint to declare `widget_writes: true` (default false). This field does not elevate upstream account permissions. Prepare freezes account, source, operation, validated input, bindings and the page target (or item revision for direct management calls). Execute requires explicit host confirmation, revalidates the context and claims the prepared record transactionally. Repeated confirmations return the existing run; a changed context returns a conflict. Transport ambiguity is `unknown`, and store startup changes unfinished `executing` runs to `unknown`. No automatic replay occurs. Deterministic failures before dispatch are `failed`. Query refreshes cannot overwrite the recorded action result.

Entity links retain both plugin and connection IDs. Local link writes require explicit confirmation and the current item revision. Changing a binding does not retarget existing links to a new account. Source plugin data remains external; cached query results are not persisted as business facts.

Studio HTTP does not create synthetic sessions or globally reuse MCP transports. Widget authoring tools are provided by the Desktop `widget-authoring` plugin and require Code mode. All tools use explicit session routing; user-requested agent analysis uses ordinary session submit/cancel/events.

## Upgrades

The release upgrade from schema v24 to v25 converts structured content (Markdown, tables, charts, galleries, timelines, grids and read-only forms) into editable React source packages in the v25 layout, `<home>/canvases/<id>/revisions/<hash>` with `canvas.json`. Referenced attachments and public HTTP(S) images are embedded as data URLs; public fetches use no credentials or proxy, reject private/loopback addresses, and have bounded time and size. An image that cannot be embedded becomes a placeholder that keeps its alt text and caption. Unsupported content aborts the upgrade and leaves the old database intact. The v24 release stored only current content, so no past versions are invented.

The upgrade from released schema v26 to v30 combines Studio naming, native documents, tables and archives in one transaction. Unpublished intermediate versions 27–29 are no longer accepted; databases already at v30 keep their schema and data without running a migration. The upgrade:

- `canvas_resources`, `canvas_revisions`, `canvas_saves`, `canvas_mounts`, `canvas_actions` and `canvas_links` become `studio_items` (every row a `widget`), `studio_item_revisions`, `studio_item_saves`, `studio_mounts`, `widget_actions` and `widget_links`; library kinds and favorite IDs move from `canvas` to `studio`; `sessions.loaded_app_ids` becomes `loaded_plugin_ids`, mapping `app-authoring` to `plugin-authoring` and `canvas` to `widget-authoring`.
- Every revision and draft is rewritten into `<home>/studio/<id>` with `widget.json`, `pluginID` sources and the `@pudding/widget` import, so every content hash changes. Heads, active selections, parents, saves and draft bases are remapped; build receipts are dropped, and opening a widget performs a real build of the converted source. Stored action specs and link entities rename `appID` to `pluginID`; action records keep the source hash they were prepared against.
- Packages are installed before the SQL commit and are reused on retry. A failure rolls back the database, and the whole-database upgrade backup is kept. `<home>/canvases` stays in place as the source matching that backup.

Plugin packages, connections and enablement move from the App layout at daemon start (see [Plugins](plugins.md)). Canonical messages are never rewritten: model requests read stored `builtin_app_load` results and `app_skill` references under the current plugin names, and keep stored `app_mcp__*` and `canvas_*` calls under the names those tools have now.

The same transaction creates `studio_item_content`, document body/hash/authorship fields on revisions, a request fingerprint on saves, `studio_table_ids` for row/column identity history, and the archive timestamp/index. Migration tests start from the released v26 layout, fail at the final archive index, verify that all database changes and the version roll back to v26, then retry and reopen. Canonical messages, widget versions and sources are preserved; native documents, tables and archive state are also exercised after migration. The upgrade makes one pre-migration database backup; reopening v30 does not create another.

Development snapshots produced by the abandoned archive migration can be restored with `scripts/restore-canvas-dev` against a copied v26 database; the next start upgrades the result. This recovery is not part of daemon startup.

## Widget-owned persistent data

Protocol 17 / schema 33 adds `widget_data`, keyed by item ID and independent of session mounts or source revisions. GET returns `{version,data}`, initially `{version:0,data:{}}`. PUT requires `{revisionHash,expectedVersion,data}`; data is a JSON object of at most `contracts/widget.json.maxStorageBytes` bytes (256 KiB), and direct management calls without a `targetID` require the active source. Guest writes include the host-owned `targetID`: protocol 21 validates the page’s pinned source, so an already-open old revision can keep writing after a default upgrade. The item must be a live, unarchived Widget. No caller-supplied session ID or storage owner is accepted.

Writes replace the object and increment its version in one transaction. Stale data versions return HTTP 409 `widget_data_conflict`; an unauthorized source or revoked target returns the existing item conflict. There is no automatic retry: callers must read and reconcile stale or uncertain writes. Renaming, source upgrades, closing views and restarts preserve data; archiving blocks access but keeps it, and permanent deletion cascades. Schema 33 migration adds only this table; it does not rewrite any source or old migration.

Page state is stored separately in widget_pages (protocol 20 / schema 35), keyed by item, opening scope and source revision. The SDK hydrates the committed snapshot before author code runs. Refresh and restart retain it, while explicit tab close clears it. Core restores participant bindings into a paused run without replaying old requests. Data formats, interface names and business rules remain author-defined; conversation history is not a state source.


## Pinned page revisions (protocol 21 / schema 36)

`widget_page_pins(item_id,scope,revision_hash)` is the authority for each open content page's source. `POST /studio/items/{id}/pages/select` with `{scope}` resolves or creates a pin from activeRevision; with `{scope,revisionHash}` it explicitly switches that scope to the current default and retires its previous targets. Historical previews do not create pins. Schema 36 seeds existing active-version snapshots and preserves all state; the migration is transactional.

A successful build and activation changes the default for new scopes, not existing pins. Refresh and restart keep the old pin and restore its snapshot with a new target and paused interaction. Explicit close deletes that scope's pin and snapshots. Source revisions remain immutable and retained. Different versions never silently consume each other's page snapshots.

`POST /studio/items/{id}/pages/authorize` validates the revision and target against a live built page and its pin. Interactions, page data writes and page-bound external actions retain authority across default changes but lose it on page close/switch or binding changes. Action specifications persist the target so a prepared action cannot execute after its page is replaced. Direct management actions without a target still require the current active source and resource revision.

Shared storage remains item-scoped across coexisting versions. Authors must preserve data-format compatibility and unknown fields; CAS prevents stale writes but does not implement schema compatibility or data rollback.


## Widget session connections (protocol 22 / schema 37)

Widgets request candidates from the desktop opening context, then call `interaction.connect({sessionIDs})`. The host provides one human identity plus at most 15 explicitly selected sessions. Studio has no preferred sessions, a conversation prefers itself, and a split prefers both visible conversations. Defaults are initial UI choices, never backend focus or automatic membership.

Protocol 23 adds candidate presentation metadata: `model.id`, `model.name`, and a self-contained `model.icon` image derived by Desktop from the session and provider configuration. `participantID` is present for an already connected session, allowing a widget to match the public participant identity without routing fields in the public run. Credentials and provider endpoints are not exposed. Widgets using these fields require protocol 23; the page-state and database schemas are unchanged.

`PUT /widget-runs/:runID/participants` updates the authoritative connection set. Retained sessions keep participant IDs; removed sessions lose authorization and only this widget's pending/running work is cancelled. Page state and canonical notices remain intact; newly connected sessions never receive prior receipts. The SDK receives `connectedSessionIDs` for redisplaying existing selections. Connection alone does not trigger a model request.

Core no longer stores business roles. Authors store seats, readiness, participants' decisions and phases in page state, keyed by trusted actor.participantID. The actor contains runID/participantID/notificationID, never author-supplied identity. Schema 37 removes old host roles from saved interaction JSON, preserving IDs, connections and page data (no table layout change). Refresh/restart restores the same IDs into a new paused execution; the author's continue control calls resume/onResume without replaying old work. Pause affects automatic models, while human state writes and new notices remain valid. Disconnect removes connections without clearing page data. Explicit close clears the page.


## Content plugin split

The UI keeps a flat plugin list: Content Library (`artifact`), Note Authoring (`note-authoring`), Document Authoring (`document-authoring`) and Table Authoring (`sheet-authoring`). Enablement and session loading are independent. Loading one plugin never implicitly loads another; already loaded plugins remain until explicitly unloaded.

On upgrade, `plugins.content_plugins_split` records the one-time settings migration: explicit `artifact` enablement (including the legacy `studio` alias) is copied to new type plugins only where they have no explicit value. Later enable/disable changes do not propagate. Historical loaded `studio`/`artifact` sessions retain only common list/open access and load type capabilities on demand. Stored content, revisions and historical tool names are unchanged.

The Documents editor uses structured `document` content with DOCX import/export. Its AI tools create documents, read text/outlines/nodes and apply atomic local body operations with CAS, immutable session-authored history and selective user undo. Headers/footers, layout and unmentioned assets are preserved; AI range leases and complex layout edits remain deferred. Native typed tables are not XLSX workbooks. Tables embedded in Markdown or DOCX remain part of their owning documents.

### Native sheet identity

Standalone tables now use `sheet` in tools, API contracts, storage interfaces and database kinds (schema v42 / protocol 31). See [Sheets](sheets.md) for the complete compatibility and retirement contract. Embedded document/Markdown tables are unchanged.


### Page 插入点与编辑过程

Page create/read/edit 的工具结果提供 `bodyBytes`，始终为整篇正文的 UTF-8 字节长度（分页读取也不变）。模型追加时直接使用该值作为 begin 的 from/to，只提交新增文字及分段换行，避免计算中文/emoji 字节数或重复替换未改动的末段。旧结果字段保持兼容。

`page_edit begin` 支持 `from == to` 的 UTF-8 字节边界作为插入点，覆盖空页、段中、段间与文末追加；插入点同样参与范围锁、冲突检测、取消、整批提交和选择性撤销。相邻段落推荐使用同一个连续范围，不相邻范围仍分别保护，前端以单光标依次展示。新建写作先创建空页，再用插入点提交第一稿。当前模型目录只提供空正文创建及带 action 的分步编辑；不再暴露 body/edits 一次性写入。执行层在权限检查后拒绝当前 page 工具的旧写法并提供恢复步骤，避免已有会话模仿历史调用绕过光标、取消和打字机展示。已发布的 doc/note 别名及底层内容 API 保留旧参数兼容，历史记录不改写；已经完成的创建调用仍可幂等重试。

成功的 `commit` 工具回复附带脱敏的 `editState` 最终范围快照，供前端在刷新正文前对齐展示位置；写入不会等待前端动画。回复不包含范围原文和租约令牌，不能据此继续提交。

### Page block storage (schema 45)

Native Pages now persist `page-blocks-v1` JSON in `studio_item_content.body`, with an explicit `content_format`. The snapshot contains `schemaVersion`, a monotonic `generation`, a source prefix and ordered blocks (`id`, `type`, `version`, `markdown`). Markdown fragments include their original separators: exporting simply joins them. Paragraphs/headings have independent identities; lists, blockquotes, tables, fenced code and unsupported source are atomic blocks in this first schema. Inline ASTs, nested-item IDs and annotations are future extensions.

`GET /content` retains the exact Markdown `body`, SHA-256 `contentHash` and `revisionID`, and adds `structure`. There are not two editable bodies: the stored JSON is authoritative and the Markdown response is derived. Human saves keep the existing CAS/body request, optionally carrying `blockHints` with stable IDs and UTF-8 spans. Core reparses and validates spans; it owns types and versions. The editor maps identities through typing, moving, copying, splitting, saving and reopening without inserting IDs into exported Markdown. Copies get new IDs; a split keeps the original ID on the first block; a merge keeps the first surviving ID. Versions increase for changed/new blocks, including restoration, while untouched blocks retain theirs.

`page_read` adds block metadata (`id`, `type`, `version`, UTF-8 `from`/`to`, `complete`) for the returned body window. `page_edit begin` accepts `blocks: [{blockID, version, placement}]`, where placement is `replace`, `before` or `after`. Every targeted block is checked against the current snapshot; unrelated changes do not invalidate the request. Source targets/byte ranges remain compatible for partial selections and empty pages. Existing leases, sequential live animation, cancellation and atomic commit are reused. One batch creates one immutable revision. Structured undo follows block identity (including repeated text and deleted blocks), preserves disjoint human edits and rejects ambiguous conflicts; legacy revisions retain their character-based undo path.

Migration converts working Page snapshots only, after the existing migration backup. Historical content, IDs, hashes, authors, request fingerprints, messages and leases remain unchanged. New revisions carry their own format; history reads/restores decode both formats. JSON-looking Markdown is never guessed to be structured data. Sheets keep their existing body format. No project `.md` files are migrated.


## Page names from content (schema 46)

Page bodies contain only normal Markdown blocks; there is no synthetic filename node. New Pages default to `nameMode: auto`. At every accepted content save (including AI commits, undo and restore), the same SQLite transaction projects the first top-level H1 into `name`, or takes a 40-code-point summary of the first visible paragraph. Inline formatting, link destinations, frontmatter and code are excluded; names are bounded to 200 UTF-8 bytes. `nameFallback` stores the localized empty-content label supplied at creation, defaulting to `Untitled page`. Initial empty creation retains its supplied name until the first content save.

`PATCH /studio/items/{id}` with `name` pins a Page to `nameMode: manual`, including a rename to the same visible name. Optional `nameMode: auto` resumes following the current body without editing its content. Both use the existing optimistic item revision check. Other item kinds retain their naming behavior. The rename tool also pins the name; ordinary AI authoring edits a normal heading block instead.

Migration 46 preserves names, bodies, block identities, hashes, timestamps, immutable revisions and idempotency receipts. An exact derived-name match (or a known empty untitled label) enables auto mode; independent existing names stay manual. History restores content only and respects the current naming mode, so restoring an older body never unpins a manual name. Preserve-only snapshots and retries never rename the visible item. Markdown exports retain the body bytes and use the name solely for the filename.
