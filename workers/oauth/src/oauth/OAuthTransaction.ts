import type { Env } from "../env"
import { json, readString } from "../http"
import { randomToken, sha256Base64URL, timingSafeEqual } from "./crypto"
import {
  exchangeProviderCode,
  listUserInstallations,
  puddingOAuthProvider,
} from "./providers"
import {
  REDEEM_TTL_MS,
  type OAuthTransactionRecord,
} from "./protocol"

const STORAGE_KEY = "transaction"

export class OAuthTransaction {
  constructor(
    private readonly state: DurableObjectState,
    private readonly env: Env,
  ) {}

  async fetch(request: Request): Promise<Response> {
    const path = new URL(request.url).pathname
    if (request.method === "POST" && path === "/init") {
      return this.initialize(request)
    }
    if (request.method === "POST" && path === "/callback") {
      return this.receiveCallback(request)
    }
    if (request.method === "POST" && path === "/fail") {
      return this.fail(request)
    }
    if (request.method === "POST" && path === "/redeem") {
      return this.redeem(request)
    }
    return json({ error: "not_found" }, 404)
  }

  async alarm(): Promise<void> {
    await this.state.storage.deleteAll()
  }

  private async initialize(request: Request): Promise<Response> {
    const existing =
      await this.state.storage.get<OAuthTransactionRecord>(STORAGE_KEY)
    if (existing) return json({ error: "transaction_exists" }, 409)

    const record = (await request.json()) as OAuthTransactionRecord
    if (!record.transactionID || record.status !== "pending") {
      return json({ error: "invalid_transaction" }, 400)
    }
    await this.state.storage.put(STORAGE_KEY, record)
    await this.state.storage.setAlarm(record.expiresAt)
    return json({ ok: true }, 201)
  }

  private async receiveCallback(request: Request): Promise<Response> {
    const record = await this.readActive()
    if (!record) return json({ error: "transaction_expired" }, 410)

    const body = (await request.json()) as Record<string, unknown>
    const provider = readString(body.provider)
    const code = readString(body.code)
    const installationID = readString(body.installation_id)
    if (record.provider !== provider || !code) {
      return json({ error: "invalid_callback" }, 400)
    }
    if (
      record.status === "callback_received" &&
      record.redeemSecret &&
      record.token
    ) {
      return json(this.handoff(record))
    }
    if (record.status !== "pending") {
      return json({ error: "transaction_not_pending" }, 409)
    }

    const configuredProvider = puddingOAuthProvider(provider, this.env)
    if (!configuredProvider) {
      return json({ error: "provider_not_configured" }, 503)
    }
    try {
      const token = await exchangeProviderCode(configuredProvider, code)
      const accessToken = readString(token.access_token)
      const installations = await listUserInstallations(
        configuredProvider,
        accessToken,
      )
      if (installations.length === 0) {
        if (record.flow === "install" && !record.installationAttempted) {
          const installationURL = new URL(configuredProvider.installURL)
          installationURL.searchParams.set("state", record.transactionID)
          await this.state.storage.put(STORAGE_KEY, {
            ...record,
            installationAttempted: true,
          } satisfies OAuthTransactionRecord)
          return json(
            {
              error: "installation_required",
              installation_url: installationURL.toString(),
            },
            409,
          )
        }
        throw new Error(
          "Pudding Connector is not installed for an accessible GitHub account or organization.",
        )
      }
      const selectedInstallation =
        installations.find(
          (installation) => installation.id === installationID,
        ) ?? installations[0]!
      const redeemSecret = randomToken()
      const updated: OAuthTransactionRecord = {
        ...record,
        status: "callback_received",
        redeemSecret,
        token: {
          ...token,
          installation_id: selectedInstallation.id,
          installation_ids: installations.map(
            (installation) => installation.id,
          ),
          installation_url: selectedInstallation.htmlURL,
        },
        expiresAt: Date.now() + REDEEM_TTL_MS,
        installationID: selectedInstallation.id,
      }
      await this.state.storage.put(STORAGE_KEY, updated)
      await this.state.storage.setAlarm(updated.expiresAt)
      return json(this.handoff(updated))
    } catch (error) {
      return json(
        {
          error: "token_exchange_failed",
          error_description:
            error instanceof Error ? error.message : "Token exchange failed.",
        },
        502,
      )
    }
  }

  private async fail(request: Request): Promise<Response> {
    const record = await this.readActive()
    if (!record) return json({ error: "transaction_expired" }, 410)
    const body = (await request.json()) as Record<string, unknown>
    const provider = readString(body.provider)
    if (record.provider !== provider) {
      return json({ error: "invalid_callback" }, 400)
    }
    await this.state.storage.deleteAll()
    return json({
      client: record.client,
      client_state: record.clientState,
      provider: record.provider,
    })
  }

  private async redeem(request: Request): Promise<Response> {
    const record = await this.readActive()
    if (!record) return json({ error: "transaction_expired" }, 410)
    if (
      record.status !== "callback_received" ||
      !record.redeemSecret ||
      !record.token
    ) {
      return json({ error: "transaction_not_redeemable" }, 409)
    }

    const body = (await request.json()) as Record<string, unknown>
    const secret = readString(body.secret)
    const verifier = readString(body.verifier)
    const challenge = verifier ? await sha256Base64URL(verifier) : ""
    if (
      !timingSafeEqual(secret, record.redeemSecret) ||
      !timingSafeEqual(challenge, record.challenge)
    ) {
      return json({ error: "invalid_redemption" }, 403)
    }

    await this.state.storage.put(STORAGE_KEY, {
      ...record,
      status: "redeeming",
    } satisfies OAuthTransactionRecord)

    await this.state.storage.deleteAll()
    return json({
      ...record.token,
      installation_id: record.installationID,
    })
  }

  private async readActive(): Promise<OAuthTransactionRecord | null> {
    const record =
      (await this.state.storage.get<OAuthTransactionRecord>(STORAGE_KEY)) ?? null
    if (!record) return null
    if (record.expiresAt <= Date.now()) {
      await this.state.storage.deleteAll()
      return null
    }
    return record
  }

  private handoff(record: OAuthTransactionRecord) {
    return {
      client: record.client,
      client_state: record.clientState,
      expires_at: new Date(record.expiresAt).toISOString(),
      ticket: `${record.transactionID}.${record.redeemSecret}`,
      provider: record.provider,
    }
  }
}
