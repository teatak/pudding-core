import type { MutationOptions, QueryClient } from "@tanstack/react-query";

import type { Session } from "../api/client";
import { mutationKeys, queryKeys } from "../api/queryKeys";
import { modelSelectionKey } from "./modelSelection";

export type SessionModelSettingsPatch = {
  provider: string;
  model: string;
  reasoningEffort?: string;
};

export type SessionModelSettingsChange = {
  token: string;
  sessionID: string;
  body: SessionModelSettingsPatch;
};

export function sessionModelSettingsMutationOptions(
  queryClient: QueryClient,
  sessionID: string,
  updateSession: (token: string, sessionID: string, body: SessionModelSettingsPatch) => Promise<Session>,
  setReasoningEffortForModel: (modelKey: string, effort: string) => void,
): MutationOptions<Session, Error, SessionModelSettingsChange, void> {
  return {
    mutationKey: mutationKeys.sessionModelSettings(sessionID),
    scope: { id: `session-model-settings:${sessionID}` },
    mutationFn: ({ token, sessionID, body }) => updateSession(token, sessionID, body),
    onSuccess: async (updated, { body }) => {
      // A fetch started before the PATCH must not replace its confirmed result.
      await Promise.all([
        queryClient.cancelQueries({ queryKey: queryKeys.sessions(), exact: true }),
        queryClient.cancelQueries({ queryKey: queryKeys.session(updated.id), exact: true }),
      ]);
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
        previous
          ? { ...previous, sessions: previous.sessions.map((item) => item.id === updated.id ? updated : item) }
          : previous,
      );
      queryClient.setQueryData(queryKeys.session(updated.id), updated);
      if (body.reasoningEffort !== undefined) {
        const modelKey = modelSelectionKey(body);
        setReasoningEffortForModel(
          modelKey,
          updated.reasoningModelKey === modelKey ? updated.reasoningEffort || "" : "",
        );
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessionUsage(updated.id) });
    },
  };
}
