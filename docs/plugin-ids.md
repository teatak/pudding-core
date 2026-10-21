# Built-in plugin IDs

Current canonical IDs, updated 2026-10-21:

| Previous ID | Canonical ID | Default Skill ID |
| --- | --- | --- |
| `artifacts` (earlier `studio`) | `artifact` | `artifact` |
| `markdown`, `note-authoring` | `page-authoring` | `page-authoring` |
| `tables` | `sheet-authoring` | `sheet-authoring` |
| `collaboration` | `subagent` | `subagent` |
| `web` | `web-search-fetch` | `web-search-fetch` |

The earlier `docx` / `document-authoring` product is not part of the current Page/Sheet catalog. Do not relabel its retained structured data as Markdown. Canonical content kinds, references and tool names are listed in [Content naming](content-naming.md).

The [Sheet rename](sheets.md) gives `sheet-authoring` native `sheet` content and `builtin_sheet_*` tools; it does not introduce XLSX workbooks or formulas. Markdown tables remain part of a Page body.

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

The Web Search & Fetch rename also accepts the old `skills/web/SKILL.md`
selector and existing plugin-page links. The old `config/web.yaml` credentials
and selection migrate once to standard Connections with a single active connection
shared by search and fetch. It retains the `/tools/web` compatibility API and `builtin_web_search` /
`builtin_web_fetch` tool names. Enabling the plugin, saving credentials and
selecting the active connection remain separate operations.

Ship the paired Core and Desktop changes together by pinning the Core source
in `core.lock.json`. Do not infer a need to rewrite database history from a
plugin identity rename.

## Retirement record

Retain these runtime aliases for six calendar months after the **first stable
Desktop release containing the corresponding rename**, not six months after
its discussion or commit.

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
