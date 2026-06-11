export const queryKeys = {
  sessions: () => ["sessions"] as const,
  session: (sessionID: string) => ["session", sessionID] as const,
  messages: (sessionID: string) => ["session", sessionID, "messages"] as const,
  settings: () => ["settings"] as const,
};
