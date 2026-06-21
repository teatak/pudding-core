import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

type ScrollMode = "bottom" | "history";

const BOTTOM_THRESHOLD_PX = 48;
const USER_SCROLL_INTENT_MS = 500;
const RESIZE_STABILIZE_FRAMES = 14;
const ANCHOR_SELECTOR = "[data-transcript-anchor-id], [data-transcript-item-id]";
const ASSISTANT_ANCHOR_SELECTOR = '[data-transcript-anchor-role="assistant"]';
const ANCHOR_LINE_RATIO = 0.3;

type ScrollAnchor = {
  itemID: string;
  offsetTop: number;
};

type HistorySnapshot = {
  scrollHeight: number;
  scrollTop: number;
};

type HistoryLoader = {
  hasMore: boolean;
  isLoading: boolean;
  loadMore: () => Promise<unknown> | void;
  preloadMarginPx?: number;
};

function scrollToBottomNow(node: HTMLElement) {
  node.scrollTop = Math.max(0, node.scrollHeight - node.clientHeight);
}

export function useTranscriptScroll({
  historyLoader,
  itemKeys,
  sessionID,
}: {
  historyLoader?: HistoryLoader;
  itemKeys: string[];
  sessionID: string;
}) {
  const viewportNodeRef = useRef<HTMLDivElement | null>(null);
  const [viewportNode, setViewportNode] = useState<HTMLDivElement | null>(null);
  const [contentNode, setContentNode] = useState<HTMLDivElement | null>(null);
  const [mode, setModeState] = useState<ScrollMode>("bottom");
  const modeRef = useRef<ScrollMode>("bottom");
  const anchorRef = useRef<ScrollAnchor | null>(null);
  const historySnapshotRef = useRef<HistorySnapshot | null>(null);
  const historyLoaderRef = useRef<HistoryLoader | undefined>(historyLoader);
  const historyLoadLockedRef = useRef(false);
  const programmaticScrollRef = useRef(false);
  const programmaticScrollVersionRef = useRef(0);
  const lastScrollTopRef = useRef(0);
  const lastUserScrollDirectionRef = useRef<"up" | "down" | null>(null);
  const rafRef = useRef<number | null>(null);
  const userScrollIntentUntilRef = useRef(0);
  const itemKeysKey = useMemo(() => itemKeys.join("\n"), [itemKeys]);

  useEffect(() => {
    historyLoaderRef.current = historyLoader;
  }, [historyLoader]);

  const setMode = useCallback((next: ScrollMode) => {
    modeRef.current = next;
    setModeState((current) => (current === next ? current : next));
  }, []);

  const cancelScheduledStick = useCallback(() => {
    if (rafRef.current !== null) {
      window.cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    }
  }, []);

  const markUserScrollIntent = useCallback(() => {
    userScrollIntentUntilRef.current = performance.now() + USER_SCROLL_INTENT_MS;
  }, []);

  const hasRecentUserScrollIntent = useCallback(() => {
    return performance.now() <= userScrollIntentUntilRef.current;
  }, []);

  const markProgrammaticScroll = useCallback(() => {
    const version = programmaticScrollVersionRef.current + 1;
    programmaticScrollVersionRef.current = version;
    programmaticScrollRef.current = true;
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        if (programmaticScrollVersionRef.current !== version) {
          return;
        }
        const node = viewportNodeRef.current;
        if (node) {
          lastScrollTopRef.current = node.scrollTop;
        }
        programmaticScrollRef.current = false;
      });
    });
  }, []);

  const stickToBottomIfNeeded = useCallback(
    ({ stabilizeFrames = 0 }: { stabilizeFrames?: number } = {}) => {
      if (modeRef.current !== "bottom") {
        return;
      }
      const node = viewportNodeRef.current;
      if (!node) {
        return;
      }
      markProgrammaticScroll();
      scrollToBottomNow(node);
      lastScrollTopRef.current = node.scrollTop;

      if (stabilizeFrames <= 0) {
        return;
      }
      cancelScheduledStick();
      let remaining = stabilizeFrames;
      const tick = () => {
        rafRef.current = null;
        if (modeRef.current !== "bottom") {
          return;
        }
        const current = viewportNodeRef.current;
        if (!current) {
          return;
        }
        markProgrammaticScroll();
        scrollToBottomNow(current);
        lastScrollTopRef.current = current.scrollTop;
        remaining -= 1;
        if (remaining > 0) {
          rafRef.current = window.requestAnimationFrame(tick);
        }
      };
      rafRef.current = window.requestAnimationFrame(tick);
    },
    [cancelScheduledStick, markProgrammaticScroll],
  );
  const captureAnchor = useCallback(({ clearOnMiss = true }: { clearOnMiss?: boolean } = {}) => {
    const viewport = viewportNodeRef.current;
    if (!viewport) {
      anchorRef.current = null;
      return null;
    }
    const viewportRect = viewport.getBoundingClientRect();
    const anchor =
      bestVisibleAnchor(viewport.querySelectorAll<HTMLElement>(ASSISTANT_ANCHOR_SELECTOR), viewportRect) ||
      bestVisibleAnchor(viewport.querySelectorAll<HTMLElement>(ANCHOR_SELECTOR), viewportRect);
    if (anchor || clearOnMiss) {
      anchorRef.current = anchor;
    }
    return anchor;
  }, []);
  const preserveHistoryPosition = useCallback(() => {
    const viewport = viewportNodeRef.current;
    const anchor = captureAnchor({ clearOnMiss: false });
    if (viewport) {
      historySnapshotRef.current = {
        scrollHeight: viewport.scrollHeight,
        scrollTop: viewport.scrollTop,
      };
      setMode("history");
    }
    return anchor;
  }, [captureAnchor, setMode]);

  const restoreAnchorNow = useCallback(() => {
    const viewport = viewportNodeRef.current;
    const anchor = anchorRef.current;
    if (!viewport) {
      return false;
    }
    if (anchor) {
      const target = findAnchorTarget(viewport, anchor.itemID);
      if (target) {
        const viewportTop = viewport.getBoundingClientRect().top;
        const nextOffsetTop = target.getBoundingClientRect().top - viewportTop;
        markProgrammaticScroll();
        viewport.scrollTop += nextOffsetTop - anchor.offsetTop;
        lastScrollTopRef.current = viewport.scrollTop;
        return true;
      }
    }
    const snapshot = historySnapshotRef.current;
    if (!snapshot) {
      return false;
    }
    markProgrammaticScroll();
    viewport.scrollTop = Math.max(0, snapshot.scrollTop + viewport.scrollHeight - snapshot.scrollHeight);
    lastScrollTopRef.current = viewport.scrollTop;
    return true;
  }, [markProgrammaticScroll]);
  const maintainHistoryAnchor = useCallback(
    ({ stabilizeFrames = 0 }: { stabilizeFrames?: number } = {}) => {
      if (modeRef.current !== "history") {
        return;
      }
      const restoreOrCapture = () => {
        if (restoreAnchorNow()) {
          return true;
        }
        return Boolean(captureAnchor({ clearOnMiss: false }));
      };
      if (!restoreOrCapture() || stabilizeFrames <= 0) {
        return;
      }
      cancelScheduledStick();
      let remaining = stabilizeFrames;
      const tick = () => {
        rafRef.current = null;
        if (modeRef.current !== "history") {
          return;
        }
        restoreOrCapture();
        remaining -= 1;
        if (remaining > 0) {
          rafRef.current = window.requestAnimationFrame(tick);
        }
      };
      rafRef.current = window.requestAnimationFrame(tick);
    },
    [cancelScheduledStick, captureAnchor, restoreAnchorNow],
  );

  const releaseHistoryLoadLock = useCallback(() => {
    const releaseAfterFrame = (remaining: number) => {
      window.requestAnimationFrame(() => {
        if (modeRef.current === "history") {
          maintainHistoryAnchor({ stabilizeFrames: 1 });
        }
        if (remaining > 0) {
          releaseAfterFrame(remaining - 1);
          return;
        }
        if (historyLoaderRef.current?.isLoading) {
          releaseAfterFrame(2);
          return;
        }
        const viewport = viewportNodeRef.current;
        if (viewport) {
          lastScrollTopRef.current = viewport.scrollTop;
        }
        historyLoadLockedRef.current = false;
      });
    };
    releaseAfterFrame(4);
  }, [maintainHistoryAnchor]);

  const maybeLoadHistory = useCallback(
    (scrollTop: number, isScrollingUp: boolean) => {
      const viewport = viewportNodeRef.current;
      const loader = historyLoaderRef.current;
      if (!viewport || !loader) {
        return;
      }
      const userRequestsOlder =
        isScrollingUp ||
        (scrollTop <= 1 && lastUserScrollDirectionRef.current === "up" && hasRecentUserScrollIntent());
      const preloadMargin = Math.max(loader.preloadMarginPx || 0, Math.round(viewport.clientHeight * 0.75));
      if (
        modeRef.current !== "history" ||
        !userRequestsOlder ||
        !loader.hasMore ||
        loader.isLoading ||
        historyLoadLockedRef.current ||
        scrollTop > preloadMargin
      ) {
        return;
      }
      historyLoadLockedRef.current = true;
      preserveHistoryPosition();
      void Promise.resolve(loader.loadMore())
        .catch(() => undefined)
        .finally(releaseHistoryLoadLock);
    },
    [hasRecentUserScrollIntent, preserveHistoryPosition, releaseHistoryLoadLock],
  );

  const enterBottomMode = useCallback(
    ({ stabilizeFrames = 1 }: { stabilizeFrames?: number } = {}) => {
      setMode("bottom");
      anchorRef.current = null;
      historySnapshotRef.current = null;
      historyLoadLockedRef.current = false;
      lastUserScrollDirectionRef.current = null;
      stickToBottomIfNeeded({ stabilizeFrames });
    },
    [setMode, stickToBottomIfNeeded],
  );
  const viewportRef = useCallback((node: HTMLDivElement | null) => {
    viewportNodeRef.current = node;
    setViewportNode(node);
  }, []);
  const contentRef = useCallback((node: HTMLDivElement | null) => {
    setContentNode(node);
  }, []);

  useLayoutEffect(() => {
    enterBottomMode({ stabilizeFrames: 1 });
  }, [enterBottomMode, sessionID]);

  useLayoutEffect(() => {
    if (modeRef.current === "bottom") {
      stickToBottomIfNeeded({ stabilizeFrames: 1 });
      return;
    }
    maintainHistoryAnchor({ stabilizeFrames: 3 });
  }, [itemKeysKey, maintainHistoryAnchor, stickToBottomIfNeeded]);

  useEffect(() => {
    const node = viewportNode;
    if (!node) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target)) {
        return;
      }
      if (
        event.key === "ArrowUp" ||
        event.key === "ArrowDown" ||
        event.key === "PageUp" ||
        event.key === "PageDown" ||
        event.key === "Home" ||
        event.key === "End" ||
        event.key === " "
      ) {
        markUserScrollIntent();
        if (node.scrollTop <= 1 && (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home")) {
          setMode("history");
          captureAnchor({ clearOnMiss: false });
          maybeLoadHistory(node.scrollTop, true);
        }
      }
    };
    const onScroll = () => {
      const previousScrollTop = lastScrollTopRef.current;
      const currentScrollTop = node.scrollTop;
      lastScrollTopRef.current = currentScrollTop;
      if (programmaticScrollRef.current) {
        return;
      }
      const distance = node.scrollHeight - node.scrollTop - node.clientHeight;
      if (distance <= BOTTOM_THRESHOLD_PX) {
        setMode("bottom");
        anchorRef.current = null;
        historySnapshotRef.current = null;
        return;
      }
      const isScrollingUp = currentScrollTop < previousScrollTop - 1;
      if (isScrollingUp || hasRecentUserScrollIntent()) {
        setMode("history");
        captureAnchor({ clearOnMiss: !isScrollingUp });
        maybeLoadHistory(currentScrollTop, isScrollingUp);
        return;
      }
      if (modeRef.current === "bottom") {
        stickToBottomIfNeeded({ stabilizeFrames: 2 });
        return;
      }
      captureAnchor({ clearOnMiss: false });
    };
    const onWheel = (event: WheelEvent) => {
      markUserScrollIntent();
      if (event.deltaY < 0) {
        lastUserScrollDirectionRef.current = "up";
        if (node.scrollTop <= 1) {
          setMode("history");
          captureAnchor({ clearOnMiss: false });
          maybeLoadHistory(node.scrollTop, true);
        }
      } else if (event.deltaY > 0) {
        lastUserScrollDirectionRef.current = "down";
      }
    };
    node.addEventListener("wheel", onWheel, { passive: true });
    node.addEventListener("touchstart", markUserScrollIntent, { passive: true });
    node.addEventListener("pointerdown", markUserScrollIntent, { passive: true });
    node.addEventListener("scroll", onScroll);
    window.addEventListener("keydown", onKeyDown);
    onScroll();
    return () => {
      node.removeEventListener("wheel", onWheel);
      node.removeEventListener("touchstart", markUserScrollIntent);
      node.removeEventListener("pointerdown", markUserScrollIntent);
      node.removeEventListener("scroll", onScroll);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [
    captureAnchor,
    hasRecentUserScrollIntent,
    markUserScrollIntent,
    maybeLoadHistory,
    setMode,
    sessionID,
    stickToBottomIfNeeded,
    viewportNode,
  ]);

  useEffect(() => {
    const viewport = viewportNode;
    if (!viewport) {
      return;
    }
    const ro = new ResizeObserver(() => {
      if (modeRef.current === "bottom") {
        stickToBottomIfNeeded({ stabilizeFrames: RESIZE_STABILIZE_FRAMES });
        return;
      }
      maintainHistoryAnchor({ stabilizeFrames: 1 });
    });
    ro.observe(viewport);
    if (contentNode) {
      ro.observe(contentNode);
    }
    return () => ro.disconnect();
  }, [contentNode, maintainHistoryAnchor, sessionID, stickToBottomIfNeeded, viewportNode]);

  useEffect(() => {
    const onResize = () => {
      if (modeRef.current === "bottom") {
        stickToBottomIfNeeded({ stabilizeFrames: RESIZE_STABILIZE_FRAMES });
        return;
      }
      maintainHistoryAnchor({ stabilizeFrames: 1 });
    };
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [maintainHistoryAnchor, stickToBottomIfNeeded]);

  useEffect(() => cancelScheduledStick, [cancelScheduledStick]);

  return {
    captureAnchor,
    enterBottomMode,
    mode,
    preserveHistoryPosition,
    showJumpLatest: mode === "history",
    stickToBottomIfNeeded,
    viewportRef,
    contentRef,
  };
}

function bestVisibleAnchor(nodes: NodeListOf<HTMLElement>, viewportRect: DOMRect) {
  const anchorLine = viewportRect.top + viewportRect.height * ANCHOR_LINE_RATIO;
  let firstIntersecting: ScrollAnchor | null = null;
  for (const node of nodes) {
    const rect = node.getBoundingClientRect();
    if (rect.height <= 0 || rect.bottom <= viewportRect.top || rect.top >= viewportRect.bottom) {
      continue;
    }
    const itemID = anchorID(node);
    if (!itemID) {
      continue;
    }
    const anchor = { itemID, offsetTop: rect.top - viewportRect.top };
    if (!firstIntersecting) {
      firstIntersecting = anchor;
    }
    if (rect.top <= anchorLine && rect.bottom >= anchorLine) {
      return anchor;
    }
  }
  return firstIntersecting;
}

function anchorID(node: HTMLElement) {
  return node.dataset.transcriptAnchorId || node.dataset.transcriptItemId || "";
}

function findAnchorTarget(viewport: HTMLElement, itemID: string) {
  for (const node of viewport.querySelectorAll<HTMLElement>(ASSISTANT_ANCHOR_SELECTOR)) {
    if (anchorID(node) === itemID) {
      return node;
    }
  }
  for (const node of viewport.querySelectorAll<HTMLElement>(ANCHOR_SELECTOR)) {
    if (anchorID(node) === itemID) {
      return node;
    }
  }
  return null;
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
