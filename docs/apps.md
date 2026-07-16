# Apps and Connection Fields

Apps are local packages under `<home>/apps/<id>/`. Each app declares endpoints,
auth methods, skills, and optional connection fields in `app.yaml`.

## MCP Apps

Ordinary MCP servers are managed as simplified Apps with `kind: mcp`; there is
no separate MCP registry or loader. The Apps page imports the standard
`mcpServers` JSON format through **Add MCP App**, and lists built-in, installed,
and MCP Apps together under Installed.

Each MCP App has exactly one MCP endpoint and does not require an App
connection. `stdio` servers require Code mode, while streamable HTTP servers
require Work mode. Environment variables and HTTP headers are stored in the
private MCP override file rather than `app.yaml`. Enabled MCP Apps appear in the
compact Available Apps prompt and are loaded on demand with
`builtin_app_load`; there is no `builtin_mcp_load` tool.

## LLM Authoring

App creation follows the same on-demand pattern as Skill creation:

1. Read the builtin `app-creator` Skill when the user explicitly asks to create
   or update an App.
2. Enter Code mode and load the `code.app` Toolkit.
3. For updates, inspect visible package files through the read-only `app` file
   scope. Hidden connection and MCP override files are never exposed there.
4. Call `builtin_app_save` with a complete UTF-8 package and either `create` or
   `update`.

There is no Draft or publish step. The save tool builds and validates a hidden
candidate directory, then replaces the installed directory. Validation or write
failure leaves the previous installed App unchanged. Creation refuses an
existing id; updates refuse builtin and runtime Apps. Saving is a persistent App
write, so Ask and Auto approval modes request confirmation; Full Access does not.

`builtin_app_save` accepts text files only, so authored icons use SVG. Secrets
and connection values are configured through App Connections and must never be
written into the package.

## Endpoint Kinds

Apps can declare REST, GraphQL, and MCP endpoints under `endpoints`.

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
discovery exposes configured MCP endpoints as model-callable tools after the App
is loaded. The daemon starts `stdio` endpoints on demand and stops their process
when the session App binding is cleared or the daemon exits.

## Connection Fields

Use `connection.fields` for per-connection values that are not auth secrets but
must be attached to most app API calls, such as `hotelCode`, `tenantId`, or
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

- `id`: stable field id. It must be unique in the app.
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

## Skill Guidance

Core prompt assembly does not inline app-specific connection field rules. If an
LLM needs to know how an app-specific field is injected, document it in the
app's skill, for example:

```md
- Connections require `hotelCode`. The app injects it as query parameter
  `hotelCode` for GET/DELETE, JSON body field `hotelCode` for POST/PUT/PATCH,
  and header `X-Hotel-Code`; do not duplicate it unless the user explicitly
  wants to override the value for one call.
```
