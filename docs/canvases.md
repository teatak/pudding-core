# Unified Canvas resource contract

Protocol 5 unifies structured Canvas content and App-backed pages under `/canvases`. Schema v26 uses one resource/revision store and session mounts that carry no content copies. Shared client schemas and limits live in `contracts/workbench.ts` and `contracts/workbench.json`. Desktop product behavior and acceptance records live in its `docs/canvases.md`.

A canvas has an integer optimistic `revision`, immutable `headRevision` / `activeRevision` source hashes, optional `sourceSessionID`, source-slot bindings and `bindingVersion`. Deleting the origin session clears the provenance link without deleting the canvas. Source files are atomically installed under `<home>/workbenches/<id>/revisions/<hash>` before SQLite records the reference. Failed saves cannot overwrite the active source.

Structured revisions store `{kind,title,item}` in SQLite and become active on save; App revisions store immutable source packages and require a matching build receipt before activation. Revision reads are discriminated as `kind: structured` with `content`, or `kind: app` with `package` and `manifest`. A save accepts exactly one of `content` or `package`. Both renderer types share the same resource ID, optimistic revision and history, including restoration across renderer types.

Migration v26 preserves old saved resource IDs and all App revisions, grants, actions and links. Clean session copies become references; unsaved copies become independent recovered resources. Favorites and recent-open foreign keys point to the new canonical tables. Session loaded-App IDs replace `workbench-authoring` with `canvas`. Original filesystem paths, SDK names and manifest format remain internal runtime contracts so existing packages keep their hashes.

Routes (all require the existing loopback startup token):

- `GET/POST /canvases`, `GET/DELETE /canvases/{id}`.
- `GET/POST /canvases/{id}/revisions`, `GET /canvases/{id}/revisions/{hash}`.
- `POST /canvases/{id}/build-receipts`, `POST /canvases/{id}/activate`.
- `PUT /canvases/{id}/bindings`, `POST /canvases/{id}/grants`, `DELETE /canvases/{id}/grants/{operationID}`.
- `POST /canvases/{id}/queries/{operationID}`.
- `POST /canvases/{id}/actions/{operationID}/prepare`, `POST /canvases/{id}/action-runs/{actionID}/execute`, `GET /canvases/{id}/actions`.
- `GET/POST /canvases/{id}/links`, `DELETE /canvases/{id}/links/{linkID}`.

`POST /sessions/{id}/canvases/{canvasID}/open` opens or reuses a session reference. The session `/canvas/items` API projects canonical content; updates require `expectedRevision`. Removing an item removes only its mount. Global deletion removes the resource from all views. The old `/workbenches`, `/canvas/saved`, and `/canvas/items/{id}/save` routes are absent. Library `savedItemID` now references the canonical resource, not a separate saved-content table.

Queries validate the exact saved operation, parameter schema, selected App/connection and current grant. Grants require trusted-host read confirmation and bind the operation hash and authorization fingerprint. A generated `effectHint` never grants permission. Only declared parameters are substituted; no server-side generated code is evaluated. REST and GraphQL share `internal/appexec` with session tools, whose original session/mode gates remain intact. Workbench requests cannot override connection-owned query/body fields.

Actions additionally require the installed endpoint to declare `workbench_writes: true` (default false). This field does not elevate upstream account permissions. Prepare freezes account, source, operation, validated input and resource revision. Execute requires explicit host confirmation, revalidates the context and claims the prepared record transactionally. Repeated confirmations return the existing run; a changed context returns a conflict. Transport ambiguity is `unknown`, and store startup changes unfinished `executing` runs to `unknown`. No automatic replay occurs. Deterministic failures before dispatch are `failed`. Query refreshes cannot overwrite the recorded action result.

Entity links retain both App and connection IDs. Local link writes require explicit confirmation and the current resource revision. Changing a binding does not retarget existing links to a new account. Source App data remains external; cached query results are not persisted as business facts.

Workbench HTTP does not create synthetic sessions or globally reuse MCP transports. Authoring tools are provided by the single Desktop `canvas` App; source creation/build/activation require Code mode, while structured rendering and resource discovery/opening are also available in Chat mode. All tools use explicit session routing; user-requested agent analysis uses ordinary session submit/cancel/events.

The sandbox runtime continues to use its internal Workbench types and source format. It does not introduce a second resource lifecycle.
