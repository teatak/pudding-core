# Studio item contract

Protocol 12 serves Studio items under `/studio/items`. Creation requires an explicit `kind`: `doc` for native GFM Markdown, `table` for typed tabular data, or `widget` for LLM-authored React source packages built by Desktop. Shared schemas live in `contracts/studio.ts` (items, documents, tables and versions) and `contracts/widget.ts` (widget packages, manifests, builds, bridge, queries, actions and links). Limits live in `contracts/studio.json` and `contracts/widget.json`. Desktop product behavior and acceptance records live in its documentation.

An item has an integer optimistic `revision`, a `headRevision` and optional `sourceSessionID`. Widgets additionally use `activeRevision` source hashes, source-slot `bindings` and `bindingVersion`. Document and table heads refer to immutable snapshot IDs; their content hash is separate. Deleting the origin session clears the provenance link without deleting the item. Widget sources are atomically installed under `<home>/studio/<id>/revisions/<hash>` before SQLite records the reference; failed saves cannot overwrite the active source. Session views are mounts that carry no content copies.

Every widget revision references an immutable package of `widget.json` plus `src/*.tsx`. Manifest sources name an installed plugin endpoint as `{pluginID, endpoint}`, and generated code imports `@pudding/widget`. New activation requests require a matching build receipt. Revision reads return `kind`, `revision`, `package` and `manifest`.

## Routes

All routes require the loopback startup token.

- `GET/POST /studio/items`, `GET/PATCH/DELETE /studio/items/{id}`, `PATCH /studio/items/{id}/appearance`. Creation accepts `kind`, `name`, optional `sourceSessionID`, and an optional `body` (Markdown string for a document, structured JSON object for a table). Rename requires `name` and `expectedRevision`.
- `GET /studio/items/{id}/content` reads a native document or table. `PUT` writes documents; `POST /studio/items/{id}/operations` writes tables. `POST /studio/items/{id}/assets` stores a raw raster upload; `GET /studio/items/{id}/assets/{filename}` reads it with the startup token.
- `POST/GET /studio/items/{id}/draft` starts or reads the persistent widget working copy, initialized from the current source head. `GET/PUT /studio/items/{id}/draft/file` reads or edits one file; writes require the previous `draftHash`, and `null` deletes. An incomplete draft is allowed. `POST /studio/items/{id}/draft/commit` validates the complete package, publishes one immutable revision and advances the source head only when it still matches the draft's base. Draft conflicts return `currentDraftHash`; source conflicts return `currentRevision` and `currentHeadRevision`. Drafts live under `<home>/studio/<id>/draft`.
- `GET /studio/items/{id}/revisions`, `GET /studio/items/{id}/revisions/{hash}`.
- `POST /studio/items/{id}/build-receipts`, `POST /studio/items/{id}/activate`, `PUT /studio/items/{id}/bindings`.
- `POST /studio/items/{id}/queries/{operationID}`.
- `POST /studio/items/{id}/actions/{operationID}/prepare`, `POST /studio/items/{id}/action-runs/{actionID}/execute`, `GET /studio/items/{id}/actions`.
- `GET/POST /studio/items/{id}/links`, `DELETE /studio/items/{id}/links/{linkID}`.
- `POST /sessions/{id}/studio/items/{itemID}/open` opens or reuses a session mount; `GET /sessions/{id}/studio/mounts` lists them and `DELETE /sessions/{id}/studio/mounts/{mountID}` removes one mount. Global deletion removes the item from all views. Library favorites reference the item as `studio:<id>`.

All native content and widget operations validate the item kind. Documents and tables do not have builds, activation, plugin bindings or widget drafts.

## Archives

Protocol 12 / schema v30 adds `archivedAt` to all three item kinds. `GET /studio/items` lists active items; `?scope=archived` lists archived items. `POST /studio/items/{id}/archive` and `/restore` require `expectedRevision`; changing state increments the metadata revision without changing content history or `updatedAt`. Repeating the same state is idempotent and never extends retention. Archived items are unavailable to normal reads/writes, tools, active lists and session mounts. Restore preserves content, versions, drafts, bindings, mounts and favorites.

The daemon checks at startup and hourly, permanently deleting items archived for at least 30 days. `DELETE /studio/items/{id}` with `expectedRevision` is immediate permanent deletion. Cleanup first records an inaccessible tombstone, removes `<home>/studio/<id>` (including assets and widget files), then deletes the database row with cascading dependent history and references. Filesystem failures leave the tombstone for the next cleanup; restoring or changing the item after a cleanup scan invalidates its revision and prevents stale deletion. Desktop normal removal uses archive without confirmation after saving local drafts; only permanent deletion asks for confirmation.

## Native documents

The working Markdown and its SHA-256 content hash live in SQLite `studio_item_content`. Each write atomically updates that body, the item head and an immutable `studio_item_revisions` snapshot with an author (`user`, or trusted session ID + turn ID). Storing the body with its versions keeps a document write in one transaction; there is no second filesystem copy of the working Markdown. Revision IDs include the parent and request ID, so restoring identical content creates a new version. Revision lists omit bodies; individual revision reads return `{kind, revision}` with `revision.body`. Deleting the origin session does not remove content or historical authorship.

Writes require `clientRequestID` and exactly one of `body`, `edits`, `restoreRevision` or `undoRevision`. Full replacement and restore require `expectedHash`; stale content returns 409 `{error: "content_conflict", currentHash}`. Anchor edits resolve every `old` string against the latest body, require a unique non-overlapping match and apply atomically, so unrelated human changes remain intact. The same request ID and payload return the original write result without applying it again; reuse with different content is rejected.

Desktop seals each human editing pause after 1 second; every persisted human save is already a version before a later AI edit or restore. `preserveOnly: true` archives an unsaved human body without replacing the working content, then returns the current body for the load-latest conflict action. AI tools cannot use this option. Saving the local body after a conflict explicitly compares against a fresh hash; both authors' versions remain available.

`contracts/studio.json` bounds a document at 2 MiB UTF-8, a raster asset at 10 MiB and one edit call at 100 anchors. Tool reads accept Unicode character offsets and limits (default 16000, at most 16384 characters / 64 KiB) and return `nextOffset` when needed. PNG, JPEG, GIF and WebP uploads are detected from their bytes and atomically stored at `<home>/studio/<id>/content/assets/<sha256>.<ext>`. Markdown uses relative `assets/...` paths. Desktop fetches these authenticated assets as blobs, suppresses remote image requests and never executes raw HTML. Export returns the Markdown source unchanged.

The optional Core `studio` plugin exposes `builtin_studio_list`, `builtin_studio_open`, `builtin_doc_create`, `builtin_doc_read` and `builtin_doc_edit` in Chat mode. Tools execute in Core with explicit session and turn scope, without Desktop or per-write approval; scheduled turns and child sessions use the same path. Creation opens a mount and returns `itemID`, `mountID`, `contentHash` and `revisionID`. Content is read explicitly through tools, not injected into system instructions. There is no AI deletion tool. Widget source editing remains a Code-mode capability of `widget-authoring`.

## Native tables

`kind: table` stores `{columns: [{id, name, type, options?}], rows: [{id, cells: {columnID: value}}]}` in `studio_item_content`. Supported types are text, finite number, date (`YYYY-MM-DD`), select (declared options), checkbox (boolean) and link (HTTP/HTTPS). Missing cells and `null` are blank. IDs are 1–100 ASCII letters, digits, underscores or hyphens; order is independent of identity. Deleted IDs cannot be reused, even after an add/delete pair in one batch. Restoring a historical version revives the same identities.

`POST /studio/items/{id}/operations` accepts `clientRequestID` and exactly one of `operations`, `restoreRevision` or `undoRevision`. Operations are `set_cell`, `add_row`, `delete_row`, `move_row`, `add_column`, `update_column`, `delete_column` and `move_column`. Cell operations name `rowID` and `columnID`; `expected` optionally compares an old value, with null meaning blank. Without it, the latest write to that cell wins. Moves use optional `beforeID`; omission appends. Column updates contain the complete column with its unchanged ID. Type conversions and their cell updates are one atomic batch. Missing targets, invalid types and limit violations reject the entire batch. Unrelated human and AI edits survive because writes apply to the current body by ID, without whole-body replacement.

Every successful write atomically updates content/hash/head, immutable version, author and request fingerprint. Retrying the same ID/payload returns the original result without replaying it over newer edits. Reuse with different operations is rejected. Restore requires `expectedHash` and appends a new version; it never rewinds the history. Optional expected hashes produce `content_conflict`; stale cell expectations produce `cell_conflict` with row/column IDs.

The Chat-mode `studio` plugin also exposes `builtin_table_create`, `builtin_table_read` and `builtin_table_update`. Creation opens a mount. Reading accepts optional row/column IDs, then applies offset/limit to selected rows in canonical order; responses provide `nextOffset`, identity metadata and JSON or CSV. Writes run entirely in Core with trusted session/turn authorship, without Desktop or per-write approval. `@table/<id>(name)` references the item.

Limits come from `contracts/studio.json`: 10,000 rows, 100 columns, 10 MiB serialized JSON, 64 KiB per string cell, 1,000 operations per batch and 200 rows / 1 MiB per tool read. Column names and select options are at most 200 UTF-8 bytes; at most 100 distinct nonempty select options. Oversized read rows require a narrower column selection. There are no formulas, filter views or merged cells.

## Selective AI undo

Protocol 11 adds `undoRevision` to the existing document and table write requests; table writes may also specify `rowID`. This is a user-only operation, not an AI tool. The selected immutable version must belong to this item, have session authorship and a parent, and be in the current head's ancestry. The before/after pair comes from canonical snapshots, never from Desktop state. AI creations have no parent and cannot be undone by deleting content.

Within one transaction, Core maps the inverse onto current content and appends a user-authored version. Documents use exact character changes: later disjoint edits are retained, overlapping edits (including ambiguous insertion boundaries) reject the operation. Tables compare stable IDs and individual cells/column fields. A row undo only applies to changed cells in a row present before and after, with unchanged columns in the selected change. Row/column additions, deletions and order changes use whole-change undo; later structural changes that make the order ambiguous cause a conflict. Restoring a deleted row/column revives its original identity.

Conflicts return `content_conflict` and do not write any content or version. Repeating an identical request ID/payload returns its saved result. A new request against an already reverted change is checked against current content and may conflict; this also applies to whole-change undo after one of its rows was reverted. No force or fuzzy merge mode is provided. This uses schema v29 unchanged.

Desktop derives change review from the same version pair. Keeping a change is temporary local review progress; undo uses these APIs. Selection rewriting and filling a column prepare an editable session-scoped Chat draft using existing Studio tools, and do not send a turn automatically.

## Queries and actions

Queries validate the exact saved operation, parameter schema and current installed plugin connection. A unique usable connection is selected automatically; multiple usable connections require an explicit binding. No per-query authorization is stored. Automatic queries require both a `read` effect hint and read-only transport semantics (REST GET without body, or a GraphQL query). A generated `effectHint` alone never grants write access. Only declared parameters are substituted; no server-side generated code is evaluated. REST and GraphQL share `internal/pluginexec` with session tools, whose session/mode gates remain intact. Widget requests cannot override connection-owned query/body fields.

Actions additionally require the installed endpoint to declare `widget_writes: true` (default false). This field does not elevate upstream account permissions. Prepare freezes account, source, operation, validated input and item revision. Execute requires explicit host confirmation, revalidates the context and claims the prepared record transactionally. Repeated confirmations return the existing run; a changed context returns a conflict. Transport ambiguity is `unknown`, and store startup changes unfinished `executing` runs to `unknown`. No automatic replay occurs. Deterministic failures before dispatch are `failed`. Query refreshes cannot overwrite the recorded action result.

Entity links retain both plugin and connection IDs. Local link writes require explicit confirmation and the current item revision. Changing a binding does not retarget existing links to a new account. Source plugin data remains external; cached query results are not persisted as business facts.

Studio HTTP does not create synthetic sessions or globally reuse MCP transports. Widget authoring tools are provided by the Desktop `widget-authoring` plugin and require Code mode. All tools use explicit session routing; user-requested agent analysis uses ordinary session submit/cancel/events.

## Upgrades

The release upgrade from schema v24 to v25 converts structured content (Markdown, tables, charts, galleries, timelines, grids and read-only forms) into editable React source packages in the v25 layout, `<home>/canvases/<id>/revisions/<hash>` with `canvas.json`. Referenced attachments and public HTTP(S) images are embedded as data URLs; public fetches use no credentials or proxy, reject private/loopback addresses, and have bounded time and size. An image that cannot be embedded becomes a placeholder that keeps its alt text and caption. Unsupported content aborts the upgrade and leaves the old database intact. The v24 release stored only current content, so no past versions are invented.

Schema v27 renames canvases to Studio items and Apps to plugins in one transaction:

- `canvas_resources`, `canvas_revisions`, `canvas_saves`, `canvas_mounts`, `canvas_actions` and `canvas_links` become `studio_items` (every row a `widget`), `studio_item_revisions`, `studio_item_saves`, `studio_mounts`, `widget_actions` and `widget_links`; library kinds and favorite IDs move from `canvas` to `studio`; `sessions.loaded_app_ids` becomes `loaded_plugin_ids`, mapping `app-authoring` to `plugin-authoring` and `canvas` to `widget-authoring`.
- Every revision and draft is rewritten into `<home>/studio/<id>` with `widget.json`, `pluginID` sources and the `@pudding/widget` import, so every content hash changes. Heads, active selections, parents, saves and draft bases are remapped; build receipts are dropped, and opening a widget performs a real build of the converted source. Stored action specs and link entities rename `appID` to `pluginID`; action records keep the source hash they were prepared against.
- Packages are installed before the SQL commit and are reused on retry. A failure rolls back the database, and the whole-database upgrade backup is kept. `<home>/canvases` stays in place as the source matching that backup.

Plugin packages, connections and enablement move from the App layout at daemon start (see [Plugins](plugins.md)). Canonical messages are never rewritten: model requests read stored `builtin_app_load` results and `app_skill` references under the current plugin names, and keep stored `app_mcp__*` and `canvas_*` calls under the names those tools have now.

Schema v28 adds `studio_item_content`, document body/hash/authorship fields on revisions, and a request fingerprint on saves. Existing widgets and their versions remain unchanged. Migration tests start from v27, inject a transactional failure, verify rollback and retry, and check persisted document versions after reopening the database.

Schema v29 adds `studio_table_ids` to retain row and column identity history; table bodies reuse the native content and version tables. The v28 upgrade test verifies existing document preservation, injected failure rollback, retry and restart.

Development snapshots produced by the abandoned archive migration can be restored with `scripts/restore-canvas-dev` against a copied v26 database; the next start upgrades the result. This recovery is not part of daemon startup.
