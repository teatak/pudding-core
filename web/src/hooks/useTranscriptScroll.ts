import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

type ScrollMode = "bottom" | "history";

const BOTTOM_THRESHOLD_PX = 48;
const USER_SCROLL_INTENT_MS = 500;
const RESIZE_STABILIZE_FRAMES = 14;
const ITEM_SELECTOR = "[data-transcript-item-id]";
const ASSISTANT_ITEM_SELECTOR = '[data-transcript-item-role="assistant"]';

type ScrollAnchor = {
  itemID: string;
  offsetTop: number;
};

function scrollToBottomNow(node: HTMLElement) {
  node.scrollTop = Math.max(0, node.scrollHeight - node.clientHeight);
}

export function useTranscriptScroll({
  itemKeys,
  sessionID,
}: {
  itemKeys: string[];
  sessionID: string;
}) {
  const viewportNodeRef = useRef<HTMLDivElement | null>(null);
  const [viewportNode, setViewportNode] = useState<HTMLDivElement | null>(null);
  const [contentNode, setContentNode] = useState<HTMLDivElement | null>(null);
  const [mode, setModeState] = useState<ScrollMode>("bottom");
  const modeRef = useRef<ScrollMode>("bottom");
  const anchorRef = useRef<ScrollAnchor | null>(null);
  const rafRef = useRef<number | null>(null);
  const userScrollIntentUntilRef = useRef(0);
  const itemKeysKey = useMemo(() => itemKeys.join("\n"), [itemKeys]);

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

  const stickToBottomIfNeeded = useCallback(
    ({ stabilizeFrames = 0 }: { stabilizeFrames?: number } = {}) => {
      if (modeRef.current !== "bottom") {
        return;
      }
      const node = viewportNodeRef.current;
      if (!node) {
        return;
      }
      scrollToBottomNow(node);

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
        scrollToBottomNow(current);
        remaining -= 1;
        if (remaining > 0) {
          rafRef.current = window.requestAnimationFrame(tick);
        }
      };
      rafRef.current = window.requestAnimationFrame(tick);
    },
    [cancelScheduledStick],
  );
  const captureAnchor = useCallback(({ clearOnMiss = true }: { clearOnMiss?: boolean } = {}) => {
    const viewport = viewportNodeRef.current;
    if (!viewport) {
      anchorRef.current = null;
      return null;
    }
    const viewportRect = viewport.getBoundingClientRect();
    const assistantAnchor = firstTopVisibleAnchor(
      viewport.querySelectorAll<HTMLElement>(ASSISTANT_ITEM_SELECTOR),
      viewportRect,
    );
    if (assistantAnchor || clearOnMiss) {
      anchorRef.current = assistantAnchor;
    }
    return assistantAnchor;
  }, []);

  const restoreAnchorNow = useCallback(() => {
    const viewport = viewportNodeRef.current;
    const anchor = anchorRef.current;
    if (!viewport || !anchor) {
      return false;
    }
    const nodes = viewport.querySelectorAll<HTMLElement>(ITEM_SELECTOR);
    let target: HTMLElement | null = null;
    for (const node of nodes) {
      if (node.dataset.transcriptItemId === anchor.itemID) {
        target = node;
        break;
      }
    }
    if (!target) {
      return false;
    }
    const viewportTop = viewport.getBoundingClientRect().top;
    const nextOffsetTop = target.getBoundingClientRect().top - viewportTop;
    viewport.scrollTop += nextOffsetTop - anchor.offsetTop;
    return true;
  }, []);
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

  const enterBottomMode = useCallback(
    ({ stabilizeFrames = 1 }: { stabilizeFrames?: number } = {}) => {
      setMode("bottom");
      anchorRef.current = null;
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
    maintainHistoryAnchor({ stabilizeFrames: 1 });
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
      }
    };
    const onScroll = () => {
      const distance = node.scrollHeight - node.scrollTop - node.clientHeight;
      if (distance <= BOTTOM_THRESHOLD_PX) {
        setMode("bottom");
        anchorRef.current = null;
        return;
      }
      if (modeRef.current === "bottom" && !hasRecentUserScrollIntent()) {
        stickToBottomIfNeeded({ stabilizeFrames: 2 });
        return;
      }
      setMode("history");
      captureAnchor({ clearOnMiss: hasRecentUserScrollIntent() });
    };
    node.addEventListener("wheel", markUserScrollIntent, { passive: true });
    node.addEventListener("touchstart", markUserScrollIntent, { passive: true });
    node.addEventListener("pointerdown", markUserScrollIntent, { passive: true });
    node.addEventListener("scroll", onScroll);
    window.addEventListener("keydown", onKeyDown);
    onScroll();
    return () => {
      node.removeEventListener("wheel", markUserScrollIntent);
      node.removeEventListener("touchstart", markUserScrollIntent);
      node.removeEventListener("pointerdown", markUserScrollIntent);
      node.removeEventListener("scroll", onScroll);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [
    captureAnchor,
    hasRecentUserScrollIntent,
    markUserScrollIntent,
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
    showJumpLatest: mode === "history",
    stickToBottomIfNeeded,
    viewportRef,
    contentRef,
  };
}

function firstTopVisibleAnchor(nodes: NodeListOf<HTMLElement>, viewportRect: DOMRect) {
  for (const node of nodes) {
    const rect = node.getBoundingClientRect();
    if (rect.top < viewportRect.top || rect.top > viewportRect.bottom) {
      continue;
    }
    const itemID = node.dataset.transcriptItemId;
    if (!itemID) {
      continue;
    }
    return { itemID, offsetTop: rect.top - viewportRect.top };
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
