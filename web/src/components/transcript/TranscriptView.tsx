import { ArrowDown, CircleAlert } from "@/components/icons";
import { useState } from "react";

import { ChatColumn } from "@/components/ChatColumn";
import { Spinner } from "@/components/Spinner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useScopedSelectAll } from "@/hooks/useScopedSelectAll";
import { useI18n } from "@/i18n";
import type { TranscriptTurnReveal } from "@/state/transcriptRevealStore";

import { TranscriptList } from "./TranscriptList";
import type {
  TranscriptDisplaySettings,
  TranscriptSearchState,
  TranscriptTurnVM,
  TurnDisclosureState,
} from "./types";

export function TranscriptView({
  disclosure,
  displaySettings,
  cloningMessageID,
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
  onCloneMessage,
  onJumpLatest,
  onLatestChange,
  onLoadHistory,
  onTurnRevealComplete,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSteer,
  onQueuedSave,
  searchSlot,
  searchState,
  sessionID,
  showJumpLatest,
  token,
  turnReveal,
  turns,
}: {
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  cloningMessageID?: string;
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
  onCloneMessage?: (messageID: string) => void;
  onJumpLatest: () => void;
  onLatestChange?: (isAtLatest: boolean) => void;
  onLoadHistory: () => Promise<unknown> | void;
  onTurnRevealComplete?: (serial: number) => void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSteer?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  searchSlot: "primary" | "split";
  searchState: TranscriptSearchState;
  sessionID: string;
  showJumpLatest: boolean;
  token: string;
  turnReveal?: TranscriptTurnReveal;
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

  if (!isLoading && !isError && !hasItems) {
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
            cloningMessageID={cloningMessageID}
            hasMoreHistory={hasMoreHistory}
            isLoadingHistory={isLoadingHistory}
            jumpLatestSignal={jumpLatestSignal}
            onAssistantRevealComplete={onAssistantRevealComplete}
            onCloneMessage={onCloneMessage}
            onLatestChange={onLatestChange}
            onLoadHistory={onLoadHistory}
            onTurnRevealComplete={onTurnRevealComplete}
            onQueuedCancel={onQueuedCancel}
            onQueuedEditStart={onQueuedEditStart}
            onQueuedSteer={onQueuedSteer}
            onQueuedSave={onQueuedSave}
            scrollElement={viewportNode}
            searchSlot={searchSlot}
            searchState={searchState}
            sessionID={sessionID}
            token={token}
            turnReveal={turnReveal}
            turns={turns}
          />
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
              ? "absolute right-5 z-40 h-9 gap-1.5 rounded-full px-3 text-sm font-semibold shadow-md [&_svg]:size-4"
              : "absolute right-5 z-40 rounded-full border border-border bg-card shadow-md hover:bg-muted"
          }
          size={newMessageCount > 0 ? "default" : "icon"}
          style={{ bottom: "calc(var(--pudding-composer-overlay-height, 0px) + 1.25rem)" }}
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
