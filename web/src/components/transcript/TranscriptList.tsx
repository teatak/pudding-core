import { useVirtualizer } from "@tanstack/react-virtual";
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { TranscriptTurn } from "./TranscriptTurn";
import type { TranscriptTurnVM, TurnDisclosureState } from "./types";

const ESTIMATED_TURN_HEIGHT = 180;
const HISTORY_LOAD_SCROLL_TOP_PX = 120;
const LIST_PADDING_BOTTOM_PX = 32;
const LIST_PADDING_TOP_PX = 16;
const TURN_GAP_PX = 16;
const TURN_OVERSCAN = 6;
const SCROLL_END_THRESHOLD_PX = 8;
const ANCHOR_RESTORE_EPSILON_PX = 0.75;
const BOTTOM_STICK_STABILIZE_FRAMES = 4;
const CONTENT_STICK_STABILIZE_FRAMES = 8;

type HistoryLoadState = "idle" | "loading" | "settling";
type DisclosureOpenState = { openedAtLatest: boolean; userScrollSeq: number };
type ViewportAnchor = { top: number; turnID: string };

export const TranscriptList = memo(function TranscriptList({
  disclosure,
  hasMoreHistory,
  isLoadingHistory,
  jumpLatestSignal,
  onAssistantRevealComplete,
  onLatestChange,
  onLoadHistory,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  scrollElement,
  sessionID,
  turns,
}: {
  disclosure?: TurnDisclosureState;
  hasMoreHistory: boolean;
  isLoadingHistory: boolean;
  jumpLatestSignal: number;
  onAssistantRevealComplete?: (turnID: string) => void;
  onLatestChange?: (isAtLatest: boolean) => void;
  onLoadHistory: () => Promise<unknown> | void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  scrollElement: HTMLDivElement | null;
  sessionID: string;
  turns: TranscriptTurnVM[];
}) {
  const autoStickRef = useRef(true);
  const isAtLatestRef = useRef(true);
  const initialScrollSessionRef = useRef("");
  const stickRafRef = useRef<number | null>(null);
  const stickRunRef = useRef(0);
  const lastScrollTopRef = useRef(0);
  const programmaticScrollIgnoreUntilRef = useRef(0);
  const userScrollSeqRef = useRef(0);
  const viewportAnchorRef = useRef<ViewportAnchor | null>(null);
  const disclosureOpenStateRef = useRef(new Map<string, DisclosureOpenState>());
  const [listElement, setListElement] = useState<HTMLDivElement | null>(null);
  const virtualizer = useVirtualizer({
    anchorTo: "end",
    count: turns.length,
    directDomUpdates: true,
    estimateSize: (index) => estimateTurnHeight(turns[index]),
    followOnAppend: true,
    gap: TURN_GAP_PX,
    getItemKey: (index) => turns[index]?.key || index,
    getScrollElement: () => scrollElement,
    overscan: TURN_OVERSCAN,
    paddingEnd: LIST_PADDING_BOTTOM_PX,
    paddingStart: LIST_PADDING_TOP_PX,
    scrollEndThreshold: SCROLL_END_THRESHOLD_PX,
    useAnimationFrameWithResizeObserver: true,
  });
  virtualizer.shouldAdjustScrollPositionOnItemSizeChange = disableVirtualizerSizeAdjustment;
  const virtualItems = virtualizer.getVirtualItems();
  const setVirtualizerContainer = useCallback(
    (node: HTMLDivElement | null) => {
      virtualizer.containerRef(node);
      setListElement((current) => (current === node ? current : node));
    },
    [virtualizer],
  );

  const setLatestState = useCallback(
    (next: boolean) => {
      if (isAtLatestRef.current === next) {
        return;
      }
      isAtLatestRef.current = next;
      onLatestChange?.(next);
    },
    [onLatestChange],
  );

  const cancelScheduledStick = useCallback(() => {
    stickRunRef.current += 1;
    if (stickRafRef.current !== null) {
      window.cancelAnimationFrame(stickRafRef.current);
      stickRafRef.current = null;
    }
  }, []);

  const disableAutoStick = useCallback(() => {
    autoStickRef.current = false;
    viewportAnchorRef.current = captureViewportAnchor(scrollElement);
    cancelScheduledStick();
  }, [cancelScheduledStick, scrollElement]);

  const updatePinned = useCallback(() => {
    if (!scrollElement) {
      return;
    }
    if (distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX) {
      autoStickRef.current = true;
      viewportAnchorRef.current = null;
      setLatestState(true);
    }
  }, [scrollElement, setLatestState]);

  const scrollToLatest = useCallback(() => {
    if (!scrollElement) {
      return;
    }
    if (distanceFromBottom(scrollElement) > ANCHOR_RESTORE_EPSILON_PX) {
      virtualizer.scrollToEnd();
      scrollElement.scrollTop = Math.max(0, scrollElement.scrollHeight - scrollElement.clientHeight);
    }
    lastScrollTopRef.current = scrollElement.scrollTop;
    autoStickRef.current = true;
    viewportAnchorRef.current = null;
    setLatestState(true);
  }, [scrollElement, setLatestState, virtualizer]);

  const settleAfterDisclosureClose = useCallback(
    (shouldRestoreLatest: boolean) => {
      let remaining = BOTTOM_STICK_STABILIZE_FRAMES;
      const tick = () => {
        if (!scrollElement) {
          return;
        }
        if (distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX) {
          autoStickRef.current = true;
          viewportAnchorRef.current = null;
          setLatestState(true);
          lastScrollTopRef.current = scrollElement.scrollTop;
          return;
        }
        if (shouldRestoreLatest) {
          scrollToLatest();
          remaining -= 1;
          if (remaining > 0) {
            window.requestAnimationFrame(tick);
          }
          return;
        }
        autoStickRef.current = false;
        viewportAnchorRef.current = captureViewportAnchor(scrollElement);
        setLatestState(false);
      };
      window.requestAnimationFrame(tick);
    },
    [scrollElement, scrollToLatest, setLatestState],
  );

  const handleDisclosureOpenChange = useCallback(
    (key: string, open: boolean) => {
      if (!disclosure || disclosure.isOpen(key) === open) {
        return;
      }

      if (open) {
        disclosureOpenStateRef.current.set(key, {
          openedAtLatest: scrollElement
            ? distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX || isAtLatestRef.current
            : isAtLatestRef.current,
          userScrollSeq: userScrollSeqRef.current,
        });
        disableAutoStick();
        disclosure.setOpen(key, true);
        return;
      }

      const openState = disclosureOpenStateRef.current.get(key);
      disclosureOpenStateRef.current.delete(key);
      const currentlyAtLatest = scrollElement
        ? distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX || isAtLatestRef.current
        : isAtLatestRef.current;
      disclosure.setOpen(key, false);
      const userScrolledDuringDisclosure = openState ? userScrollSeqRef.current !== openState.userScrollSeq : false;
      settleAfterDisclosureClose(currentlyAtLatest || (Boolean(openState?.openedAtLatest) && !userScrolledDuringDisclosure));
    },
    [
      disableAutoStick,
      disclosure,
      scrollElement,
      settleAfterDisclosureClose,
    ],
  );

  const listDisclosure = useMemo<TurnDisclosureState | undefined>(() => {
    if (!disclosure) {
      return undefined;
    }
    return {
      isOpen: disclosure.isOpen,
      setOpen: handleDisclosureOpenChange,
    };
  }, [disclosure, handleDisclosureOpenChange]);

  const stickToLatestIfPinned = useCallback(
    (frames = 1) => {
      if (!autoStickRef.current) {
        return;
      }
      const run = stickRunRef.current + 1;
      stickRunRef.current = run;
      if (stickRafRef.current !== null) {
        window.cancelAnimationFrame(stickRafRef.current);
        stickRafRef.current = null;
      }
      let remaining = frames;
      const tick = () => {
        stickRafRef.current = null;
        if (!autoStickRef.current || stickRunRef.current !== run) {
          return;
        }
        scrollToLatest();
        remaining -= 1;
        if (remaining > 0) {
          stickRafRef.current = window.requestAnimationFrame(tick);
        }
      };
      tick();
    },
    [scrollToLatest],
  );

  const loadHistory = useCallback(async () => {
    const anchor = captureViewportAnchor(scrollElement);
    const result = await onLoadHistory();
    restoreViewportAnchorOverFrames(scrollElement, anchor);
    return result;
  }, [onLoadHistory, scrollElement]);

  const isNearTop = useCallback(() => Boolean(scrollElement && scrollElement.scrollTop < HISTORY_LOAD_SCROLL_TOP_PX), [scrollElement]);
  const historyLoader = useHistoryLoadController({
    getScrollElement: () => scrollElement,
    hasMore: hasMoreHistory,
    isLoading: isLoadingHistory,
    isNearTop,
    loadMore: loadHistory,
  });

  const handleAssistantContentGrow = useCallback(() => {
    stickToLatestIfPinned(CONTENT_STICK_STABILIZE_FRAMES);
  }, [stickToLatestIfPinned]);

  const handleContentResize = useCallback(() => {
    if (autoStickRef.current) {
      stickToLatestIfPinned(CONTENT_STICK_STABILIZE_FRAMES);
      return;
    }
    viewportAnchorRef.current = captureViewportAnchor(scrollElement);
    if (scrollElement && distanceFromBottom(scrollElement) > SCROLL_END_THRESHOLD_PX) {
      setLatestState(false);
    }
  }, [scrollElement, setLatestState, stickToLatestIfPinned]);

  useEffect(() => {
    if (!scrollElement) {
      return;
    }
    lastScrollTopRef.current = scrollElement.scrollTop;
  }, [scrollElement]);

  useEffect(() => {
    if (!scrollElement || turns.length === 0 || initialScrollSessionRef.current === sessionID) {
      return;
    }
    initialScrollSessionRef.current = sessionID;
    window.requestAnimationFrame(scrollToLatest);
  }, [scrollElement, scrollToLatest, sessionID, turns.length]);

  useEffect(() => {
    if (jumpLatestSignal <= 0) {
      return;
    }
    window.requestAnimationFrame(scrollToLatest);
  }, [jumpLatestSignal, scrollToLatest]);

  useEffect(() => {
    const node = scrollElement;
    if (!node) {
      return;
    }
    const onScroll = () => {
      const previousScrollTop = lastScrollTopRef.current;
      const nextScrollTop = node.scrollTop;
      const movingUp = nextScrollTop < previousScrollTop - 1;
      const movingDown = nextScrollTop > previousScrollTop + 1;
      const nearTop = nextScrollTop < HISTORY_LOAD_SCROLL_TOP_PX;
      const bottomDistance = distanceFromBottom(node);
      lastScrollTopRef.current = nextScrollTop;
      const isProgrammaticScroll = performance.now() < programmaticScrollIgnoreUntilRef.current;
      if (!autoStickRef.current) {
        viewportAnchorRef.current = captureViewportAnchor(node);
      }
      if (!autoStickRef.current && !isProgrammaticScroll && (movingUp || movingDown)) {
        userScrollSeqRef.current += 1;
      }
      if (movingUp || nearTop) {
        historyLoader.request();
        disableAutoStick();
      }
      if (bottomDistance <= SCROLL_END_THRESHOLD_PX && (movingDown || autoStickRef.current)) {
        autoStickRef.current = true;
        viewportAnchorRef.current = null;
        setLatestState(true);
      } else if ((movingUp || nearTop) && bottomDistance > SCROLL_END_THRESHOLD_PX) {
        setLatestState(false);
      }
      historyLoader.check();
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY !== 0) {
        userScrollSeqRef.current += 1;
      }
      if (event.deltaY < 0) {
        disableAutoStick();
        historyLoader.request();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target)) {
        return;
      }
      if (isDisclosureToggleTarget(event.target)) {
        return;
      }
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") {
        userScrollSeqRef.current += 1;
        disableAutoStick();
        historyLoader.request();
      } else if (event.key === "ArrowDown" || event.key === "PageDown" || event.key === "End" || event.key === " ") {
        userScrollSeqRef.current += 1;
      }
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    node.addEventListener("wheel", onWheel, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    updatePinned();
    return () => {
      node.removeEventListener("scroll", onScroll);
      node.removeEventListener("wheel", onWheel);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [disableAutoStick, historyLoader.check, historyLoader.request, scrollElement, setLatestState, updatePinned]);

  useEffect(() => {
    if (!scrollElement) {
      return;
    }
    const stick = () => {
      if (autoStickRef.current) {
        stickToLatestIfPinned(BOTTOM_STICK_STABILIZE_FRAMES);
      }
    };
    const observer = new ResizeObserver(stick);
    observer.observe(scrollElement);
    window.addEventListener("resize", stick);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", stick);
    };
  }, [scrollElement, stickToLatestIfPinned]);

  useEffect(() => {
    if (!listElement) {
      return;
    }
    const observer = new ResizeObserver(() => {
      handleContentResize();
    });
    observer.observe(listElement);
    return () => observer.disconnect();
  }, [handleContentResize, listElement]);

  useEffect(() => {
    historyLoader.reset();
    disclosureOpenStateRef.current.clear();
    autoStickRef.current = true;
    viewportAnchorRef.current = null;
    setLatestState(true);
  }, [historyLoader.reset, sessionID, setLatestState]);

  useLayoutEffect(() => {
    if (!scrollElement || autoStickRef.current) {
      return;
    }
    const anchor = viewportAnchorRef.current ?? captureViewportAnchor(scrollElement);
    viewportAnchorRef.current = anchor;
    restoreViewportAnchorOverFrames(scrollElement, anchor, () => {
      programmaticScrollIgnoreUntilRef.current = performance.now() + 120;
    });
  }, [scrollElement, turns]);

  useEffect(() => {
    return () => {
      cancelScheduledStick();
      virtualizer.shouldAdjustScrollPositionOnItemSizeChange = undefined;
    };
  }, [cancelScheduledStick, virtualizer]);

  if (turns.length === 0) {
    return null;
  }

  return (
    <div ref={setVirtualizerContainer} className="relative min-w-0">
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
              disclosure={listDisclosure}
              onAssistantContentGrow={handleAssistantContentGrow}
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

function useHistoryLoadController({
  getScrollElement,
  hasMore,
  isLoading,
  isNearTop,
  loadMore,
}: {
  getScrollElement: () => HTMLDivElement | null;
  hasMore: boolean;
  isLoading: boolean;
  isNearTop: () => boolean;
  loadMore: () => Promise<unknown> | unknown;
}) {
  const optionsRef = useRef({ getScrollElement, hasMore, isLoading, isNearTop, loadMore });
  const pendingForceRef = useRef(false);
  const pendingMoreRef = useRef(false);
  const pumpRef = useRef<() => void>(() => {});
  const stateRef = useRef<HistoryLoadState>("idle");

  useEffect(() => {
    optionsRef.current = { getScrollElement, hasMore, isLoading, isNearTop, loadMore };
  }, [getScrollElement, hasMore, isLoading, isNearTop, loadMore]);

  const setControllerState = useCallback((next: HistoryLoadState) => {
    stateRef.current = next;
  }, []);

  const pump = useCallback(() => {
    if (stateRef.current !== "idle" || !pendingMoreRef.current) {
      return;
    }

    const options = optionsRef.current;
    const force = pendingForceRef.current;
    if (!options.hasMore || options.isLoading || (!force && !options.isNearTop())) {
      return;
    }

    pendingForceRef.current = false;
    pendingMoreRef.current = false;
    setControllerState("loading");
    void Promise.resolve(options.loadMore())
      .catch(() => undefined)
      .then(async () => {
        setControllerState("settling");
        await waitForScrollSettle(options.getScrollElement);
      })
      .finally(() => {
        setControllerState("idle");
        window.requestAnimationFrame(() => pumpRef.current());
      });
  }, [setControllerState]);

  useEffect(() => {
    pumpRef.current = pump;
  }, [pump]);

  const check = useCallback(() => {
    pump();
  }, [pump]);

  const request = useCallback(
    ({ force = false }: { force?: boolean } = {}) => {
      pendingMoreRef.current = true;
      pendingForceRef.current ||= force;
      pump();
    },
    [pump],
  );

  const reset = useCallback(() => {
    pendingForceRef.current = false;
    pendingMoreRef.current = false;
    setControllerState("idle");
  }, [setControllerState]);

  return useMemo(() => ({ check, request, reset }), [check, request, reset]);
}

function estimateTurnHeight(turn: TranscriptTurnVM | undefined) {
  if (!turn) {
    return ESTIMATED_TURN_HEIGHT;
  }
  if (turn.user && !turn.assistant) {
    return 96;
  }
  if (!turn.assistant) {
    return 140;
  }
  return 220;
}

function distanceFromBottom(node: HTMLElement) {
  return Math.max(0, node.scrollHeight - node.clientHeight - node.scrollTop);
}

function disableVirtualizerSizeAdjustment() {
  return false;
}

function waitForScrollSettle(getScrollElement: () => HTMLElement | null) {
  return new Promise<void>((resolve) => {
    let frameCount = 0;
    let lastScrollHeight = -1;
    let lastScrollTop = -1;
    let stableFrames = 0;

    const tick = () => {
      const node = getScrollElement();
      if (!node) {
        resolve();
        return;
      }

      frameCount += 1;
      const scrollHeight = node.scrollHeight;
      const scrollTop = node.scrollTop;
      if (Math.abs(scrollTop - lastScrollTop) < 0.5 && Math.abs(scrollHeight - lastScrollHeight) < 1) {
        stableFrames += 1;
      } else {
        stableFrames = 0;
      }
      lastScrollHeight = scrollHeight;
      lastScrollTop = scrollTop;

      if (stableFrames >= 2 || frameCount >= 12) {
        resolve();
        return;
      }
      window.requestAnimationFrame(tick);
    };

    window.requestAnimationFrame(tick);
  });
}

function captureViewportAnchor(node: HTMLElement | null): ViewportAnchor | null {
  if (!node) {
    return null;
  }
  const viewportRect = node.getBoundingClientRect();
  let best: { element: HTMLElement; top: number } | null = null;
  for (const element of Array.from(node.querySelectorAll<HTMLElement>("[data-transcript-turn-id]"))) {
    const turnID = element.dataset.transcriptTurnId;
    if (!turnID) {
      continue;
    }
    const rect = element.getBoundingClientRect();
    if (rect.bottom <= viewportRect.top || rect.top >= viewportRect.bottom) {
      continue;
    }
    const top = rect.top - viewportRect.top;
    if (!best || Math.abs(top) < Math.abs(best.top)) {
      best = { element, top };
    }
  }
  if (!best) {
    return null;
  }
  const turnID = best.element.dataset.transcriptTurnId;
  return turnID ? { top: best.top, turnID } : null;
}

function restoreViewportAnchorOverFrames(node: HTMLElement | null, anchor: ViewportAnchor | null, beforeScroll?: () => void) {
  if (!node || !anchor) {
    return;
  }
  let remaining = 8;
  const tick = () => {
    restoreViewportAnchor(node, anchor, beforeScroll);
    remaining -= 1;
    if (remaining > 0) {
      window.requestAnimationFrame(tick);
    }
  };
  window.requestAnimationFrame(tick);
}

function restoreViewportAnchor(node: HTMLElement, anchor: ViewportAnchor, beforeScroll?: () => void) {
  const element = node.querySelector<HTMLElement>(`[data-transcript-turn-id="${CSS.escape(anchor.turnID)}"]`);
  if (!element) {
    return false;
  }
  const viewportRect = node.getBoundingClientRect();
  const top = element.getBoundingClientRect().top - viewportRect.top;
  const delta = top - anchor.top;
  if (Math.abs(delta) < ANCHOR_RESTORE_EPSILON_PX) {
    return false;
  }
  beforeScroll?.();
  node.scrollTop += delta;
  return true;
}

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

function isDisclosureToggleTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && Boolean(target.closest("summary"));
}
