import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";

import { listQueuedInputs, updateQueuedInput, type QueuedInput } from "@/api/client";
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
  const markAssistantRevealed = useOverlayStore((state) => state.markAssistantRevealed);
  const reconcileMessages = useOverlayStore((state) => state.reconcileMessages);
  const overlayPendingUsers = useOverlayStore((state) => state.pendingUsers[sessionID] || EMPTY_PENDING);
  const assistantOverlays = useOverlayStore(
    useShallow((state) => Object.values(state.assistants).filter((overlay) => overlay.sessionID === sessionID)),
  );
  const turnPhase = useOverlayStore((state) => state.turnPhases[sessionID]);
  const { messages, query: turnsQuery, turnDurationByID, turns } = useTranscriptTurns(token, sessionID);

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

  const transcript = useTranscriptViewModel({
    assistantOverlays,
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
  }, [messages, reconcileMessages, sessionID, turnsQuery.isSuccess]);

  return {
    markAssistantRevealed,
    pendingUsers,
    transcript,
    turnsQuery,
    updateQueued,
  };
}

function mergePendingUsers(queuedInputs: QueuedInput[], overlayPending: PendingUserMessage[]) {
  const out: PendingUserMessage[] = [];
  const seen = new Set<string>();
  for (const input of queuedInputs) {
    if (input.status !== "queued" && input.status !== "editing") {
      continue;
    }
    seen.add(input.clientMessageID);
    out.push({
      clientMessageID: input.clientMessageID,
      createdAt: input.createdAt,
      sessionID: input.sessionID,
      status: input.status,
      text: input.text,
    });
  }
  for (const pending of overlayPending) {
    if (seen.has(pending.clientMessageID)) {
      continue;
    }
    out.push(pending);
  }
  return out;
}
