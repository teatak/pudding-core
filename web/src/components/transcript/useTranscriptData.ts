import { useEffect } from "react";
import { useShallow } from "zustand/react/shallow";

import { type PendingUserMessage, useOverlayStore } from "@/state/overlayStore";

import { useTranscriptTurns } from "./useTranscriptTurns";
import { useTranscriptViewModel } from "./useTranscriptViewModel";

const EMPTY_PENDING: PendingUserMessage[] = [];

export function useTranscriptData({
  sessionID,
  sessionRunning,
  token,
}: {
  sessionID: string;
  sessionRunning: boolean;
  token: string;
}) {
  const markAssistantRevealed = useOverlayStore((state) => state.markAssistantRevealed);
  const reconcileMessages = useOverlayStore((state) => state.reconcileMessages);
  const reconcileTurns = useOverlayStore((state) => state.reconcileTurns);
  const pendingUsers = useOverlayStore((state) => state.pendingUsers[sessionID] || EMPTY_PENDING);
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
  };
}
