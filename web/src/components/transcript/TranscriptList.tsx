import { useVirtualizer } from "@tanstack/react-virtual";
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
const LIST_PADDING_TOP_PX = 22;
const TURN_GAP_PX = 12;
const TURN_OVERSCAN = 8;
const SCROLL_END_THRESHOLD_PX = 8;
const TURN_ESTIMATED_HEIGHT_PX = 320;
const TAIL_ESTIMATED_HEIGHT_PX = 180;
const TAIL_ITEM_KEY = "pudding-transcript-tail";
const TURN_REVEAL_CLEAR_MS = 2400;
const TURN_REVEAL_TOP_RATIO = 0.38;

type HistoryLoadState = "idle" | "loading" | "settling";

export const TranscriptList = memo(function TranscriptList({
  cloningMessageID,
  disclosure,
  displaySettings,
  footer,
  hasMoreHistory,
  isLoadingHistory,
  jumpLatestSignal,
  onAssistantRevealComplete,
  onCloneMessage,
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
  cloningMessageID?: string;
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  footer?: ReactNode;
  hasMoreHistory: boolean;
  isLoadingHistory: boolean;
  jumpLatestSignal: number;
  onAssistantRevealComplete?: (turnID: string) => void;
  onCloneMessage?: (messageID: string) => void;
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
  const activeSearchElementsRef = useRef<HTMLElement[]>([]);
  const activeSearchTargetRef = useRef("");
  const followLatestRef = useRef(true);
  const initialScrollSessionRef = useRef("");
  const isAtLatestRef = useRef(true);
  const listElementRef = useRef<HTMLDivElement | null>(null);
  const previousLastTurnKeyRef = useRef<string | null>(null);
  const revealedMessageElementsRef = useRef<HTMLElement[]>([]);
  const revealedTurnSerialRef = useRef(0);
  const revealClearTimerRef = useRef<number | null>(null);
  const structure = useTranscriptStructure(sessionID, turns);
  const { itemKeys, turnIndexByID } = structure;
  const turnCount = itemKeys.length - 1;
  const lastTurnKey = itemKeys[turnCount - 1] || "";
  const shouldFollowTurnAppend = isAtLatestRef.current;

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

  const getItemKey = useCallback((index: number) => itemKeys[index] ?? index, [itemKeys]);
  const estimateSize = useCallback(
    (index: number) => index === turnCount ? TAIL_ESTIMATED_HEIGHT_PX : TURN_ESTIMATED_HEIGHT_PX,
    [turnCount],
  );
  const handleVirtualizerChange = useCallback(
    (instance: { isAtEnd: (threshold?: number) => boolean }) => {
      const atLatest = instance.isAtEnd(SCROLL_END_THRESHOLD_PX);
      if (atLatest || !followLatestRef.current) {
        setLatestState(atLatest);
      }
    },
    [setLatestState],
  );
  const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>({
    anchorTo: "end",
    count: itemKeys.length,
    estimateSize,
    followOnAppend: false,
    gap: TURN_GAP_PX,
    getItemKey,
    getScrollElement: () => scrollElement,
    onChange: handleVirtualizerChange,
    overscan: TURN_OVERSCAN,
    paddingStart: LIST_PADDING_TOP_PX,
    scrollEndThreshold: SCROLL_END_THRESHOLD_PX,
    // resizeItem 会同步修正 scrollTop；turn transform 必须在同一帧提交。
    useFlushSync: true,
  });
  const virtualItems = virtualizer.getVirtualItems();
  const virtualRenderVersion = virtualItems.map((item) => item.key).join("\u0000");
  const measureElement = useCallback((node: HTMLDivElement | null) => {
    if (!node) {
      virtualizer.measureElement(null);
      return;
    }
    queueMicrotask(() => {
      if (node.isConnected) {
        virtualizer.measureElement(node);
      }
    });
  }, [virtualizer]);
  const setListElement = useCallback((node: HTMLDivElement | null) => {
    listElementRef.current = node;
  }, []);

  const isNearTop = useCallback(
    () => Boolean(scrollElement && scrollElement.scrollTop < HISTORY_LOAD_SCROLL_TOP_PX),
    [scrollElement],
  );
  const historyLoader = useHistoryLoadController({
    getScrollElement: () => scrollElement,
    hasMore: hasMoreHistory,
    isLoading: isLoadingHistory,
    isNearTop,
    loadMore: onLoadHistory,
  });

  useLayoutEffect(() => {
    virtualizer.measure();
    initialScrollSessionRef.current = "";
    previousLastTurnKeyRef.current = null;
    followLatestRef.current = true;
    activeSearchTargetRef.current = "";
    revealedTurnSerialRef.current = 0;
    historyLoader.reset();
  }, [historyLoader.reset, sessionID, virtualizer]);

  useLayoutEffect(() => {
    if (
      !scrollElement ||
      itemKeys.length === 0 ||
      initialScrollSessionRef.current === sessionID ||
      turnReveal ||
      searchState.target
    ) {
      return;
    }
    initialScrollSessionRef.current = sessionID;
    followLatestRef.current = true;
    virtualizer.scrollToEnd({ behavior: "instant" });
    setLatestState(true);
  }, [itemKeys.length, scrollElement, searchState.target, sessionID, setLatestState, turnReveal, virtualizer]);

  useLayoutEffect(() => {
    const previousLastTurnKey = previousLastTurnKeyRef.current;
    previousLastTurnKeyRef.current = lastTurnKey;
    if (!previousLastTurnKey || previousLastTurnKey === lastTurnKey || !shouldFollowTurnAppend) {
      return;
    }
    followLatestRef.current = true;
    virtualizer.scrollToEnd({ behavior: "auto" });
  }, [lastTurnKey, shouldFollowTurnAppend, virtualizer]);

  useLayoutEffect(() => {
    if (jumpLatestSignal <= 0) {
      return;
    }
    followLatestRef.current = true;
    virtualizer.scrollToEnd({ behavior: "auto" });
  }, [jumpLatestSignal, virtualizer]);

  useEffect(() => {
    const node = listElementRef.current;
    if (!node) {
      return;
    }
    let followFrame: number | null = null;
    let previousHeight = node.getBoundingClientRect().height;
    const observer = new ResizeObserver(() => {
      const nextHeight = node.getBoundingClientRect().height;
      if (Math.abs(nextHeight - previousHeight) < 1) {
        return;
      }
      previousHeight = nextHeight;
      if (!followLatestRef.current) {
        return;
      }
      if (followFrame !== null) {
        return;
      }
      followFrame = window.requestAnimationFrame(() => {
        followFrame = null;
        if (!followLatestRef.current) {
          return;
        }
        virtualizer.scrollToEnd({ behavior: "auto" });
        setLatestState(true);
      });
    });
    observer.observe(node);
    return () => {
      observer.disconnect();
      if (followFrame !== null) {
        window.cancelAnimationFrame(followFrame);
      }
    };
  }, [setLatestState, virtualizer]);

  useEffect(() => {
    if (!scrollElement) {
      return;
    }
    let previousHeight = scrollElement.clientHeight;
    let previousWidth = scrollElement.clientWidth;
    const observer = new ResizeObserver(() => {
      const nextHeight = scrollElement.clientHeight;
      const nextWidth = scrollElement.clientWidth;
      if (nextHeight === previousHeight && nextWidth === previousWidth) {
        return;
      }
      previousHeight = nextHeight;
      previousWidth = nextWidth;
      if (!followLatestRef.current) {
        return;
      }
      virtualizer.scrollToEnd({ behavior: "auto" });
      setLatestState(true);
    });
    observer.observe(scrollElement, { box: "border-box" });
    return () => observer.disconnect();
  }, [scrollElement, setLatestState, virtualizer]);

  useEffect(() => {
    const node = scrollElement;
    if (!node) {
      return;
    }
    const onScroll = () => {
      const atLatest = virtualizer.isAtEnd(SCROLL_END_THRESHOLD_PX);
      if (atLatest) {
        followLatestRef.current = true;
      }
      if (atLatest || !followLatestRef.current) {
        setLatestState(atLatest);
      }
      if (node.scrollTop < HISTORY_LOAD_SCROLL_TOP_PX) {
        historyLoader.request();
      } else {
        historyLoader.check();
      }
    };
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) {
        followLatestRef.current = false;
        historyLoader.request();
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditableTarget(event.target) || isDisclosureToggleTarget(event.target)) {
        return;
      }
      if (event.key === "ArrowUp" || event.key === "PageUp" || event.key === "Home") {
        followLatestRef.current = false;
        historyLoader.request();
      }
    };
    const onPointerDown = (event: PointerEvent) => {
      if (event.button === 0 && pointerHitsVerticalScrollbar(event, node)) {
        followLatestRef.current = false;
      }
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    node.addEventListener("wheel", onWheel, { passive: true });
    node.addEventListener("pointerdown", onPointerDown, { passive: true });
    window.addEventListener("keydown", onKeyDown);
    setLatestState(virtualizer.isAtEnd(SCROLL_END_THRESHOLD_PX));
    return () => {
      node.removeEventListener("scroll", onScroll);
      node.removeEventListener("wheel", onWheel);
      node.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [historyLoader.check, historyLoader.request, scrollElement, setLatestState, virtualizer]);

  useLayoutEffect(() => {
    if (!scrollElement || !turnReveal || revealedTurnSerialRef.current === turnReveal.serial) {
      return;
    }
    const turnIndex = turnIndexByID.get(turnReveal.turnID);
    if (turnIndex === undefined) {
      return;
    }
    const turnElement = findTurnElement(scrollElement, turnReveal.turnID);
    if (!turnElement) {
      virtualizer.scrollToIndex(turnIndex, { align: "center", behavior: "auto" });
      return;
    }
    alignElementInViewport(scrollElement, turnElement);
    initialScrollSessionRef.current = sessionID;
    followLatestRef.current = false;
    revealedTurnSerialRef.current = turnReveal.serial;
    setLatestState(false);

    if (revealClearTimerRef.current !== null) {
      window.clearTimeout(revealClearTimerRef.current);
    }
    clearElementAttribute(revealedMessageElementsRef.current, "data-transcript-turn-reveal");
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
      clearElementAttribute(revealElements, "data-transcript-turn-reveal");
      revealedMessageElementsRef.current = [];
      revealClearTimerRef.current = null;
    }, TURN_REVEAL_CLEAR_MS);
    onTurnRevealComplete?.(turnReveal.serial);
  }, [
    onTurnRevealComplete,
    scrollElement,
    sessionID,
    setLatestState,
    turnIndexByID,
    turnReveal,
    virtualizer,
    virtualRenderVersion,
  ]);

  useLayoutEffect(() => {
    const target = searchState.target;
    if (!scrollElement || !target) {
      activeSearchTargetRef.current = "";
      return;
    }
    const targetKey = searchTargetKey(target, searchState.terms);
    if (activeSearchTargetRef.current === targetKey) {
      return;
    }
    const turnIndex = turnIndexByID.get(target.turnID);
    if (turnIndex === undefined) {
      return;
    }
    const turnElement = findTurnElement(scrollElement, target.turnID);
    if (!turnElement) {
      virtualizer.scrollToIndex(turnIndex, { align: "center", behavior: "auto" });
      return;
    }
    const activeRange = transcriptSearchTargetRange(scrollElement, target, searchState.terms);
    const messageElement = turnElement.querySelector<HTMLElement>(
      `[data-transcript-message-id="${CSS.escape(target.messageID)}"]`,
    );
    alignElementInViewport(scrollElement, activeRange?.getBoundingClientRect() || messageElement || turnElement);
    initialScrollSessionRef.current = sessionID;
    followLatestRef.current = false;
    activeSearchTargetRef.current = targetKey;
    setLatestState(false);
  }, [
    scrollElement,
    searchState.target,
    searchState.terms,
    sessionID,
    setLatestState,
    turnIndexByID,
    virtualizer,
    virtualRenderVersion,
  ]);

  useLayoutEffect(() => {
    clearElementAttribute(activeSearchElementsRef.current, "data-transcript-search-active");
    activeSearchElementsRef.current = [];
    const names = transcriptSearchHighlightNames(searchSlot);
    clearTranscriptSearchHighlights(names);
    const listElement = listElementRef.current;
    if (!listElement || searchState.terms.length === 0) {
      return;
    }
    const target = searchState.target;
    if (target) {
      const turnElement = findTurnElement(listElement, target.turnID);
      if (turnElement) {
        const messageElements = Array.from(
          turnElement.querySelectorAll<HTMLElement>(
            `[data-transcript-message-id="${CSS.escape(target.messageID)}"]`,
          ),
        );
        const roleElements = Array.from(
          turnElement.querySelectorAll<HTMLElement>(
            `[data-transcript-message-role="${target.role}"]`,
          ),
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
      }
    }

    const registry = transcriptHighlightRegistry();
    const HighlightConstructor = transcriptHighlightConstructor();
    if (!registry || !HighlightConstructor) {
      return;
    }
    ensureTranscriptSearchHighlightStyles();
    const messageRoots = Array.from(
      listElement.querySelectorAll<HTMLElement>("[data-transcript-message-id]"),
    );
    const matchRanges = messageRoots.flatMap((root) => textMatchRanges(root, searchState.terms));
    const activeRange = target
      ? transcriptSearchTargetRange(listElement, target, searchState.terms)
      : undefined;
    if (matchRanges.length > 0) {
      registry.set(names.matches, new HighlightConstructor(...matchRanges));
    }
    if (activeRange) {
      registry.set(names.active, new HighlightConstructor(activeRange));
    }
    return () => clearTranscriptSearchHighlights(names);
  }, [searchSlot, searchState.target, searchState.terms, virtualRenderVersion]);

  useEffect(() => {
    return () => {
      if (revealClearTimerRef.current !== null) {
        window.clearTimeout(revealClearTimerRef.current);
      }
      clearElementAttribute(revealedMessageElementsRef.current, "data-transcript-turn-reveal");
      clearElementAttribute(activeSearchElementsRef.current, "data-transcript-search-active");
      clearTranscriptSearchHighlights(transcriptSearchHighlightNames(searchSlot));
      revealedMessageElementsRef.current = [];
      activeSearchElementsRef.current = [];
    };
  }, [searchSlot]);

  if (turns.length === 0 && !footer) {
    return null;
  }

  return (
    <div
      ref={setListElement}
      className="relative min-w-0"
      role="list"
      style={{ height: virtualizer.getTotalSize() }}
    >
      {virtualItems.length > 0 ? (
        <div
          className="absolute top-0 left-0 grid w-full min-w-0"
          style={{
            gap: TURN_GAP_PX,
            transform: `translateY(${virtualItems[0].start}px)`,
          }}
        >
          {virtualItems.map((virtualItem) => {
            if (virtualItem.index === turnCount) {
              return (
                <div
                  key={virtualItem.key}
                  ref={measureElement}
                  aria-hidden={!footer}
                  className="min-w-0"
                  data-index={virtualItem.index}
                  role="presentation"
                >
                  <div className={footer ? "grid" : undefined} style={footer ? { gap: TURN_GAP_PX } : undefined}>
                    {footer}
                    <div
                      aria-hidden="true"
                      style={{
                        height: `calc(var(--pudding-composer-mask-height) - ${TURN_GAP_PX}px + var(--pudding-composer-overlay-height))`,
                      }}
                    />
                  </div>
                </div>
              );
            }
            const turn = turns[virtualItem.index];
            if (!turn) {
              return null;
            }
            return (
              <div
                key={virtualItem.key}
                ref={measureElement}
                aria-posinset={virtualItem.index + 1}
                aria-setsize={turns.length}
                className="min-w-0"
                data-index={virtualItem.index}
                role="listitem"
              >
                <TranscriptTurn
                  cloningMessageID={cloningMessageID}
                  disclosure={disclosure}
                  displaySettings={displaySettings}
                  sessionID={sessionID}
                  onAssistantRevealComplete={onAssistantRevealComplete}
                  onCloneMessage={onCloneMessage}
                  onQueuedCancel={onQueuedCancel}
                  onQueuedEditStart={onQueuedEditStart}
                  onQueuedSteer={onQueuedSteer}
                  onQueuedSave={onQueuedSave}
                  token={token}
                  turn={turn}
                />
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
});

function findTurnElement(root: HTMLElement, turnID: string) {
  return root.querySelector<HTMLElement>(
    `[data-transcript-turn-id="${CSS.escape(turnID)}"]`,
  );
}

function alignElementInViewport(
  scrollElement: HTMLElement,
  target: HTMLElement | DOMRect,
) {
  const viewportRect = scrollElement.getBoundingClientRect();
  const targetRect = target instanceof HTMLElement ? target.getBoundingClientRect() : target;
  const elementTop = targetRect.top - viewportRect.top;
  scrollElement.scrollTop += elementTop - scrollElement.clientHeight * TURN_REVEAL_TOP_RATIO;
}

function clearElementAttribute(elements: HTMLElement[], attribute: string) {
  for (const element of elements) {
    element.removeAttribute(attribute);
  }
}

function searchTargetKey(target: TranscriptSearchTarget, terms: string[]) {
  return `${target.turnID}:${target.messageID}:${target.occurrenceIndex}:${terms.join("\u0000")}`;
}

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
      color: #171717;
      background-color: #f9e663;
    }
    ::highlight(pudding-conversation-search-active-primary),
    ::highlight(pudding-conversation-search-active-split) {
      color: #171717;
      background-color: #ff9632;
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
  const pendingMoreRef = useRef(false);
  const pumpRef = useRef<() => void>(() => {});
  const stateRef = useRef<HistoryLoadState>("idle");

  useEffect(() => {
    optionsRef.current = { getScrollElement, hasMore, isLoading, isNearTop, loadMore };
  }, [getScrollElement, hasMore, isLoading, isNearTop, loadMore]);

  const pump = useCallback(() => {
    if (stateRef.current !== "idle" || !pendingMoreRef.current) {
      return;
    }
    const options = optionsRef.current;
    if (!options.hasMore || options.isLoading || !options.isNearTop()) {
      return;
    }
    pendingMoreRef.current = false;
    stateRef.current = "loading";
    void Promise.resolve(options.loadMore())
      .catch(() => undefined)
      .then(async () => {
        stateRef.current = "settling";
        await waitForScrollSettle(options.getScrollElement);
      })
      .finally(() => {
        stateRef.current = "idle";
        window.requestAnimationFrame(() => pumpRef.current());
      });
  }, []);

  useEffect(() => {
    pumpRef.current = pump;
  }, [pump]);

  const check = useCallback(() => pump(), [pump]);
  const request = useCallback(() => {
    if (stateRef.current !== "idle") {
      return;
    }
    pendingMoreRef.current = true;
    pump();
  }, [pump]);
  const reset = useCallback(() => {
    pendingMoreRef.current = false;
    stateRef.current = "idle";
  }, []);

  return useMemo(() => ({ check, request, reset }), [check, request, reset]);
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

function pointerHitsVerticalScrollbar(event: PointerEvent, element: HTMLElement) {
  if (element.dataset.scrollbarState === "hidden" || element.scrollHeight <= element.clientHeight) {
    return false;
  }
  const rect = element.getBoundingClientRect();
  const scrollbarWidth = Math.max(12, element.offsetWidth - element.clientWidth);
  return event.clientX >= rect.right - scrollbarWidth && event.clientX <= rect.right;
}

function useTranscriptStructure(sessionID: string, turns: TranscriptTurnVM[]) {
  const tailItemKey = `${TAIL_ITEM_KEY}:${sessionID}`;
  const structureRef = useRef<{
    itemKeys: string[];
    turnIndexByID: Map<string, number>;
  }>({
    itemKeys: [tailItemKey],
    turnIndexByID: new Map(),
  });
  const current = structureRef.current;
  const structureChanged =
    current.itemKeys.length !== turns.length + 1 ||
    current.itemKeys[current.itemKeys.length - 1] !== tailItemKey ||
    turns.some((turn, index) => current.itemKeys[index] !== turn.key);
  if (!structureChanged) {
    return current;
  }
  const indexes = new Map<string, number>();
  const itemKeys = turns.map((turn, index) => {
    indexes.set(turn.key, index);
    if (turn.anchorID) {
      indexes.set(turn.anchorID, index);
    }
    if (turn.turnID) {
      indexes.set(turn.turnID, index);
    }
    return turn.key;
  });
  const next = {
    itemKeys: [...itemKeys, tailItemKey],
    turnIndexByID: indexes,
  };
  structureRef.current = next;
  return next;
}
