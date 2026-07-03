# Tool Migration Status

> Last checked: 2026-07-04. Old project is `pudding-core-old`; it is reference only.

## Scope

This document tracks LLM-callable tool migration from the old project to the new
`pudding-core` architecture.

Rules for new tools:

- Session-scoped behavior by default.
- No backend focus state.
- Local filesystem access must go through chat/workspace capability and explicit
  workspace directory authorization.
- When adding or renaming a tool, update:
  - tool schema / implementation
  - prompt guidance when the model needs workflow hints
  - transcript display name and i18n

## Current New Builtins

| Tool | Mode | Old equivalent | Status | Prompt | Transcript |
| --- | --- | --- | --- | --- | --- |
| `request_capability` | chat | New architecture replacement for implicit mode / FS consent paths | Done | Yes | Yes |
| `builtin_time_get_current` | chat | `builtin_time_get_current` | Done | Yes | Yes |
| `builtin_web_search` | chat | `builtin_web_search` | Done | Yes | Yes |
| `builtin_web_fetch` | chat | `builtin_web_fetch` | Done | Yes | Yes |
| `builtin_history_search` | chat | `builtin_history_search` | Done, semantics changed to current-session default | Yes | Yes |
| `builtin_history_get_message` | chat | `builtin_history_get_message` | Done, session-scoped | Yes | Yes |
| `builtin_skill_read` | chat | New on-demand skill body reader | Done | Yes | Yes |
| `builtin_file_list` | workspace | `builtin_files_list` | Done, renamed and scope-based | Partial | Yes |
| `builtin_file_read` | workspace | `builtin_files_read` | Done, text-focused | Partial | Yes |
| `builtin_file_stat` | workspace | `builtin_files_stat` | Done | Partial | Yes |
| `builtin_file_search` | workspace | `builtin_files_search` | Done | Yes | Yes |
| `builtin_file_slice` | workspace | `builtin_files_head` / `tail` / `slice` | Done, combines head/tail/slice via `origin` and `order` | Yes | Yes |
| `builtin_file_write` | workspace | `builtin_files_write` | Done | Skill prompt | Yes |
| `builtin_file_patch` | workspace | `builtin_files_apply_patch` | Done, single exact replacement call shape | Skill prompt | Yes |
| `builtin_file_delete` | workspace | `builtin_files_delete` | Done | Skill prompt | Yes |
| `builtin_file_move` | workspace | `builtin_files_move` | Done | Skill prompt | Yes |
| `builtin_file_copy` | workspace | `builtin_files_copy` | Done | Schema | Yes |
| `builtin_skill_validate` | workspace | `builtin_skill_validate` | Done, draft-id based | Skill prompt | Yes |
| `builtin_skill_submit` | workspace | `builtin_skill_install_from_draft` | Done, submits for Settings review instead of installing directly | Skill prompt | Yes |
| `builtin_rest_request` | chat | `builtin_http_request` | Done, app endpoint based | Yes | Yes |
| `builtin_graphql_request` | chat | `builtin_graphql_request` | Done, app endpoint based | Yes | Yes |
| `builtin_graphql_introspect` | chat | `builtin_graphql_introspect` | Done, app endpoint based with in-process schema cache | Yes | Yes |
| `builtin_graphql_search` | chat | `builtin_graphql_search` | Done, app endpoint based with in-process schema cache | Yes | Yes |
| `builtin_weather_get` | chat | `builtin_weather_get` | Done, wttr.in JSON backed | Yes | Yes |

Notes:

- `builtin_file_slice` intentionally replaces old `head` and `tail`; keep only
  this one unless user workflows prove separate tools are clearer.
- `builtin_history_search` no longer treats empty `session_id` as global search.
  Empty means current session through `tool.Call.SessionID`.
- Attachments and local folders are now canonical content parts plus upload APIs,
  not old-style LLM filesystem discovery.

## Old Builtins

| Old tool | New status | Notes / decision |
| --- | --- | --- |
| `builtin_session_end` | Not migrated | New app currently has no idle/end-session tool contract. Defer until voice/KWS/session lifecycle needs it. |
| `builtin_time_get_current` | Migrated | Same concept. |
| `builtin_files_list` | Migrated as `builtin_file_list` | Scope-based; workspace authorization enforced. |
| `builtin_files_read` | Migrated as `builtin_file_read` | Text read only; media/PDF/audio should be handled separately. |
| `builtin_files_write` | Migrated as `builtin_file_write` | Writable scopes are restricted. |
| `builtin_files_mkdir` | Not migrated | Can be added as `builtin_file_mkdir`; not urgent unless workflows need directory creation without writing a file. |
| `builtin_files_apply_patch` | Migrated as `builtin_file_patch` | New shape handles exact string replacement. |
| `builtin_files_search` | Migrated as `builtin_file_search` | Literal text search. |
| `builtin_files_copy` | Migrated as `builtin_file_copy` | Copy within one writable scope/root; directory copies require `recursive=true`. |
| `builtin_files_move` | Migrated as `builtin_file_move` | Rename/move within authorized scopes. |
| `builtin_files_delete` | Migrated as `builtin_file_delete` | Recursive delete guarded by argument. |
| `builtin_files_stat` | Migrated as `builtin_file_stat` | Includes MIME where available. |
| `builtin_files_head` | Folded into `builtin_file_slice` | Use `origin=start`. |
| `builtin_files_tail` | Folded into `builtin_file_slice` | Use `origin=end`; reverse order supported. |
| `builtin_files_slice` | Migrated as `builtin_file_slice` | Supports start/end and tail-style reads. |
| `builtin_skill_validate` | Migrated | New tool validates staged skill draft by `draft_id`. |
| `builtin_skill_status` | Not migrated | Consider later for Settings/skill authoring UX; not required for basic skill creation. |
| `builtin_skill_install_from_draft` | Replaced by `builtin_skill_submit` | New flow asks Settings/user review; no direct install. |
| `builtin_tool_doc_read` | Not migrated | Current tool schemas are inline; add only if builtin tool docs grow again. |
| `builtin_speaker_get_current` | Not migrated | Depends on voice/speaker identity subsystem. |
| `builtin_fact` | Not migrated | Depends on speaker profile / memory design. |
| `builtin_preference` | Not migrated | Depends on speaker profile / memory design. |
| `builtin_http_request` | Migrated as `builtin_rest_request` | New app endpoint model injects auth/config headers. |
| `builtin_graphql_request` | Migrated | New app endpoint model. |
| `builtin_graphql_introspect` | Migrated | Uses configured GraphQL app endpoints and injected auth. |
| `builtin_graphql_search` | Migrated | Uses full introspection plus in-process cache. |
| `builtin_vision_view_image` | Not migrated as a tool | Current image attachments are delivered to model/provider directly when supported; view/canvas handling is UI-side. |
| `builtin_vision_capture_screen` | Not migrated | Needs desktop native capture + approval design. |
| `builtin_vision_capture_camera` | Not migrated | Needs hardware/camera daemon ownership design. |
| `builtin_vision_save_capture` | Not migrated | Depends on capture tools. |
| `builtin_weather_get` | Migrated | Dedicated wttr.in JSON wrapper with short cache. |
| `builtin_web_search` | Migrated | Tavily-backed config. |
| `builtin_web_fetch` | Migrated | Tavily extract-backed config. |
| `builtin_pudding_set_alias` | Not migrated | Voice/KWS naming feature; defer. |
| `builtin_pudding_list_aliases` | Not migrated | Voice/KWS naming feature; defer. |
| `builtin_history_search` | Migrated | FTS5, current-session default. |
| `builtin_history_get_message` | Migrated | Session-scoped message lookup. |

## Cross-Cutting Check

Current new builtins have transcript display names and i18n entries:

- `request_capability`
- `builtin_time_get_current`
- `builtin_web_search`
- `builtin_web_fetch`
- `builtin_history_search`
- `builtin_history_get_message`
- `builtin_skill_read`
- `builtin_file_list`
- `builtin_file_read`
- `builtin_file_stat`
- `builtin_file_search`
- `builtin_file_slice`
- `builtin_file_write`
- `builtin_file_patch`
- `builtin_file_delete`
- `builtin_file_move`
- `builtin_file_copy`
- `builtin_skill_validate`
- `builtin_skill_submit`
- `builtin_rest_request`
- `builtin_graphql_request`
- `builtin_graphql_introspect`
- `builtin_graphql_search`
- `builtin_weather_get`

Prompt coverage:

- Chat mode mentions time, weather, web, history, REST/GraphQL endpoints, and capability escalation.
- Workspace mode mentions file search/slice and workspace directory authorization.
- Skill creator skill prompt mentions skill/file authoring tools.

## Recommended Next Migration Order

1. PDF/text extraction as file capability
   - Reason: attachments now exist; PDF needs a way to expose text to LLM when
     provider cannot read PDFs directly.
2. Audio transcript / analysis path
   - Reason: audio attachments currently need provider support or external
     transcription; local fallback is still missing.
3. `builtin_file_mkdir`
   - Reason: useful for workspace authoring, but `builtin_file_write` already
     creates parent directories.
4. Vision capture / speaker / fact / preference / alias tools
   - Reason: require larger subsystem decisions around daemon-owned hardware,
     speaker identity, memory, and privacy controls.

## Open Questions

- Should PDF/audio extraction be exposed as file tools, attachment tools, or
  contextbuilder preprocessing?
- Do we want `builtin_tool_doc_read` back once builtin tool docs exceed the
  inline schema descriptions?
