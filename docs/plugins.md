# Plugins and Connection Fields

Plugins are local packages under `<home>/plugins/<id>/`. Each plugin declares endpoints,
auth methods, skills, and optional connection fields in `plugin.yaml`.

Installs created before plugins were renamed from Apps kept packages in
`<home>/apps/<id>/` with `app.yaml` and `.pudding-app-lock.json`, connections in
`config/app-connections.yaml` (`app:` per connection), and enablement under the
`apps` key of `config/settings.yaml`. The daemon moves that layout once at start;
each step is idempotent, so an interrupted move finishes on the next start. A name
present in both `apps` and `plugins` is left in place and logged.

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
discovery exposes configured MCP endpoints as model-callable tools after the plugin
is loaded. The daemon starts `stdio` endpoints on demand and stops their process
when the session plugin binding is cleared or the daemon exits.

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
