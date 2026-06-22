import { ArrowDown, CircleAlert, Loader2 } from "lucide-react";
import { useCallback, useState, type Ref } from "react";

import { ChatColumn } from "@/components/ChatColumn";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";

import { TranscriptList } from "./TranscriptList";
import type { TranscriptTurnVM, TurnDisclosureState } from "./types";

export function TranscriptView({
  contentRef,
  disclosure,
  hasMoreHistory,
  hasItems,
  isError,
  isFetchingNextPage,
  isLoading,
  isLoadingHistory,
  isPending,
  newMessageCount,
  onAssistantContentGrow,
  onAssistantRevealComplete,
  onJumpLatest,
  onLoadHistory,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  showJumpLatest,
  submitError,
  turns,
  viewportRef,
}: {
  contentRef: Ref<HTMLDivElement>;
  disclosure?: TurnDisclosureState;
  hasMoreHistory: boolean;
  hasItems: boolean;
  isError: boolean;
  isFetchingNextPage: boolean;
  isLoading: boolean;
  isLoadingHistory: boolean;
  isPending: boolean;
  newMessageCount: number;
  onAssistantContentGrow?: () => void;
  onAssistantRevealComplete?: (turnID: string) => void;
  onJumpLatest: () => void;
  onLoadHistory: () => Promise<unknown> | void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  showJumpLatest: boolean;
  submitError?: string | null;
  turns: TranscriptTurnVM[];
  viewportRef: Ref<HTMLDivElement>;
}) {
  const { t } = useI18n();
  const [viewportNode, setViewportNode] = useState<HTMLDivElement | null>(null);
  const handleViewportRef = useCallback(
    (node: HTMLDivElement | null) => {
      setViewportNode(node);
      setRef(viewportRef, node);
    },
    [viewportRef],
  );
  const jumpLatestLabel =
    newMessageCount > 0 ? t("transcript.newMessages").replace("{count}", String(newMessageCount)) : t("transcript.jumpLatest");

  if (isPending) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground">
        <Loader2 className="size-5 animate-spin" aria-label={t("common.loading")} />
      </div>
    );
  }

  if (!isLoading && !isError && !hasItems && !submitError) {
    return <div className="min-h-0 flex-1" />;
  }

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden">
      <div
        ref={handleViewportRef}
        className="h-full overflow-y-auto overscroll-none [contain:strict] [overflow-anchor:none]"
        data-transcript-viewport
      >
        <ChatColumn innerRef={contentRef}>
          {isError ? (
            <Alert className="mb-4" variant="destructive">
              <CircleAlert className="h-3.5 w-3.5" />
              <AlertDescription>{t("transcript.loadFailed")}</AlertDescription>
            </Alert>
          ) : null}
          <TranscriptList
            disclosure={disclosure}
            hasMoreHistory={hasMoreHistory}
            isLoadingHistory={isLoadingHistory}
            onAssistantContentGrow={onAssistantContentGrow}
            onAssistantRevealComplete={onAssistantRevealComplete}
            onLoadHistory={onLoadHistory}
            onQueuedCancel={onQueuedCancel}
            onQueuedEditStart={onQueuedEditStart}
            onQueuedSave={onQueuedSave}
            scrollElement={viewportNode}
            turns={turns}
          />
          {submitError ? (
            <Alert className="mt-4" variant="destructive">
              <CircleAlert className="h-3.5 w-3.5" />
              <AlertDescription>{submitError}</AlertDescription>
            </Alert>
          ) : null}
        </ChatColumn>
      </div>
      {isFetchingNextPage ? (
        <div className="pointer-events-none absolute inset-x-0 top-3 z-20 flex justify-center text-muted-foreground">
          <div className="rounded-full border border-border bg-card/90 px-2 py-1 shadow-sm">
            <Loader2 className="size-3.5 animate-spin" aria-label={t("common.loading")} />
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

function setRef<T>(ref: Ref<T>, value: T | null) {
  if (typeof ref === "function") {
    ref(value);
    return;
  }
  if (ref) {
    (ref as { current: T | null }).current = value;
  }
}
