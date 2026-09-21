# Public client contracts

`runtime.json` is the single source for `protocolVersion` and browser capacity limits.
The Go daemon embeds it through this package; desktop generates `web/contracts/` from the same pinned revision.
`api.ts` and `events.ts` are the TypeScript/Zod client validation definitions for the REST and SSE protocol.
Server structs and lifecycle semantics remain owned by core; verify both sides when changing a wire field.

Protocol 2 adds the external `-ui-dir` startup contract and removes mobile pairing/device-token authentication.
Clients reject incompatible daemon handshakes rather than attaching to an older embedded-UI runtime.
The generated client files must not be edited or committed in desktop.

Protocol 3 adds scheduled task definitions, run history, and daemon scheduling. Desktop requires these routes; the handshake rejects older daemons rather than presenting a management page backed by missing endpoints.
