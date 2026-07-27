import type { Env } from "../env"
import {
  json,
  jsonHeaders,
  readEnvString,
  readString,
  readStringArray,
  readStringRecord,
} from "../http"

type OAuthProvider = {
  token_url: string
  client_id?: string
  client_id_env?: string
  client_secret_env: string
  allowed_redirect_uris?: string[]
  extra_token_params?: Record<string, string>
}

type OAuthExchangeRequest = {
  code?: unknown
  redirect_uri?: unknown
  code_verifier?: unknown
}

const defaultProviders: Record<string, OAuthProvider> = {
  github: {
    token_url: "https://github.com/login/oauth/access_token",
    client_id: "Ov23li6YcOqhzvGBD9s4",
    client_secret_env: "GITHUB_CLIENT_SECRET",
    allowed_redirect_uris: [
      "http://localhost:9669/oauth/callback/github",
      "http://127.0.0.1:9669/oauth/callback/github",
      "http://localhost:9679/oauth/callback/github",
      "http://127.0.0.1:9679/oauth/callback/github",
    ],
  },
  gmail: {
    token_url: "https://oauth2.googleapis.com/token",
    client_id:
      "226317408426-s2jpl76do0qegl9vesjn1osrkbos1t9o.apps.googleusercontent.com",
    client_secret_env: "GOOGLE_CLIENT_SECRET",
    allowed_redirect_uris: [
      "http://localhost:9669/oauth/callback/gmail",
      "http://127.0.0.1:9669/oauth/callback/gmail",
      "http://localhost:9679/oauth/callback/gmail",
      "http://127.0.0.1:9679/oauth/callback/gmail",
    ],
  },
}

export async function routeLegacyOAuth(
  request: Request,
  url: URL,
  env: Env,
  corsHeaders: Record<string, string>,
): Promise<Response | null> {
  if (
    request.method !== "POST" ||
    !isLegacyOAuthHost(url.hostname)
  ) {
    return null
  }
  const providerID = providerFromPath(url.pathname)
  if (!providerID) return null
  return exchangeOAuthCode(providerID, request, env, corsHeaders)
}

async function exchangeOAuthCode(
  providerID: string,
  request: Request,
  env: Env,
  corsHeaders: HeadersInit,
): Promise<Response> {
  const provider = loadProviders(env)[providerID]
  if (!provider) {
    return json({ ok: false, reason: "provider_not_configured" }, 404, corsHeaders)
  }

  const clientID =
    provider.client_id ||
    (provider.client_id_env
      ? readEnvString(env, provider.client_id_env)
      : "")
  const clientSecret = readEnvString(env, provider.client_secret_env)
  if (!clientID || !clientSecret) {
    return json(
      { ok: false, reason: "provider_secret_not_configured" },
      500,
      corsHeaders,
    )
  }

  let body: OAuthExchangeRequest
  try {
    body = (await request.json()) as OAuthExchangeRequest
  } catch {
    return json({ ok: false, reason: "invalid_json" }, 400, corsHeaders)
  }

  const code = readString(body.code)
  const redirectURI = readString(body.redirect_uri)
  const codeVerifier = readString(body.code_verifier)
  if (!code) {
    return json({ ok: false, reason: "missing_code" }, 400, corsHeaders)
  }
  if (!isAllowedRedirectURI(providerID, redirectURI, provider)) {
    return json(
      { ok: false, reason: "redirect_uri_not_allowed" },
      400,
      corsHeaders,
    )
  }

  const tokenBody = new URLSearchParams({
    client_id: clientID,
    client_secret: clientSecret,
    code,
    redirect_uri: redirectURI,
    grant_type: "authorization_code",
    ...(provider.extra_token_params ?? {}),
  })
  if (codeVerifier) tokenBody.set("code_verifier", codeVerifier)

  const tokenResponse = await fetch(provider.token_url, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/x-www-form-urlencoded",
      "User-Agent": "Pudding OAuth Worker",
    },
    body: tokenBody,
  })
  return new Response(await tokenResponse.text(), {
    status: tokenResponse.status,
    headers: {
      ...jsonHeaders,
      ...corsHeaders,
      "Cache-Control": "no-store",
    },
  })
}

function loadProviders(env: Env): Record<string, OAuthProvider> {
  return {
    ...defaultProviders,
    ...parseProviderJSON(readEnvString(env, "OAUTH_PROVIDERS")),
  }
}

function parseProviderJSON(raw: string | undefined): Record<string, OAuthProvider> {
  if (!raw) return {}
  try {
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {}

    const providers: Record<string, OAuthProvider> = {}
    for (const [id, value] of Object.entries(parsed)) {
      if (
        !isProviderID(id) ||
        !value ||
        typeof value !== "object" ||
        Array.isArray(value)
      ) {
        continue
      }
      const item = value as Record<string, unknown>
      const tokenURL = readString(item.token_url)
      const clientID = readString(item.client_id)
      const clientIDEnv = readString(item.client_id_env)
      const clientSecretEnv = readString(item.client_secret_env)
      if (!tokenURL || (!clientID && !clientIDEnv) || !clientSecretEnv) continue

      providers[id] = {
        token_url: tokenURL,
        client_id: clientID || undefined,
        client_id_env: clientIDEnv,
        client_secret_env: clientSecretEnv,
        allowed_redirect_uris: readStringArray(item.allowed_redirect_uris),
        extra_token_params: readStringRecord(item.extra_token_params),
      }
    }
    return providers
  } catch {
    return {}
  }
}

function providerFromPath(pathname: string): string | null {
  return pathname.match(/^\/([a-z0-9][a-z0-9_-]*)\/exchange$/)?.[1] ?? null
}

function isAllowedRedirectURI(
  providerID: string,
  raw: string,
  provider: OAuthProvider,
): boolean {
  if (!raw) return false
  const configured = provider.allowed_redirect_uris ?? []
  if (configured.length > 0) return configured.includes(raw)

  try {
    const url = new URL(raw)
    const isLoopback =
      url.hostname === "127.0.0.1" || url.hostname === "localhost"
    return (
      url.protocol === "http:" &&
      isLoopback &&
      url.pathname === `/oauth/callback/${providerID}`
    )
  } catch {
    return false
  }
}

function isProviderID(value: string): boolean {
  return /^[a-z0-9][a-z0-9_-]*$/.test(value)
}

function isLegacyOAuthHost(hostname: string): boolean {
  return (
    hostname === "oauth.x-t.top" ||
    hostname === "localhost" ||
    hostname === "127.0.0.1"
  )
}
