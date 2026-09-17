import { useIsMutating, useMutation, useQueryClient } from "@tanstack/react-query";

import { updateSession, type Session } from "@/api/client";
import { mutationKeys } from "@/api/queryKeys";
import { modelSelectionKey } from "@/lib/modelSelection";
import { sessionModelSettingsMutationOptions, type SessionModelSettingsPatch } from "@/lib/sessionModelSettings";
import { useReasoningEffortPreferenceStore } from "@/state/reasoningEffortPreferenceStore";

type ModelSelection = { provider: string; model: string };

export function useSessionModelSettings(token: string, session: Session) {
  const queryClient = useQueryClient();
  const mutationKey = mutationKeys.sessionModelSettings(session.id);
  const isPending = useIsMutating({ mutationKey, exact: true }) > 0;
  const setReasoningEffortForModel = useReasoningEffortPreferenceStore((state) => state.setForModel);
  const mutation = useMutation(sessionModelSettingsMutationOptions(
    queryClient,
    session.id,
    updateSession,
    setReasoningEffortForModel,
  ));

  const save = async (body: SessionModelSettingsPatch) => {
    // The mutation cache changes synchronously, before disabled props rerender.
    if (queryClient.isMutating({ mutationKey, exact: true }) > 0) {
      throw new Error("Session model settings are already being saved");
    }
    // Variables retain request ownership if this observer receives new props.
    await mutation.mutateAsync({ token, sessionID: session.id, body });
  };
  const modelKey = modelSelectionKey(session);

  return {
    value: { provider: session.provider, model: session.model },
    reasoningValue: modelKey && session.reasoningModelKey === modelKey ? session.reasoningEffort || "" : "",
    isPending,
    error: mutation.error,
    selectModel: (value: ModelSelection) => save(value),
    selectReasoning: (reasoningEffort: string) => save({
      provider: session.provider || "",
      model: session.model || "",
      reasoningEffort,
    }),
  };
}
