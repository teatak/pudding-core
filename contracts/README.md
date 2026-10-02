# Public client contracts

`runtime.json` is the single source for `protocolVersion` and browser capacity limits.
The Go daemon embeds it through this package; desktop generates `web/contracts/` from the same pinned revision.
`api.ts` and `events.ts` are the TypeScript/Zod client validation definitions for the REST and SSE protocol.
Server structs and lifecycle semantics remain owned by core; verify both sides when changing a wire field.

Protocol 2 adds the external `-ui-dir` startup contract and removes mobile pairing/device-token authentication.
Clients reject incompatible daemon handshakes rather than attaching to an older embedded-UI runtime.
The generated client files must not be edited or committed in desktop.

Protocol 3 adds scheduled task definitions, run history, and daemon scheduling. Desktop requires these routes; the handshake rejects older daemons rather than presenting a management page backed by missing endpoints.

Protocol 4 adds independent canvas resources, immutable source revisions, App bindings/query grants, action runs and cross-App links. `canvas.json` owns runtime limits and `canvas.ts` the client schemas. Old daemons are rejected at handshake. Installed App endpoints must explicitly set `canvas_writes: true` to enable confirmed canvas actions; the default is disabled.

Protocol 7 removes canvas query grants and their routes. Canvas queries reuse installed App connections; ambiguous accounts require an explicit source binding. Automatic reads are limited to REST GET without body and GraphQL queries. Schema v28 drops the old grants data.

Protocol 8 renames canvases to Studio items and Apps to plugins. Items live under `/studio/items` with a `kind` (`doc`, `table`, `widget`; this release creates widgets); session views are `/sessions/{id}/studio/mounts`. `studio.ts` owns the item and version schemas, `widget.ts` the widget package, manifest, build, bridge, query, action and link schemas, and `widget.json` the widget runtime limits. Widget manifests are `widget.json` with plugin sources (`pluginID`), and generated source imports `@pudding/widget`. Plugin routes are `/plugins`, `/plugin-skills`, `/plugin-assets`, `/plugin-connections` and `/plugin-oauth`; sessions report `loadedPluginIDs`. Installed endpoints opt into confirmed widget writes with `widget_writes: true`. Old daemons are rejected at handshake; no `/canvases` or `/apps` routes remain.
