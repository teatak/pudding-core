# Content naming

Updated 2026-10-18. This is the current naming reference for the Page / Sheet migration. Older documents and frozen migration fixtures may describe previous product identities; they are not instructions to advertise those identities in new requests.

| Product | Canonical kind / reference | Canonical contracts | AI tools / plugin |
| --- | --- | --- | --- |
| Page (Markdown) | `page`, `@page/<id>` | `PageContent`, `PageWrite`, `PageEditOperation`, `pageContent`, `pageEditState` | `builtin_page_create/read/edit/rename`, `page-authoring` |
| Sheet (typed tabular data) | `sheet`, `@sheet/<id>` | `SheetBody`, `SheetContent`, `SheetWrite`, `sheetContent` | `builtin_sheet_create/read/edit`, `sheet-authoring` |
| Widget | `widget` | `StudioItem`, widget contracts | `widget-authoring` |
| Shared content | `content` | `ContentSnapshot`, `ContentAuthor`, `ContentHash`, `ContentConflict` | `builtin_artifact_list/open`, `artifact` |

The artifact-list schema must advertise exactly `page`, `sheet`, and `widget`. Execution still accepts old filter values at explicit input boundaries. New model instructions, examples and generated references must use the canonical identities. Page text and Markdown tables inside a Page do not become Sheet items.

## What remains, and why

This inventory distinguishes a runtime identity defect from an implementation-only name. It is not a claim that every source symbol has already been renamed.

| Remaining spelling | Location / category | Treatment |
| --- | --- | --- |
| `builtin_doc_*`, `builtin_note_*` | `internal/tool/note_compat.go`; history and permission projections | Preserve the exact compatibility map. Advertise only `builtin_page_*`. |
| `builtin_table_create/read/update`, legacy `table` payload | Content-tool compatibility and `StudioArgs.LegacyTable` | Accept old requests, project to Sheet, reject simultaneous `sheet` and `table` payloads. Do not advertise old tools. |
| `note`, `doc`, `table` kinds / old reference prefixes | Creation, list filters, migration and persisted-reference readers | Normalize at the documented boundary; do not rewrite immutable history, IDs, hashes or fingerprints. |
| `note-authoring`, `markdown`, `tables` | Plugin/Skill identity compatibility | Resolve to `page-authoring` / `sheet-authoring`, without duplicate plugins. See [Plugin IDs](plugin-ids.md). |
| `NoteContent`, `NoteWrite`, `NoteEdit`, `NoteDefinitions`, `BuiltinNoteAuthoringID` | Existing source aliases | These refer to Page implementations. New touched code should consume the Page names. They are not separate products. |
| `NoteStore`, `GetNote`, `WriteNote`, `CreateNote`, `NoteEditRange`, `NoteEditOperation`, `UndoNote` | Internal Go methods, lease types and their callers/tests | Still implementation-only rename debt. A complete rename requires updating store interfaces, implementations and callers as one compile-checked change; it is not a database migration. |
| `internal/store/note*.go`, `internal/engine/note.go`, `internal/api/notes.go`, `internal/plugin/builtin_note.go` | Internal source filenames | Still implementation-only rename debt. Do not confuse a filename with an advertised kind or tool name. |
| `web/src/studio/note/`, `NoteDetail`, `useNote*`, `noteToolCompat.ts`, `data-note` | Desktop implementation paths, hooks and smoke selectors | Still implementation/test naming debt. Keep selectors synchronized with smoke scripts when renaming; preserve persisted storage keys or migrate them explicitly. |
| `DocumentContent`, `DocumentWrite`, `DocumentEditOperation` | Legacy TypeScript aliases for Markdown | Existing aliases remain; new Page code should import the canonical Page contracts directly. These aliases do not enable the retired structured-document product. |
| `studio_document_edits`, `studio_document_edit_requests`, old schema SQL | Persistence and frozen upgrade fixtures | Preserve. These tables still hold Page leases and retry records; removing or renaming them casually breaks upgrades. |
| SQL `table`, HTML/Markdown `table`, ProseMirror `table_*`, external `@tanstack/react-table`, table icons | Technical tabular concepts and third-party APIs | Keep. They are not legacy Sheet product identities. |
| Ordinary English “note”, LSP “document”, `.md`, `.csv` | Prose or format/protocol terms | Keep when they do not name a Studio item kind or API. |

## Safety constraints

Renaming a Go/TypeScript symbol does not justify changing its JSON tags or field order. Request serialization participates in idempotency hashes. Keep historical fixtures, migration SQL, original message/tool names, stored IDs, source bytes and version hashes unchanged. `updatedAt` remains the time of the latest saved content revision; metadata-only changes do not reorder the Studio list.

Do not replace every `table` token: SQL, DOM and Markdown tables remain tables. Do not erase old kinds from migrations or remove aliases while retained configurations, sessions and references still need them.

## Compatibility retirement

Runtime aliases remain for six calendar months after the first stable Desktop release containing the corresponding rename. The first stable release/date and removal date are still pending; do not invent a deadline from the commit date. Fill the release record in [Plugin IDs](plugin-ids.md) and the existing Studio/Sheet retirement records when the release ships. Before removal, migrate mutable retained identities or explicitly retain their read projection. Never rewrite canonical message history or retry identities. Implementation-only symbol cleanup does not have to wait for this runtime compatibility window.

## Regression checks and paired delivery

`TestStudioCatalogAdvertisesPageAndSheet` prevents the artifact-list schema from reintroducing `note` or `table` as current kinds. `TestCanonicalCatalogRetainsLegacyContentInputs` checks old names and the legacy Sheet payload boundary without weakening the canonical schema.

Desktop must pin a Core commit containing the exact contracts it consumes. The streaming extension needs both Page operation `ranges` and tool-event `argsText`; regenerating contracts from an older lock is a build failure, not merely a missing feature. Validate the default dependency path without `PUDDING_CORE_DIR`, then run the normal tests/build. Local overrides are useful for development but are not release evidence.
