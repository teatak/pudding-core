# Workbench resource contract

Protocol 4 adds independent `/workbenches` resources; schema v25 adds source references, grants, action runs and explicit entity links. Shared client schemas and limits live in `contracts/workbench.ts` and `contracts/workbench.json`. Desktop product behavior and acceptance records live in its `docs/custom-workbenches.md`.

A workbench has an integer optimistic `revision`, immutable `headRevision` / `activeRevision` source hashes, optional `sourceSessionID`, source-slot bindings and `bindingVersion`. Deleting the origin session clears the provenance link without deleting the workbench. Source files are atomically installed under `<home>/workbenches/<id>/revisions/<hash>` before SQLite records the reference. Failed saves cannot overwrite the active source.

Routes (all require the existing loopback startup token):

- `GET/POST /workbenches`, `GET/DELETE /workbenches/{id}`.
- `GET/POST /workbenches/{id}/revisions`, `GET /workbenches/{id}/revisions/{hash}`.
- `POST /workbenches/{id}/build-receipts`, `POST /workbenches/{id}/activate`.
- `PUT /workbenches/{id}/bindings`, `POST /workbenches/{id}/grants`, `DELETE /workbenches/{id}/grants/{operationID}`.
- `POST /workbenches/{id}/queries/{operationID}`.
- `POST /workbenches/{id}/actions/{operationID}/prepare`, `POST /workbenches/{id}/action-runs/{actionID}/execute`, `GET /workbenches/{id}/actions`.
- `GET/POST /workbenches/{id}/links`, `DELETE /workbenches/{id}/links/{linkID}`.

Queries validate the exact saved operation, parameter schema, selected App/connection and current grant. Grants require trusted-host read confirmation and bind the operation hash and authorization fingerprint. A generated `effectHint` never grants permission. Only declared parameters are substituted; no server-side generated code is evaluated. REST and GraphQL share `internal/appexec` with session tools, whose original session/mode gates remain intact. Workbench requests cannot override connection-owned query/body fields.

Actions additionally require the installed endpoint to declare `workbench_writes: true` (default false). This field does not elevate upstream account permissions. Prepare freezes account, source, operation, validated input and resource revision. Execute requires explicit host confirmation, revalidates the context and claims the prepared record transactionally. Repeated confirmations return the existing run; a changed context returns a conflict. Transport ambiguity is `unknown`, and store startup changes unfinished `executing` runs to `unknown`. No automatic replay occurs. Deterministic failures before dispatch are `failed`. Query refreshes cannot overwrite the recorded action result.

Entity links retain both App and connection IDs. Local link writes require explicit confirmation and the current resource revision. Changing a binding does not retarget existing links to a new account. Source App data remains external; cached query results are not persisted as business facts.

Workbench HTTP does not create synthetic sessions or globally reuse MCP transports. Authoring tools remain Desktop runtime-provided Code tools with explicit session routing; user-requested agent analysis uses ordinary session submit/cancel/events.
