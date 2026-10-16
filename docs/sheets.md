# Native sheets

Canonical identities, agreed 2026-10-16:

| Layer | Canonical name |
| --- | --- |
| Plugin | `sheet-authoring` (Sheet Authoring / 表格创作) |
| Tools | `builtin_sheet_create`, `builtin_sheet_read`, `builtin_sheet_edit` |
| Create-tool content field | `sheet` |
| Item, mount and revision kind / mention | `sheet` / `@sheet/<id>` |
| Go storage interface | `SheetStore`, `CreateSheet`, `GetSheet`, `WriteSheet` |
| Shared data contracts | `SheetBody`, `SheetContent`, `SheetOperation`, `SheetWrite` |
| Client API | `createSheet`, `getSheet`, `writeSheet`, `readSheetRevision` |
| Identity tombstone table | `studio_sheet_ids` |
| Versions | SQLite schema 42; protocol 31 |

The existing generic HTTP URLs `/studio/items`, `/studio/items/{id}/content`,
`/studio/items/{id}/operations` and `/revisions/{hash}` remain unchanged; they
contain no table-specific spelling. They use canonical sheet payloads and the
same authentication, authorization, conflict and idempotency checks. `write`
is the storage/API persistence operation; the model-facing modification tool
is `edit`. Operation discriminators such as `update_column` are unchanged.

## Database upgrade

v42 rebuilds the Studio kind constraint to admit `sheet` instead of `table`,
updates all existing standalone table kinds (including archived/deleted items),
and renames `studio_table_ids`. Historical migrations through v41 remain intact.
Early v41 databases with `doc`/`rich-document` retain their existing repair before
this migration. The upgrade uses the existing pre-upgrade backup, one v42
transaction, and foreign-key validation before commit. A failure rolls back the
v42 changes. SQL TABLE terminology and document-embedded table nodes are unrelated.

Bodies, immutable revisions, hashes, timestamps, row/column tombstones, saved
requests, mount/item IDs and historical messages are not rewritten. New creates
use sheet IDs; retries of pre-rename deterministic creates reuse their old IDs.
No duplicate tool definitions, data copies or additional capability are introduced.

## Compatibility and retirement

Only these three executable aliases are accepted through canonical authorization:
`builtin_table_create` → `builtin_sheet_create`, `builtin_table_read` →
`builtin_sheet_read`, `builtin_table_update` → `builtin_sheet_edit`.
The old create argument `table` maps to `sheet`; supplying both fields is rejected.
Model history projects the tool name and create argument without mutating storage.
Current-turn native continuations follow the same projection. The API accepts old
creation `kind=table`, but stores and returns `sheet`. Historical `@table/` and
structured table references remain readable in Desktop; newly generated references
use sheet. An old Desktop does not satisfy protocol 31 and must upgrade with Core.

Retain runtime aliases for six calendar months after the first stable Desktop
release containing this change. First release, release date and remove-after date
are **pending**; record them when that release ships. Remove runtime alias helpers
and alias-only tests in the first maintenance release on/after that deadline.
Keep the v42 database migration so skipped-release upgrades remain supported.
Never add a runtime clock gate or rewrite canonical messages to retire aliases.

The old `tables` plugin ID has its separate retirement record in [Plugin IDs](plugin-ids.md).
