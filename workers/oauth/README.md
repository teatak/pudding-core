# Pudding Website and OAuth Worker

Cloudflare Worker for the public Pudding website and OAuth authorization-code exchange.

Public pages:

```text
GET /
GET /privacy
GET /terms
GET /support
GET /data-deletion
```

The desktop app should:

1. Open the provider authorization URL with a desktop redirect URI.
2. Receive `code` through the local daemon callback, for example `http://localhost:9669/oauth/callback/github`.
3. The local daemon posts the code to `https://oauth.x-t.top/<provider>/exchange`.
4. Store the returned provider token locally.

Example:

```json
{
  "code": "...",
  "redirect_uri": "http://localhost:9669/oauth/callback/github"
}
```

## Built-in Providers

GitHub and Gmail are built in:

```text
POST /github/exchange
POST /gmail/exchange
```

Required Cloudflare secrets:

```bash
npx wrangler secret put GITHUB_CLIENT_SECRET
npx wrangler secret put GOOGLE_CLIENT_SECRET
```

OAuth callback URLs must include:

```text
http://localhost:9669/oauth/callback/github
http://localhost:9669/oauth/callback/gmail
```

## Setup

```bash
cd workers/oauth
pnpm install
npx wrangler deploy
```

## Add More Providers

Add provider config through `OAUTH_PROVIDERS`:

```json
{
  "slack": {
    "token_url": "https://slack.com/api/oauth.v2.access",
    "client_id": "...",
    "client_secret_env": "SLACK_CLIENT_SECRET",
    "allowed_redirect_uris": [
      "http://127.0.0.1:9679/oauth/callback/slack"
    ]
  }
}
```

Then set the corresponding secrets:

```bash
npx wrangler secret put OAUTH_PROVIDERS
npx wrangler secret put SLACK_CLIENT_SECRET
npx wrangler deploy
```

If you do not want to put a public client id in `OAUTH_PROVIDERS`, use
`client_id_env` and store that value as a Worker secret instead.

The route becomes:

```text
POST /slack/exchange
```

Optional CORS allowlist:

```bash
npx wrangler secret put ALLOWED_ORIGINS
```

Use comma-separated origins. The desktop daemon does not need CORS.

## Custom Domains

This Worker is bound to the public website and the OAuth API domains:

```text
https://x-t.top
https://oauth.x-t.top
```

Public website requests use `x-t.top`. Public `GET` requests on
`oauth.x-t.top` redirect to the website; token exchange endpoints remain on
`oauth.x-t.top`.

`wrangler.toml`:

```toml
[[routes]]
pattern = "x-t.top"
custom_domain = true

[[routes]]
pattern = "oauth.x-t.top"
custom_domain = true
```
