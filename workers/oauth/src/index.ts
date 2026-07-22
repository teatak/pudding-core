import { publicPage } from "./site"

interface Env extends Record<string, unknown> {
  GITHUB_CLIENT_SECRET?: string
  GOOGLE_CLIENT_SECRET?: string
  ALLOWED_ORIGINS?: string
  OAUTH_PROVIDERS?: string
  ASSETS?: Fetcher
}

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
    client_id: "226317408426-s2jpl76do0qegl9vesjn1osrkbos1t9o.apps.googleusercontent.com",
    client_secret_env: "GOOGLE_CLIENT_SECRET",
    allowed_redirect_uris: [
      "http://localhost:9669/oauth/callback/gmail",
      "http://127.0.0.1:9669/oauth/callback/gmail",
      "http://localhost:9679/oauth/callback/gmail",
      "http://127.0.0.1:9679/oauth/callback/gmail",
    ],
  },
}

const jsonHeaders = {
  "Content-Type": "application/json; charset=utf-8",
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url)
    const corsHeaders = buildCORSHeaders(request, env)

    if (request.method === "OPTIONS") {
      return new Response(null, {
        status: 204,
        headers: corsHeaders,
      })
    }

    if (request.method === "GET" || request.method === "HEAD") {
      if (isPublicAsset(url.pathname) && env.ASSETS) {
        return env.ASSETS.fetch(request)
      }

      const page = publicPage(url)
      if (page) {
        if (url.hostname === "oauth.x-t.top") {
          return Response.redirect(`https://x-t.top${url.pathname}${url.search}`, 308)
        }
        return request.method === "HEAD" ? withoutBody(page) : page
      }
    }

    const provider = providerFromPath(url.pathname)
    if (request.method === "POST" && provider && isOAuthHost(url.hostname)) {
      return exchangeOAuthCode(provider, request, env, corsHeaders)
    }

    return json({ ok: false, reason: "not_found" }, 404, corsHeaders)
  },
}

async function exchangeOAuthCode(
  providerID: string,
  request: Request,
  env: Env,
  corsHeaders: Record<string, string>,
): Promise<Response> {
  const provider = loadProviders(env)[providerID]
  if (!provider) {
    return json({ ok: false, reason: "provider_not_configured" }, 404, corsHeaders)
  }

  const clientID =
    provider.client_id ||
    (provider.client_id_env ? readEnvString(env, provider.client_id_env) : "")
  const clientSecret = readEnvString(env, provider.client_secret_env)
  if (!clientID || !clientSecret) {
    return json({ ok: false, reason: "provider_secret_not_configured" }, 500, corsHeaders)
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
    return json({ ok: false, reason: "redirect_uri_not_allowed" }, 400, corsHeaders)
  }

  const tokenBody = new URLSearchParams({
    client_id: clientID,
    client_secret: clientSecret,
    code,
    redirect_uri: redirectURI,
    grant_type: "authorization_code",
    ...(provider.extra_token_params ?? {}),
  })
  if (codeVerifier) {
    tokenBody.set("code_verifier", codeVerifier)
  }

  const tokenResponse = await fetch(provider.token_url, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/x-www-form-urlencoded",
      "User-Agent": "Pudding OAuth Worker",
    },
    body: tokenBody,
  })

  const responseText = await tokenResponse.text()
  return new Response(responseText, {
    status: tokenResponse.status,
    headers: {
      ...jsonHeaders,
      ...corsHeaders,
      "Cache-Control": "no-store",
    },
  })
}

function loadProviders(env: Env): Record<string, OAuthProvider> {
  const configured = parseProviderJSON(readEnvString(env, "OAUTH_PROVIDERS"))
  return {
    ...defaultProviders,
    ...configured,
  }
}

function parseProviderJSON(raw: string | undefined): Record<string, OAuthProvider> {
  if (!raw) return {}

  try {
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {}

    const providers: Record<string, OAuthProvider> = {}
    for (const [id, value] of Object.entries(parsed)) {
      if (!isProviderID(id) || !value || typeof value !== "object" || Array.isArray(value)) {
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
  const match = pathname.match(/^\/([a-z0-9][a-z0-9_-]*)\/exchange$/)
  if (!match) return null
  return match[1]
}

function isAllowedRedirectURI(
  providerID: string,
  raw: string,
  provider: OAuthProvider,
): boolean {
  if (!raw) return false

  const configured = provider.allowed_redirect_uris ?? []
  if (configured.length > 0) {
    return configured.includes(raw)
  }

  try {
    const url = new URL(raw)
    const isLoopback = url.hostname === "127.0.0.1" || url.hostname === "localhost"
    return (
      url.protocol === "http:" &&
      isLoopback &&
      url.pathname === `/oauth/callback/${providerID}`
    )
  } catch {
    return false
  }
}

function buildCORSHeaders(request: Request, env: Env): Record<string, string> {
  const origin = request.headers.get("Origin")
  const allowedOrigins = parseCSV(readEnvString(env, "ALLOWED_ORIGINS"))
  const headers: Record<string, string> = {
    "Access-Control-Allow-Methods": "POST, OPTIONS",
    "Access-Control-Allow-Headers": "Content-Type",
    Vary: "Origin",
  }

  if (origin && allowedOrigins.includes(origin)) {
    headers["Access-Control-Allow-Origin"] = origin
  }

  return headers
}

function parseCSV(value: string | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
}

function readString(value: unknown): string {
  return typeof value === "string" ? value.trim() : ""
}

function readEnvString(env: Env, key: string): string {
  return readString(env[key])
}

function readStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.map(readString).filter(Boolean)
}

function readStringRecord(value: unknown): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined

  const result: Record<string, string> = {}
  for (const [key, item] of Object.entries(value)) {
    if (!key || typeof item !== "string") continue
    result[key] = item
  }
  return result
}

function isProviderID(value: string): boolean {
  return /^[a-z0-9][a-z0-9_-]*$/.test(value)
}

function isPublicAsset(pathname: string): boolean {
  return pathname === "/logo.png" || pathname === "/og.png"
}

function isOAuthHost(hostname: string): boolean {
  return (
    hostname === "oauth.x-t.top" ||
    hostname === "localhost" ||
    hostname === "127.0.0.1"
  )
}

function withoutBody(response: Response): Response {
  return new Response(null, {
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
  })
}

function json(body: unknown, status: number, headers: Record<string, string>): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      ...jsonHeaders,
      ...headers,
      "Cache-Control": "no-store",
    },
  })
}
