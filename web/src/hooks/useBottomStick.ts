import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

type BottomFollowMode = "following" | "detached";

const RESIZE_STABILIZE_FRAMES = 2;

function scrollToBottom(node: HTMLElement) {
  node.scrollTop = Math.max(0, node.scrollHeight - node.clientHeight);
}

function maxScrollTop(node: HTMLElement) {
  return Math.max(0, node.scrollHeight - node.clientHeight);
}

function isAtScrollEnd(node: HTMLElement) {
  return Math.ceil(node.scrollTop) >= Math.floor(maxScrollTop(node));
}

function isAtScrollStart(node: HTMLElement) {
  return node.scrollTop <= 0;
}

export function useBottomStick({ sessionID }: { sessionID: string }) {
  const viewportNodeRef = useRef<HTMLDivElement | null>(null);
  const [viewportNode, setViewportNode] = useState<HTMLDivElement | null>(null);
  const [mode, setModeState] = useState<BottomFollowMode>("following");
  const modeRef = useRef<BottomFollowMode>("following");
  const rafRef = useRef<number | null>(null);
  const programmaticVersionRef = useRef(0);
  const programmaticScrollRef = useRef(false);
  const lastScrollTopRef = useRef(0);

  const viewportRef = useCallback((node: HTMLDivElement | null) => {
    viewportNodeRef.current = node;
    setViewportNode(node);
  }, []);

  const contentRef = useCallback((_node: HTMLDivElement | null) => {}, []);

  const setMode = useCallback((next: BottomFollowMode) => {
    modeRef.current = next;
    setModeState((current) => (current === next ? current : next));
  }, []);

  const cancelScheduledStick = useCallback(() => {
    if (rafRef.current !== null) {
      window.cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    }
  }, []);

  const markProgrammaticScroll = useCallback(() => {
    const version = programmaticVersionRef.current + 1;
    programmaticVersionRef.current = version;
    programmaticScrollRef.current = true;
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        if (programmaticVersionRef.current === version) {
          programmaticScrollRef.current = false;
        }
      });
    });
  }, []);

  const stickToBottomIfNeeded = useCallback(
    ({ stabilizeFrames = 0 }: { stabilizeFrames?: number } = {}) => {
      if (modeRef.current !== "following") {
        return;
      }
      const node = viewportNodeRef.current;
      if (!node) {
        return;
      }

      markProgrammaticScroll();
      scrollToBottom(node);
      lastScrollTopRef.current = node.scrollTop;

      if (stabilizeFrames <= 0) {
        return;
      }

      cancelScheduledStick();
      let remaining = stabilizeFrames;
      const tick = () => {
        rafRef.current = null;
        if (modeRef.current !== "following") {
          return;
        }
        const current = viewportNodeRef.current;
        if (!current) {
          return;
        }
        markProgrammaticScroll();
        scrollToBottom(current);
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

  const enterBottomMode = useCallback(
    ({ stabilizeFrames = 1 }: { stabilizeFrames?: number } = {}) => {
      setMode("following");
      stickToBottomIfNeeded({ stabilizeFrames });
    },
    [setMode, stickToBottomIfNeeded],
  );

  useLayoutEffect(() => {
    enterBottomMode({ stabilizeFrames: 2 });
  }, [enterBottomMode, sessionID]);

  useEffect(() => {
    const node = viewportNode;
    if (!node) {
      return;
    }
    lastScrollTopRef.current = node.scrollTop;
    const detach = () => {
      cancelScheduledStick();
      setMode("detached");
    };
    const followIfAtEnd = () => {
      if (isAtScrollEnd(node)) {
        setMode("following");
      }
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) {
        if (isAtScrollStart(node)) {
          event.preventDefault();
        }
        detach();
        return;
      }
      if (event.deltaY > 0) {
        if (isAtScrollEnd(node)) {
          event.preventDefault();
        }
        followIfAtEnd();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target)) {
        return;
      }
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") {
        detach();
      }
    };
    const onScroll = () => {
      const previousScrollTop = lastScrollTopRef.current;
      const currentScrollTop = node.scrollTop;
      lastScrollTopRef.current = currentScrollTop;
      if (programmaticScrollRef.current) {
        return;
      }
      if (currentScrollTop < previousScrollTop) {
        detach();
        return;
      }
      if (currentScrollTop > previousScrollTop) {
        followIfAtEnd();
      }
    };
    node.addEventListener("wheel", onWheel, { passive: false });
    node.addEventListener("scroll", onScroll);
    window.addEventListener("keydown", onKeyDown);
    onScroll();
    return () => {
      node.removeEventListener("wheel", onWheel);
      node.removeEventListener("scroll", onScroll);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [cancelScheduledStick, setMode, viewportNode]);

  useEffect(() => {
    const viewport = viewportNode;
    if (!viewport) {
      return;
    }
    const observer = new ResizeObserver(() => {
      stickToBottomIfNeeded({ stabilizeFrames: RESIZE_STABILIZE_FRAMES });
    });
    observer.observe(viewport);
    return () => observer.disconnect();
  }, [stickToBottomIfNeeded, viewportNode]);

  useEffect(() => {
    const onResize = () => {
      stickToBottomIfNeeded({ stabilizeFrames: RESIZE_STABILIZE_FRAMES });
    };
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [stickToBottomIfNeeded]);

  useEffect(() => cancelScheduledStick, [cancelScheduledStick]);

  return {
    contentRef,
    enterBottomMode,
    mode,
    showJumpLatest: mode === "detached",
    stickToBottomIfNeeded,
    viewportRef,
  };
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
