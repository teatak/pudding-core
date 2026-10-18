# Content naming

Updated 2026-10-18. Canonical Studio products are Page, Sheet and Widget.

| Product | Source/API names | Identity |
| --- | --- | --- |
| Page | PageContent, PageWrite, PageStore, CreatePage/GetPage/WritePage, PageEditOperation | page, @page, page-authoring, builtin_page_* |
| Sheet | SheetContent, SheetBody, SheetStore, CreateSheet/GetSheet/WriteSheet | sheet, @sheet, sheet-authoring, builtin_sheet_* |

## Internal source migration

The Page implementation is in `internal/store/page*.go`, `internal/store/sqlitestore/pages.go` and `page_edit_operations.go`, `internal/api/pages.go`, `internal/engine/page.go`, `internal/tool/page.go` and `internal/plugin/builtin_page.go`. The store interfaces, implementations, dispatch, leases, events and all callers/tests use Page identifiers. Redundant internal Note aliases have been removed, not wrapped. `NormalizeLegacyPageTool` in `internal/tool/page_compat.go` owns the old tool-name projection.

Desktop uses `web/src/studio/page/`, PageDetail, usePageEdits, usePageDrafts and Page client functions; see its paired naming guide. This source-only rename must preserve the insertion-point, streaming, cancellation, retry and selective-undo behavior on the latest main branch.

## Intentional residuals

- Executable old `builtin_doc_*`, `builtin_note_*`, `builtin_table_*` names and old plugin IDs are accepted only by the explicit compatibility adapters. New catalogs use Page/Sheet names.
- Historical `note`, `doc`, `table` kinds, references, IDs, request field names and retry fingerprints are retained or normalized only at their existing input/migration boundaries.
- Public TypeScript Note*/Document* exports remain exact aliases to Page schemas for compatibility. Internal Go code is not a public API and no longer needs Note aliases.
- SQLite `studio_document_edits` and `studio_document_edit_requests`, old migration SQL and all frozen testdata remain unchanged. They still carry Page leases and idempotency records.
- SQL/HTML/Markdown/ProseMirror tables, document formats, ordinary English notes, operating-system Notes applications and release notes are not Studio product names and must not be renamed.

Do not change JSON tags or serialized struct field order during a source rename. Metadata-only updates keep updatedAt and list order; only saved content revisions change them. No database or protocol migration is part of this refactor.

## Retirement and regression

Keep runtime aliases until six calendar months after the first stable Desktop release containing the corresponding rename. The first stable release and remove-after date are still pending; the commit date is not the retirement date. Follow docs/plugin-ids.md and the Studio/Sheet retirement records; never rewrite immutable history.

TestPageSourceUsesCanonicalIdentifiers rejects obsolete Studio identifiers in production Go code and obsolete Page implementation paths. Existing compatibility, frozen migration, insertion-point, batch lease, undo, engine and API tests continue to exercise the same wire and persisted behavior. Desktop must pin the paired Core commit and validate its default dependency path without a local override.
