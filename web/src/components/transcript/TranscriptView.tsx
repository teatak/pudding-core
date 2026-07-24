import { ArrowDown, CircleAlert } from "lucide-react";
import { useState } from "react";

import { ChatColumn } from "@/components/ChatColumn";
import { Spinner } from "@/components/Spinner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useScopedSelectAll } from "@/hooks/useScopedSelectAll";
import { useI18n } from "@/i18n";

import { TranscriptList } from "./TranscriptList";
import type { TranscriptDisplaySettings, TranscriptTurnVM, TurnDisclosureState } from "./types";

export function TranscriptView({
  disclosure,
  displaySettings,
  hasMoreHistory,
  hasItems,
  isError,
  isFetchingNextPage,
  isLoading,
  isLoadingHistory,
  isPending,
  jumpLatestSignal,
  newMessageCount,
  onAssistantRevealComplete,
  onJumpLatest,
  onLatestChange,
  onLoadHistory,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSteer,
  onQueuedSave,
  sessionID,
  showJumpLatest,
  submitError,
  token,
  turns,
}: {
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  hasMoreHistory: boolean;
  hasItems: boolean;
  isError: boolean;
  isFetchingNextPage: boolean;
  isLoading: boolean;
  isLoadingHistory: boolean;
  isPending: boolean;
  jumpLatestSignal: number;
  newMessageCount: number;
  onAssistantRevealComplete?: (turnID: string) => void;
  onJumpLatest: () => void;
  onLatestChange?: (isAtLatest: boolean) => void;
  onLoadHistory: () => Promise<unknown> | void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSteer?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  sessionID: string;
  showJumpLatest: boolean;
  submitError?: string | null;
  token: string;
  turns: TranscriptTurnVM[];
}) {
  const { t } = useI18n();
  const [viewportNode, setViewportNode] = useState<HTMLDivElement | null>(null);
  useScopedSelectAll(viewportNode);
  const jumpLatestLabel =
    newMessageCount > 0 ? t("transcript.newMessages").replace("{count}", String(newMessageCount)) : t("transcript.jumpLatest");

  if (isPending) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground">
        <Spinner className="size-5" aria-label={t("common.loading")} />
      </div>
    );
  }

  if (!isLoading && !isError && !hasItems && !submitError) {
    return <div className="min-h-0 flex-1" />;
  }

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden">
      <div
        ref={setViewportNode}
        className="pudding-transcript-viewport h-full overflow-x-hidden overflow-y-auto overscroll-none [contain:strict] [overflow-anchor:none]"
        data-select-all-scope="transcript"
        data-transcript-viewport
      >
        <ChatColumn>
          {isError ? (
            <Alert className="mb-4 min-w-0" variant="destructive">
              <CircleAlert className="h-3.5 w-3.5" />
              <AlertDescription className="min-w-0 overflow-hidden break-words">{t("transcript.loadFailed")}</AlertDescription>
            </Alert>
          ) : null}
          <TranscriptList
            disclosure={disclosure}
            displaySettings={displaySettings}
            hasMoreHistory={hasMoreHistory}
            isLoadingHistory={isLoadingHistory}
            jumpLatestSignal={jumpLatestSignal}
            onAssistantRevealComplete={onAssistantRevealComplete}
            onLatestChange={onLatestChange}
            onLoadHistory={onLoadHistory}
            onQueuedCancel={onQueuedCancel}
            onQueuedEditStart={onQueuedEditStart}
            onQueuedSteer={onQueuedSteer}
            onQueuedSave={onQueuedSave}
            scrollElement={viewportNode}
            sessionID={sessionID}
            token={token}
            turns={turns}
          />
          {submitError ? (
            <Alert className="mt-4 min-w-0" variant="destructive">
              <CircleAlert className="h-3.5 w-3.5" />
              <AlertDescription className="min-w-0 overflow-hidden break-words">{submitError}</AlertDescription>
            </Alert>
          ) : null}
        </ChatColumn>
      </div>
      {isFetchingNextPage ? (
        <div className="pointer-events-none absolute inset-x-0 top-3 z-20 flex justify-center text-muted-foreground">
          <div className="rounded-full border border-border bg-card/90 px-2 py-1 shadow-sm">
            <Spinner className="size-3.5" aria-label={t("common.loading")} />
          </div>
        </div>
      ) : null}
      {showJumpLatest && hasItems ? (
        <Button
          aria-label={jumpLatestLabel}
          className={
            newMessageCount > 0
              ? "absolute right-5 bottom-5 z-20 h-9 gap-1.5 rounded-full px-3 text-sm font-semibold shadow-md [&_svg]:size-4"
              : "absolute right-5 bottom-5 z-20 rounded-full border border-border bg-card shadow-md hover:bg-muted"
          }
          size={newMessageCount > 0 ? "default" : "icon"}
          type="button"
          variant={newMessageCount > 0 ? "default" : "ghost"}
          onClick={onJumpLatest}
        >
          <ArrowDown />
          {newMessageCount > 0 ? <span>{jumpLatestLabel}</span> : null}
        </Button>
      ) : null}
    </div>
  );
}
