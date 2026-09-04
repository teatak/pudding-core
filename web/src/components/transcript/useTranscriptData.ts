import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";

import { listQueuedInputs, steerQueuedInput, updateQueuedInput, type QueuedInput } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { type PendingUserMessage, useOverlayStore } from "@/state/overlayStore";

import { useTranscriptTurns } from "./useTranscriptTurns";
import { useTranscriptViewModel } from "./useTranscriptViewModel";

const EMPTY_PENDING: PendingUserMessage[] = [];
const EMPTY_QUEUED: QueuedInput[] = [];

export function useTranscriptData({
  sessionID,
  sessionRunning,
  token,
}: {
  sessionID: string;
  sessionRunning: boolean;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const addPendingUser = useOverlayStore((state) => state.addPendingUser);
  const markAssistantRevealed = useOverlayStore((state) => state.markAssistantRevealed);
  const reconcileMessages = useOverlayStore((state) => state.reconcileMessages);
  const reconcileTurns = useOverlayStore((state) => state.reconcileTurns);
  const overlayPendingUsers = useOverlayStore((state) => state.pendingUsers[sessionID] || EMPTY_PENDING);
  const assistantOverlays = useOverlayStore(
    useShallow((state) => Object.values(state.assistants).filter((overlay) => overlay.sessionID === sessionID)),
  );
  const compactRun = useOverlayStore((state) => state.compactRuns[sessionID]);
  const turnPhase = useOverlayStore((state) => state.turnPhases[sessionID]);
  const {
    hasMoreHistory,
    isLoadingHistory,
    loadHistory,
    messages,
    query: turnsQuery,
    revealTurn,
    turnDurationByID,
    turns,
  } = useTranscriptTurns(token, sessionID);

  const queuedInputsQuery = useQuery({
    queryKey: queryKeys.queuedInputs(sessionID),
    queryFn: () => listQueuedInputs(token, sessionID),
    enabled: Boolean(token && sessionID),
  });

  const pendingUsers = useMemo(
    () => mergePendingUsers(queuedInputsQuery.data?.queuedInputs || EMPTY_QUEUED, overlayPendingUsers),
    [overlayPendingUsers, queuedInputsQuery.data?.queuedInputs],
  );

  const updateQueuedMutation = useMutation({
    mutationFn: ({
      clientMessageID,
      status,
      text,
    }: {
      clientMessageID: string;
      status: "queued" | "editing" | "cancelled";
      text?: string;
    }) => updateQueuedInput(token, sessionID, clientMessageID, { status, text }),
    onError: () => toast.error(t("transcript.queuedUpdateFailed")),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
  });

  const updateQueued = useCallback(
    (clientMessageID: string, patch: { status: "queued" | "editing" | "cancelled"; text?: string }) =>
      updateQueuedMutation.mutateAsync({ clientMessageID, ...patch }),
    [updateQueuedMutation],
  );

  const steerQueuedMutation = useMutation({
    mutationFn: ({ clientMessageID, turnID }: { clientMessageID: string; input?: PendingUserMessage; turnID: string }) =>
      steerQueuedInput(token, sessionID, clientMessageID, turnID),
    onMutate: ({ clientMessageID, input, turnID }) => {
      if (!input) {
        return undefined;
      }
      addPendingUser({
        clientMessageID,
        createdAt: input.createdAt,
        parts: input.parts,
        sessionID,
        status: "steering",
        text: input.text,
        turnID,
      });
      return { input };
    },
    onError: (_error, _variables, context) => {
      if (context?.input) {
        addPendingUser(context.input);
      }
      toast.error(t("transcript.guideQueuedFailed"));
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.turns(sessionID) });
    },
  });

  const steerQueued = useCallback(
    (clientMessageID: string, turnID: string) =>
      steerQueuedMutation.mutateAsync({
        clientMessageID,
        input: pendingUsers.find((input) => input.clientMessageID === clientMessageID),
        turnID,
      }),
    [pendingUsers, steerQueuedMutation],
  );

  const transcript = useTranscriptViewModel({
    assistantOverlays,
    compactRun,
    pendingUsers,
    sessionID,
    sessionRunning,
    turnDurationByID,
    turnPhase,
    turns,
  });

  useEffect(() => {
    if (!turnsQuery.isSuccess) {
      return;
    }
    reconcileMessages(sessionID, messages);
    reconcileTurns(sessionID, turns, sessionRunning);
  }, [messages, reconcileMessages, reconcileTurns, sessionID, sessionRunning, turns, turnsQuery.isSuccess]);

  return {
    markAssistantRevealed,
    hasMoreHistory,
    isLoadingHistory,
    loadHistory,
    pendingUsers,
    revealTurn,
    transcript,
    turnsQuery,
    steerQueued,
    updateQueued,
  };
}

function mergePendingUsers(queuedInputs: QueuedInput[], overlayPending: PendingUserMessage[]) {
  const out: PendingUserMessage[] = [];
  const indexByClientID = new Map<string, number>();
  for (const input of queuedInputs) {
    if (input.status !== "queued" && input.status !== "editing") {
      continue;
    }
    indexByClientID.set(input.clientMessageID, out.length);
    out.push({
      clientMessageID: input.clientMessageID,
      parts: input.parts,
      createdAt: input.createdAt,
      sessionID: input.sessionID,
      status: input.status,
      text: input.text,
    });
  }
  for (const pending of overlayPending) {
    const existingIndex = indexByClientID.get(pending.clientMessageID);
    if (existingIndex !== undefined) {
      // mutation 成功后 queued query 仍可能短暂保留旧快照；steering/steered
      // overlay 必须优先，否则气泡会先退回“稍后发送”再跳入当前 turn。
      if (pending.status === "steering" || pending.status === "steered") {
        out[existingIndex] = pending;
      }
      continue;
    }
    indexByClientID.set(pending.clientMessageID, out.length);
    out.push(pending);
  }
  return out.sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt));
}
