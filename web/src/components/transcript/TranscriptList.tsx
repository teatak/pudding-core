import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef } from "react";

import { TranscriptTurn } from "./TranscriptTurn";
import type { TranscriptTurnVM, TurnDisclosureState } from "./types";

const HISTORY_LOAD_SCROLL_TOP_PX = 120;
const LIST_PADDING_BOTTOM_PX = 32;
const LIST_PADDING_TOP_PX = 16;
const TURN_GAP_PX = 16;
const SCROLL_END_THRESHOLD_PX = 8;
const ANCHOR_RESTORE_EPSILON_PX = 0.75;
const BOTTOM_STICK_STABILIZE_FRAMES = 4;
const CONTENT_STICK_STABILIZE_FRAMES = 8;
const VIEWPORT_ANCHOR_MAX_TOP_RATIO = 0.7;
const VIEWPORT_ANCHOR_MIN_TOP_RATIO = 0.3;
const VIEWPORT_ANCHOR_TARGET_RATIO = 0.5;

type HistoryLoadState = "idle" | "loading" | "settling";
type DisclosureOpenState = { openedAtLatest: boolean; userScrollSeq: number };
type ResizeAnchor = { anchorID: string; top: number; topRatio: number };
type HistoryAnchor = { top: number; turnID: string };
type CapturedAnchor<T> = T & { element: HTMLElement };

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
  const disclosureOpenStateRef = useRef(new Map<string, DisclosureOpenState>());
  const initialScrollSessionRef = useRef("");
  const isAtLatestRef = useRef(true);
  const lastScrollTopRef = useRef(0);
  const listElementRef = useRef<HTMLDivElement | null>(null);
  const programmaticScrollIgnoreUntilRef = useRef(0);
  const stickRafRef = useRef<number | null>(null);
  const stickRunRef = useRef(0);
  const userScrollSeqRef = useRef(0);
  const resizeAnchorRef = useRef<ResizeAnchor | null>(null);
  const viewportResizeSettleTimerRef = useRef<number | null>(null);

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

  const releaseViewportResizeAnchor = useCallback(() => {
    resizeAnchorRef.current = null;
    if (viewportResizeSettleTimerRef.current !== null) {
      window.clearTimeout(viewportResizeSettleTimerRef.current);
      viewportResizeSettleTimerRef.current = null;
    }
  }, []);

  const holdViewportResizeAnchor = useCallback(() => {
    if (!scrollElement) {
      return null;
    }
    const anchor = resizeAnchorRef.current ?? captureResizeAnchor(scrollElement);
    resizeAnchorRef.current = anchor;
    if (viewportResizeSettleTimerRef.current !== null) {
      window.clearTimeout(viewportResizeSettleTimerRef.current);
    }
    viewportResizeSettleTimerRef.current = window.setTimeout(() => {
      resizeAnchorRef.current = null;
      viewportResizeSettleTimerRef.current = null;
    }, 240);
    return anchor;
  }, [scrollElement]);

  const disableAutoStick = useCallback(() => {
    autoStickRef.current = false;
    cancelScheduledStick();
  }, [cancelScheduledStick]);

  const scrollToLatest = useCallback(() => {
    if (!scrollElement) {
      return;
    }
    releaseViewportResizeAnchor();
    if (distanceFromBottom(scrollElement) > ANCHOR_RESTORE_EPSILON_PX) {
      scrollElement.scrollTop = Math.max(0, scrollElement.scrollHeight - scrollElement.clientHeight);
      markProgrammaticScroll(programmaticScrollIgnoreUntilRef);
    }
    lastScrollTopRef.current = scrollElement.scrollTop;
    autoStickRef.current = true;
    setLatestState(true);
  }, [releaseViewportResizeAnchor, scrollElement, setLatestState]);

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

  const settleAfterDisclosureClose = useCallback(
    (shouldRestoreLatest: boolean) => {
      let remaining = BOTTOM_STICK_STABILIZE_FRAMES;
      const tick = () => {
        if (!scrollElement) {
          return;
        }
        if (distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX) {
          autoStickRef.current = true;
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
    [disableAutoStick, disclosure, scrollElement, settleAfterDisclosureClose],
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

  const loadHistory = useCallback(async () => {
    const anchor = captureHistoryViewportAnchor(scrollElement);
    const result = await onLoadHistory();
    restoreHistoryAnchorOverFrames(scrollElement, anchor, () => markProgrammaticScroll(programmaticScrollIgnoreUntilRef));
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

  const restoreResizeAnchorIfDetached = useCallback((anchorOverride?: ResizeAnchor | null) => {
    if (!scrollElement || autoStickRef.current) {
      return;
    }
    const anchor = anchorOverride ?? resizeAnchorRef.current ?? captureResizeAnchor(scrollElement);
    resizeAnchorRef.current = anchor;
    restoreResizeAnchorOverFrames(scrollElement, anchor, () => markProgrammaticScroll(programmaticScrollIgnoreUntilRef), "ratio");
  }, [scrollElement]);

  const handleContentResize = useCallback(() => {
    if (autoStickRef.current) {
      stickToLatestIfPinned(CONTENT_STICK_STABILIZE_FRAMES);
      return;
    }
    if (resizeAnchorRef.current) {
      restoreResizeAnchorIfDetached(resizeAnchorRef.current);
      return;
    }
    if (performance.now() < programmaticScrollIgnoreUntilRef.current) {
      return;
    }
    if (scrollElement && distanceFromBottom(scrollElement) > SCROLL_END_THRESHOLD_PX) {
      setLatestState(false);
    }
  }, [restoreResizeAnchorIfDetached, scrollElement, setLatestState, stickToLatestIfPinned]);

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
      const isProgrammaticScroll = performance.now() < programmaticScrollIgnoreUntilRef.current;
      lastScrollTopRef.current = nextScrollTop;

      if (!autoStickRef.current && !isProgrammaticScroll && (movingUp || movingDown)) {
        releaseViewportResizeAnchor();
      }
      if (!autoStickRef.current && !isProgrammaticScroll && (movingUp || movingDown)) {
        userScrollSeqRef.current += 1;
      }
      if (!isProgrammaticScroll && movingUp) {
      historyLoader.request();
      disableAutoStick();
    }
    if (bottomDistance <= SCROLL_END_THRESHOLD_PX && (movingDown || autoStickRef.current)) {
      releaseViewportResizeAnchor();
      autoStickRef.current = true;
      setLatestState(true);
    } else if ((movingUp || nearTop) && bottomDistance > SCROLL_END_THRESHOLD_PX) {
        setLatestState(false);
      }
      historyLoader.check();
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY !== 0) {
        releaseViewportResizeAnchor();
        userScrollSeqRef.current += 1;
      }
      if (event.deltaY < 0) {
        disableAutoStick();
        historyLoader.request();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target) || isDisclosureToggleTarget(event.target)) {
        return;
      }
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") {
        releaseViewportResizeAnchor();
        userScrollSeqRef.current += 1;
        disableAutoStick();
        historyLoader.request();
      } else if (event.key === "ArrowDown" || event.key === "PageDown" || event.key === "End" || event.key === " ") {
        releaseViewportResizeAnchor();
        userScrollSeqRef.current += 1;
      }
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    node.addEventListener("wheel", onWheel, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    if (distanceFromBottom(node) <= SCROLL_END_THRESHOLD_PX) {
      autoStickRef.current = true;
      setLatestState(true);
    }
    return () => {
      node.removeEventListener("scroll", onScroll);
      node.removeEventListener("wheel", onWheel);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [disableAutoStick, historyLoader.check, historyLoader.request, releaseViewportResizeAnchor, scrollElement, setLatestState]);

  useEffect(() => {
    const handleViewportResize = () => {
      if (autoStickRef.current) {
        stickToLatestIfPinned(BOTTOM_STICK_STABILIZE_FRAMES);
        return;
      }
      restoreResizeAnchorIfDetached(holdViewportResizeAnchor());
    };
    const observer = new ResizeObserver(handleViewportResize);
    if (scrollElement) {
      observer.observe(scrollElement);
    }
    window.addEventListener("resize", handleViewportResize);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", handleViewportResize);
    };
  }, [holdViewportResizeAnchor, restoreResizeAnchorIfDetached, scrollElement, stickToLatestIfPinned]);

  useEffect(() => {
    const node = listElementRef.current;
    if (!node) {
      return;
    }
    const observer = new ResizeObserver(() => {
      handleContentResize();
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [handleContentResize]);

  useEffect(() => {
    historyLoader.reset();
    disclosureOpenStateRef.current.clear();
    releaseViewportResizeAnchor();
    autoStickRef.current = true;
    setLatestState(true);
  }, [historyLoader.reset, releaseViewportResizeAnchor, sessionID, setLatestState]);

  useEffect(() => {
    return () => {
      cancelScheduledStick();
      releaseViewportResizeAnchor();
    };
  }, [cancelScheduledStick, releaseViewportResizeAnchor]);

  if (turns.length === 0) {
    return null;
  }

  return (
    <div
      ref={listElementRef}
      className="grid min-w-0"
      style={{ gap: TURN_GAP_PX, paddingBottom: LIST_PADDING_BOTTOM_PX, paddingTop: LIST_PADDING_TOP_PX }}
    >
      {turns.map((turn) => (
        <TranscriptTurn
          key={turn.key}
          disclosure={listDisclosure}
          onAssistantContentGrow={handleAssistantContentGrow}
          onAssistantRevealComplete={onAssistantRevealComplete}
          onQueuedCancel={onQueuedCancel}
          onQueuedEditStart={onQueuedEditStart}
          onQueuedSave={onQueuedSave}
          turn={turn}
        />
      ))}
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
      if (stateRef.current !== "idle") {
        return;
      }
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

function distanceFromBottom(node: HTMLElement) {
  return Math.max(0, node.scrollHeight - node.clientHeight - node.scrollTop);
}

function markProgrammaticScroll(ref: { current: number }) {
  ref.current = performance.now() + 120;
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

function captureResizeAnchor(node: HTMLElement | null): ResizeAnchor | null {
  if (!node) {
    return null;
  }
  return captureBestVisibleAnchor(node, "[data-transcript-ai-anchor]", (element) => {
    const anchorID = element.dataset.transcriptAiAnchor;
    if (!anchorID) {
      return null;
    }
    return { anchorID };
  });
}

function captureHistoryViewportAnchor(node: HTMLElement | null): HistoryAnchor | null {
  if (!node) {
    return null;
  }
  return captureBestVisibleAnchor(
    node,
    "[data-transcript-turn-id]",
    (element) => {
      const turnID = element.dataset.transcriptTurnId;
      return turnID ? { turnID } : null;
    },
    0,
  );
}

function captureBestVisibleAnchor<T extends object>(
  node: HTMLElement,
  selector: string,
  getIDs: (element: HTMLElement) => T | null,
  targetTopOverride?: number,
): CapturedAnchor<T & { top: number; topRatio: number }> | null {
  const viewportRect = node.getBoundingClientRect();
  const targetTop = targetTopOverride ?? viewportAnchorTargetTop(viewportRect.height);
  let best: { element: HTMLElement; ids: T; top: number } | null = null;
  for (const element of Array.from(node.querySelectorAll<HTMLElement>(selector))) {
    const ids = getIDs(element);
    if (!ids) {
      continue;
    }
    const rect = element.getBoundingClientRect();
    if (rect.bottom <= viewportRect.top || rect.top >= viewportRect.bottom) {
      continue;
    }
    const top = rect.top - viewportRect.top;
    if (!best || Math.abs(top - targetTop) < Math.abs(best.top - targetTop)) {
      best = { element, ids, top };
    }
  }
  if (!best) {
    return null;
  }
  return {
    ...best.ids,
    element: best.element,
    top: best.top,
    topRatio: viewportRect.height > 0 ? best.top / viewportRect.height : VIEWPORT_ANCHOR_TARGET_RATIO,
  };
}

function viewportAnchorTargetTop(viewportHeight: number) {
  const minTop = Math.max(0, viewportHeight * VIEWPORT_ANCHOR_MIN_TOP_RATIO);
  const maxTop = Math.max(minTop, viewportHeight * VIEWPORT_ANCHOR_MAX_TOP_RATIO);
  return Math.min(Math.max(viewportHeight * VIEWPORT_ANCHOR_TARGET_RATIO, minTop), maxTop);
}

function restoreResizeAnchorOverFrames(
  node: HTMLElement | null,
  anchor: ResizeAnchor | null,
  beforeScroll?: () => void,
  restoreMode: "saved" | "ratio" = "saved",
) {
  if (!node || !anchor) {
    return;
  }
  let remaining = 8;
  const tick = () => {
    restoreResizeAnchor(node, anchor, beforeScroll, restoreMode);
    remaining -= 1;
    if (remaining > 0) {
      window.requestAnimationFrame(tick);
    }
  };
  window.requestAnimationFrame(tick);
}

function restoreResizeAnchor(
  node: HTMLElement,
  anchor: ResizeAnchor,
  beforeScroll?: () => void,
  restoreMode: "saved" | "ratio" = "saved",
) {
  const element = findResizeAnchorElement(node, anchor);
  if (!element) {
    return false;
  }
  const viewportRect = node.getBoundingClientRect();
  const top = element.getBoundingClientRect().top - viewportRect.top;
  const targetTop = restoreMode === "ratio" ? viewportRect.height * anchor.topRatio : anchor.top;
  const delta = top - targetTop;
  if (Math.abs(delta) < ANCHOR_RESTORE_EPSILON_PX) {
    return false;
  }
  beforeScroll?.();
  node.scrollTop += delta;
  return true;
}

function restoreHistoryAnchorOverFrames(node: HTMLElement | null, anchor: HistoryAnchor | null, beforeScroll?: () => void) {
  if (!node || !anchor) {
    return;
  }
  let remaining = 8;
  const tick = () => {
    restoreHistoryAnchor(node, anchor, beforeScroll);
    remaining -= 1;
    if (remaining > 0) {
      window.requestAnimationFrame(tick);
    }
  };
  window.requestAnimationFrame(tick);
}

function restoreHistoryAnchor(node: HTMLElement, anchor: HistoryAnchor, beforeScroll?: () => void) {
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

function findResizeAnchorElement(node: HTMLElement, anchor: ResizeAnchor) {
  if (!anchor.anchorID) {
    return null;
  }
  return node.querySelector<HTMLElement>(`[data-transcript-ai-anchor="${CSS.escape(anchor.anchorID)}"]`);
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
