import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useReducer, useRef } from "react";
import { toast } from "sonner";
import { listQueuedInputs, reorderQueuedInputs, steerQueuedInput, updateQueuedInput, type ContentPart } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { refreshInputRequestForQueuedInput } from "@/lib/inputFlowQueries";
import { useOverlayStore, type PendingUserMessage } from "@/state/overlayStore";

const EMPTY: PendingUserMessage[] = [];

// 队列显示顺序 = REST 权威顺序 + 投递中的 overlay 项。
// 已引导的项会先被 promoted 而离开 REST 列表,再以 overlay 身份回来:
// 让它回到上一帧相邻的位置,而不是当新到达项追加到末尾(那会打乱整条队列)。
// 从未出现在权威列表里的 overlay 项(还没落库)才追加到末尾。
export function mergeQueuedInputs(
  entries: PendingUserMessage[],
  pending: PendingUserMessage[],
  previousOrder: string[],
): PendingUserMessage[] {
  const queuedByID = new Map(entries.map((entry) => [entry.clientMessageID, entry]));
  const inflightByID = new Map<string, PendingUserMessage>();
  for (const input of pending) {
    if (input.status === "steering" || input.status === "queued" || input.status === "editing") {
      inflightByID.set(input.clientMessageID, input);
    }
  }
  const merged = entries.map((entry) => entry.clientMessageID);
  for (const id of inflightByID.keys()) {
    if (queuedByID.has(id) || merged.includes(id)) continue;
    const index = previousOrder.indexOf(id);
    if (index < 0) {
      merged.push(id);
      continue;
    }
    const after = nearestPresent(previousOrder, index - 1, -1, merged);
    if (after >= 0) {
      merged.splice(after + 1, 0, id);
      continue;
    }
    const before = nearestPresent(previousOrder, index + 1, previousOrder.length, merged);
    if (before >= 0) merged.splice(before, 0, id);
    else merged.push(id);
  }
  return merged.map((id) => {
    const input = inflightByID.get(id);
    if (input?.status === "steering") return input;
    return queuedByID.get(id) ?? input!;
  });
}

// 找最近一个仍在显示中的邻居,返回它在显示列表里的下标。
function nearestPresent(order: string[], from: number, to: number, displayed: string[]) {
  const step = to > from ? 1 : -1;
  for (let i = from; to > from ? i <= to : i >= to; i += step) {
    if (order[i] === undefined) break;
    const index = displayed.indexOf(order[i]);
    if (index >= 0) return index;
  }
  return -1;
}

export function useQueuedInputs(token: string, sessionID: string) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [, renderQueueOrder] = useReducer((revision: number) => revision + 1, 0);
  // 上一帧的显示顺序:投递中的项靠它找回归属位置。
  const queueOrderRef = useRef<string[]>([]);
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
    const merged = mergeQueuedInputs(entries, pending, queueOrderRef.current);
    queueOrderRef.current = merged.map((input) => input.clientMessageID);
    return merged;
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
