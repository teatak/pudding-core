import { useVirtualizer } from "@tanstack/react-virtual";
import { memo, useCallback, useEffect, useRef } from "react";

import { TranscriptTurn } from "./TranscriptTurn";
import type { TranscriptTurnVM, TurnDisclosureState } from "./types";

const ESTIMATED_TURN_HEIGHT = 180;
const HISTORY_LOAD_SCROLL_TOP_PX = 120;
const LIST_PADDING_BOTTOM_PX = 32;
const LIST_PADDING_TOP_PX = 16;
const TURN_GAP_PX = 16;
const TURN_OVERSCAN = 6;

export const TranscriptList = memo(function TranscriptList({
  disclosure,
  hasMoreHistory,
  isLoadingHistory,
  onAssistantContentGrow,
  onAssistantRevealComplete,
  onLoadHistory,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  scrollElement,
  turns,
}: {
  disclosure?: TurnDisclosureState;
  hasMoreHistory: boolean;
  isLoadingHistory: boolean;
  onAssistantContentGrow?: () => void;
  onAssistantRevealComplete?: (turnID: string) => void;
  onLoadHistory: () => Promise<unknown> | void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  scrollElement: HTMLDivElement | null;
  turns: TranscriptTurnVM[];
}) {
  const loaderRef = useRef({ hasMoreHistory, isLoadingHistory, onLoadHistory });
  const loadingLockedRef = useRef(false);
  const virtualizer = useVirtualizer({
    anchorTo: "end",
    count: turns.length,
    directDomUpdates: true,
    estimateSize: () => ESTIMATED_TURN_HEIGHT,
    followOnAppend: true,
    gap: TURN_GAP_PX,
    getItemKey: (index) => turns[index]?.key || index,
    getScrollElement: () => scrollElement,
    overscan: TURN_OVERSCAN,
    paddingEnd: LIST_PADDING_BOTTOM_PX,
    paddingStart: LIST_PADDING_TOP_PX,
    scrollEndThreshold: 80,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualItems = virtualizer.getVirtualItems();

  useEffect(() => {
    loaderRef.current = { hasMoreHistory, isLoadingHistory, onLoadHistory };
  }, [hasMoreHistory, isLoadingHistory, onLoadHistory]);

  const maybeLoadHistory = useCallback((node: HTMLDivElement) => {
    const loader = loaderRef.current;
    if (
      loadingLockedRef.current ||
      loader.isLoadingHistory ||
      !loader.hasMoreHistory ||
      node.scrollTop > HISTORY_LOAD_SCROLL_TOP_PX
    ) {
      return;
    }

    loadingLockedRef.current = true;
    void Promise.resolve(loader.onLoadHistory())
      .catch(() => undefined)
      .finally(() => {
        loadingLockedRef.current = false;
      });
  }, []);

  useEffect(() => {
    const node = scrollElement;
    if (!node) {
      return;
    }
    const onScroll = () => {
      maybeLoadHistory(node);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target)) {
        return;
      }
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") {
        maybeLoadHistory(node);
      }
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    return () => {
      node.removeEventListener("scroll", onScroll);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [maybeLoadHistory, scrollElement]);

  if (turns.length === 0) {
    return null;
  }

  return (
    <div ref={virtualizer.containerRef} className="relative min-w-0">
      {virtualItems.map((virtualItem) => {
        const turn = turns[virtualItem.index];
        if (!turn) {
          return null;
        }
        return (
          <div
            key={virtualItem.key}
            ref={virtualizer.measureElement}
            className="absolute top-0 left-0 w-full min-w-0"
            data-index={virtualItem.index}
          >
            <TranscriptTurn
              disclosure={disclosure}
              onAssistantContentGrow={onAssistantContentGrow}
              onAssistantRevealComplete={onAssistantRevealComplete}
              onQueuedCancel={onQueuedCancel}
              onQueuedEditStart={onQueuedEditStart}
              onQueuedSave={onQueuedSave}
              turn={turn}
            />
          </div>
        );
      })}
    </div>
  );
});

function isEditableTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  if (target.isContentEditable) {
    return true;
  }
  const tagName = target.tagName.toLowerCase();
  return tagName === "input" || tagName === "textarea" || tagName === "select";
}
