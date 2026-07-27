# Pudding Website and OAuth Worker

Cloudflare Worker for the public Pudding website and OAuth handoff services.

Public pages:

```text
GET /
GET /privacy
GET /terms
GET /support
GET /data-deletion
```

## Unified Pudding OAuth

The current Pudding GitHub App uses the public `x-t.top` callback:

1. A client posts `provider`, `client`, `client_state`, `flow`, and a SHA-256
   challenge to `POST https://x-t.top/oauth/start`.
2. The Worker creates a short-lived Durable Object transaction and returns the
   GitHub App installation URL for `flow: "install"` or the user
   authorization URL for `flow: "authorize"`.
3. GitHub redirects to
   `GET https://x-t.top/oauth/callback/github`. With GitHub's
   "Request user authorization during installation" option enabled, an initial
   installation continues into OAuth without a second Pudding action.
4. The Worker validates state, exchanges the code for a user token, verifies
   that Pudding Connector has at least one installation accessible to that
   token, and stores the response only for the five-minute device handoff
   window.
5. The result page opens either `pudding://` or `pudding-mobile://` with an
   opaque single-use ticket.
6. The client posts that ticket and its verifier to
   `POST https://x-t.top/oauth/redeem`. Successful redemption deletes the
   transaction.

Expiring GitHub App user tokens are refreshed through
`POST https://x-t.top/oauth/refresh`. Disconnecting a Connection revokes its
single GitHub App user token through `POST https://x-t.top/oauth/revoke`
before the device removes the local secret.

Required configuration:

```text
PUDDING_GITHUB_CLIENT_ID
PUDDING_GITHUB_CLIENT_SECRET
PUDDING_GITHUB_APP_ID
PUDDING_GITHUB_APP_SLUG
```

The client secret must be configured as a Wrangler secret.

## Legacy desktop exchange

The existing desktop flow remains available on `oauth.x-t.top`:

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
npx wrangler secret put PUDDING_GITHUB_CLIENT_SECRET
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
