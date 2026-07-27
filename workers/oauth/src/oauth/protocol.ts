export type OAuthClient = "desktop" | "mobile"
export type OAuthFlow = "authorize" | "install"

export type OAuthTransactionRecord = {
  challenge: string
  client: OAuthClient
  clientState: string
  createdAt: number
  expiresAt: number
  flow: OAuthFlow
  provider: string
  status: "pending" | "callback_received" | "redeeming"
  transactionID: string
  installationAttempted?: boolean
  installationID?: string
  redeemSecret?: string
  token?: Record<string, unknown>
}

export const CLIENT_SCHEMES: Record<OAuthClient, string> = {
  desktop: "pudding",
  mobile: "pudding-mobile",
}

export const START_TTL_MS = 10 * 60 * 1000
export const REDEEM_TTL_MS = 5 * 60 * 1000

export function isOAuthClient(value: string): value is OAuthClient {
  return value === "desktop" || value === "mobile"
}

export function isOAuthFlow(value: string): value is OAuthFlow {
  return value === "authorize" || value === "install"
}

export function isOpaqueToken(value: string): boolean {
  return /^[A-Za-z0-9_-]{32,256}$/.test(value)
}

export function parseRedemptionTicket(
  value: string,
): { transactionID: string; secret: string } | null {
  const separator = value.indexOf(".")
  if (separator <= 0 || separator !== value.lastIndexOf(".")) return null
  const transactionID = value.slice(0, separator)
  const secret = value.slice(separator + 1)
  return isOpaqueToken(transactionID) && isOpaqueToken(secret)
    ? { transactionID, secret }
    : null
}
