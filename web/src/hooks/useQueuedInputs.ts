import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useReducer } from "react";
import { toast } from "sonner";
import { listQueuedInputs, reorderQueuedInputs, steerQueuedInput, updateQueuedInput, type ContentPart } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { refreshInputRequestForQueuedInput } from "@/lib/inputFlowQueries";
import { useOverlayStore, type PendingUserMessage } from "@/state/overlayStore";

const EMPTY: PendingUserMessage[] = [];

export function useQueuedInputs(token: string, sessionID: string) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [, renderQueueOrder] = useReducer((revision: number) => revision + 1, 0);
  const pending = useOverlayStore((state) => state.pendingUsers[sessionID] || EMPTY);
  const query = useQuery({
    queryKey: queryKeys.queuedInputs(sessionID),
    queryFn: () => listQueuedInputs(token, sessionID),
    enabled: Boolean(token && sessionID),
  });
  const inputs = useMemo(() => {
    // REST array order is authoritative; only unacknowledged arrivals/steers
    // come from the transient overlay. Never re-sort by creation time.
    const entries: PendingUserMessage[] = (query.data?.queuedInputs || []).flatMap((input) =>
      input.status === "queued" || input.status === "editing" ? [{ ...input, status: input.status }] : []);
    for (const input of pending) {
      const index = entries.findIndex((entry) => entry.clientMessageID === input.clientMessageID);
      if (input.status === "steered" || (input.status === "submitting" && input.turnID)) {
        if (index >= 0) entries.splice(index, 1);
      } else if (input.status === "steering" || input.status === "queued" || input.status === "editing") {
        if (index < 0) entries.push(input);
        else if (input.status === "steering") entries[index] = input;
      }
    }
    return entries;
  }, [pending, query.data]);
  const refresh = () => queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
  const mutation = useMutation({
    onMutate: (action) => {
      if (action.type !== "reorder") return;
      const cancelled = queryClient.cancelQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      queryClient.setQueryData<Awaited<ReturnType<typeof listQueuedInputs>>>(queryKeys.queuedInputs(sessionID), (data) => {
        if (!data) return data;
        const byID = new Map(data.queuedInputs.map((input) => [input.clientMessageID, input]));
        return { queuedInputs: action.ids.flatMap((id) => byID.has(id) ? [byID.get(id)!] : []) };
      });
      // Query notifications run on a later task. Render the same cache snapshot
      // in the drag-end batch so clearing the drag transform cannot show the
      // old order first. No separate optimistic order is kept in UI state.
      renderQueueOrder();
      return cancelled;
    },
    mutationFn: async (action:
      | { type: "update"; id: string; status: "queued" | "editing" | "cancelled"; text?: string; parts?: ContentPart[] }
      | { type: "reorder"; ids: string[] }
      | { type: "steer"; input: PendingUserMessage; turnID: string }) => {
      if (action.type === "reorder") return reorderQueuedInputs(token, sessionID, action.ids);
      if (action.type === "update") {
        const { id, type: _type, ...patch } = action;
        return updateQueuedInput(token, sessionID, id, patch);
      }
      useOverlayStore.getState().addPendingUser({ ...action.input, status: "steering", turnID: action.turnID });
      try {
        return await steerQueuedInput(token, sessionID, action.input.clientMessageID, action.turnID);
      } catch (error) {
        useOverlayStore.getState().addPendingUser(action.input);
        throw error;
      }
    },
    onError: () => toast.error(t("transcript.queuedUpdateFailed")),
    onSettled: async (_data, _error, action) => {
      await Promise.all([
        refresh(),
        action.type === "reorder" ? undefined : refreshInputRequestForQueuedInput(queryClient, sessionID, action.type === "update" ? action.id : action.input.clientMessageID),
      ]);
    },
  });
  return { inputs, query, mutation };
}
