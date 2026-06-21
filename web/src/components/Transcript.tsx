import { ArrowDown, CircleAlert, Loader2 } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { listQueuedInputs, updateQueuedInput, type QueuedInput } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChatColumn } from "@/components/ChatColumn";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { TranscriptList } from "@/components/transcript/TranscriptList";
import { useTranscriptTurns } from "@/components/transcript/useTranscriptTurns";
import { useTranscriptViewModel } from "@/components/transcript/useTranscriptViewModel";
import { useTranscriptScroll } from "@/hooks/useTranscriptScroll";
import { useI18n } from "@/i18n";
import { type PendingUserMessage, useOverlayStore } from "@/state/overlayStore";

const HISTORY_PRELOAD_MARGIN_PX = 640;
const EMPTY_PENDING: PendingUserMessage[] = [];
const EMPTY_QUEUED: QueuedInput[] = [];

type TranscriptProps = {
  token: string;
  sessionID: string;
  sessionRunning?: boolean;
  submitError?: string | null;
};

export function Transcript({ token, sessionID, sessionRunning = false, submitError }: TranscriptProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const markAssistantRevealed = useOverlayStore((state) => state.markAssistantRevealed);
  const reconcileMessages = useOverlayStore((state) => state.reconcileMessages);
  const pendingUsersBySession = useOverlayStore((state) => state.pendingUsers);
  const assistantsByID = useOverlayStore((state) => state.assistants);
  const turnPhase = useOverlayStore((state) => state.turnPhases[sessionID]);
  const { messages, query: turnsQuery, turnDurationByID, turns } = useTranscriptTurns(token, sessionID);
  const [disclosureByKey, setDisclosureByKey] = useState<Record<string, boolean>>({});
  const queuedInputsQuery = useQuery({
    queryKey: queryKeys.queuedInputs(sessionID),
    queryFn: () => listQueuedInputs(token, sessionID),
    enabled: Boolean(token && sessionID),
  });
  const pendingUsers = useMemo(
    () => mergePendingUsers(queuedInputsQuery.data?.queuedInputs || EMPTY_QUEUED, pendingUsersBySession[sessionID] || EMPTY_PENDING),
    [pendingUsersBySession, queuedInputsQuery.data?.queuedInputs, sessionID],
  );
  const assistantOverlays = useMemo(
    () => Object.values(assistantsByID).filter((overlay) => overlay.sessionID === sessionID),
    [assistantsByID, sessionID],
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
  const scroll = useTranscriptScroll({
    historyLoader: {
      hasMore: Boolean(turnsQuery.hasNextPage),
      isLoading: turnsQuery.isFetchingNextPage,
      loadMore: turnsQuery.fetchNextPage,
      preloadMarginPx: HISTORY_PRELOAD_MARGIN_PX,
    },
    itemKeys: transcript.itemKeys,
    sessionID,
  });
  const pendingIDsRef = useRef<Set<string>>(new Set());
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

  useEffect(() => {
    if (!turnsQuery.isSuccess) {
      return;
    }
    reconcileMessages(sessionID, messages);
  }, [messages, reconcileMessages, sessionID, turnsQuery.isSuccess]);

  const handleAssistantRevealComplete = useCallback(
    (turnID: string) => {
      markAssistantRevealed(turnID);
      window.requestAnimationFrame(() => {
        scroll.stickToBottomIfNeeded({ stabilizeFrames: 4 });
      });
    },
    [markAssistantRevealed, scroll.stickToBottomIfNeeded],
  );

  useLayoutEffect(() => {
    const previous = pendingIDsRef.current;
    const next = new Set(pendingUsers.map((message) => message.clientMessageID));
    pendingIDsRef.current = next;
    if (pendingUsers.some((message) => !previous.has(message.clientMessageID))) {
      scroll.enterBottomMode({ stabilizeFrames: 1 });
    }
  }, [pendingUsers, scroll.enterBottomMode]);

  if (turnsQuery.isPending) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground">
        <Loader2 className="size-5 animate-spin" aria-label={t("common.loading")} />
      </div>
    );
  }

  if (!turnsQuery.isLoading && !turnsQuery.isError && !transcript.hasItems && !submitError) {
    return <div className="min-h-0 flex-1" />;
  }

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden">
      <div
        ref={scroll.viewportRef}
        className="h-full overflow-y-auto overscroll-contain [overflow-anchor:none]"
        data-transcript-viewport
      >
        <div ref={scroll.contentRef}>
          <ChatColumn className="grid gap-4 pt-4 pb-8">
            <div className="h-px" aria-hidden="true" />
            {turnsQuery.isFetchingNextPage ? (
              <div className="flex justify-center py-2 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" aria-label={t("common.loading")} />
              </div>
            ) : null}
            {turnsQuery.isError ? (
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{t("transcript.loadFailed")}</AlertDescription>
              </Alert>
            ) : null}
            <TranscriptList
              disclosure={disclosure}
              onQueuedCancel={(clientMessageID) => updateQueued(clientMessageID, { status: "cancelled" })}
              onQueuedEditStart={(clientMessageID) => updateQueued(clientMessageID, { status: "editing" })}
              onQueuedSave={(clientMessageID, text) => updateQueued(clientMessageID, { status: "queued", text })}
              onAssistantContentGrow={scroll.stickToBottomIfNeeded}
              onAssistantRevealComplete={handleAssistantRevealComplete}
              turns={transcript.turnVMs}
            />
            {submitError ? (
              <Alert variant="destructive">
                <CircleAlert className="h-3.5 w-3.5" />
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            ) : null}
          </ChatColumn>
        </div>
      </div>
      {scroll.showJumpLatest && transcript.hasItems ? (
        <Button
          aria-label={t("transcript.jumpLatest")}
          className="absolute right-5 bottom-5 rounded-full border border-border bg-card shadow-md hover:bg-muted"
          size="icon"
          type="button"
          variant="ghost"
          onClick={() => scroll.enterBottomMode({ stabilizeFrames: 1 })}
        >
          <ArrowDown />
        </Button>
      ) : null}
    </div>
  );
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
