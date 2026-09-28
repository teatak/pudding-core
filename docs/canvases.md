# Unified Canvas resource contract

Protocol 7 supports React source canvases under `/canvases`. Schema v27 retires structured content to offline archives; v28 deletes the obsolete per-query grants column. The resource keeps one source/revision store with session mounts that carry no content copies. Shared client schemas and limits live in `contracts/workbench.ts` and `contracts/workbench.json`. Desktop product behavior and acceptance records live in its `docs/canvases.md`.

A canvas has an integer optimistic `revision`, immutable `headRevision` / `activeRevision` source hashes, optional `sourceSessionID`, source-slot bindings and `bindingVersion`. Deleting the origin session clears the provenance link without deleting the canvas. Source files are atomically installed under `<home>/workbenches/<id>/revisions/<hash>` before SQLite records the reference. Failed saves cannot overwrite the active source.

Every revision references an immutable source package and requires a matching build receipt before activation. Saves accept `package` only; revision reads return `kind: app`, `package` and `manifest`. No structured content writer, layout writer or alternate renderer remains.

Migration v27 first archives existing structured versions to `<home>/archives/legacy-canvas/<timestamp>-<digest>/`. Archives contain original JSON, source metadata, existing revision relationships, mounts/favorites, readable static HTML, Markdown/CSV and referenced local assets. Missing assets are explicitly listed; inaccessible assets and other archive failures abort migration. Files are verified and synced before an atomic directory rename; only then does the SQL transaction retire structured content. Deterministic archive identities allow safe retry after a database failure. Original React source paths, hashes, bindings, action records and links remain intact. Old per-query grants are deleted in v28. Mixed resources retain App versions; an active structured version is replaced by the newest previously built App version, or remains a draft if none was built.

Archive membership comes from filesystem manifests, not a database flag. Listing returns an empty array for a fresh installation and after complete cleanup. Cleanup validates exact IDs, removes each manifest last, and retains failed entries for retry. The desktop shows the entry inside Canvas only while archives exist. Migration backups are separate whole-database recovery files and are not removed by archive cleanup.

Routes (all require the existing loopback startup token):

- `GET/POST /canvases`, `GET/DELETE /canvases/{id}`.
- `POST/GET /canvases/{id}/draft` starts or reads the persistent working copy, initialized from the current source head. `GET/PUT /canvases/{id}/draft/file` reads or edits one file; writes require the previous `draftHash`, and `null` deletes. An incomplete draft is allowed. `POST /canvases/{id}/draft/commit` validates the complete package, publishes one immutable revision and advances the source head only when it still matches the draft's base. Draft conflicts return `currentDraftHash`; source conflicts return `currentRevision` and `currentHeadRevision`. `GET /canvases/{id}/revisions` and `GET /canvases/{id}/revisions/{hash}` expose committed versions. No database migration is needed; drafts live under `<home>/workbenches/<id>/draft`.
- `POST /canvases/{id}/build-receipts`, `POST /canvases/{id}/activate`.
- `PUT /canvases/{id}/bindings`.
- `POST /canvases/{id}/queries/{operationID}`.
- `POST /canvases/{id}/actions/{operationID}/prepare`, `POST /canvases/{id}/action-runs/{actionID}/execute`, `GET /canvases/{id}/actions`.
- `GET/POST /canvases/{id}/links`, `DELETE /canvases/{id}/links/{linkID}`.
- `GET /canvas-archives`, `GET /canvas-archives/{id}/preview`, `GET /canvas-archives/{id}/export`.
- `DELETE /canvas-archives` with `{ids,confirm:true}`; these management routes are not exposed as LLM tools.

`POST /sessions/{id}/canvases/{canvasID}/open` opens or reuses a session reference. The session `/canvas/items` API lists resource references. Its former POST/PUT/PATCH writers are removed. Removing an item removes only its mount. Global deletion removes the resource from all views. The old `/workbenches`, `/canvas/saved`, and `/canvas/items/{id}/save` routes are absent. Library `savedItemID` now references the canonical resource, not a separate saved-content table.

Queries validate the exact saved operation, parameter schema, and current installed App connection. A unique usable connection is selected automatically; multiple usable connections require an explicit canvas binding. No per-query authorization is stored. Automatic queries require both a `read` effect hint and read-only transport semantics (REST GET without body, or a GraphQL query). A generated `effectHint` alone never grants write access. Only declared parameters are substituted; no server-side generated code is evaluated. REST and GraphQL share `internal/appexec` with session tools, whose original session/mode gates remain intact. Workbench requests cannot override connection-owned query/body fields.

Actions additionally require the installed endpoint to declare `workbench_writes: true` (default false). This field does not elevate upstream account permissions. Prepare freezes account, source, operation, validated input and resource revision. Execute requires explicit host confirmation, revalidates the context and claims the prepared record transactionally. Repeated confirmations return the existing run; a changed context returns a conflict. Transport ambiguity is `unknown`, and store startup changes unfinished `executing` runs to `unknown`. No automatic replay occurs. Deterministic failures before dispatch are `failed`. Query refreshes cannot overwrite the recorded action result.

Entity links retain both App and connection IDs. Local link writes require explicit confirmation and the current resource revision. Changing a binding does not retarget existing links to a new account. Source App data remains external; cached query results are not persisted as business facts.

Workbench HTTP does not create synthetic sessions or globally reuse MCP transports. Authoring tools are provided by the single Desktop `canvas` App; source creation/build/activation require Code mode, while resource discovery, reading and opening are available in Chat mode. All tools use explicit session routing; user-requested agent analysis uses ordinary session submit/cancel/events.

The sandbox runtime continues to use its internal Workbench types and source format. It does not introduce a second resource lifecycle.
