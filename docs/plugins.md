# Plugins and Connection Fields

Plugins are local packages under `<home>/plugins/<id>/`. Each plugin declares endpoints,
auth methods, skills, and optional connection fields in `plugin.yaml`.

Installs created before plugins were renamed from Apps kept packages in
`<home>/apps/<id>/` with `app.yaml` and `.pudding-app-lock.json`, connections in
`config/app-connections.yaml` (`app:` per connection), and enablement under the
`apps` key of `config/settings.yaml`. The daemon moves that layout once at start;
each step is idempotent, so an interrupted move finishes on the next start. A name
present in both `apps` and `plugins` is left in place and logged.

Loading a plugin requests its missing capability through the existing approval flow and
continues the same load after approval. Turn-only and session approvals retain their
normal scopes; denial or cancellation preserves the existing loaded state and account binding.

## MCP Plugins

Ordinary MCP servers are managed as simplified plugins with `kind: mcp`; there is
no separate MCP registry or loader. The Plugins page imports the standard
`mcpServers` JSON format through **Add MCP Plugin**, and lists built-in, installed,
and MCP plugins together under Installed.

Each MCP plugin has exactly one MCP endpoint and does not require a plugin
connection. `stdio` servers require Code mode, while streamable HTTP servers
require Work mode. Environment variables and HTTP headers are stored in the
private MCP override file rather than `plugin.yaml`. Enabled MCP plugins appear in the
compact Available Plugins prompt and are loaded on demand with
`builtin_plugin_load`; there is no `builtin_mcp_load` tool.

## LLM Authoring

Plugin creation is provided by the built-in Plugin Authoring plugin:

1. Enter Code mode and call `builtin_plugin_load(plugin_id="plugin-authoring")` when the
   user explicitly asks to create or update a plugin.
2. The load returns the bundled `plugin-creator` Skill and exposes
   `builtin_plugin_save` for the session.
3. For updates, inspect visible package files through the read-only `plugin` file
   scope. Hidden connection and MCP override files are never exposed there.
4. Call `builtin_plugin_save` with a complete UTF-8 package and either `create` or
   `update`.

There is no Draft or publish step. The save tool builds and validates a hidden
candidate directory, then replaces the installed directory. Validation or write
failure leaves the previous installed plugin unchanged. Creation refuses an
existing id; updates refuse builtin and runtime plugins. Saving is a persistent plugin
write, so Ask and Auto approval modes request confirmation; Full Access does not.

`builtin_plugin_save` accepts text files only, so authored icons use SVG. Secrets
and connection values are configured through plugin Connections and must never be
written into the package.

## Endpoint Kinds

Plugins can declare REST, GraphQL, and MCP endpoints under `endpoints`.

```yaml
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
  github_graphql:
    kind: graphql
    url: https://api.github.com/graphql
  github_mcp:
    kind: mcp
    transport: streamable_http
    url: https://example.com/mcp
```

For MCP endpoints, use `transport: streamable_http` with `url`, or
`transport: stdio` with `command` and optional `args` / `env`. Runtime tool
discovery exposes only resolved MCP endpoints as model-callable tools after the plugin
is loaded. The daemon starts `stdio` endpoints on demand and stops their process
when the session plugin binding is cleared or the daemon exits.

## Connection selection

`connection.selection` declares the plugin's connection policy. Omission means
`user`: reuse the chat's saved binding; without one, automatically use the sole
usable connection. Multiple usable connections require an explicit account choice.
The model selects an account matching the user's intent through
`builtin_plugin_load(plugin_id, connection_id)`, asking in chat if ambiguous.
`single_active` allows multiple saved connections but at most one active connection
shared by all tools. `automatic` is reserved and rejected by manifest validation;
the sole-connection behavior is part of `user`, not a separate manifest mode.

```yaml
connection:
  selection: single_active
```

`GET/PATCH /plugins/:id/connection-bindings` reads or changes the global selection
for `single_active` plugins: `{ "bindings": { "active": "connection-id" } }`.
An empty ID clears the selection. Standard `/plugin-connections` CRUD only changes
credentials. Saving, selecting, and enabling/disabling a plugin are independent.
Both policies support configuration-only connections (`authType: none`) when
authentication is optional, including plugins without an `auth` declaration.

For `user` plugins, `builtin_plugin_load` with `connection_id` sets the chat's
binding, including for an already-loaded plugin. The tool returns redacted account
candidates so the model can select an account matching the user's intent, asking
in chat if ambiguous. The chat header and tool results have no manual connection
picker. The authenticated `GET/PUT /sessions/:id/plugins/:pluginID/connection` API
still exposes the binding. A REST/GraphQL `connection` argument only checks the
resolved account; use the load tool to change it.

REST, GraphQL and MCP share the same binding-or-sole-connection resolution.
Automatic use does not write a binding; if another usable connection is added, an
unbound chat must choose an account. Cached MCP calls revalidate the current binding
and credentials before execution. Another session, including a cloned or child
session, does not inherit the selection. The SQLite binding survives restarts and
is removed with its session. Missing/deleted/recreated connections and changed
account identities require a new selection, with no automatic fallback.
User-bound widgets retain their own explicit resource bindings.

Web Search & Fetch declares `single_active`, keeps Chat availability, and migrates
the previous release's single provider record into standard Connections. The
legacy `web` plugin ID and `/tools/web` API remain compatible. The latter projects
both features onto the one active connection: conflicting simultaneous provider
selections are rejected atomically; saving keys alone never selects a connection.

## Gmail OAuth

Gmail requests `gmail.readonly` and `gmail.send`, in addition to the Google account
identity scopes. The Gmail plugin can read mail and send new messages or replies
through `POST /users/me/messages/send`; these scopes do not grant draft editing or
mailbox modification permissions.

To upgrade an existing read-only connection, run a Core build with the sending
scope, install Gmail plugin `1.0.0-preview.2` or later, and authorize Gmail again
from Plugins → Gmail → Connections. The sole usable connection is used
automatically; for multiple accounts the model selects the intended connection.
Updating Google Cloud's consent configuration alone does not add permissions to
existing tokens. A `connection_required` error concerns
the chat binding and does not establish whether the token has sending permission.

## Connection Fields

Use `connection.fields` for per-connection values that are not auth secrets but
must be attached to most plugin API calls, such as `hotelCode`, `tenantId`, or
environment codes.

Pudding shows these fields in the connection dialog, stores the values with the
connection, and makes them available to endpoint calls. Connection field values
are returned by the connection detail API, not by the connection list API.

```yaml
connection:
  fields:
    - id: hotelCode
      label: 酒店代码
      required: true
      inject:
        - target: query
          name: hotelCode
          methods: [GET, DELETE]
        - target: body
          name: hotelCode
          methods: [POST, PUT, PATCH]
        - target: header
          name: X-Hotel-Code
```

Field properties:

- `id`: stable field id. It must be unique in the plugin.
- `label`: display label in the connection dialog.
- `description`: optional helper text.
- `placeholder`: optional input placeholder.
- `required`: rejects saving the connection when empty.
- `secret`: stores the field as a connection value and hides it in the input.
- `inject`: optional list of request injection rules.

Injection rule properties:

- `target`: `query`, `body`, or `header`.
- `name`: request key/header name. Defaults to the field `id`.
- `methods`: optional HTTP method allowlist. If omitted, the rule applies to all
  REST methods.

Injection behavior:

- `query` adds the value to request query parameters.
- `body` adds the value to `body_json`; it requires `body_json` to be an object
  and cannot be used with `body_text`.
- `header` adds the value to request headers.
- Pudding does not overwrite explicit query/body/header values already provided
  by the tool call.
- Forbidden hop-by-hop headers such as `Host` and `Content-Length` are rejected.

## Token Exchange Authentication

Use `token_exchange` when an API requires connection credentials to be
exchanged for a short-lived access token. The body maps request keys to
`connection.fields` ids. Pudding performs the exchange at request time, caches
the returned token until shortly before expiry, and sends it as endpoint auth.

```yaml
auth:
  required: true
  methods:
    - id: app-credentials
      type: token_exchange
      token_exchange:
        url: https://example.com/oauth/token
        body_fields:
          client_id: clientId
          client_secret: clientSecret
        access_token_field: access_token
        expires_in_field: expires_in
        token_type: Bearer
connection:
  fields:
    - id: clientId
      required: true
    - id: clientSecret
      required: true
      secret: true
```

Token exchange currently applies to REST and GraphQL endpoints. Access token
and expiry fields may use dotted JSON paths. Connection credentials and token
responses are never exposed as model-callable arguments or tool output.

## Connection Endpoint URLs

REST and GraphQL endpoints may opt into a connection-specific base URL with
`url_config`. Endpoints that do not declare `url_config` cannot be overridden
and do not show an address field in the connection form.

```yaml
endpoints:
  grafana_rest:
    kind: rest
    url: http://localhost:3000
    url_config:
      label: Grafana address
      description: Root URL of the Grafana instance.
      placeholder: https://grafana.example.com
      required: true
```

The connection URL takes precedence over the endpoint URL declared in
`plugin.yaml`. An optional URL config may be left empty to keep the plugin default;
`required: true` requires every connection to provide an address. Overrides
are keyed by endpoint name, so one connection can customize multiple declared
REST or GraphQL endpoints and the same plugin can connect to different self-hosted
instances.

Only `http` and `https` URLs without userinfo, query parameters, or fragments
are accepted. Authentication and connection field injection remain unchanged.
MCP endpoints continue to use the plugin-level private MCP override configuration.

## Skill Guidance

Core prompt assembly does not inline plugin-specific connection field rules. If an
LLM needs to know how a plugin-specific field is injected, document it in the
plugin's skill, for example:

```md
- Connections require `hotelCode`. The plugin injects it as query parameter
  `hotelCode` for GET/DELETE, JSON body field `hotelCode` for POST/PUT/PATCH,
  and header `X-Hotel-Code`; do not duplicate it unless the user explicitly
  wants to override the value for one call.
```

## Third-party catalogs

Protocol 27 adds `GET/POST /plugins/sources` and `DELETE /plugins/sources/:sourceID`. A source has `id`, `url`, and `official`; POST accepts `{ "url": "https://example.com/plugins/registry.json" }`. HTTPS URLs without credentials or fragments are normalized, deduplicated and persisted in `config/plugin-sources.yaml` (up to 32 third-party sources). The official source cannot be removed. Removing a source leaves installed plugin files, enablement and connections untouched. Desktop validates and fetches each catalog independently.

`POST /plugins/install` accepts optional `pluginID` and `pluginVersion` assertions alongside `packageJSON`, `packageSHA256`, and `sourceURL`; mismatches fail before writing. Package locks preserve source identity across restarts. An existing plugin cannot be replaced by an installation from a different nonempty source (`409 plugin_source_conflict`), including a catalog trying to adopt a local/MCP plugin. Authored package edits keep their existing workflow. Catalog and update selection must use both local plugin ID and source URL.

## Gmail token refresh

Gmail connections refresh access tokens within two minutes of expiry through
`https://teatak.com/gmail/refresh`, using the same Google client credentials
as the authorization exchange. Concurrent calls share the refreshed credentials;
new credentials are saved for subsequent turns and restarts. Google may omit a
replacement refresh token, token type, scopes or refresh expiry; these stored
fields are retained when omitted. Reauthorization of the same Google account
also retains its refresh token when the exchange omits one.

Only a missing/expired refresh token or Google's `invalid_grant` response
requires reauthorization. Network errors, rate limits, server errors, malformed
responses and client configuration errors leave credentials and account bindings
intact and are reported as refresh failures. A Gmail API 401 alone does not prove
that the refresh token is revoked. Deploy the OAuth worker's `/gmail/refresh`
route before distributing this Core change.

## Interactive session login

Protocol 52 adds `auth.methods[].type: session` for non-OAuth services that return
session credentials from a form or JSON login. This is a declarative HTTP flow,
not executable plugin code. `token_exchange` remains the automatic, single-token
exchange used at request time.

```yaml
auth:
  required: true
  methods:
    - id: account-login
      type: session
      session:
        endpoint: private_api
        path: /login
        encoding: form
        inputs:
          - {id: account, label: Account, required: true}
          - {id: password, label: Password, required: true, secret: true}
          - {id: otp, label: Verification code, secret: true}
        parameters: {action: login}
        body_fields: {username: account, password: password, otp: otp}
        success_field: success
        error_code_field: error.code
        challenges: {"otp_required": [otp]}
        outputs: {sid: data.sid}
        account_input: account
connection:
  fields:
    - id: sid
      required: true
      secret: true
      inject: [{target: header, name: X-Session-ID}]
endpoints:
  private_api:
    kind: rest
    url: https://server.example/api
    url_config: {label: Server URL, required: true}
```

`POST /plugin-connections/:id/login` accepts `{pluginID, name, authMethodID,
endpointURLs, inputs}`. It returns `{status: "success", connection}`, or
`{status: "challenge", code, inputs: [fieldID]}`, or `{status: "error", code}`.
These are settings operations, not model tools. Login inputs are never written
to connection storage. A challenge identifies declared inputs for the desktop
form; the next attempt explicitly submits them with the original login inputs.
It does not create a connection or retain an unfinished server-side login.

Core posts to the selected endpoint plus the declared relative path, with a
15-second timeout, a 64 KiB response limit and no redirects. Use HTTPS to protect
passwords in transit; TLS verification is enabled by default. Inputs preserve exact
values, including password whitespace. Raw provider errors and credentials are
not returned to the desktop. A configured `success_field` must be JSON `true`;
`outputs` map response JSON paths to declared secret connection fields, with at
least one required credential. Optional omitted outputs replace, rather than
inherit, values from the previous account. `expires_in_field`, when supplied,
must be a positive number of seconds (at most one year).

Core saves the validated fields, account and expiry in one existing atomic
connection write, checking the prior revision while holding the storage lock.
A failed login leaves the old connection untouched; concurrent changes yield
HTTP 409. Session connection detail responses omit managed credential fields.
Saving a name change does not log in again; changing the endpoint requires login.
Local credential storage follows the existing restricted-permission connection
file; `secret` does not promise encryption at rest.

The initial implementation does not retain passwords or trusted-device tokens
for automatic re-login, run scripts, or replay failed business requests. An
expired session requires an explicit sign-in in settings. Cookie-based flows,
multi-request challenges and per-request signatures are not part of this
contract. Synology uses this flow with SID and optional SynoToken outputs;
its protocol details stay in the plugin manifest, outside Core.


## Connection endpoint TLS settings

Protocol 53 adds `endpointInsecureTLS?: Record<string, boolean>` to connection
write, login and OAuth-start requests, and connection detail responses. The
connection file stores it as `endpoint_insecure_tls`. The desktop shows an
**Ignore server certificate errors** switch below each configurable HTTPS
endpoint, off by default. Enabling it skips both certificate-chain and hostname
verification for that connection's endpoint; it does not merely add a private CA.

Only configurable REST and GraphQL endpoints accept this setting. Unknown,
fixed or non-HTTPS endpoints cannot opt in. Omitted settings preserve an existing
exception only while the effective server URL is unchanged; an empty object
explicitly clears all exceptions. Changing the address in the desktop clears its
switch, including when changing to HTTP. Changing a session connection's TLS
policy requires signing in again, validating the chosen policy before saving.
Failed sign-in preserves the old connection and settings.

Login, assistant REST/GraphQL calls, GraphQL schema discovery and Core-side
widget requests share the same scoped HTTP client helper. Strict and insecure
transports keep separate pools, and neither changes the global transport or
follows redirects. OAuth provider calls and token-exchange URLs do not inherit
an endpoint's exception. TLS settings are supplied by saved connection bindings,
not tool or widget arguments. Trust-policy changes invalidate schema caches and
saved connection grants. The setting survives restart and never applies to a
browser navigation or a NAS link opened outside Core.
