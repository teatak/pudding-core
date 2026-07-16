---
name: app-creator
description: Create or update a local Pudding App when the user asks to package an API, GraphQL service, or remote MCP integration as an App.
---

# App Creator

Use this Skill for declarative Pudding Apps. Use a global Skill instead when the
request only needs reusable instructions and does not expose an external
endpoint. Runtime-provided UI Apps such as Canvas are registered by the desktop
runtime and are not created through this flow.

## Storage And Lifecycle

Installed Apps live under the Pudding data directory:

```text
<home>/apps/<app-id>/app.yaml
<home>/apps/<app-id>/assets/icon.svg
<home>/apps/<app-id>/skills/<skill-id>/SKILL.md
```

Dev builds use `~/.pudding-dev`; release builds use `~/.pudding`.

There is no draft, submit, or publish step. `builtin_app_save` validates a
complete package in an isolated directory and replaces the installed App only
after validation succeeds. A failed create or update leaves the previous
installed App unchanged.

## Standard Flow

1. Confirm what service the App represents and which user requests should use it.
2. Choose a lowercase kebab-case App id and a package version such as `0.1.0`.
3. Ensure Code capability and load `code.app`.
4. For an update, also load `code.files-read` and inspect visible package files
   with `scope="app"`. Hidden connection and runtime override files are not
   exposed.
5. Build the complete package: `app.yaml`, an SVG icon, and at least one focused
   App Skill when endpoint-specific guidance is needed.
6. Call `builtin_app_save` with `operation="create"` or `operation="update"`.
   Include every package-managed file that should remain after the save.
   Ask and Auto approval modes require user confirmation because this persists
   an installed App package; Full Access saves directly.
7. If the App requires credentials or connection fields, direct the user to its
   Apps settings. Never put credentials in App files or ask the user to paste
   secrets into the conversation.
8. When configuration is available, load the App with `builtin_app_load` and run
   a small read-only smoke test.

Do not write directly into the Apps directory. Direct writes can leave an
invalid partial App that prevents the catalog from loading.

## Package Shape

`app.yaml` defines identity, presentation, endpoints, authentication, connection
fields, and Skill paths:

```yaml
id: example-service
name: Example Service
version: 0.1.0
description: Read and update Example Service records.
icon:
  svg: assets/icon.svg
  color:
    light: "#0F766E"
    dark: "#5EEAD4"
  background:
    light: "#ECFDF5"
    dark: "#12322F"
auth:
  required: true
  methods:
    - id: bearer
      type: bearer
      label: API token
      default: true
endpoints:
  example_rest:
    kind: rest
    url: https://api.example.com
skills:
  - skills/example/SKILL.md
```

Supported endpoint kinds are:

- `rest` with an HTTP or HTTPS base `url`.
- `graphql` with an HTTP or HTTPS endpoint `url`.
- `mcp` with `transport: streamable_http` and an HTTP or HTTPS `url`.
- `mcp` with `transport: stdio`, `command`, optional `args`, and optional `env`.

The daemon starts stdio MCP endpoints on demand when the App is loaded. Keep the
command and arguments portable where possible; use platform overrides when an
executable path or invocation differs by operating system.

Endpoint names use lowercase letters, numbers, and underscores and start with a
letter. Prefer names scoped to the App, such as `github_rest` rather than
`default`.

## Connections

Authentication methods describe what the connection UI should collect. Actual
tokens, passwords, and OAuth credentials belong to connection storage, never
`app.yaml`.

Use `connection.fields` for tenant ids, database names, hotel codes, or other
per-connection values:

```yaml
connection:
  fields:
    - id: tenantId
      label: Tenant ID
      required: true
      inject:
        - target: header
          name: X-Tenant-ID
```

Mark a field `secret: true` when its value should be hidden in the connection
form. Supported injection targets are `query`, `body`, `header`, and `env`.

## App Skills

App Skills explain how and when to use that App's endpoints. Their frontmatter
uses the same shape as global Skills:

```markdown
---
name: example-records
description: Read and update Example Service records when the user asks about them.
---

# Example Records

- Use endpoint `example_rest`.
- Read before writing when an update depends on current state.
- Explain destructive requests before executing them.
```

REST Apps expose `builtin_rest_request`. GraphQL Apps expose
`builtin_graphql_request`, `builtin_graphql_introspect`, and
`builtin_graphql_search`. Remote MCP tools are discovered after the App has a
configured connection and is loaded.

## Icons

Create `assets/icon.svg` by default. Use a square viewBox, simple geometry,
strong contrast, and no scripts, remote URLs, fonts, or embedded credentials.

## Save Rules

- `operation="create"` refuses to replace an existing App.
- `operation="update"` requires an installed, non-builtin App.
- `app_id` must match `id` in `app.yaml`.
- `version` must match `version` in `app.yaml` when the manifest declares it.
- `files` is the complete package-managed file set, not a patch.
- Only UTF-8 text files are accepted by the authoring tool; use SVG for icons.
- Never include `.pudding-app-lock.json` or `.pudding-mcp-overrides.yaml`.
- Do not create README, changelog, or installation notes unless they are needed
  by an App Skill at runtime.
