export const queryKeys = {
  sessions: () => ["sessions"] as const,
  session: (sessionID: string) => ["session", sessionID] as const,
  messages: (sessionID: string) => ["session", sessionID, "messages", "pages"] as const,
  turns: (sessionID: string) => ["session", sessionID, "turns", "pages"] as const,
  settings: () => ["settings"] as const,
  providers: () => ["providers"] as const,
  providerModels: (name: string) => ["providers", name, "models"] as const,
  provider: (name: string) => ["providers", name] as const,
};
