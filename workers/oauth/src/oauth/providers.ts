import type { Env } from "../env"
import { readEnvString } from "../http"

export type PuddingOAuthProvider = {
  id: "github"
  appID: string
  appSlug: string
  authorizationURL: string
  callbackURL: string
  clientID: string
  clientSecret: string
  installURL: string
  tokenURL: string
}

export type UserInstallation = {
  account: string
  htmlURL: string
  id: string
}

export function puddingOAuthProvider(
  providerID: string,
  env: Env,
): PuddingOAuthProvider | null {
  if (providerID !== "github") return null

  const appSlug =
    readEnvString(env, "PUDDING_GITHUB_APP_SLUG") || "pudding-connector"
  const clientID = readEnvString(env, "PUDDING_GITHUB_CLIENT_ID")
  const clientSecret = readEnvString(env, "PUDDING_GITHUB_CLIENT_SECRET")
  if (!clientID || !clientSecret) return null

  return {
    id: "github",
    appID: readEnvString(env, "PUDDING_GITHUB_APP_ID") || "4187984",
    appSlug,
    authorizationURL: "https://github.com/login/oauth/authorize",
    callbackURL: "https://x-t.top/oauth/callback/github",
    clientID,
    clientSecret,
    installURL: `https://github.com/apps/${encodeURIComponent(appSlug)}/installations/new`,
    tokenURL: "https://github.com/login/oauth/access_token",
  }
}

export async function exchangeProviderCode(
  provider: PuddingOAuthProvider,
  code: string,
): Promise<Record<string, unknown>> {
  return requestProviderToken(provider, {
    code,
    redirect_uri: provider.callbackURL,
  })
}

export async function refreshProviderToken(
  provider: PuddingOAuthProvider,
  refreshToken: string,
): Promise<Record<string, unknown>> {
  return requestProviderToken(provider, {
    grant_type: "refresh_token",
    refresh_token: refreshToken,
  })
}

export async function listUserInstallations(
  provider: PuddingOAuthProvider,
  accessToken: string,
): Promise<UserInstallation[]> {
  const response = await fetch("https://api.github.com/user/installations", {
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${accessToken}`,
      "User-Agent": "Pudding OAuth Worker",
      "X-GitHub-Api-Version": "2026-03-10",
    },
  })
  const payload = (await response.json().catch(() => null)) as Record<
    string,
    unknown
  > | null
  if (!response.ok || !payload) {
    const detail =
      typeof payload?.message === "string"
        ? payload.message
        : `GitHub installation lookup failed with HTTP ${response.status}.`
    throw new Error(detail)
  }
  const expectedAppID = Number(provider.appID)
  const installations = Array.isArray(payload.installations)
    ? payload.installations
    : []
  return installations.flatMap((value) => {
    if (!value || typeof value !== "object") return []
    const installation = value as Record<string, unknown>
    const id = installation.id
    const appID = installation.app_id
    const htmlURL = installation.html_url
    if (
      (typeof id !== "number" && typeof id !== "string") ||
      (Number.isFinite(expectedAppID) && Number(appID) !== expectedAppID) ||
      typeof htmlURL !== "string"
    ) {
      return []
    }
    const account =
      installation.account &&
      typeof installation.account === "object" &&
      typeof (installation.account as Record<string, unknown>).login ===
        "string"
        ? String(
            (installation.account as Record<string, unknown>).login,
          ).trim()
        : ""
    return [
      {
        account,
        htmlURL: htmlURL.trim(),
        id: String(id),
      },
    ]
  })
}

export async function revokeProviderToken(
  provider: PuddingOAuthProvider,
  accessToken: string,
): Promise<void> {
  const response = await fetch(
    `https://api.github.com/applications/${encodeURIComponent(provider.clientID)}/token`,
    {
      method: "DELETE",
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Basic ${btoa(`${provider.clientID}:${provider.clientSecret}`)}`,
        "Content-Type": "application/json",
        "User-Agent": "Pudding OAuth Worker",
        "X-GitHub-Api-Version": "2026-03-10",
      },
      body: JSON.stringify({ access_token: accessToken }),
    },
  )
  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as Record<
      string,
      unknown
    > | null
    const detail =
      typeof payload?.message === "string"
        ? payload.message
        : `GitHub token revocation failed with HTTP ${response.status}.`
    throw new Error(detail)
  }
}

async function requestProviderToken(
  provider: PuddingOAuthProvider,
  values: Record<string, string>,
): Promise<Record<string, unknown>> {
  const response = await fetch(provider.tokenURL, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/x-www-form-urlencoded",
      "User-Agent": "Pudding OAuth Worker",
    },
    body: new URLSearchParams({
      client_id: provider.clientID,
      client_secret: provider.clientSecret,
      ...values,
    }),
  })
  const payload = (await response.json().catch(() => null)) as Record<
    string,
    unknown
  > | null
  if (!response.ok || !payload || typeof payload.access_token !== "string") {
    const detail =
      typeof payload?.error_description === "string"
        ? payload.error_description
        : typeof payload?.error === "string"
          ? payload.error
          : `GitHub token exchange failed with HTTP ${response.status}.`
    throw new Error(detail)
  }
  return payload
}
