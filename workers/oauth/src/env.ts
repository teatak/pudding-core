export interface Env extends Record<string, unknown> {
  ALLOWED_ORIGINS?: string
  ASSETS?: Fetcher
  GITHUB_CLIENT_SECRET?: string
  GOOGLE_CLIENT_SECRET?: string
  OAUTH_PROVIDERS?: string
  OAUTH_TRANSACTIONS: DurableObjectNamespace
  PUDDING_GITHUB_APP_ID?: string
  PUDDING_GITHUB_APP_SLUG?: string
  PUDDING_GITHUB_CLIENT_ID?: string
  PUDDING_GITHUB_CLIENT_SECRET?: string
}
