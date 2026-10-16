# Built-in plugin IDs

Canonical IDs, agreed 2026-10-16:

| Previous ID | Canonical ID | Default Skill ID |
| --- | --- | --- |
| `artifacts` (earlier `studio`) | `artifact` | `artifact` |
| `markdown` | `note-authoring` | `note-authoring` |
| `docx` | `document-authoring` | `document-authoring` |
| `tables` | `sheet-authoring` | `sheet-authoring` |
| `collaboration` | `subagent` | `subagent` |

All other plugin IDs stay unchanged. This is a plugin-identity rename, not a
product, content-kind, tool, format, API-route or database-schema rename.
In particular, `sheet-authoring` owns the existing native `table` content and
`builtin_table_*` tools; it does not introduce XLSX workbooks or formulas.

## Compatibility

The catalog advertises exactly one canonical definition per built-in. New
loads, default Skill references and mentions use those IDs. Legacy IDs and
matching default Skill IDs/registered paths resolve to the same definition,
through the same enablement, capability and child-delegation checks. Aliases
are not extra plugins and remain reserved against installed-package collisions.

`store.CurrentPluginID` is the Core identity projection. Settings normalize on
read/write; explicit canonical settings win over legacy settings, and legacy
`artifacts` wins over still older `studio` when no canonical setting exists.
The existing one-time content-plugin split stays one-time. Existing sessions
normalize and deduplicate loaded IDs on read and explicit writes; reading does
not rewrite original rows. Historical messages, saved tool names, retry
fingerprints, content hashes and global/third-party Skill IDs are not rewritten.
Old plugin Skill references are projected only when building current context.
Desktop shares one local identity adapter for builtin labels, icons and alias
search; installed plugin metadata is not relabeled or rewritten.

Ship the paired Core and Desktop changes together by pinning the Core source
in `core.lock.json`. There is no database migration or protocol-version bump.

## Retirement record

Retain these runtime aliases for six calendar months after the **first stable
Desktop release containing this rename**, not six months after this discussion.

| Field | Value |
| --- | --- |
| First stable release | Pending |
| Release date | Pending |
| Remove-after date | Release date plus six calendar months, clamped to month end |
| Removal release | First maintenance release on/after remove-after date |

At release, fill this record. Before removal, normalize retained configuration
and session identities, including inactive sessions, or explicitly retain their
read projection; never strand old persisted data by simply deleting the map.
Remove expired executable alias handling and alias-only tests, retain canonical
load/unload/permission tests, and keep canonical historical messages untouched.
Do not add a runtime clock gate or silently discard disabled settings.
