# Plugin runtime implementation

Plugin authoring, endpoint/authentication configuration, connections, Skills and Hub packaging are maintained in the **[Hub plugin guide](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md)** / **[中文](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.zh-CN.md)**. The [Hub developer index](https://github.com/teatak/pudding-hub/blob/main/docs/README.md) also covers widget integration and runtime capabilities. This file retains Core storage, API and compatibility details rather than another authoring guide.

Installed packages live under `<home>/plugins/<id>/`, with `plugin.yaml` and package-managed Skill/assets. At startup, Core migrates the old Apps layout (`apps/`, `app.yaml`, app locks, connection config and settings keys) once. Steps are idempotent; an interrupted migration resumes, and a name collision is left in place and logged. New packages use plugins only. Built-in identity aliases have a separate [retirement record](plugin-ids.md).

Loading uses the existing capability approval flow and continues the same load after approval. Turn/session approvals retain their scope; denial preserves loaded state and account binding. Runtime/built-in plugins remain distinct from installed declarative packages; see [built-in design](builtin-plugins-design.md).

## MCP Plugins

Public configuration: [Hub endpoint types](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#endpoint-types).

Simplified `kind: mcp` definitions and private overrides are handled by [MCP configuration](../internal/plugin/mcp_plugin_config.go) and [overrides](../internal/plugin/mcp_overrides.go). They use the same on-demand plugin loader. Resolved stdio processes stop when their session binding is cleared or the daemon exits; no separate MCP registry/loader exists.

## LLM Authoring

Public workflow: [Hub Skills and model tools](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#skills-and-model-tools).

The bundled [Plugin Creator Skill](../internal/plugin/embed/plugin-authoring/SKILL.md) stays self-contained for offline runtime use. Complete text packages are validated in hidden candidate directories before replacement. Failure leaves the previous package intact. Plugin file scope exposes package content, not connection or private MCP files. Local installed-package writes and Hub release publishing are separate workflows.

## Endpoint Kinds

The exact runtime fields are owned by [types](../internal/plugin/types.go), with validation in the [loader](../internal/plugin/loader.go). Author-facing REST, GraphQL, MCP, `url_config` and `widget_writes` examples live in [Hub](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#endpoint-types).

## Connection selection

Public policy and account switching: [Hub connections](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#connections-and-account-selection).

`GET/PATCH /plugins/:id/connection-bindings` manages the global active selection for `single_active`: `{ "bindings": { "active": "connection-id" } }`; an empty ID clears it. `/plugin-connections` CRUD only saves credentials/configuration. `GET/PUT /sessions/:id/plugins/:pluginID/connection` manages a user-policy session binding. Saving, binding and enablement are independent.

REST, GraphQL and MCP resolve the same current binding or sole usable connection. Automatic sole resolution does not persist a binding. Cached MCP calls revalidate identity/credentials before execution. Session bindings are SQLite-owned, survive restart, do not propagate to cloned/child sessions, and are deleted with the session. Missing/deleted/recreated identities require a new choice without fallback. Widgets maintain their own explicit resource bindings.

The Web Search & Fetch `single_active` plugin migrates the previous single-provider record into standard Connections. The legacy `web` ID and `/tools/web` projection remain compatible; contradictory provider selections fail atomically, and saving a key alone never changes the active selection.

## Gmail OAuth

Public setup and scope semantics: [Hub authentication](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#authentication) and [Gmail Skill](https://github.com/teatak/pudding-hub/blob/main/plugins/gmail/skills/mailbox/SKILL.md).

Core requests Gmail read/send and account identity scopes. Existing read-only tokens require reauthorization with a build requesting the sending scope and an appropriate plugin package; changing the Cloud consent configuration alone does not modify tokens. `connection_required` describes binding, not proof that a token has a sending scope.

## Connection Fields

Field definitions and injection examples are maintained in [Hub](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#connection-fields-and-injection). Connection detail APIs include configured values while list APIs do not. Managed session credentials are redacted; the model must not read private connection configuration through package file scope.

## Token Exchange Authentication

The declarative `token_exchange` schema and example are maintained in [Hub](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#token-exchange). Runtime requests cache the short-lived token until shortly before expiry. Credentials and raw token responses remain host-managed, outside model-callable arguments/results.

## Connection Endpoint URLs

Author-facing `url_config` and TLS behavior: [Hub](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#per-connection-addresses-and-tls). Resolution and endpoint-keyed overrides are implemented in [connection endpoint URLs](../internal/plugin/connection_endpoint_urls.go).

## Skill Guidance

Current authoring examples live in [Hub](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#skills-and-model-tools). Core does not automatically inline plugin-specific field injection rules into the system prompt; the plugin Skill carries those service-specific instructions. Bundled runtime Skills remain with Core.

## Third-party catalogs

Protocol 27 adds `GET/POST /plugins/sources` and `DELETE /plugins/sources/:sourceID`. POST accepts `{ "url": "https://example.com/plugins/registry.json" }`. HTTPS URLs without credentials/fragments are normalized, deduplicated and persisted in `config/plugin-sources.yaml`, with at most 32 third-party sources. The official source cannot be removed; removing a source preserves installed packages, enablement and connections.

`POST /plugins/install` accepts `packageJSON`, `packageSHA256`, `sourceURL` and optional `pluginID`/`pluginVersion` assertions. Mismatches fail before writing. Locks preserve source identity across restarts. Replacing an installed plugin from another nonempty source fails with `409 plugin_source_conflict`; authored edits retain their own workflow. Catalog/update selection uses local plugin ID together with source URL.

## Gmail token refresh

Access tokens refresh within two minutes of expiry through `https://teatak.com/gmail/refresh`, using the authorization exchange's client credentials. Concurrent calls share the refreshed result, which is persisted. Omitted replacement refresh tokens, token type, scopes or refresh expiry retain their old values; reauthorization of the same Google identity follows the same rule.

Only missing/expired refresh tokens or Google's `invalid_grant` require reauthorization. Network/rate-limit/server/malformed-response/client-configuration failures preserve credentials and bindings and return refresh failure. An API 401 alone is not proof of a revoked refresh token. Deploy the worker refresh route before releasing the Core change.

## Interactive session login

Manifest configuration is maintained in [Hub](https://github.com/teatak/pudding-hub/blob/main/docs/plugin-development.md#interactive-session-login). Protocol 52 adds `POST /plugin-connections/:id/login`, accepting `{pluginID,name,authMethodID,endpointURLs,inputs}`. Results are success with connection, challenge with code/input IDs, or a structured error code. This is a settings operation, not a model tool.

The HTTP login is bounded to 15 seconds and 64 KiB without redirects. The host preserves exact input, does not persist password inputs or unfinished challenges, and does not return raw provider errors. Successful credentials/account/expiry are saved atomically under the connection lock; concurrent revision changes return 409, failures preserve the old connection. Optional omitted outputs do not inherit another account's credentials. A name-only edit does not log in again; endpoint changes require login. Cookie sessions, automatic re-login and executable login scripts are not supported.

## Connection endpoint TLS settings

Protocol 53 adds `endpointInsecureTLS?: Record<string, boolean>` to connection write, login and OAuth-start requests and detail responses; configuration stores `endpoint_insecure_tls`. Only configurable HTTPS REST/GraphQL endpoints may opt in. Omission preserves an exception only while the effective URL remains unchanged; `{}` clears it. A default-address exception stores that effective address explicitly so a plugin upgrade cannot redirect it to another server.

Login, REST/GraphQL, schema discovery and Core widget requests share a scoped client helper. Strict/insecure transports have separate pools, do not change global TLS behavior and do not follow redirects. OAuth/token-exchange URLs and browser navigation do not inherit exceptions. URL/trust-policy changes invalidate schema caches and saved connection grants. Managed credentials are redacted from textual responses, URLs, echoed headers/JSON and network errors; outgoing requests still receive their actual credentials.
