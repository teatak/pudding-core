import type { Env } from "./env"
import { buildCORSHeaders, json, withoutBody } from "./http"
import { routeLegacyOAuth } from "./oauth/legacy"
import { routePuddingOAuth } from "./oauth/router"
import { publicPage } from "./site"

export { OAuthTransaction } from "./oauth/OAuthTransaction"

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url)
    const corsHeaders = buildCORSHeaders(request, env)

    if (request.method === "OPTIONS") {
      return new Response(null, { status: 204, headers: corsHeaders })
    }

    const puddingOAuth = await routePuddingOAuth(
      request,
      url,
      env,
      corsHeaders,
    )
    if (puddingOAuth) return puddingOAuth

    if (request.method === "GET" || request.method === "HEAD") {
      if (isPublicAsset(url.pathname) && env.ASSETS) {
        return env.ASSETS.fetch(request)
      }
      const page = publicPage(url)
      if (page) {
        if (url.hostname === "oauth.x-t.top") {
          return Response.redirect(
            `https://x-t.top${url.pathname}${url.search}`,
            308,
          )
        }
        return request.method === "HEAD" ? withoutBody(page) : page
      }
    }

    const legacyOAuth = await routeLegacyOAuth(
      request,
      url,
      env,
      corsHeaders,
    )
    if (legacyOAuth) return legacyOAuth

    return json({ ok: false, reason: "not_found" }, 404, corsHeaders)
  },
}

function isPublicAsset(pathname: string): boolean {
  return pathname === "/logo.png" || pathname === "/og.png"
}
