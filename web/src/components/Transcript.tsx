import { useQuery } from "@tanstack/react-query";
import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";

import { getSettings, type ContentPart } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { TranscriptView } from "@/components/transcript/TranscriptView";
import type { TranscriptTurnVM } from "@/components/transcript/types";
import { useTranscriptData } from "@/components/transcript/useTranscriptData";
import { transcriptDisplaySettings } from "@/lib/appSettings";
import { useOverlayStore } from "@/state/overlayStore";

type TranscriptProps = {
  token: string;
  sessionID: string;
  sessionRunning?: boolean;
  submitError?: string | null;
};

export function Transcript({ token, sessionID, sessionRunning = false, submitError }: TranscriptProps) {
  const [disclosureByKey, setDisclosureByKey] = useState<Record<string, boolean>>({});
  const [isAtLatest, setIsAtLatest] = useState(true);
  const [jumpLatestSignal, setJumpLatestSignal] = useState(0);
  const [newMessageCount, setNewMessageCount] = useState(0);
  const runningTurnID = useOverlayStore((state) => state.runningTurns[sessionID]);
  const { hasMoreHistory, isLoadingHistory, loadHistory, markAssistantRevealed, steerQueued, transcript, turnsQuery, updateQueued } =
    useTranscriptData({
      sessionID,
      sessionRunning,
      token,
    });
  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => getSettings(token),
    enabled: Boolean(token),
    staleTime: 30_000,
  });
  const displaySettings = useMemo(
    () => transcriptDisplaySettings(settingsQuery.data?.settings),
    [settingsQuery.data?.settings],
  );
  const disclosureByKeyRef = useRef(disclosureByKey);
  const sessionIDRef = useRef(sessionID);
  const turnVMsRef = useRef<TranscriptTurnVM[]>([]);
  disclosureByKeyRef.current = disclosureByKey;
  sessionIDRef.current = sessionID;
  const cancelQueued = useCallback(
    (clientMessageID: string) => updateQueued(clientMessageID, { status: "cancelled" }),
    [updateQueued],
  );
  const startQueuedEdit = useCallback(
    (clientMessageID: string) => updateQueued(clientMessageID, { status: "editing" }),
    [updateQueued],
  );
  const saveQueued = useCallback(
    (clientMessageID: string, text: string) => updateQueued(clientMessageID, { status: "queued", text }),
    [updateQueued],
  );
  const guideQueued = useCallback(
    (clientMessageID: string) => {
      if (!runningTurnID) {
        return Promise.reject(new Error("turn_not_active"));
      }
      return steerQueued(clientMessageID, runningTurnID);
    },
    [runningTurnID, steerQueued],
  );
  const disclosure = useMemo(
    () => ({
      hasState: (key: string) =>
        Object.prototype.hasOwnProperty.call(disclosureByKeyRef.current, `${sessionIDRef.current}:${key}`),
      isOpen: (key: string) => Boolean(disclosureByKeyRef.current[`${sessionIDRef.current}:${key}`]),
      setOpen: (key: string, open: boolean) => {
        const scopedKey = `${sessionIDRef.current}:${key}`;
        setDisclosureByKey((previous) => {
          if (Object.prototype.hasOwnProperty.call(previous, scopedKey) && previous[scopedKey] === open) {
            return previous;
          }
          // Keep explicit false values so a streamed parent group cannot reopen after the user closes it.
          return { ...previous, [scopedKey]: open };
        });
      },
    }),
    [],
  );

  const handleAssistantRevealComplete = useCallback((turnID: string) => markAssistantRevealed(turnID), [markAssistantRevealed]);

  useLayoutEffect(() => {
    const previous = turnVMsRef.current;
    const next = transcript.turnVMs;
    turnVMsRef.current = next;
    if (isAtLatest) {
      setNewMessageCount((count) => (count === 0 ? count : 0));
      return;
    }
    const appended = appendedMessageCount(previous, next);
    if (appended > 0) {
      setNewMessageCount((count) => count + appended);
    }
  }, [isAtLatest, transcript.turnVMs]);

  const handleLatestChange = useCallback((next: boolean) => {
    setIsAtLatest(next);
    if (next) {
      setNewMessageCount(0);
    }
  }, []);

  const handleJumpLatest = useCallback(() => {
    setNewMessageCount(0);
    setIsAtLatest(true);
    setJumpLatestSignal((signal) => signal + 1);
  }, []);

  return (
    <TranscriptView
      disclosure={disclosure}
      displaySettings={displaySettings}
      hasItems={transcript.hasItems}
      isError={turnsQuery.isError}
      isFetchingNextPage={turnsQuery.isFetchingNextPage}
      hasMoreHistory={hasMoreHistory}
      isLoading={turnsQuery.isLoading}
      isLoadingHistory={isLoadingHistory}
      isPending={turnsQuery.isPending}
      jumpLatestSignal={jumpLatestSignal}
      newMessageCount={newMessageCount}
      sessionID={sessionID}
      showJumpLatest={!isAtLatest}
      submitError={submitError}
      token={token}
      turns={transcript.turnVMs}
      onAssistantRevealComplete={handleAssistantRevealComplete}
      onJumpLatest={handleJumpLatest}
      onLatestChange={handleLatestChange}
      onLoadHistory={loadHistory}
      onQueuedCancel={cancelQueued}
      onQueuedEditStart={startQueuedEdit}
      onQueuedSteer={runningTurnID ? guideQueued : undefined}
      onQueuedSave={saveQueued}
    />
  );
}

function appendedMessageCount(previous: TranscriptTurnVM[], next: TranscriptTurnVM[]) {
  if (previous.length === 0 || next.length < previous.length) {
    return 0;
  }
  let count = 0;
  for (let index = 0; index < previous.length; index += 1) {
    if (previous[index].key !== next[index]?.key) {
      // pending 通常先进入 live turn;若未来出现 pending 直接替换为 canonical turn 的漏计,再按 clientMessageID 对账补。
      if (
        previous.length === next.length &&
        index === previous.length - 1 &&
        previous[index].clientMessageID &&
        previous[index].clientMessageID === next[index]?.clientMessageID
      ) {
        return Math.max(0, messageUnitCount(next[index]) - messageUnitCount(previous[index]));
      }
      return 0;
    }
    count += Math.max(0, messageUnitCount(next[index]) - messageUnitCount(previous[index]));
  }
  for (let index = previous.length; index < next.length; index += 1) {
    count += messageUnitCount(next[index]);
  }
  return count;
}

function messageUnitCount(turn: TranscriptTurnVM) {
  const sequenceUnits =
    turn.sequence?.reduce(
      (count, item) =>
        count + (item.kind === "guide" || hasAssistantMessageContent(item.assistant) ? 1 : 0),
      0,
    ) || 0;
  return (
    (turn.user ? 1 : 0) +
    (turn.compact ? 1 : 0) +
    (turn.assistant && hasAssistantMessageContent(turn.assistant) ? 1 : 0) +
    sequenceUnits
  );
}

function hasAssistantMessageContent(assistant: NonNullable<TranscriptTurnVM["assistant"]>) {
  if (assistant.kind === "phase") {
    return false;
  }
  if (assistant.kind === "live") {
    return Boolean(
      assistant.overlay.text.trim() || assistant.overlay.parts.length > 0 || assistant.overlay.error,
    );
  }
  return assistant.messages.some((message) => message.parts.some(contentPartHasVisibleOutput));
}

function contentPartHasVisibleOutput(part: ContentPart) {
  switch (part.type) {
    case "text":
    case "thought":
      return Boolean(part.text.trim());
    case "tool_use":
    case "tool_result":
      return true;
    case "attachment":
    case "local_folder":
    case "project_reference":
    case "ui_context":
    case "form_result":
      return false;
  }
}
