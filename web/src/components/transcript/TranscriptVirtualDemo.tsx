import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, History, Loader2, RadioTower, RotateCcw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";

import { TranscriptTurn } from "./TranscriptTurn";
import type { TranscriptTurnVM } from "./types";
import { useTranscriptData } from "./useTranscriptData";

type DemoTurn = {
  id: string;
  role: "assistant" | "user";
  serial: number;
  text: string;
};

const INITIAL_COUNT = 24;
const PAGE_SIZE = 20;
const OVERSCAN = 6;
const TOP_LOAD_PX = 120;
const BOTTOM_STICK_STABILIZE_FRAMES = 4;

type HistoryLoadState = "idle" | "loading" | "settling";
type ViewportAnchor = { top: number; turnID: string };

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
  const [state, setState] = useState<HistoryLoadState>("idle");

  useEffect(() => {
    optionsRef.current = { getScrollElement, hasMore, isLoading, isNearTop, loadMore };
  }, [getScrollElement, hasMore, isLoading, isNearTop, loadMore]);

  const setControllerState = useCallback((next: HistoryLoadState) => {
    stateRef.current = next;
    setState((current) => (current === next ? current : next));
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

  return useMemo(() => ({ check, request, reset, state }), [check, request, reset, state]);
}

export function TranscriptVirtualDemo({
  sessionID,
  sessionRunning = false,
  token,
}: {
  sessionID?: string;
  sessionRunning?: boolean;
  token?: string;
}) {
  if (token && sessionID) {
    return <TranscriptVirtualRealDemo sessionID={sessionID} sessionRunning={sessionRunning} token={token} />;
  }
  return <TranscriptVirtualFakeDemo />;
}

function TranscriptVirtualFakeDemo() {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const streamTimerRef = useRef<number | null>(null);
  const lastScrollTopRef = useRef(0);
  const [isAtLatest, setIsAtLatest] = useState(true);
  const [loadingHistory, setLoadingHistory] = useState(false);
  const [turns, setTurns] = useState(() => makeTurns(1, INITIAL_COUNT));
  const oldestSerial = turns[0]?.serial ?? 1;
  const hasMoreHistory = oldestSerial > -80;
  const virtualizer = useVirtualizer({
    anchorTo: "end",
    count: turns.length,
    directDomUpdates: true,
    estimateSize: (index) => estimateTurnHeight(turns[index]),
    followOnAppend: true,
    gap: 16,
    getItemKey: (index) => turns[index]?.id || index,
    getScrollElement: () => scrollRef.current,
    overscan: OVERSCAN,
    paddingEnd: 32,
    paddingStart: 16,
    scrollEndThreshold: 80,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualItems = virtualizer.getVirtualItems();

  const updatePinned = useCallback(() => {
    const node = scrollRef.current;
    if (!node) {
      return;
    }
    setIsAtLatest(node.scrollHeight - node.clientHeight - node.scrollTop <= 80);
  }, []);

  const loadHistory = useCallback(() => {
    setLoadingHistory(true);
    return new Promise<void>((resolve) => {
      window.setTimeout(() => {
        setTurns((current) => {
          const first = current[0]?.serial ?? 1;
          return [...makeTurns(first - PAGE_SIZE, PAGE_SIZE), ...current];
        });
        setLoadingHistory(false);
        resolve();
      }, 160);
    });
  }, []);
  const isNearTop = useCallback(() => {
    const node = scrollRef.current;
    return Boolean(node && node.scrollTop < TOP_LOAD_PX);
  }, []);
  const historyLoader = useHistoryLoadController({
    getScrollElement: () => scrollRef.current,
    hasMore: hasMoreHistory,
    isLoading: loadingHistory,
    isNearTop,
    loadMore: loadHistory,
  });

  useEffect(() => {
    const node = scrollRef.current;
    if (!node) {
      return;
    }
    const onScroll = () => {
      const previousScrollTop = lastScrollTopRef.current;
      const nextScrollTop = node.scrollTop;
      lastScrollTopRef.current = nextScrollTop;
      updatePinned();
      if (nextScrollTop < previousScrollTop - 1 || nextScrollTop < TOP_LOAD_PX) {
        historyLoader.request();
      }
      historyLoader.check();
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) {
        historyLoader.request();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target)) {
        return;
      }
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") {
        historyLoader.request();
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
  }, [historyLoader.check, historyLoader.request, updatePinned]);

  useEffect(() => {
    window.requestAnimationFrame(() => virtualizer.scrollToEnd());
  }, [virtualizer]);

  useEffect(() => {
    return () => {
      if (streamTimerRef.current !== null) {
        window.clearInterval(streamTimerRef.current);
      }
    };
  }, []);

  const reset = useCallback(() => {
    if (streamTimerRef.current !== null) {
      window.clearInterval(streamTimerRef.current);
      streamTimerRef.current = null;
    }
    historyLoader.reset();
    setLoadingHistory(false);
    setTurns(makeTurns(1, INITIAL_COUNT));
    window.requestAnimationFrame(() => virtualizer.scrollToEnd());
  }, [historyLoader.reset, virtualizer]);

  const stream = useCallback(() => {
    if (streamTimerRef.current !== null) {
      return;
    }
    const id = `stream:${Date.now()}`;
    const serial = (turns.at(-1)?.serial ?? 0) + 1;
    const fullText = streamText(serial);
    let cursor = 0;
    setTurns((current) => [...current, { id, role: "assistant", serial, text: "" }]);
    window.requestAnimationFrame(() => virtualizer.scrollToEnd());
    streamTimerRef.current = window.setInterval(() => {
      cursor = Math.min(fullText.length, cursor + 8);
      setTurns((current) => current.map((turn) => (turn.id === id ? { ...turn, text: fullText.slice(0, cursor) } : turn)));
      if (cursor >= fullText.length && streamTimerRef.current !== null) {
        window.clearInterval(streamTimerRef.current);
        streamTimerRef.current = null;
      }
    }, 35);
  }, [turns, virtualizer]);

  const status = useMemo(() => {
    if (historyLoader.state !== "idle" || loadingHistory) {
      return "loading history";
    }
    return isAtLatest ? "pinned to latest" : "reading history";
  }, [historyLoader.state, isAtLatest, loadingHistory]);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-background">
      <div className="pointer-events-none absolute inset-x-0 top-3 z-20 flex justify-center">
        <div className="pointer-events-auto flex items-center gap-2 rounded-full border border-border bg-background/95 px-2 py-1 shadow-sm backdrop-blur">
          <span className="px-2 text-xs font-medium text-muted-foreground">{turns.length} turns · {status}</span>
          <Button size="sm" type="button" variant="outline" onClick={() => historyLoader.request({ force: true })}>
            <History />
            History
          </Button>
          <Button size="sm" type="button" variant="outline" onClick={stream}>
            <RadioTower />
            Stream
          </Button>
          <Button size="sm" type="button" variant="outline" onClick={reset}>
            <RotateCcw />
            Reset
          </Button>
        </div>
      </div>

      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto overscroll-none [contain:strict] [overflow-anchor:none]">
        <div ref={virtualizer.containerRef} className="relative mx-auto w-[calc(100%-2.5rem)] max-w-3xl">
          {virtualItems.map((virtualItem) => {
            const turn = turns[virtualItem.index];
            if (!turn) {
              return null;
            }
            return (
              <div
                key={virtualItem.key}
                ref={virtualizer.measureElement}
                className="absolute top-0 left-0 w-full"
                data-index={virtualItem.index}
              >
                <DemoBubble turn={turn} />
              </div>
            );
          })}
        </div>
      </div>

      {!isAtLatest ? (
        <Button
          className="absolute right-5 bottom-28 z-20 rounded-full border border-border bg-card shadow-md hover:bg-muted"
          size="icon"
          type="button"
          variant="ghost"
          onClick={() => virtualizer.scrollToEnd()}
        >
          <ArrowDown />
        </Button>
      ) : null}

      <div className="shrink-0 px-5 pb-5">
        <div className="mx-auto flex min-h-24 max-w-3xl items-end gap-3 rounded-[28px] border border-border bg-card p-4 shadow-sm">
          <div className="min-w-0 flex-1 text-sm text-muted-foreground">Demo composer. No real submit, no SSE, no query.</div>
          <Button type="button" onClick={stream}>
            Stream
          </Button>
        </div>
      </div>
    </div>
  );
}

function TranscriptVirtualRealDemo({
  sessionID,
  sessionRunning,
  token,
}: {
  sessionID: string;
  sessionRunning: boolean;
  token: string;
}) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const isAtLatestRef = useRef(true);
  const lastScrollTopRef = useRef(0);
  const [scrollNode, setScrollNode] = useState<HTMLDivElement | null>(null);
  const [isAtLatest, setIsAtLatest] = useState(true);
  const { markAssistantRevealed, transcript, turnsQuery, updateQueued } = useTranscriptData({
    sessionID,
    sessionRunning,
    token,
  });
  const turns = transcript.turnVMs;
  const virtualizer = useVirtualizer({
    anchorTo: "end",
    count: turns.length,
    directDomUpdates: true,
    estimateSize: (index) => estimateRealTurnHeight(turns[index]),
    followOnAppend: true,
    gap: 16,
    getItemKey: (index) => turns[index]?.key || index,
    getScrollElement: () => scrollRef.current,
    overscan: OVERSCAN,
    paddingEnd: 32,
    paddingStart: 16,
    scrollEndThreshold: 80,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualItems = virtualizer.getVirtualItems();

  const updatePinned = useCallback(() => {
    const node = scrollRef.current;
    if (!node) {
      return;
    }
    const pinned = node.scrollHeight - node.clientHeight - node.scrollTop <= 80;
    isAtLatestRef.current = pinned;
    setIsAtLatest(pinned);
  }, []);

  const loadHistory = useCallback(async () => {
    const anchor = captureViewportAnchor(scrollRef.current);
    const result = await turnsQuery.fetchNextPage({ cancelRefetch: false });
    restoreViewportAnchorOverFrames(scrollRef.current, anchor);
    return result;
  }, [turnsQuery]);
  const isNearTop = useCallback(() => {
    const node = scrollRef.current;
    return Boolean(node && node.scrollTop < TOP_LOAD_PX);
  }, []);
  const historyLoader = useHistoryLoadController({
    getScrollElement: () => scrollRef.current,
    hasMore: Boolean(turnsQuery.hasNextPage),
    isLoading: turnsQuery.isFetchingNextPage,
    isNearTop,
    loadMore: loadHistory,
  });

  const scrollToLatest = useCallback(() => {
    const node = scrollRef.current;
    if (!node) {
      return;
    }
    virtualizer.scrollToEnd();
    node.scrollTop = Math.max(0, node.scrollHeight - node.clientHeight);
    lastScrollTopRef.current = node.scrollTop;
    isAtLatestRef.current = true;
    setIsAtLatest(true);
  }, [virtualizer]);

  const stickToLatestIfPinned = useCallback(
    (frames = 1) => {
      if (!isAtLatestRef.current) {
        return;
      }
      let remaining = frames;
      const tick = () => {
        if (!isAtLatestRef.current) {
          return;
        }
        scrollToLatest();
        remaining -= 1;
        if (remaining > 0) {
          window.requestAnimationFrame(tick);
        }
      };
      tick();
    },
    [scrollToLatest],
  );

  const followIfPinned = useCallback(() => {
    if (!isAtLatestRef.current) {
      return;
    }
    stickToLatestIfPinned(2);
  }, [stickToLatestIfPinned]);

  const handleScrollRef = useCallback((node: HTMLDivElement | null) => {
    scrollRef.current = node;
    if (node) {
      lastScrollTopRef.current = node.scrollTop;
    }
    setScrollNode(node);
  }, []);

  useEffect(() => {
    const node = scrollNode;
    if (!node) {
      return;
    }
    const onScroll = () => {
      const previousScrollTop = lastScrollTopRef.current;
      const nextScrollTop = node.scrollTop;
      lastScrollTopRef.current = nextScrollTop;
      updatePinned();
      if (nextScrollTop < previousScrollTop - 1 || nextScrollTop < TOP_LOAD_PX) {
        historyLoader.request();
      }
      historyLoader.check();
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) {
        historyLoader.request();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target)) {
        return;
      }
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") {
        historyLoader.request();
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
  }, [historyLoader.check, historyLoader.request, scrollNode, updatePinned]);

  useEffect(() => {
    historyLoader.reset();
  }, [historyLoader.reset, sessionID]);

  useEffect(() => {
    if (!turnsQuery.isSuccess) {
      return;
    }
    window.requestAnimationFrame(scrollToLatest);
  }, [scrollToLatest, sessionID, turnsQuery.isSuccess]);

  useEffect(() => {
    if (!scrollNode) {
      return;
    }
    const stick = () => stickToLatestIfPinned(BOTTOM_STICK_STABILIZE_FRAMES);
    const observer = new ResizeObserver(stick);
    observer.observe(scrollNode);
    window.addEventListener("resize", stick);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", stick);
    };
  }, [scrollNode, stickToLatestIfPinned]);

  const status = historyLoader.state !== "idle" || turnsQuery.isFetchingNextPage ? "loading history" : isAtLatest ? "pinned to latest" : "reading history";

  if (turnsQuery.isPending) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground">
        <Loader2 className="size-5 animate-spin" />
      </div>
    );
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-background">
      <div className="pointer-events-none absolute inset-x-0 top-3 z-20 flex justify-center">
        <div className="pointer-events-auto flex items-center gap-2 rounded-full border border-border bg-background/95 px-2 py-1 shadow-sm backdrop-blur">
          <span className="px-2 text-xs font-medium text-muted-foreground">{turns.length} real turns · {status}</span>
          <Button
            disabled={!turnsQuery.hasNextPage || historyLoader.state !== "idle" || turnsQuery.isFetchingNextPage}
            size="sm"
            type="button"
            variant="outline"
            onClick={() => historyLoader.request({ force: true })}
          >
            <History />
            History
          </Button>
        </div>
      </div>

      <div ref={handleScrollRef} className="min-h-0 flex-1 overflow-y-auto overscroll-none [contain:strict] [overflow-anchor:none]">
        <div ref={virtualizer.containerRef} className="relative mx-auto w-[calc(100%-2.5rem)] max-w-3xl">
          {virtualItems.map((virtualItem) => {
            const turn = turns[virtualItem.index];
            if (!turn) {
              return null;
            }
            return (
              <div
                key={virtualItem.key}
                ref={virtualizer.measureElement}
                className="absolute top-0 left-0 w-full"
                data-index={virtualItem.index}
              >
                <TranscriptTurn
                  turn={turn}
                  onAssistantContentGrow={followIfPinned}
                  onAssistantRevealComplete={markAssistantRevealed}
                  onQueuedCancel={(clientMessageID) => updateQueued(clientMessageID, { status: "cancelled" })}
                  onQueuedEditStart={(clientMessageID) => updateQueued(clientMessageID, { status: "editing" })}
                  onQueuedSave={(clientMessageID, text) => updateQueued(clientMessageID, { status: "queued", text })}
                />
              </div>
            );
          })}
        </div>
      </div>

      {!isAtLatest ? (
        <Button
          className="absolute right-5 bottom-28 z-20 rounded-full border border-border bg-card shadow-md hover:bg-muted"
          size="icon"
          type="button"
          variant="ghost"
          onClick={() => virtualizer.scrollToEnd()}
        >
          <ArrowDown />
        </Button>
      ) : null}

      <div className="shrink-0 px-5 pb-5">
        <div className="mx-auto flex min-h-24 max-w-3xl items-end rounded-[28px] border border-border bg-card p-4 text-sm text-muted-foreground shadow-sm">
          Real data demo. Composer disabled.
        </div>
      </div>
    </div>
  );
}

function DemoBubble({ turn }: { turn: DemoTurn }) {
  const isUser = turn.role === "user";
  return (
    <div className={isUser ? "flex justify-end" : "flex justify-start"}>
      <article
        className={
          isUser
            ? "max-w-[72%] rounded-2xl bg-muted px-4 py-3 text-base font-semibold leading-7 text-foreground"
            : "max-w-full whitespace-pre-wrap px-0 py-1 text-base font-semibold leading-8 text-foreground"
        }
      >
        <div className="mb-1 text-xs font-medium text-muted-foreground">{isUser ? "USER" : "ASSISTANT"} · #{turn.serial}</div>
        {turn.text || "Streaming..."}
      </article>
    </div>
  );
}

function makeTurns(start: number, count: number) {
  return Array.from({ length: count }, (_, index) => {
    const serial = start + index;
    return {
      id: `demo:${serial}`,
      role: serial % 4 === 0 ? "user" : "assistant",
      serial,
      text: demoText(serial),
    } satisfies DemoTurn;
  });
}

function demoText(serial: number) {
  const base = [
    `This is a local virtualized chat turn ${serial}.`,
    "The scroll container owns the virtualizer directly, and the inner container is measured by TanStack Virtual.",
    "Older history is prepended above this message while the current viewport should stay visually stable.",
  ];
  const extra = Math.abs(serial) % 5;
  for (let index = 0; index < extra; index += 1) {
    base.push(`Extra paragraph ${index + 1}: variable height content to force real measurement corrections after render.`);
  }
  return base.join("\n\n");
}

function streamText(serial: number) {
  return [
    `Streaming local demo turn ${serial}.`,
    "Tokens are appended into the newest assistant row. If the viewport is pinned, it should follow the end.",
    "If you scroll up, the new output should grow below without stealing the viewport.",
    "This intentionally avoids real backend data so we can isolate list behavior.",
  ].join("\n\n");
}

function estimateTurnHeight(turn: DemoTurn | undefined) {
  if (!turn) {
    return 180;
  }
  return turn.role === "user" ? 96 : 160 + (Math.abs(turn.serial) % 5) * 56;
}

function estimateRealTurnHeight(turn: TranscriptTurnVM | undefined) {
  if (!turn) {
    return 180;
  }
  if (turn.user && !turn.assistant) {
    return 96;
  }
  if (!turn.assistant) {
    return 140;
  }
  return 220;
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

function restoreViewportAnchorOverFrames(node: HTMLElement | null, anchor: ViewportAnchor | null) {
  if (!node || !anchor) {
    return;
  }
  let remaining = 8;
  const tick = () => {
    restoreViewportAnchor(node, anchor);
    remaining -= 1;
    if (remaining > 0) {
      window.requestAnimationFrame(tick);
    }
  };
  window.requestAnimationFrame(tick);
}

function restoreViewportAnchor(node: HTMLElement, anchor: ViewportAnchor) {
  const element = node.querySelector<HTMLElement>(`[data-transcript-turn-id="${CSS.escape(anchor.turnID)}"]`);
  if (!element) {
    return;
  }
  const viewportRect = node.getBoundingClientRect();
  const top = element.getBoundingClientRect().top - viewportRect.top;
  node.scrollTop += top - anchor.top;
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
