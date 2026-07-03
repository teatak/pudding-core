# Apps and Connection Fields

Apps are local packages under `<home>/apps/<id>/`. Each app declares endpoints,
auth methods, skills, and optional connection fields in `app.yaml`.

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

