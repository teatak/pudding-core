import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";

import type { ContentPart } from "@/api/client";
import { TranscriptView } from "@/components/transcript/TranscriptView";
import type { TranscriptTurnVM } from "@/components/transcript/types";
import { useTranscriptData } from "@/components/transcript/useTranscriptData";
import { useBottomStick } from "@/hooks/useBottomStick";

type TranscriptProps = {
  token: string;
  sessionID: string;
  sessionRunning?: boolean;
  submitError?: string | null;
};

export function Transcript({ token, sessionID, sessionRunning = false, submitError }: TranscriptProps) {
  const [disclosureByKey, setDisclosureByKey] = useState<Record<string, boolean>>({});
  const [newMessageCount, setNewMessageCount] = useState(0);
  const { markAssistantRevealed, pendingUsers, transcript, turnsQuery, updateQueued } = useTranscriptData({
    sessionID,
    sessionRunning,
    token,
  });
  const bottomStick = useBottomStick({ sessionID });
  const viewportRef = useCallback(
    (node: HTMLDivElement | null) => {
      bottomStick.viewportRef(node);
    },
    [bottomStick.viewportRef],
  );
  const pendingIDsRef = useRef<Set<string>>(new Set());
  const turnVMsRef = useRef<TranscriptTurnVM[]>([]);
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
      isOpen: (key: string) => Boolean(disclosureByKey[`${sessionID}:${key}`]),
      setOpen: (key: string, open: boolean) => {
        const scopedKey = `${sessionID}:${key}`;
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
    [disclosureByKey, sessionID],
  );

  const handleAssistantRevealComplete = useCallback(
    (turnID: string) => {
      markAssistantRevealed(turnID);
      window.requestAnimationFrame(() => {
        bottomStick.stickToBottomIfNeeded({ stabilizeFrames: 2 });
      });
    },
    [bottomStick.stickToBottomIfNeeded, markAssistantRevealed],
  );

  useLayoutEffect(() => {
    const previous = pendingIDsRef.current;
    const next = new Set(pendingUsers.map((message) => message.clientMessageID));
    pendingIDsRef.current = next;
    if (bottomStick.mode === "following" && pendingUsers.some((message) => !previous.has(message.clientMessageID))) {
      bottomStick.enterBottomMode({ stabilizeFrames: 2 });
    }
  }, [bottomStick.enterBottomMode, bottomStick.mode, pendingUsers]);

  useLayoutEffect(() => {
    const previous = turnVMsRef.current;
    const next = transcript.turnVMs;
    turnVMsRef.current = next;
    if (bottomStick.mode === "following") {
      setNewMessageCount((count) => (count === 0 ? count : 0));
      return;
    }
    const appended = appendedMessageCount(previous, next);
    if (appended > 0) {
      setNewMessageCount((count) => count + appended);
    }
  }, [bottomStick.mode, transcript.turnVMs]);

  const handleJumpLatest = useCallback(() => {
    setNewMessageCount(0);
    bottomStick.enterBottomMode({ stabilizeFrames: 2 });
  }, [bottomStick.enterBottomMode]);

  return (
    <TranscriptView
      contentRef={bottomStick.contentRef}
      disclosure={disclosure}
      hasItems={transcript.hasItems}
      isError={turnsQuery.isError}
      isFetchingNextPage={turnsQuery.isFetchingNextPage}
      hasMoreHistory={Boolean(turnsQuery.hasNextPage)}
      isLoading={turnsQuery.isLoading}
      isLoadingHistory={turnsQuery.isFetchingNextPage}
      isPending={turnsQuery.isPending}
      newMessageCount={newMessageCount}
      showJumpLatest={bottomStick.showJumpLatest}
      submitError={submitError}
      turns={transcript.turnVMs}
      viewportRef={viewportRef}
      onAssistantContentGrow={bottomStick.stickToBottomIfNeeded}
      onAssistantRevealComplete={handleAssistantRevealComplete}
      onJumpLatest={handleJumpLatest}
      onLoadHistory={turnsQuery.fetchNextPage}
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
  return (turn.user ? 1 : 0) + (turn.assistant && hasAssistantMessageContent(turn.assistant) ? 1 : 0);
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
