import type { Env } from "../env"
import { json, readString } from "../http"
import { randomToken } from "./crypto"
import {
  puddingOAuthProvider,
  refreshProviderToken,
  revokeProviderToken,
} from "./providers"
import {
  CLIENT_SCHEMES,
  isOAuthClient,
  isOAuthFlow,
  isOpaqueToken,
  parseRedemptionTicket,
  START_TTL_MS,
  type OAuthTransactionRecord,
} from "./protocol"

export async function routePuddingOAuth(
  request: Request,
  url: URL,
  env: Env,
  corsHeaders: Record<string, string>,
): Promise<Response | null> {
  if (!isPuddingOAuthHost(url.hostname)) return null

  if (request.method === "POST" && url.pathname === "/oauth/start") {
    return startOAuth(request, env, corsHeaders)
  }
  if (
    request.method === "GET" &&
    url.pathname.startsWith("/oauth/providers/")
  ) {
    if (url.pathname.endsWith("/install")) {
      return providerInstallationRedirect(url, env)
    }
    return providerDetails(url, env, corsHeaders)
  }
  if (
    request.method === "GET" &&
    url.pathname.startsWith("/oauth/callback/")
  ) {
    return finishOAuthCallback(request, url, env)
  }
  if (request.method === "POST" && url.pathname === "/oauth/redeem") {
    return redeemOAuth(request, env, corsHeaders)
  }
  if (request.method === "POST" && url.pathname === "/oauth/refresh") {
    return refreshOAuth(request, env, corsHeaders)
  }
  if (request.method === "POST" && url.pathname === "/oauth/revoke") {
    return revokeOAuth(request, env, corsHeaders)
  }
  return null
}

function providerInstallationRedirect(url: URL, env: Env): Response {
  const providerID = decodeURIComponent(
    url.pathname
      .slice("/oauth/providers/".length)
      .replace(/\/install$/, ""),
  )
  const provider = puddingOAuthProvider(providerID, env)
  if (!provider) {
    return Response.redirect("https://x-t.top/", 302)
  }
  return Response.redirect(provider.installURL, 302)
}

function providerDetails(
  url: URL,
  env: Env,
  corsHeaders: Record<string, string>,
): Response {
  const providerID = decodeURIComponent(
    url.pathname.slice("/oauth/providers/".length),
  )
  const provider = puddingOAuthProvider(providerID, env)
  if (!provider) {
    return json({ error: "provider_not_configured" }, 404, corsHeaders)
  }
  return json(
    {
      installation_url: provider.installURL,
      provider: provider.id,
    },
    200,
    corsHeaders,
  )
}

async function startOAuth(
  request: Request,
  env: Env,
  corsHeaders: Record<string, string>,
): Promise<Response> {
  let body: Record<string, unknown>
  try {
    body = (await request.json()) as Record<string, unknown>
  } catch {
    return json({ error: "invalid_json" }, 400, corsHeaders)
  }

  const providerID = readString(body.provider)
  const client = readString(body.client)
  const clientState = readString(body.client_state)
  const challenge = readString(body.challenge)
  const flow = readString(body.flow) || "authorize"
  if (
    !isOAuthClient(client) ||
    !isOAuthFlow(flow) ||
    !isOpaqueToken(clientState) ||
    !isOpaqueToken(challenge)
  ) {
    return json({ error: "invalid_start_request" }, 400, corsHeaders)
  }

  const provider = puddingOAuthProvider(providerID, env)
  if (!provider) {
    return json({ error: "provider_not_configured" }, 503, corsHeaders)
  }

  const transactionID = randomToken()
  const createdAt = Date.now()
  const record: OAuthTransactionRecord = {
    challenge,
    client,
    clientState,
    createdAt,
    expiresAt: createdAt + START_TTL_MS,
    flow,
    provider: provider.id,
    status: "pending",
    transactionID,
  }
  const response = await transactionStub(env, transactionID).fetch(
    "https://oauth.internal/init",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(record),
    },
  )
  if (!response.ok) {
    return json({ error: "transaction_create_failed" }, 502, corsHeaders)
  }

  // Authorize first for both flows. Existing installations can then complete
  // immediately; first-time users are redirected to installation only after
  // the callback confirms that no accessible installation exists.
  const authorizationURL = new URL(provider.authorizationURL)
  authorizationURL.searchParams.set("client_id", provider.clientID)
  authorizationURL.searchParams.set("redirect_uri", provider.callbackURL)
  authorizationURL.searchParams.set("state", transactionID)
  return json(
    {
      app_id: provider.appID,
      authorization_url: authorizationURL.toString(),
      expires_at: new Date(record.expiresAt).toISOString(),
      provider: provider.id,
    },
    201,
    corsHeaders,
  )
}

async function finishOAuthCallback(
  _request: Request,
  url: URL,
  env: Env,
): Promise<Response> {
  const providerID = url.pathname.slice("/oauth/callback/".length)
  const transactionID = url.searchParams.get("state")?.trim() ?? ""
  if (!isOpaqueToken(transactionID)) {
    return oauthResultPage({
      title: "Authorization expired",
      detail: "Return to Pudding and start the connection again.",
    })
  }

  const provider = puddingOAuthProvider(providerID, env)
  if (!provider) {
    return oauthResultPage({
      title: "Authorization unavailable",
      detail: "This connection provider is not configured.",
    })
  }

  const providerError = url.searchParams.get("error")?.trim()
  if (providerError) {
    const response = await transactionStub(env, transactionID).fetch(
      "https://oauth.internal/fail",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ provider: provider.id }),
      },
    )
    const handoff = await responseJSON(response)
    if (response.ok && handoff) {
      return oauthResultPage({
        title: "Authorization cancelled",
        detail:
          url.searchParams.get("error_description")?.trim() ||
          "GitHub did not authorize this connection.",
        openURL: clientHandoffURL(handoff, {
          error: providerError,
        }),
      })
    }
    return oauthResultPage({
      title: "Authorization cancelled",
      detail: "Return to Pudding and try again.",
    })
  }

  const code = url.searchParams.get("code")?.trim() ?? ""
  if (!code) {
    return oauthResultPage({
      title: "Authorization failed",
      detail: "GitHub did not return an authorization code.",
    })
  }

  const response = await transactionStub(env, transactionID).fetch(
    "https://oauth.internal/callback",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        code,
        installation_id: url.searchParams.get("installation_id")?.trim(),
        provider: provider.id,
      }),
    },
  )
  const handoff = await responseJSON(response)
  if (
    response.status === 409 &&
    readString(handoff?.error) === "installation_required"
  ) {
    const installationURL = readString(handoff?.installation_url)
    if (installationURL.startsWith(provider.installURL)) {
      return Response.redirect(installationURL, 302)
    }
  }
  if (!response.ok || !handoff) {
    return oauthResultPage({
      title:
        response.status === 410
          ? "Authorization expired"
          : "Authorization failed",
      detail:
        readString(handoff?.error_description) ||
        (response.status === 410
          ? "Return to Pudding and start the connection again."
          : "GitHub could not complete this connection."),
    })
  }
  return oauthResultPage({
    title: "GitHub connected",
    detail: "Pudding is ready to finish this connection on your device.",
    openURL: clientHandoffURL(handoff),
  })
}

async function redeemOAuth(
  request: Request,
  env: Env,
  corsHeaders: Record<string, string>,
): Promise<Response> {
  let body: Record<string, unknown>
  try {
    body = (await request.json()) as Record<string, unknown>
  } catch {
    return json({ error: "invalid_json" }, 400, corsHeaders)
  }

  const ticket = parseRedemptionTicket(readString(body.ticket))
  const verifier = readString(body.verifier)
  if (!ticket || !isOpaqueToken(verifier)) {
    return json({ error: "invalid_redemption_request" }, 400, corsHeaders)
  }
  const response = await transactionStub(env, ticket.transactionID).fetch(
    "https://oauth.internal/redeem",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ secret: ticket.secret, verifier }),
    },
  )
  return new Response(response.body, {
    status: response.status,
    headers: {
      "Cache-Control": "no-store",
      "Content-Type": "application/json; charset=utf-8",
      ...corsHeaders,
    },
  })
}

async function refreshOAuth(
  request: Request,
  env: Env,
  corsHeaders: Record<string, string>,
): Promise<Response> {
  let body: Record<string, unknown>
  try {
    body = (await request.json()) as Record<string, unknown>
  } catch {
    return json({ error: "invalid_json" }, 400, corsHeaders)
  }
  const providerID = readString(body.provider)
  const refreshToken = readString(body.refresh_token)
  if (!refreshToken || refreshToken.length > 4096) {
    return json({ error: "invalid_refresh_request" }, 400, corsHeaders)
  }
  const provider = puddingOAuthProvider(providerID, env)
  if (!provider) {
    return json({ error: "provider_not_configured" }, 503, corsHeaders)
  }
  try {
    return json(
      await refreshProviderToken(provider, refreshToken),
      200,
      corsHeaders,
    )
  } catch (error) {
    return json(
      {
        error: "token_refresh_failed",
        error_description:
          error instanceof Error ? error.message : "Token refresh failed.",
      },
      502,
      corsHeaders,
    )
  }
}

async function revokeOAuth(
  request: Request,
  env: Env,
  corsHeaders: Record<string, string>,
): Promise<Response> {
  let body: Record<string, unknown>
  try {
    body = (await request.json()) as Record<string, unknown>
  } catch {
    return json({ error: "invalid_json" }, 400, corsHeaders)
  }
  const providerID = readString(body.provider)
  const accessToken = readString(body.access_token)
  if (!accessToken || accessToken.length > 4096) {
    return json({ error: "invalid_revoke_request" }, 400, corsHeaders)
  }
  const provider = puddingOAuthProvider(providerID, env)
  if (!provider) {
    return json({ error: "provider_not_configured" }, 503, corsHeaders)
  }
  try {
    await revokeProviderToken(provider, accessToken)
    return new Response(null, {
      status: 204,
      headers: {
        "Cache-Control": "no-store",
        ...corsHeaders,
      },
    })
  } catch (error) {
    return json(
      {
        error: "token_revoke_failed",
        error_description:
          error instanceof Error ? error.message : "Token revocation failed.",
      },
      502,
      corsHeaders,
    )
  }
}

function transactionStub(env: Env, transactionID: string): DurableObjectStub {
  return env.OAUTH_TRANSACTIONS.get(
    env.OAUTH_TRANSACTIONS.idFromName(transactionID),
  )
}

async function responseJSON(
  response: Response,
): Promise<Record<string, unknown> | null> {
  return response.json().catch(() => null) as Promise<Record<
    string,
    unknown
  > | null>
}

function clientHandoffURL(
  handoff: Record<string, unknown>,
  extra: Record<string, string> = {},
): string {
  const client = readString(handoff.client)
  const provider = readString(handoff.provider)
  const scheme = isOAuthClient(client) ? CLIENT_SCHEMES[client] : ""
  if (!scheme || !provider) return ""

  const url = new URL(
    `${scheme}://oauth/connected/${encodeURIComponent(provider)}`,
  )
  for (const [name, value] of Object.entries({
    ticket: readString(handoff.ticket),
    state: readString(handoff.client_state),
    ...extra,
  })) {
    if (value) url.searchParams.set(name, value)
  }
  return url.toString()
}

function oauthResultPage(options: {
  title: string
  detail: string
  openURL?: string
}): Response {
  const openURL = options.openURL?.trim() ?? ""
  const openURLJSON = JSON.stringify(openURL)
  const action = openURL
    ? `<a class="button" href="${escapeHTML(openURL)}">Open Pudding</a>
       <script>const target=${openURLJSON};setTimeout(()=>{window.location.href=target},350);</script>`
    : ""
  const document = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>${escapeHTML(options.title)} — Pudding</title>
  <style>
    :root{color-scheme:light dark;font-family:Inter,ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    *{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f7f7f5;color:#1d1d20;padding:24px}
    main{width:min(440px,100%);padding:40px;border:1px solid rgba(29,29,32,.1);border-radius:28px;background:#fff;box-shadow:0 24px 70px rgba(29,29,32,.09);text-align:center}
    .mark{display:grid;place-items:center;width:58px;height:58px;margin:0 auto 24px;border-radius:18px;background:#eeecff;color:#4d3ce0;font-size:28px;font-weight:700}
    h1{margin:0 0 10px;font-size:25px;line-height:1.2}p{margin:0;color:#66666f;line-height:1.6}
    .button{display:inline-flex;margin-top:26px;padding:12px 20px;border-radius:999px;background:#4d3ce0;color:#fff;text-decoration:none;font-weight:650}
    @media(prefers-color-scheme:dark){body{background:#11110f;color:#f7f7f5}main{background:#191917;border-color:rgba(255,255,255,.12);box-shadow:none}p{color:#aaa9a2}}
  </style>
</head>
<body><main><div class="mark">P</div><h1>${escapeHTML(options.title)}</h1><p>${escapeHTML(options.detail)}</p>${action}</main></body>
</html>`
  return new Response(document, {
    status: 200,
    headers: {
      "Cache-Control": "no-store",
      "Content-Security-Policy":
        "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
      "Content-Type": "text/html; charset=utf-8",
      "Permissions-Policy":
        "camera=(), microphone=(), geolocation=(), payment=()",
      "Referrer-Policy": "no-referrer",
      "X-Content-Type-Options": "nosniff",
      "X-Frame-Options": "DENY",
    },
  })
}

function escapeHTML(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;")
}

function isPuddingOAuthHost(hostname: string): boolean {
  return (
    hostname === "x-t.top" ||
    hostname === "localhost" ||
    hostname === "127.0.0.1"
  )
}
