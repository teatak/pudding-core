import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, type ReactNode } from "react";

import type { TranscriptTurnReveal } from "@/state/transcriptRevealStore";

import { TranscriptTurn } from "./TranscriptTurn";
import type {
  TranscriptDisplaySettings,
  TranscriptSearchState,
  TranscriptSearchTarget,
  TranscriptTurnVM,
  TurnDisclosureState,
} from "./types";

const HISTORY_LOAD_SCROLL_TOP_PX = 120;
const LIST_PADDING_BOTTOM_PX = 36;
const LIST_PADDING_TOP_PX = 22;
const TURN_GAP_PX = 22;
const LIST_BOTTOM_SPACER_PX = Math.max(0, LIST_PADDING_BOTTOM_PX - TURN_GAP_PX);
const SCROLL_END_THRESHOLD_PX = 8;
const ANCHOR_RESTORE_EPSILON_PX = 0.75;
const BOTTOM_STICK_STABILIZE_FRAMES = 4;
const CONTENT_STICK_STABILIZE_FRAMES = 2;
const JUMP_LATEST_ANIMATION_MS = 180;
const VIEWPORT_ANCHOR_MAX_TOP_RATIO = 0.7;
const VIEWPORT_ANCHOR_MIN_TOP_RATIO = 0.3;
const VIEWPORT_ANCHOR_TARGET_RATIO = 0.5;
const POINTER_SCROLL_SETTLE_MS = 800;
const TURN_REVEAL_CLEAR_MS = 2400;
const TURN_REVEAL_TOP_RATIO = 0.38;

type HistoryLoadState = "idle" | "loading" | "settling";
type DisclosureOpenState = { openedAtLatest: boolean; userScrollSeq: number };
type ResizeAnchor = { anchorID: string; top: number; topRatio: number };
type HistoryAnchor = { top: number; turnID: string };
type CapturedAnchor<T> = T & { element: HTMLElement };
type PointerGesture = { pointerID: number; startY: number };

export const TranscriptList = memo(function TranscriptList({
  disclosure,
  displaySettings,
  footer,
  hasMoreHistory,
  isLoadingHistory,
  jumpLatestSignal,
  onAssistantRevealComplete,
  onLatestChange,
  onLoadHistory,
  onTurnRevealComplete,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSteer,
  onQueuedSave,
  scrollElement,
  searchSlot,
  searchState,
  sessionID,
  token,
  turnReveal,
  turns,
}: {
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  footer?: ReactNode;
  hasMoreHistory: boolean;
  isLoadingHistory: boolean;
  jumpLatestSignal: number;
  onAssistantRevealComplete?: (turnID: string) => void;
  onLatestChange?: (isAtLatest: boolean) => void;
  onLoadHistory: () => Promise<unknown> | void;
  onTurnRevealComplete?: (serial: number) => void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSteer?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  scrollElement: HTMLDivElement | null;
  searchSlot: "primary" | "split";
  searchState: TranscriptSearchState;
  sessionID: string;
  token: string;
  turnReveal?: TranscriptTurnReveal;
  turns: TranscriptTurnVM[];
}) {
  const autoStickRef = useRef(true);
  const activeSearchElementsRef = useRef<HTMLElement[]>([]);
  const activeSearchTargetRef = useRef("");
  const disclosureOpenStateRef = useRef(new Map<string, DisclosureOpenState>());
  const initialScrollSessionRef = useRef("");
  const isAtLatestRef = useRef(true);
  const lastClientHeightRef = useRef(0);
  const lastScrollTopRef = useRef(0);
  const latestSentinelRef = useRef<HTMLDivElement | null>(null);
  const listElementRef = useRef<HTMLDivElement | null>(null);
  const programmaticScrollIgnoreUntilRef = useRef(0);
  const revealedMessageElementsRef = useRef<HTMLElement[]>([]);
  const revealedTurnSerialRef = useRef(0);
  const revealClearTimerRef = useRef<number | null>(null);
  const stickRafRef = useRef<number | null>(null);
  const stickRunRef = useRef(0);
  const userScrollSeqRef = useRef(0);
  const pendingHistoryAnchorRef = useRef<HistoryAnchor | null>(null);
  const pendingHistoryAnchorSessionRef = useRef("");
  const pendingHistoryAnchorClearTimerRef = useRef<number | null>(null);
  const pointerGestureRef = useRef<PointerGesture | null>(null);
  const pointerScrollIntentUntilRef = useRef(0);
  const previousFirstTurnKeyRef = useRef<string | null>(null);
  const resizeAnchorRef = useRef<ResizeAnchor | null>(null);
  const smoothJumpRafRef = useRef<number | null>(null);
  const smoothJumpRef = useRef(false);
  const turnRevealRef = useRef(turnReveal);
  const viewportResizeSettleTimerRef = useRef<number | null>(null);
  turnRevealRef.current = turnReveal;
  const firstTurnKey = turns[0]?.key ?? "";
  const searchRenderVersion = useMemo(
    () => turns.map((turn) => turn.key).join("\u0000"),
    [turns],
  );

  const syncViewportScrollbar = useCallback(
    (atLatest: boolean) => {
      if (scrollElement) {
        scrollElement.dataset.scrollbarState = atLatest ? "hidden" : "visible";
      }
    },
    [scrollElement],
  );

  const setLatestState = useCallback(
    (next: boolean) => {
      syncViewportScrollbar(next);
      if (isAtLatestRef.current === next) {
        return;
      }
      isAtLatestRef.current = next;
      onLatestChange?.(next);
    },
    [onLatestChange, syncViewportScrollbar],
  );

  const cancelScheduledStick = useCallback(() => {
    stickRunRef.current += 1;
    if (stickRafRef.current !== null) {
      window.cancelAnimationFrame(stickRafRef.current);
      stickRafRef.current = null;
    }
  }, []);

  const cancelSmoothJump = useCallback(() => {
    smoothJumpRef.current = false;
    if (smoothJumpRafRef.current !== null) {
      window.cancelAnimationFrame(smoothJumpRafRef.current);
      smoothJumpRafRef.current = null;
    }
  }, []);

  const releaseViewportResizeAnchor = useCallback(() => {
    resizeAnchorRef.current = null;
    if (viewportResizeSettleTimerRef.current !== null) {
      window.clearTimeout(viewportResizeSettleTimerRef.current);
      viewportResizeSettleTimerRef.current = null;
    }
  }, []);

  const clearPendingHistoryAnchor = useCallback(() => {
    pendingHistoryAnchorRef.current = null;
    pendingHistoryAnchorSessionRef.current = "";
    if (pendingHistoryAnchorClearTimerRef.current !== null) {
      window.clearTimeout(pendingHistoryAnchorClearTimerRef.current);
      pendingHistoryAnchorClearTimerRef.current = null;
    }
  }, []);

  const schedulePendingHistoryAnchorClear = useCallback(() => {
    if (pendingHistoryAnchorClearTimerRef.current !== null) {
      window.clearTimeout(pendingHistoryAnchorClearTimerRef.current);
    }
    pendingHistoryAnchorClearTimerRef.current = window.setTimeout(clearPendingHistoryAnchor, 1500);
  }, [clearPendingHistoryAnchor]);

  const beginPendingHistoryAnchor = useCallback(() => {
    pendingHistoryAnchorRef.current = captureHistoryViewportAnchor(scrollElement);
    pendingHistoryAnchorSessionRef.current = sessionID;
    if (pendingHistoryAnchorClearTimerRef.current !== null) {
      window.clearTimeout(pendingHistoryAnchorClearTimerRef.current);
      pendingHistoryAnchorClearTimerRef.current = null;
    }
  }, [scrollElement, sessionID]);

  const refreshPendingHistoryAnchor = useCallback(() => {
    if (pendingHistoryAnchorSessionRef.current !== sessionID) {
      return;
    }
    const anchor = captureHistoryViewportAnchor(scrollElement);
    if (anchor) {
      pendingHistoryAnchorRef.current = anchor;
    }
  }, [scrollElement, sessionID]);

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
    cancelSmoothJump();
    autoStickRef.current = false;
    syncViewportScrollbar(false);
    cancelScheduledStick();
  }, [cancelScheduledStick, cancelSmoothJump, syncViewportScrollbar]);

  const scrollToLatest = useCallback((behavior: ScrollBehavior = "auto") => {
    if (!scrollElement) {
      return;
    }
    releaseViewportResizeAnchor();
    const nextScrollTop = Math.max(0, scrollElement.scrollHeight - scrollElement.clientHeight);
    if (behavior === "smooth" && distanceFromBottom(scrollElement) > ANCHOR_RESTORE_EPSILON_PX) {
      cancelScheduledStick();
      cancelSmoothJump();
      smoothJumpRef.current = true;
      autoStickRef.current = false;
      const startScrollTop = scrollElement.scrollTop;
      const startedAt = performance.now();
      programmaticScrollIgnoreUntilRef.current = startedAt + JUMP_LATEST_ANIMATION_MS + 120;
      const tick = (now: number) => {
        if (!smoothJumpRef.current) {
          smoothJumpRafRef.current = null;
          return;
        }
        const progress = Math.min(1, (now - startedAt) / JUMP_LATEST_ANIMATION_MS);
        const eased = 1 - Math.pow(1 - progress, 3);
        const targetScrollTop = Math.max(0, scrollElement.scrollHeight - scrollElement.clientHeight);
        scrollElement.scrollTop = startScrollTop + (targetScrollTop - startScrollTop) * eased;
        lastScrollTopRef.current = scrollElement.scrollTop;
        if (progress < 1 && distanceFromBottom(scrollElement) > ANCHOR_RESTORE_EPSILON_PX) {
          smoothJumpRafRef.current = window.requestAnimationFrame(tick);
          return;
        }
        scrollElement.scrollTop = targetScrollTop;
        lastScrollTopRef.current = targetScrollTop;
        smoothJumpRef.current = false;
        smoothJumpRafRef.current = null;
        autoStickRef.current = true;
        setLatestState(true);
      };
      smoothJumpRafRef.current = window.requestAnimationFrame(tick);
      return;
    }
    cancelSmoothJump();
    if (distanceFromBottom(scrollElement) > ANCHOR_RESTORE_EPSILON_PX) {
      markProgrammaticScroll(programmaticScrollIgnoreUntilRef);
      const sentinel = latestSentinelRef.current;
      if (sentinel) {
        sentinel.scrollIntoView({ block: "end", inline: "nearest" });
      } else {
        scrollElement.scrollTop = Math.max(0, scrollElement.scrollHeight - scrollElement.clientHeight);
      }
    }
    lastScrollTopRef.current = scrollElement.scrollTop;
    autoStickRef.current = true;
    setLatestState(true);
  }, [cancelScheduledStick, cancelSmoothJump, releaseViewportResizeAnchor, scrollElement, setLatestState]);

  const syncPinnedBottom = useCallback(() => {
    if (!scrollElement) {
      return;
    }
    releaseViewportResizeAnchor();
    const nextScrollTop = Math.max(0, scrollElement.scrollHeight - scrollElement.clientHeight);
    const browserClampedScrollTop =
      Math.abs(scrollElement.scrollTop - lastScrollTopRef.current) > ANCHOR_RESTORE_EPSILON_PX;
    if (browserClampedScrollTop || Math.abs(scrollElement.scrollTop - nextScrollTop) > ANCHOR_RESTORE_EPSILON_PX) {
      markProgrammaticScroll(programmaticScrollIgnoreUntilRef);
    }
    if (Math.abs(scrollElement.scrollTop - nextScrollTop) > ANCHOR_RESTORE_EPSILON_PX) {
      scrollElement.scrollTop = nextScrollTop;
    }
    lastScrollTopRef.current = scrollElement.scrollTop;
    autoStickRef.current = true;
    setLatestState(true);
  }, [releaseViewportResizeAnchor, scrollElement, setLatestState]);

  const stickToLatestIfPinned = useCallback(
    (frames = 1, options: { deferFirstFrame?: boolean } = {}) => {
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
      if (options.deferFirstFrame) {
        stickRafRef.current = window.requestAnimationFrame(tick);
        return;
      }
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
      if (!disclosure || (disclosure.hasState(key) && disclosure.isOpen(key) === open)) {
        return;
      }

      if (open) {
        const openedAtLatest = scrollElement
          ? distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX
          : isAtLatestRef.current;
        disclosureOpenStateRef.current.set(key, {
          openedAtLatest,
          userScrollSeq: userScrollSeqRef.current,
        });
        if (openedAtLatest) {
          autoStickRef.current = true;
          setLatestState(true);
        } else {
          disableAutoStick();
          setLatestState(false);
        }
        disclosure.setOpen(key, true);
        return;
      }

      const openState = disclosureOpenStateRef.current.get(key);
      disclosureOpenStateRef.current.delete(key);
      const currentlyAtLatest = scrollElement
        ? distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX
        : isAtLatestRef.current;
      disclosure.setOpen(key, false);
      const userScrolledDuringDisclosure = openState ? userScrollSeqRef.current !== openState.userScrollSeq : false;
      settleAfterDisclosureClose(currentlyAtLatest || (Boolean(openState?.openedAtLatest) && !userScrolledDuringDisclosure));
    },
    [disableAutoStick, disclosure, scrollElement, setLatestState, settleAfterDisclosureClose],
  );

  const listDisclosure = useMemo<TurnDisclosureState | undefined>(() => {
    if (!disclosure) {
      return undefined;
    }
    return {
      hasState: disclosure.hasState,
      isOpen: disclosure.isOpen,
      setOpen: handleDisclosureOpenChange,
    };
  }, [disclosure, handleDisclosureOpenChange]);

  const loadHistory = useCallback(async () => {
    beginPendingHistoryAnchor();
    try {
      const result = await onLoadHistory();
      schedulePendingHistoryAnchorClear();
      return result;
    } catch (error) {
      clearPendingHistoryAnchor();
      throw error;
    }
  }, [beginPendingHistoryAnchor, clearPendingHistoryAnchor, onLoadHistory, schedulePendingHistoryAnchorClear]);

  const isNearTop = useCallback(() => Boolean(scrollElement && scrollElement.scrollTop < HISTORY_LOAD_SCROLL_TOP_PX), [scrollElement]);
  const historyLoader = useHistoryLoadController({
    getScrollElement: () => scrollElement,
    hasMore: hasMoreHistory,
    isLoading: isLoadingHistory,
    isNearTop,
    loadMore: loadHistory,
  });

  const handleAssistantContentGrow = useCallback(() => {
    stickToLatestIfPinned(CONTENT_STICK_STABILIZE_FRAMES, { deferFirstFrame: true });
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
    // ResizeObserver runs before paint. Compensate synchronously so pinned disclosures
    // visually grow upward instead of rendering first and snapping on the next frame.
    if (smoothJumpRef.current) {
      return;
    }
    if (autoStickRef.current) {
      syncPinnedBottom();
      return;
    }
    if (scrollElement && distanceFromBottom(scrollElement) > SCROLL_END_THRESHOLD_PX) {
      setLatestState(false);
    }
    if (resizeAnchorRef.current) {
      restoreResizeAnchorIfDetached(resizeAnchorRef.current);
      return;
    }
  }, [restoreResizeAnchorIfDetached, scrollElement, setLatestState, syncPinnedBottom]);

  useEffect(() => {
    if (!scrollElement) {
      return;
    }
    lastScrollTopRef.current = scrollElement.scrollTop;
    lastClientHeightRef.current = scrollElement.clientHeight;
    syncViewportScrollbar(distanceFromBottom(scrollElement) <= SCROLL_END_THRESHOLD_PX);
  }, [scrollElement, syncViewportScrollbar]);

  useLayoutEffect(() => {
    const previousFirstTurnKey = previousFirstTurnKeyRef.current;
    previousFirstTurnKeyRef.current = firstTurnKey;
    if (
      !scrollElement ||
      pendingHistoryAnchorSessionRef.current !== sessionID ||
      !pendingHistoryAnchorRef.current ||
      previousFirstTurnKey === null ||
      previousFirstTurnKey === firstTurnKey
    ) {
      return;
    }
    restoreHistoryAnchor(scrollElement, pendingHistoryAnchorRef.current, () =>
      markProgrammaticScroll(programmaticScrollIgnoreUntilRef),
    );
    lastScrollTopRef.current = scrollElement.scrollTop;
    clearPendingHistoryAnchor();
  }, [clearPendingHistoryAnchor, firstTurnKey, scrollElement, sessionID]);

  useEffect(() => {
    if (!scrollElement || turns.length === 0 || initialScrollSessionRef.current === sessionID) {
      return;
    }
    if (turnReveal) {
      return;
    }
    initialScrollSessionRef.current = sessionID;
    window.requestAnimationFrame(() => scrollToLatest());
  }, [scrollElement, scrollToLatest, sessionID, turnReveal, turns.length]);

  useEffect(() => {
    if (jumpLatestSignal <= 0) {
      return;
    }
    window.requestAnimationFrame(() => scrollToLatest("smooth"));
  }, [jumpLatestSignal, scrollToLatest]);

  useEffect(() => {
    const node = scrollElement;
    if (!node) {
      return;
    }
    const onScroll = () => {
      const previousScrollTop = lastScrollTopRef.current;
      const nextScrollTop = node.scrollTop;
      const previousClientHeight = lastClientHeightRef.current;
      const nextClientHeight = node.clientHeight;
      const viewportHeightChanged =
        previousClientHeight > 0 &&
        Math.abs(nextClientHeight - previousClientHeight) > ANCHOR_RESTORE_EPSILON_PX;
      lastClientHeightRef.current = nextClientHeight;
      const movingUp = nextScrollTop < previousScrollTop - 1;
      const movingDown = nextScrollTop > previousScrollTop + 1;
      const nearTop = nextScrollTop < HISTORY_LOAD_SCROLL_TOP_PX;
      const bottomDistance = distanceFromBottom(node);
      const isProgrammaticScroll = performance.now() < programmaticScrollIgnoreUntilRef.current;
      const hasPointerScrollIntent = performance.now() < pointerScrollIntentUntilRef.current;
      lastScrollTopRef.current = nextScrollTop;

      if (smoothJumpRef.current) {
        if (bottomDistance <= SCROLL_END_THRESHOLD_PX) {
          cancelSmoothJump();
          autoStickRef.current = true;
          setLatestState(true);
        }
        historyLoader.check();
        return;
      }

      if (!isProgrammaticScroll && hasPointerScrollIntent && movingUp) {
        releaseViewportResizeAnchor();
        userScrollSeqRef.current += 1;
        disableAutoStick();
        historyLoader.request();
      }

      // autoStick is user intent, not a geometric guess. IME preedit, the
      // composer bottom spacer, content collapse, and Chromium caret correction
      // can all move scrollTop without a user scroll gesture. Preserve the intent
      // and repair the geometry instead of treating every upward scroll as detach.
      if (autoStickRef.current) {
        if (viewportHeightChanged || bottomDistance > ANCHOR_RESTORE_EPSILON_PX) {
          syncPinnedBottom();
        } else {
          releaseViewportResizeAnchor();
          setLatestState(true);
        }
        historyLoader.check();
        return;
      }

      if (!isProgrammaticScroll) {
        refreshPendingHistoryAnchor();
      }
      if (!autoStickRef.current && !isProgrammaticScroll && (movingUp || movingDown)) {
        releaseViewportResizeAnchor();
      }
      if (!autoStickRef.current && !isProgrammaticScroll && (movingUp || movingDown)) {
        userScrollSeqRef.current += 1;
      }
      // Once the user has detached, only a deliberate downward scroll that reaches
      // the end re-enables auto-stick.
      if (bottomDistance <= SCROLL_END_THRESHOLD_PX && movingDown) {
        releaseViewportResizeAnchor();
        autoStickRef.current = true;
        setLatestState(true);
      } else {
        if ((movingUp || nearTop) && bottomDistance > SCROLL_END_THRESHOLD_PX) {
          setLatestState(false);
        }
      }
      historyLoader.check();
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY !== 0) {
        cancelSmoothJump();
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
        cancelSmoothJump();
        releaseViewportResizeAnchor();
        userScrollSeqRef.current += 1;
      }
    };
    const beginPointerScroll = () => {
      cancelSmoothJump();
      pointerScrollIntentUntilRef.current = Number.POSITIVE_INFINITY;
      cancelScheduledStick();
    };
    const onPointerDown = (event: PointerEvent) => {
      if (event.button !== 0) {
        return;
      }
      pointerGestureRef.current = { pointerID: event.pointerId, startY: event.clientY };
      if (pointerHitsVerticalScrollbar(event, node)) {
        beginPointerScroll();
      }
    };
    const onPointerMove = (event: PointerEvent) => {
      const gesture = pointerGestureRef.current;
      if (
        !gesture ||
        gesture.pointerID !== event.pointerId ||
        Math.abs(event.clientY - gesture.startY) < 3
      ) {
        return;
      }
      beginPointerScroll();
    };
    const finishPointerScroll = () => {
      pointerGestureRef.current = null;
      if (pointerScrollIntentUntilRef.current === Number.POSITIVE_INFINITY) {
        pointerScrollIntentUntilRef.current = performance.now() + POINTER_SCROLL_SETTLE_MS;
      }
    };
    const cancelPointerScroll = () => {
      pointerGestureRef.current = null;
      pointerScrollIntentUntilRef.current = 0;
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    node.addEventListener("wheel", onWheel, { passive: true });
    node.addEventListener("pointerdown", onPointerDown, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("pointermove", onPointerMove, { passive: true });
    window.addEventListener("pointerup", finishPointerScroll, { passive: true });
    window.addEventListener("pointercancel", cancelPointerScroll, { passive: true });
    if (distanceFromBottom(node) <= SCROLL_END_THRESHOLD_PX) {
      autoStickRef.current = true;
      setLatestState(true);
    }
    return () => {
      node.removeEventListener("scroll", onScroll);
      node.removeEventListener("wheel", onWheel);
      node.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", finishPointerScroll);
      window.removeEventListener("pointercancel", cancelPointerScroll);
    };
  }, [
    cancelScheduledStick,
    cancelSmoothJump,
    disableAutoStick,
    historyLoader.check,
    historyLoader.request,
    refreshPendingHistoryAnchor,
    releaseViewportResizeAnchor,
    scrollElement,
    setLatestState,
    syncPinnedBottom,
  ]);

  useEffect(() => {
    const handleViewportResize = () => {
      if (scrollElement) {
        lastClientHeightRef.current = scrollElement.clientHeight;
      }
      if (smoothJumpRef.current) {
        return;
      }
      if (autoStickRef.current) {
        syncPinnedBottom();
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
  }, [
    holdViewportResizeAnchor,
    restoreResizeAnchorIfDetached,
    scrollElement,
    syncPinnedBottom,
  ]);

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
    clearPendingHistoryAnchor();
    releaseViewportResizeAnchor();
    cancelSmoothJump();
    const locatingTurn = Boolean(turnRevealRef.current);
    autoStickRef.current = !locatingTurn;
    setLatestState(!locatingTurn);
  }, [cancelSmoothJump, clearPendingHistoryAnchor, historyLoader.reset, releaseViewportResizeAnchor, sessionID, setLatestState]);

  useLayoutEffect(() => {
    if (
      !scrollElement ||
      !turnReveal ||
      revealedTurnSerialRef.current === turnReveal.serial
    ) {
      return;
    }
    const turnElement = scrollElement.querySelector<HTMLElement>(
      `[data-transcript-turn-id="${CSS.escape(turnReveal.turnID)}"]`,
    );
    if (!turnElement) {
      return;
    }
    cancelScheduledStick();
    cancelSmoothJump();
    disableAutoStick();
    markProgrammaticScroll(programmaticScrollIgnoreUntilRef);
    const viewportRect = scrollElement.getBoundingClientRect();
    const elementTop = turnElement.getBoundingClientRect().top - viewportRect.top;
    scrollElement.scrollTop += elementTop - scrollElement.clientHeight * TURN_REVEAL_TOP_RATIO;
    lastScrollTopRef.current = scrollElement.scrollTop;
    initialScrollSessionRef.current = sessionID;
    revealedTurnSerialRef.current = turnReveal.serial;
    setLatestState(false);

    if (revealClearTimerRef.current !== null) {
      window.clearTimeout(revealClearTimerRef.current);
    }
    for (const element of revealedMessageElementsRef.current) {
      element.removeAttribute("data-transcript-turn-reveal");
    }
    const roleElements = turnReveal.messageRole
      ? Array.from(
          turnElement.querySelectorAll<HTMLElement>(
            `[data-transcript-message-role="${turnReveal.messageRole}"]`,
          ),
        )
      : [];
    const revealElements = roleElements.length > 0 ? roleElements : [turnElement];
    revealedMessageElementsRef.current = revealElements;
    for (const element of revealElements) {
      element.setAttribute("data-transcript-turn-reveal", "true");
    }
    revealClearTimerRef.current = window.setTimeout(() => {
      for (const element of revealElements) {
        element.removeAttribute("data-transcript-turn-reveal");
      }
      revealedMessageElementsRef.current = [];
      revealClearTimerRef.current = null;
    }, TURN_REVEAL_CLEAR_MS);
    onTurnRevealComplete?.(turnReveal.serial);
  }, [
    cancelScheduledStick,
    cancelSmoothJump,
    disableAutoStick,
    onTurnRevealComplete,
    scrollElement,
    sessionID,
    setLatestState,
    turnReveal,
    turns,
  ]);

  useLayoutEffect(() => {
    for (const element of activeSearchElementsRef.current) {
      element.removeAttribute("data-transcript-search-active");
    }
    activeSearchElementsRef.current = [];
    const target = searchState.target;
    if (!scrollElement || !target) {
      activeSearchTargetRef.current = "";
      return;
    }
    const turnElement = scrollElement.querySelector<HTMLElement>(
      `[data-transcript-turn-id="${CSS.escape(target.turnID)}"]`,
    );
    if (!turnElement) {
      return;
    }
    const messageElements = Array.from(
      turnElement.querySelectorAll<HTMLElement>(
        `[data-transcript-message-id="${CSS.escape(target.messageID)}"]`,
      ),
    );
    const roleElements = Array.from(
      turnElement.querySelectorAll<HTMLElement>(`[data-transcript-message-role="${target.role}"]`),
    );
    const activeElements = messageElements.length > 0
      ? messageElements
      : roleElements.length > 0
        ? roleElements
        : [turnElement];
    activeSearchElementsRef.current = activeElements;
    for (const element of activeElements) {
      element.setAttribute("data-transcript-search-active", "true");
    }
    const targetKey = `${target.messageID}:${target.occurrenceIndex}`;
    if (activeSearchTargetRef.current === targetKey) {
      return;
    }
    cancelScheduledStick();
    cancelSmoothJump();
    disableAutoStick();
    markProgrammaticScroll(programmaticScrollIgnoreUntilRef);
    const viewportRect = scrollElement.getBoundingClientRect();
    const activeRange = transcriptSearchTargetRange(scrollElement, target, searchState.terms);
    const targetRect = activeRange?.getBoundingClientRect() || activeElements[0]?.getBoundingClientRect() || turnElement.getBoundingClientRect();
    const elementTop = targetRect.top - viewportRect.top;
    scrollElement.scrollTop += elementTop - scrollElement.clientHeight * TURN_REVEAL_TOP_RATIO;
    lastScrollTopRef.current = scrollElement.scrollTop;
    initialScrollSessionRef.current = sessionID;
    activeSearchTargetRef.current = targetKey;
    setLatestState(false);
  }, [
    cancelScheduledStick,
    cancelSmoothJump,
    disableAutoStick,
    scrollElement,
    searchState.target,
    searchState.terms,
    searchRenderVersion,
    sessionID,
    setLatestState,
  ]);

  useLayoutEffect(() => {
    const names = transcriptSearchHighlightNames(searchSlot);
    clearTranscriptSearchHighlights(names);
    if (!scrollElement || searchState.terms.length === 0) {
      return;
    }
    const registry = transcriptHighlightRegistry();
    const HighlightConstructor = transcriptHighlightConstructor();
    if (!registry || !HighlightConstructor) {
      return;
    }
    ensureTranscriptSearchHighlightStyles();
    const messageRoots = Array.from(
      scrollElement.querySelectorAll<HTMLElement>("[data-transcript-message-id]"),
    );
    const matchRanges = messageRoots.flatMap((root) => textMatchRanges(root, searchState.terms));
    const activeRange = searchState.target
      ? transcriptSearchTargetRange(scrollElement, searchState.target, searchState.terms)
      : undefined;
    if (matchRanges.length > 0) {
      registry.set(names.matches, new HighlightConstructor(...matchRanges));
    }
    if (activeRange) {
      registry.set(names.active, new HighlightConstructor(activeRange));
    }
    return () => clearTranscriptSearchHighlights(names);
  }, [scrollElement, searchRenderVersion, searchSlot, searchState.target, searchState.terms]);

  useEffect(() => {
    return () => {
      cancelScheduledStick();
      cancelSmoothJump();
      clearPendingHistoryAnchor();
      releaseViewportResizeAnchor();
      if (revealClearTimerRef.current !== null) {
        window.clearTimeout(revealClearTimerRef.current);
      }
      for (const element of revealedMessageElementsRef.current) {
        element.removeAttribute("data-transcript-turn-reveal");
      }
      for (const element of activeSearchElementsRef.current) {
        element.removeAttribute("data-transcript-search-active");
      }
      clearTranscriptSearchHighlights(transcriptSearchHighlightNames(searchSlot));
      revealedMessageElementsRef.current = [];
      activeSearchElementsRef.current = [];
    };
  }, [cancelScheduledStick, cancelSmoothJump, clearPendingHistoryAnchor, releaseViewportResizeAnchor, searchSlot]);

  if (turns.length === 0 && !footer) {
    return null;
  }

  return (
    <div
      ref={listElementRef}
      className="grid min-w-0"
      style={{ gap: TURN_GAP_PX, paddingTop: LIST_PADDING_TOP_PX }}
    >
      {turns.map((turn) => (
        <TranscriptTurn
          key={turn.key}
          disclosure={listDisclosure}
          displaySettings={displaySettings}
          sessionID={sessionID}
          onAssistantContentGrow={handleAssistantContentGrow}
          onAssistantRevealComplete={onAssistantRevealComplete}
          onQueuedCancel={onQueuedCancel}
          onQueuedEditStart={onQueuedEditStart}
          onQueuedSteer={onQueuedSteer}
          onQueuedSave={onQueuedSave}
          token={token}
          turn={turn}
        />
      ))}
      {footer}
      <div
        ref={latestSentinelRef}
        aria-hidden="true"
        className="scroll-mb-0"
        style={{
          height: `calc(${LIST_BOTTOM_SPACER_PX}px + var(--pudding-composer-overlay-height, 0px))`,
        }}
      />
    </div>
  );
});

type TranscriptHighlightNames = {
  active: string;
  matches: string;
};

type TranscriptHighlightRegistry = {
  delete: (name: string) => boolean;
  set: (name: string, highlight: object) => void;
};

type TranscriptHighlightConstructor = new (...ranges: Range[]) => object;
const TRANSCRIPT_SEARCH_HIGHLIGHT_STYLE_ID = "pudding-conversation-search-highlight-styles";

function transcriptSearchHighlightNames(slot: "primary" | "split"): TranscriptHighlightNames {
  return {
    active: `pudding-conversation-search-active-${slot}`,
    matches: `pudding-conversation-search-match-${slot}`,
  };
}

function transcriptHighlightRegistry(): TranscriptHighlightRegistry | undefined {
  return (CSS as unknown as { highlights?: TranscriptHighlightRegistry }).highlights;
}

function transcriptHighlightConstructor(): TranscriptHighlightConstructor | undefined {
  return (globalThis as unknown as { Highlight?: TranscriptHighlightConstructor }).Highlight;
}

function clearTranscriptSearchHighlights(names: TranscriptHighlightNames) {
  const registry = transcriptHighlightRegistry();
  registry?.delete(names.matches);
  registry?.delete(names.active);
}

function ensureTranscriptSearchHighlightStyles() {
  let style = document.getElementById(TRANSCRIPT_SEARCH_HIGHLIGHT_STYLE_ID) as HTMLStyleElement | null;
  if (!style) {
    style = document.createElement("style");
    style.id = TRANSCRIPT_SEARCH_HIGHLIGHT_STYLE_ID;
    document.head.append(style);
  }
  style.textContent = `
    ::highlight(pudding-conversation-search-match-primary),
    ::highlight(pudding-conversation-search-match-split) {
      color: inherit;
      background-color: oklch(0.88 0.14 88 / 0.42);
    }
    ::highlight(pudding-conversation-search-active-primary),
    ::highlight(pudding-conversation-search-active-split) {
      color: white;
      background-color: var(--transcript-reveal-accent);
      text-decoration-line: underline;
      text-decoration-color: color-mix(in oklab, white 82%, transparent);
      text-decoration-thickness: 2px;
      text-underline-offset: 2px;
      text-shadow: 0 0 0.55rem color-mix(in oklab, var(--transcript-reveal-accent) 80%, transparent);
    }
    .dark ::highlight(pudding-conversation-search-match-primary),
    .dark ::highlight(pudding-conversation-search-match-split) {
      background-color: oklch(0.72 0.13 82 / 0.3);
    }
    .dark ::highlight(pudding-conversation-search-active-primary),
    .dark ::highlight(pudding-conversation-search-active-split) {
      color: oklch(0.16 0 0);
      background-color: var(--transcript-reveal-accent);
      text-decoration-color: color-mix(in oklab, oklch(0.16 0 0) 70%, transparent);
    }
  `;
}

function transcriptSearchTargetRange(
  scrollElement: HTMLElement,
  target: TranscriptSearchTarget,
  terms: string[],
) {
  const roots = Array.from(
    scrollElement.querySelectorAll<HTMLElement>(
      `[data-transcript-message-id="${CSS.escape(target.messageID)}"]`,
    ),
  );
  const ranges = roots.flatMap((root) => textMatchRanges(root, terms));
  return ranges[target.occurrenceIndex];
}

function textMatchRanges(root: HTMLElement, terms: string[]) {
  const normalizedTerms = Array.from(
    new Set(terms.map((term) => term.toLocaleLowerCase()).filter(Boolean)),
  ).sort((left, right) => right.length - left.length);
  if (normalizedTerms.length === 0) {
    return [];
  }
  const ranges: Range[] = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    const text = node.textContent || "";
    const normalizedText = text.toLocaleLowerCase();
    const occupied: Array<{ end: number; start: number }> = [];
    for (const term of normalizedTerms) {
      let start = normalizedText.indexOf(term);
      while (start >= 0) {
        const end = start + term.length;
        if (
          end <= text.length &&
          !occupied.some((range) => start < range.end && end > range.start)
        ) {
          const range = document.createRange();
          range.setStart(node, start);
          range.setEnd(node, end);
          ranges.push(range);
          occupied.push({ end, start });
        }
        start = normalizedText.indexOf(term, start + Math.max(1, term.length));
      }
    }
    node = walker.nextNode();
  }
  return ranges;
}

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

function pointerHitsVerticalScrollbar(event: PointerEvent, node: HTMLElement) {
  if (node.scrollHeight <= node.clientHeight) {
    return false;
  }
  const rect = node.getBoundingClientRect();
  const scrollbarWidth = Math.max(0, node.offsetWidth - node.clientWidth);
  const hitWidth = Math.max(12, scrollbarWidth);
  return event.clientX >= rect.right - hitWidth && event.clientX <= rect.right;
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
