import type { QueryClient } from "@tanstack/react-query";

import { getSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";

export async function syncSessionProjectState(
  queryClient: QueryClient,
  token: string,
  sessionID: string,
  snapshot?: Session,
) {
  const session = snapshot || await getSession(token, sessionID).catch(() => undefined);
  if (session) {
    queryClient.setQueryData(queryKeys.session(sessionID), session);
    queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
      previous
        ? {
            sessions: previous.sessions.map((item) => item.id === session.id ? session : item),
          }
        : previous,
    );
  }
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
    queryClient.invalidateQueries({ queryKey: queryKeys.projects() }),
    queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project"] }),
    ...(session
      ? []
      : [queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionID), exact: true })]),
  ]);
  return session;
}
