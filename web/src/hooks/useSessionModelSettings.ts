import { useIsMutating, useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { toast } from "sonner";

import { updateSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { modelSelectionKey } from "@/lib/modelSelection";
import { useReasoningEffortPreferenceStore } from "@/state/reasoningEffortPreferenceStore";

type SessionModelSettingsBody = {
  provider: string;
  model: string;
  reasoningEffort: string;
};

type SessionModelSettingsRequest = {
  sessionID: string | undefined;
  body: SessionModelSettingsBody;
};

export function useSessionModelSettings(token: string, sessionID?: string) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const mutationKey = ["session-model-settings", sessionID] as const;
  const pending = useIsMutating({ mutationKey, exact: true }) > 0;
  const mutation = useMutation({
    mutationKey,
    // Model selection and effort changes share this scope across components.
    // Keep the next write paused until this write and its refetches finish.
    scope: { id: `session-model-settings:${sessionID || ""}` },
    retry: false,
    mutationFn: ({ sessionID: targetSessionID, body }: SessionModelSettingsRequest) => {
      if (!targetSessionID) {
        throw new Error("missing session");
      }
      return updateSession(token, targetSessionID, body);
    },
    onSuccess: (updated) => {
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
        previous
          ? { ...previous, sessions: previous.sessions.map((item) => item.id === updated.id ? updated : item) }
          : previous,
      );
      queryClient.setQueryData(queryKeys.session(updated.id), updated);
      useReasoningEffortPreferenceStore.getState().setForModel(
        modelSelectionKey(updated),
        updated.reasoningEffort || "",
      );
    },
    onError: () => {
      toast.error(t("provider.saveFailed"));
    },
    onSettled: async (_data, _error, { sessionID: targetSessionID }) => {
      if (!targetSessionID) {
        return;
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.sessions(), exact: true }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(targetSessionID), exact: true }),
        queryClient.invalidateQueries({ queryKey: queryKeys.sessionUsage(targetSessionID), exact: true }),
      ]);
    },
  });
  const update = useCallback(
    (body: SessionModelSettingsBody) => mutation.mutateAsync({ sessionID, body }),
    [mutation.mutateAsync, sessionID],
  );
  return { update, pending };
}
