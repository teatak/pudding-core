import type { Env } from "./env"

export const jsonHeaders = {
  "Content-Type": "application/json; charset=utf-8",
}

export function json(
  body: unknown,
  status = 200,
  headers: HeadersInit = {},
): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      ...jsonHeaders,
      "Cache-Control": "no-store",
      ...headers,
    },
  })
}

export function buildCORSHeaders(
  request: Request,
  env: Env,
): Record<string, string> {
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

export function readEnvString(env: Env, key: string): string {
  return readString(env[key])
}

export function readString(value: unknown): string {
  return typeof value === "string" ? value.trim() : ""
}

export function readStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.map(readString).filter(Boolean)
}

export function readStringRecord(
  value: unknown,
): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined

  const result: Record<string, string> = {}
  for (const [key, item] of Object.entries(value)) {
    if (!key || typeof item !== "string") continue
    result[key] = item
  }
  return result
}

export function withoutBody(response: Response): Response {
  return new Response(null, {
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
  })
}

function parseCSV(value: string | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
}
