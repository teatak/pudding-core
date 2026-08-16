import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { cloneSessionAtMessage, getSettings, type ContentPart, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { TranscriptView } from "@/components/transcript/TranscriptView";
import type { TranscriptSearchState, TranscriptTurnVM } from "@/components/transcript/types";
import { useTranscriptData } from "@/components/transcript/useTranscriptData";
import { useI18n } from "@/i18n";
import { transcriptDisplaySettings } from "@/lib/appSettings";
import type { AppSearch } from "@/lib/route";
import { useOverlayStore } from "@/state/overlayStore";
import {
  consumeTranscriptTurnReveal,
  useTranscriptTurnReveal,
} from "@/state/transcriptRevealStore";

type TranscriptProps = {
  token: string;
  sessionID: string;
  sessionRunning?: boolean;
  searchSlot: "primary" | "split";
  searchState: TranscriptSearchState;
  submitSignal?: number;
};

export function Transcript({
  searchSlot,
  searchState,
  sessionID,
  sessionRunning = false,
  submitSignal = 0,
  token,
}: TranscriptProps) {
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const disclosureByKeyRef = useRef<Record<string, boolean>>({});
  const [isAtLatest, setIsAtLatest] = useState(true);
  const [jumpLatestSignal, setJumpLatestSignal] = useState(0);
  const [newMessageCount, setNewMessageCount] = useState(0);
  const turnReveal = useTranscriptTurnReveal(sessionID);
  const runningTurnID = useOverlayStore((state) => state.runningTurns[sessionID]);
  const { hasMoreHistory, isLoadingHistory, loadHistory, markAssistantRevealed, revealTurn, steerQueued, transcript, turnsQuery, updateQueued } =
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
  const cloneMutation = useMutation({
    mutationFn: (messageID: string) => cloneSessionAtMessage(token, sessionID, messageID, t("session.cloneTitleSuffix")),
    onSuccess: async (cloned) => {
      queryClient.setQueryData<Session>(queryKeys.session(cloned.id), cloned);
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => ({
        sessions: [cloned, ...(previous?.sessions.filter((session) => session.id !== cloned.id) || [])],
      }));
      await navigate({
        to: "/",
        search: (previous) => {
          const next = { ...(previous as AppSearch) };
          if (searchSlot === "split") {
            next.split = cloned.id;
          } else {
            next.session = cloned.id;
            delete next.draft;
            delete next.project;
          }
          return next;
        },
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: () => toast.error(t("transcript.cloneFailed")),
  });
  const displaySettings = useMemo(
    () => transcriptDisplaySettings(settingsQuery.data?.settings),
    [settingsQuery.data?.settings],
  );
  const sessionIDRef = useRef(sessionID);
  const turnVMsRef = useRef<TranscriptTurnVM[]>([]);
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
  const moveToLatest = useCallback(() => {
    setNewMessageCount(0);
    setIsAtLatest(true);
    setJumpLatestSignal((signal) => signal + 1);
  }, []);
  const guideQueued = useCallback(
    (clientMessageID: string) => {
      if (!runningTurnID) {
        return Promise.reject(new Error("turn_not_active"));
      }
      moveToLatest();
      return steerQueued(clientMessageID, runningTurnID);
    },
    [moveToLatest, runningTurnID, steerQueued],
  );
  const disclosure = useMemo(
    () => ({
      hasState: (key: string) =>
        Object.prototype.hasOwnProperty.call(disclosureByKeyRef.current, `${sessionIDRef.current}:${key}`),
      isOpen: (key: string) => Boolean(disclosureByKeyRef.current[`${sessionIDRef.current}:${key}`]),
      setOpen: (key: string, open: boolean) => {
        const scopedKey = `${sessionIDRef.current}:${key}`;
        // Keep explicit false values so a streamed parent group cannot reopen after the user closes it.
        disclosureByKeyRef.current[scopedKey] = open;
      },
    }),
    [],
  );

  const handleAssistantRevealComplete = useCallback((turnID: string) => markAssistantRevealed(turnID), [markAssistantRevealed]);
  const handleCloneMessage = useCallback((messageID: string) => cloneMutation.mutate(messageID), [cloneMutation.mutate]);
  const handleTurnRevealComplete = useCallback(
    (serial: number) => consumeTranscriptTurnReveal(sessionID, serial),
    [sessionID],
  );

  useEffect(() => {
    if (!turnReveal || !turnsQuery.isSuccess) {
      return;
    }
    let active = true;
    void revealTurn(turnReveal.turnID).then((found) => {
      if (active && !found) {
        consumeTranscriptTurnReveal(sessionID, turnReveal.serial);
      }
    });
    return () => {
      active = false;
    };
  }, [revealTurn, sessionID, turnReveal, turnsQuery.isSuccess]);

  useEffect(() => {
    if (!searchState.target || !turnsQuery.isSuccess) {
      return;
    }
    void revealTurn(searchState.target.turnID);
  }, [revealTurn, searchState.target, turnsQuery.isSuccess]);

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

  useLayoutEffect(() => {
    if (submitSignal > 0) {
      moveToLatest();
    }
  }, [moveToLatest, submitSignal]);

  const handleLatestChange = useCallback((next: boolean) => {
    setIsAtLatest(next);
    if (next) {
      setNewMessageCount(0);
    }
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
      searchSlot={searchSlot}
      searchState={searchState}
      sessionID={sessionID}
      showJumpLatest={!isAtLatest}
      token={token}
      turnReveal={turnReveal}
      turns={transcript.turnVMs}
      onAssistantRevealComplete={handleAssistantRevealComplete}
      onCloneMessage={handleCloneMessage}
      onJumpLatest={moveToLatest}
      onLatestChange={handleLatestChange}
      onLoadHistory={loadHistory}
      onTurnRevealComplete={handleTurnRevealComplete}
      onQueuedCancel={cancelQueued}
      onQueuedEditStart={startQueuedEdit}
      onQueuedSteer={runningTurnID ? guideQueued : undefined}
      onQueuedSave={saveQueued}
      cloningMessageID={cloneMutation.isPending ? cloneMutation.variables : undefined}
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
