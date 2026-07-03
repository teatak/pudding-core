import { useQuery } from "@tanstack/react-query";
import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";

import { getSettings, type ContentPart } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { TranscriptView } from "@/components/transcript/TranscriptView";
import type { TranscriptTurnVM } from "@/components/transcript/types";
import { useTranscriptData } from "@/components/transcript/useTranscriptData";
import { transcriptDisplaySettings } from "@/lib/appSettings";

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
  const { hasMoreHistory, isLoadingHistory, loadHistory, markAssistantRevealed, transcript, turnsQuery, updateQueued } =
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
  const disclosure = useMemo(
    () => ({
      isOpen: (key: string) => Boolean(disclosureByKeyRef.current[`${sessionIDRef.current}:${key}`]),
      setOpen: (key: string, open: boolean) => {
        const scopedKey = `${sessionIDRef.current}:${key}`;
        setDisclosureByKey((previous) => {
          if (Boolean(previous[scopedKey]) === open) {
            return previous;
          }
          if (!open) {
            const next = { ...previous };
            delete next[scopedKey];
            return next;
          }
          return { ...previous, [scopedKey]: true };
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
  return (
    (turn.user ? 1 : 0) +
    (turn.compact ? 1 : 0) +
    (turn.assistant && hasAssistantMessageContent(turn.assistant) ? 1 : 0)
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
  }
}
