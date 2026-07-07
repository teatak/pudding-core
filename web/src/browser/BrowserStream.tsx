import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Loader2, MousePointer2, Undo2 } from "lucide-react";
import {
  useEffect,
  useReducer,
  useRef,
  useState,
  type ClipboardEvent as ReactClipboardEvent,
  type CompositionEvent as ReactCompositionEvent,
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type WheelEvent as ReactWheelEvent,
} from "react";
import { toast } from "sonner";

import {
  internalBrowserTab,
  listBrowserTabs,
  openBrowserURL,
  putCanvasItem,
  recoverBrowserTab,
  revealBrowserTab,
  type BrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { hasElectronNativeBrowser } from "@/browser/electronBridge";
import { ElectronNativeBrowser } from "@/browser/electronNative";
import {
  browserClipboardShortcut,
  browserKeyMessage,
  browserModifiers,
  browserMouseButton,
  browserMouseButtonMask,
  browserPayloadForItem,
  browserPayloadHasRealState,
  browserQueryStaleTimeMS,
  browserScreencastURL,
  browserTabFaviconURL,
  browserTabTitle,
  browserURLIsBlank,
  isPlainTextKey,
  preferredBrowserTab,
  renderedBrowserImageRect,
  upsertBrowserTab,
} from "@/browser/helpers";
import type {
  BrowserScreencastCaret,
  BrowserScreencastClipboard,
  BrowserScreencastCursor,
  BrowserScreencastFrame,
  BrowserScreencastMessage,
  BrowserTabsData,
} from "@/browser/types";
import { Button } from "@/components/ui/button";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

const browserCursorByTabID = new Map<string, BrowserScreencastCursor>();
const maxStreamRecoveryAttempts = 3;
const streamConnectDelayMS = 150;
const maxScreencastDeviceScaleFactor = 2;

export function forgetBrowserCursor(tabID?: string) {
  if (tabID) {
    browserCursorByTabID.delete(tabID);
  }
}

export function BrowserStream({ token, item }: { token: string; item: CanvasItem }) {
  if (hasElectronNativeBrowser()) {
    return <ElectronNativeBrowser token={token} item={item} />;
  }
  return <ScreencastBrowserStream token={token} item={item} />;
}

type BrowserStreamPhase = "idle" | "connecting" | "waiting_first_frame" | "live" | "external" | "recovering" | "error";

type BrowserStreamState = {
  phase: BrowserStreamPhase;
  frame: BrowserScreencastFrame | null;
  error: string;
  attempt: number;
};

type BrowserStreamAction =
  | { type: "idle" }
  | { type: "connecting" }
  | { type: "waiting_first_frame" }
  | { type: "live"; frame: BrowserScreencastFrame }
  | { type: "external" }
  | { type: "recovering" }
  | { type: "error"; error: string }
  | { type: "closed" }
  | { type: "retry" };

const initialBrowserStreamState: BrowserStreamState = {
  phase: "idle",
  frame: null,
  error: "",
  attempt: 0,
};

function browserStreamReducer(state: BrowserStreamState, action: BrowserStreamAction): BrowserStreamState {
  switch (action.type) {
    case "idle":
      return { ...state, phase: "idle", frame: null, error: "" };
    case "connecting":
      return { ...state, phase: "connecting", frame: null, error: "" };
    case "waiting_first_frame":
      return { ...state, phase: "waiting_first_frame", error: "" };
    case "live":
      return { ...state, phase: "live", frame: action.frame, error: "" };
    case "external":
      return { ...state, phase: "external", frame: null, error: "" };
    case "recovering":
      return { ...state, phase: "recovering", frame: null, error: "" };
    case "error":
      return { ...state, phase: "error", frame: null, error: action.error };
    case "closed":
      if (state.phase === "error" || state.phase === "external" || state.phase === "recovering") {
        return state;
      }
      return { ...state, phase: "idle", frame: null };
    case "retry":
      return { ...state, phase: "recovering", frame: null, error: "", attempt: state.attempt + 1 };
  }
}

function ScreencastBrowserStream({ token, item }: { token: string; item: CanvasItem }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const surfaceRef = useRef<HTMLDivElement | null>(null);
  const imageRef = useRef<HTMLImageElement | null>(null);
  const textInputRef = useRef<HTMLTextAreaElement | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const isComposingRef = useRef(false);
  const suppressIMECommitKeyRef = useRef(false);
  const suppressIMECommitKeyUpRef = useRef("");
  const suppressIMECommitUntilRef = useRef(0);
  const suppressClipboardShortcutKeyUpRef = useRef("");
  const skipNextInputTextRef = useRef("");
  const lastMouseMoveAtRef = useRef(0);
  const mouseButtonsRef = useRef(0);
  const repairBrowserTabKeyRef = useRef("");
  const cursorPulseTimerRef = useRef<number | undefined>(undefined);
  const streamRecoveryAttemptsRef = useRef(new Map<string, number>());
  const payload = browserPayloadForItem(item);
  const ownerSessionID = payload?.sessionID || item.sourceSessionID;
  const [streamState, dispatchStream] = useReducer(browserStreamReducer, initialBrowserStreamState);
  const streamFrame = streamState.frame;
  const streamPhase = streamState.phase;
  const streamError = streamState.error;
  const streamAttempt = streamState.attempt;
  const [llmCursor, setLLMCursor] = useState<BrowserScreencastCursor | null>(null);
  const [llmCursorPulse, setLLMCursorPulse] = useState<{ key: number; visible: boolean }>({ key: 0, visible: false });
  const tabsQuery = useQuery({
    enabled: Boolean(token && ownerSessionID),
    queryKey: ownerSessionID ? queryKeys.browserTabs(ownerSessionID) : ["browser", "missing-session"],
    queryFn: () => {
      if (!ownerSessionID) {
        throw new Error("browser session id missing");
      }
      return listBrowserTabs(token, ownerSessionID);
    },
    staleTime: browserQueryStaleTimeMS,
  });
  const tabs = tabsQuery.data?.tabs || [];
  const activeTab = preferredBrowserTab(tabs, payload);
  const busyTitle = tabsQuery.isPending ? t("browser.loading") : t("browser.empty");
  const hasRealPayloadState = browserPayloadHasRealState(payload);
  const processMode = tabsQuery.data?.processMode || activeTab?.mode || payload?.mode;
  const isExternalBrowser = processMode === "external";
  const actionTabID = activeTab?.id || payload?.tabID;
  const streamTabID = activeTab?.id || "";

  const showLLMCursor = (cursor: BrowserScreencastCursor) => {
    const next = { ...cursor, createdAt: cursor.createdAt || new Date().toISOString() };
    if (streamTabID) {
      browserCursorByTabID.set(streamTabID, next);
    }
    setLLMCursor(next);
    if (cursor.action !== "click") {
      return;
    }
    if (cursorPulseTimerRef.current) {
      window.clearTimeout(cursorPulseTimerRef.current);
    }
    setLLMCursorPulse((current) => ({ key: current.key + 1, visible: true }));
    cursorPulseTimerRef.current = window.setTimeout(() => {
      setLLMCursorPulse((current) => ({ ...current, visible: false }));
      cursorPulseTimerRef.current = undefined;
    }, 700);
  };

  useEffect(() => {
    if (!streamTabID || isExternalBrowser || streamPhase === "external") {
      setLLMCursor(null);
      setLLMCursorPulse((current) => ({ ...current, visible: false }));
      return;
    }
    setLLMCursor(browserCursorByTabID.get(streamTabID) || null);
    setLLMCursorPulse((current) => ({ ...current, visible: false }));
  }, [streamTabID, isExternalBrowser, streamPhase]);

  useEffect(() => {
    if (!isExternalBrowser) {
      return;
    }
    const id = window.setInterval(() => {
      void tabsQuery.refetch();
    }, 1500);
    return () => window.clearInterval(id);
  }, [isExternalBrowser, tabsQuery.refetch]);

  const persistTab = async (tab: BrowserTab) => {
    if (!ownerSessionID) {
      return;
    }
    const title = browserTabTitle(tab, payload?.title || t("browser.title"));
    await putCanvasItem(token, ownerSessionID, item.id, {
      id: item.id,
      sourceSessionID: ownerSessionID,
      kind: "browser",
      title,
      item: {
        ...(payload || {}),
        kind: "browser",
        sessionID: ownerSessionID,
        tabID: tab.id,
        url: tab.url,
        title,
        faviconURL: browserTabFaviconURL(tab),
        mode: tab.mode,
      },
      window: item.window,
    });
    queryClient.setQueryData(queryKeys.browserTabs(ownerSessionID), (current: BrowserTabsData | undefined) => ({
      tabs: upsertBrowserTab(current?.tabs || [], tab),
      processMode: tab.mode || current?.processMode,
    }));
    void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(ownerSessionID) });
    void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(ownerSessionID) });
    void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(ownerSessionID) });
  };

  const repairTabMutation = useMutation({
    mutationFn: async () => {
      if (!ownerSessionID || !payload?.url) {
        throw new Error("browser tab missing");
      }
      return openBrowserURL(token, ownerSessionID, { url: payload.url });
    },
    onSuccess: (tab) => {
      void persistTab(tab);
    },
    onError: () => {
      dispatchStream({ type: "idle" });
      repairBrowserTabKeyRef.current = "";
    },
  });

  useEffect(() => {
    if (
      !token ||
      !ownerSessionID ||
      activeTab ||
      isExternalBrowser ||
      tabsQuery.isPending ||
      repairTabMutation.isPending ||
      !payload?.url ||
      browserURLIsBlank(payload.url)
    ) {
      return;
    }
    const key = `${ownerSessionID}:${payload.tabID || ""}:${payload.url}`;
    if (repairBrowserTabKeyRef.current === key) {
      return;
    }
    repairBrowserTabKeyRef.current = key;
    dispatchStream({ type: "recovering" });
    repairTabMutation.mutate();
  }, [
    activeTab?.id,
    isExternalBrowser,
    ownerSessionID,
    payload?.tabID,
    payload?.url,
    repairTabMutation.isPending,
    tabsQuery.isPending,
    token,
  ]);

  const internalMutation = useMutation({
    mutationFn: async () => {
      if (!ownerSessionID || !actionTabID) {
        throw new Error("browser tab missing");
      }
      return internalBrowserTab(token, ownerSessionID, actionTabID);
    },
    onSuccess: (tab) => {
      dispatchStream({ type: "connecting" });
      void persistTab(tab);
    },
    onError: () => toast.error(t("browser.internalFailed")),
  });

  const focusExternalMutation = useMutation({
    mutationFn: async () => {
      if (!ownerSessionID || !actionTabID) {
        throw new Error("browser tab missing");
      }
      return revealBrowserTab(token, ownerSessionID, actionTabID);
    },
    onSuccess: (tab) => {
      void persistTab(tab);
    },
    onError: () => toast.error(t("browser.revealFailed")),
  });

  useEffect(() => {
    if (!token || !ownerSessionID || !streamTabID) {
      dispatchStream({ type: "idle" });
      return;
    }
    if (isExternalBrowser) {
      dispatchStream({ type: "external" });
      return;
    }
    let alive = true;
    let ws: WebSocket | null = null;
    let resizeObserver: ResizeObserver | null = null;
    let connectDelay = 0;
    let startFrame = 0;
    let connectTimeout = 0;
    let frameTimeout = 0;
    let hasFrame = false;
    let lastStartKey = "";
    let retrying = false;
    let terminalStatus: "external" | "error" | "" = "";
    const streamKey = `${ownerSessionID}:${streamTabID}`;
    dispatchStream({ type: "connecting" });

    const clearConnectTimeout = () => {
      if (connectTimeout) {
        window.clearTimeout(connectTimeout);
        connectTimeout = 0;
      }
    };
    const clearFrameTimeout = () => {
      if (frameTimeout) {
        window.clearTimeout(frameTimeout);
        frameTimeout = 0;
      }
    };
    const clearStartFrame = () => {
      if (startFrame) {
        window.cancelAnimationFrame(startFrame);
        startFrame = 0;
      }
    };
    const closeWebSocket = () => {
      if (!ws) {
        return;
      }
      const closing = ws;
      ws = null;
      if (wsRef.current === closing) {
        wsRef.current = null;
      }
      closing.close();
    };
    const recoverStream = () => {
      if (!alive || retrying) {
        return;
      }
      const attempts = streamRecoveryAttemptsRef.current.get(streamKey) || 0;
      if (attempts >= maxStreamRecoveryAttempts) {
        retrying = true;
        clearConnectTimeout();
        clearStartFrame();
        clearFrameTimeout();
        closeWebSocket();
        dispatchStream({ type: "error", error: t("browser.loadFailed") });
        return;
      }
      retrying = true;
      streamRecoveryAttemptsRef.current.set(streamKey, attempts + 1);
      dispatchStream({ type: "recovering" });
      clearConnectTimeout();
      clearStartFrame();
      clearFrameTimeout();
      closeWebSocket();
      void (async () => {
        try {
          const tab = await recoverBrowserTab(token, ownerSessionID, streamTabID);
          if (!alive) {
            return;
          }
          await persistTab(tab).catch(() => undefined);
          if (!alive) {
            return;
          }
          dispatchStream({ type: "retry" });
        } catch (error) {
          if (!alive) {
            return;
          }
          const message = error instanceof Error ? error.message : t("browser.loadFailed");
          dispatchStream({ type: "error", error: message });
        }
      })();
    };
    const watchForFrame = () => {
      clearFrameTimeout();
      frameTimeout = window.setTimeout(() => {
        if (!alive || hasFrame) {
          return;
        }
        recoverStream();
      }, 4000);
    };
    const sendStart = (force = false) => {
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        return;
      }
      const rect = surfaceRef.current?.getBoundingClientRect();
      const width = Math.max(1, Math.round(rect?.width || 0));
      const height = Math.max(1, Math.round(rect?.height || 0));
      if (width < 32 || height < 32) {
        return;
      }
      const deviceScaleFactor = Math.min(
        maxScreencastDeviceScaleFactor,
        Math.max(1, Math.round((window.devicePixelRatio || 1) * 100) / 100),
      );
      const key = `${width}:${height}:${deviceScaleFactor}`;
      if (!force && key === lastStartKey) {
        return;
      }
      lastStartKey = key;
      hasFrame = false;
      try {
        ws.send(JSON.stringify({ type: "start", width, height, everyNthFrame: 1, deviceScaleFactor }));
      } catch {
        recoverStream();
        return;
      }
      watchForFrame();
    };
    const scheduleStart = (force = false) => {
      clearStartFrame();
      startFrame = window.requestAnimationFrame(() => {
        startFrame = 0;
        sendStart(force);
      });
    };

    const openWebSocket = () => {
      if (!alive) {
        return;
      }
      ws = new WebSocket(browserScreencastURL(token, ownerSessionID, streamTabID));
      wsRef.current = ws;
      connectTimeout = window.setTimeout(() => {
        if (!ws || ws.readyState !== WebSocket.OPEN) {
          recoverStream();
        }
      }, 4000);

      ws.onopen = () => {
        if (!alive) {
          return;
        }
        clearConnectTimeout();
        dispatchStream({ type: "waiting_first_frame" });
        if (surfaceRef.current) {
          resizeObserver = new ResizeObserver(() => scheduleStart(false));
          resizeObserver.observe(surfaceRef.current);
        }
        scheduleStart(true);
      };
      ws.onmessage = (event) => {
        if (!alive) {
          return;
        }
        try {
          const message = JSON.parse(String(event.data)) as BrowserScreencastMessage;
          if (message.type === "frame") {
            hasFrame = true;
            streamRecoveryAttemptsRef.current.delete(streamKey);
            clearFrameTimeout();
            dispatchStream({ type: "live", frame: message });
          } else if (message.type === "caret") {
            moveTextInputToViewportPoint(message);
          } else if (message.type === "cursor") {
            showLLMCursor(message);
          } else if (message.type === "clipboard") {
            void handleBrowserClipboardResult(message);
          } else if (message.type === "status" && message.status === "external") {
            terminalStatus = "external";
            setLLMCursor(null);
            dispatchStream({ type: "external" });
          } else if (message.type === "error") {
            terminalStatus = "error";
            clearFrameTimeout();
            dispatchStream({ type: "error", error: message.error });
          }
        } catch {
          // Ignore malformed frames from a stale connection.
        }
      };
      ws.onerror = () => {
        if (!alive) {
          return;
        }
        recoverStream();
      };
      ws.onclose = () => {
        if (!alive) {
          return;
        }
        clearConnectTimeout();
        clearStartFrame();
        clearFrameTimeout();
        if (terminalStatus) {
          dispatchStream({ type: "closed" });
          return;
        }
        recoverStream();
      };
    };

    connectDelay = window.setTimeout(() => {
      if (!alive) {
        return;
      }
      connectDelay = 0;
      openWebSocket();
    }, streamConnectDelayMS);

    return () => {
      alive = false;
      resizeObserver?.disconnect();
      if (connectDelay) {
        window.clearTimeout(connectDelay);
        connectDelay = 0;
      }
      clearConnectTimeout();
      clearStartFrame();
      clearFrameTimeout();
      if (cursorPulseTimerRef.current) {
        window.clearTimeout(cursorPulseTimerRef.current);
        cursorPulseTimerRef.current = undefined;
      }
      closeWebSocket();
    };
  }, [token, ownerSessionID, streamTabID, isExternalBrowser, streamAttempt]);

  const sendScreencast = (message: Record<string, unknown>) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return false;
    }
    ws.send(JSON.stringify(message));
    return true;
  };

  const insertBrowserText = (text: string) => {
    if (!streamFrame || !text) {
      return;
    }
    sendScreencast({ type: "text", text });
    requestBrowserCaret();
  };

  const handleBrowserClipboardResult = (message: BrowserScreencastClipboard) => {
    if (message.action === "selectAll" || message.action === "undo" || message.action === "redo") {
      if (message.ok) {
        requestBrowserCaret();
      }
      return;
    }
    if (!message.ok) {
      toast.error(t(message.action === "paste" ? "browser.pasteFailed" : "browser.copyFailed"), { description: message.error });
      return;
    }
    focusSurface();
    if (message.action === "cut" || message.action === "paste") {
      requestBrowserCaret();
    }
  };

  const handleBrowserClipboardShortcut = (event: ReactKeyboardEvent<HTMLElement>) => {
    const action = browserClipboardShortcut(event);
    if (!action) {
      return false;
    }
    event.preventDefault();
    event.stopPropagation();
    suppressClipboardShortcutKeyUpRef.current = event.key.toLowerCase();
    sendScreencast({ type: "clipboard", action });
    return true;
  };

  const suppressClipboardShortcutKeyUp = (event: ReactKeyboardEvent<HTMLElement>) => {
    const key = event.key.toLowerCase();
    if (!suppressClipboardShortcutKeyUpRef.current || suppressClipboardShortcutKeyUpRef.current !== key) {
      return false;
    }
    event.preventDefault();
    event.stopPropagation();
    suppressClipboardShortcutKeyUpRef.current = "";
    return true;
  };

  const handleBrowserPaste = (event: ReactClipboardEvent<HTMLElement>) => {
    if (!streamFrame) {
      return;
    }
    const text = event.clipboardData.getData("text/plain");
    if (!text) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    insertBrowserText(text);
  };

  const requestBrowserCaret = () => {
    window.setTimeout(() => {
      sendScreencast({ type: "caret" });
    }, 0);
  };

  const pointForEvent = (event: { clientX: number; clientY: number }) => {
    const image = imageRef.current;
    if (!image) {
      return null;
    }
    const viewportWidth = streamFrame?.metadata?.deviceWidth || image.naturalWidth;
    const viewportHeight = streamFrame?.metadata?.deviceHeight || image.naturalHeight;
    const rect = renderedBrowserImageRect(image, viewportWidth, viewportHeight);
    if (!rect || viewportWidth <= 0 || viewportHeight <= 0) {
      return null;
    }
    const localX = event.clientX - rect.left;
    const localY = event.clientY - rect.top;
    if (localX < 0 || localY < 0 || localX > rect.width || localY > rect.height) {
      return null;
    }
    return {
      x: Math.max(0, Math.min(viewportWidth, (localX / rect.width) * viewportWidth)),
      y: Math.max(0, Math.min(viewportHeight, (localY / rect.height) * viewportHeight)),
    };
  };

  const moveTextInputToEvent = (event: { clientX: number; clientY: number }) => {
    const surface = surfaceRef.current;
    const input = textInputRef.current;
    if (!surface || !input) {
      return;
    }
    const rect = surface.getBoundingClientRect();
    const x = Math.max(4, Math.min(rect.width - 4, event.clientX - rect.left));
    const y = Math.max(4, Math.min(rect.height - 4, event.clientY - rect.top));
    input.style.left = `${x}px`;
    input.style.top = `${y}px`;
  };

  const moveTextInputToViewportPoint = (caret: BrowserScreencastCaret) => {
    if (!caret.visible) {
      return;
    }
    const surface = surfaceRef.current;
    const image = imageRef.current;
    const input = textInputRef.current;
    if (!surface || !image || !input) {
      return;
    }
    const viewportWidth = streamFrame?.metadata?.deviceWidth || image.naturalWidth;
    const viewportHeight = streamFrame?.metadata?.deviceHeight || image.naturalHeight;
    const imageRect = renderedBrowserImageRect(image, viewportWidth, viewportHeight);
    if (!imageRect || viewportWidth <= 0 || viewportHeight <= 0) {
      return;
    }
    const surfaceRect = surface.getBoundingClientRect();
    const x = imageRect.left + (caret.x / viewportWidth) * imageRect.width - surfaceRect.left;
    const y = imageRect.top + (caret.y / viewportHeight) * imageRect.height - surfaceRect.top;
    const height = Math.max(16, ((caret.height || 18) / viewportHeight) * imageRect.height);
    input.style.left = `${Math.max(4, Math.min(surfaceRect.width - 4, x))}px`;
    input.style.top = `${Math.max(4, Math.min(surfaceRect.height - 4, y))}px`;
    input.style.height = `${height}px`;
  };

  const focusSurface = () => {
    if (streamFrame) {
      textInputRef.current?.focus({ preventScroll: true });
      return;
    }
    surfaceRef.current?.focus({ preventScroll: true });
  };

  const handleSurfaceMouseMove = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!streamFrame) {
      return;
    }
    const now = performance.now();
    if (now - lastMouseMoveAtRef.current < 16) {
      return;
    }
    lastMouseMoveAtRef.current = now;
    const point = pointForEvent(event);
    if (!point) {
      return;
    }
    sendScreencast({
      type: "mouse",
      eventType: "mouseMoved",
      x: point.x,
      y: point.y,
      button: browserMouseButton(event.button),
      buttons: event.buttons || mouseButtonsRef.current,
      modifiers: browserModifiers(event),
    });
  };

  const handleSurfaceMouseDown = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!streamFrame) {
      return;
    }
    moveTextInputToEvent(event);
    event.preventDefault();
    focusSurface();
    const point = pointForEvent(event);
    if (!point) {
      return;
    }
    mouseButtonsRef.current = event.buttons || browserMouseButtonMask(event.button);
    sendScreencast({
      type: "mouse",
      eventType: "mousePressed",
      x: point.x,
      y: point.y,
      button: browserMouseButton(event.button),
      buttons: mouseButtonsRef.current,
      clickCount: event.detail || 1,
      modifiers: browserModifiers(event),
    });
  };

  const handleSurfaceMouseUp = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!streamFrame) {
      return;
    }
    event.preventDefault();
    const point = pointForEvent(event);
    if (!point) {
      return;
    }
    sendScreencast({
      type: "mouse",
      eventType: "mouseReleased",
      x: point.x,
      y: point.y,
      button: browserMouseButton(event.button),
      buttons: event.buttons,
      clickCount: event.detail || 1,
      modifiers: browserModifiers(event),
    });
    mouseButtonsRef.current = event.buttons;
    requestBrowserCaret();
  };

  const handleSurfaceWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    if (!streamFrame) {
      return;
    }
    event.preventDefault();
    focusSurface();
    const point = pointForEvent(event);
    if (!point) {
      return;
    }
    sendScreencast({
      type: "wheel",
      x: point.x,
      y: point.y,
      deltaX: event.deltaX,
      deltaY: event.deltaY,
      modifiers: browserModifiers(event),
    });
  };

  const handleSurfaceKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget) {
      return;
    }
    if (!streamFrame) {
      return;
    }
    if (handleBrowserClipboardShortcut(event)) {
      return;
    }
    event.preventDefault();
    sendScreencast(browserKeyMessage(event, "keyDown"));
  };

  const handleSurfaceKeyUp = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget) {
      return;
    }
    if (!streamFrame) {
      return;
    }
    if (suppressClipboardShortcutKeyUp(event)) {
      return;
    }
    event.preventDefault();
    sendScreencast(browserKeyMessage(event, "keyUp"));
    requestBrowserCaret();
  };

  const handleTextInput = (event: ReactFormEvent<HTMLTextAreaElement>) => {
    if (isComposingRef.current) {
      return;
    }
    const text = event.currentTarget.value;
    if (!text) {
      return;
    }
    event.currentTarget.value = "";
    if (skipNextInputTextRef.current === text) {
      skipNextInputTextRef.current = "";
      return;
    }
    skipNextInputTextRef.current = "";
    insertBrowserText(text);
  };

  const handleTextCompositionStart = () => {
    isComposingRef.current = true;
    suppressIMECommitKeyRef.current = false;
  };

  const handleTextCompositionUpdate = (event: ReactCompositionEvent<HTMLTextAreaElement>) => {
    const text = event.data || event.currentTarget.value;
    sendScreencast({
      type: "composition",
      text,
      selectionStart: text.length,
      selectionEnd: text.length,
    });
  };

  const handleTextCompositionEnd = (event: ReactCompositionEvent<HTMLTextAreaElement>) => {
    isComposingRef.current = false;
    suppressIMECommitKeyRef.current = true;
    suppressIMECommitUntilRef.current = performance.now() + 350;
    const text = event.currentTarget.value || event.data;
    sendScreencast({ type: "compositionEnd" });
    if (!text) {
      return;
    }
    event.currentTarget.value = "";
    skipNextInputTextRef.current = text;
    insertBrowserText(text);
  };

  const shouldSuppressIMECommitKey = (event: ReactKeyboardEvent<HTMLTextAreaElement>, phase: "down" | "up") => {
    const key = event.key === "Spacebar" ? " " : event.key;
    const isCommitKey = key === "Enter" || key === " ";
    if (!isCommitKey) {
      return false;
    }
    if (phase === "up" && suppressIMECommitKeyUpRef.current === key) {
      event.preventDefault();
      suppressIMECommitKeyUpRef.current = "";
      return true;
    }
    if (!suppressIMECommitKeyRef.current) {
      return false;
    }
    if (performance.now() > suppressIMECommitUntilRef.current) {
      suppressIMECommitKeyRef.current = false;
      return false;
    }
    event.preventDefault();
    suppressIMECommitKeyRef.current = false;
    if (phase === "down") {
      suppressIMECommitKeyUpRef.current = key;
    }
    return true;
  };

  const handleTextKeyDown = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (!streamFrame || isComposingRef.current || event.nativeEvent.isComposing || event.key === "Process") {
      return;
    }
    if (shouldSuppressIMECommitKey(event, "down")) {
      return;
    }
    if (handleBrowserClipboardShortcut(event)) {
      return;
    }
    if (isPlainTextKey(event)) {
      return;
    }
    event.preventDefault();
    sendScreencast(browserKeyMessage(event, "keyDown"));
  };

  const handleTextKeyUp = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (!streamFrame || isComposingRef.current || event.nativeEvent.isComposing || event.key === "Process" || isPlainTextKey(event)) {
      return;
    }
    if (shouldSuppressIMECommitKey(event, "up")) {
      return;
    }
    if (suppressClipboardShortcutKeyUp(event)) {
      return;
    }
    event.preventDefault();
    sendScreencast(browserKeyMessage(event, "keyUp"));
  };

  const busy =
    streamPhase === "connecting" ||
    streamPhase === "waiting_first_frame" ||
    streamPhase === "recovering" ||
    repairTabMutation.isPending ||
    (!streamTabID && tabsQuery.isPending);
  const title = activeTab?.title?.trim() || (hasRealPayloadState ? payload?.title : "") || busyTitle;
  const llmCursorStyle = (() => {
    if (!llmCursor || !streamFrame) {
      return null;
    }
    const surface = surfaceRef.current;
    const image = imageRef.current;
    if (!surface || !image) {
      return null;
    }
    const viewportWidth = streamFrame.metadata?.deviceWidth || image.naturalWidth;
    const viewportHeight = streamFrame.metadata?.deviceHeight || image.naturalHeight;
    const imageRect = renderedBrowserImageRect(image, viewportWidth, viewportHeight);
    if (!imageRect || viewportWidth <= 0 || viewportHeight <= 0) {
      return null;
    }
    const surfaceRect = surface.getBoundingClientRect();
    return {
      left: imageRect.left + (llmCursor.x / viewportWidth) * imageRect.width - surfaceRect.left,
      top: imageRect.top + (llmCursor.y / viewportHeight) * imageRect.height - surfaceRect.top,
    };
  })();

  if (!ownerSessionID) {
    return <div className="p-3 text-sm text-muted-foreground">{t("browser.loadFailed")}</div>;
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-card">
      <div
        ref={surfaceRef}
        className="canvas-window-no-drag relative flex min-h-0 flex-1 touch-none items-center justify-center overflow-hidden bg-muted/30 outline-none focus-visible:ring-2 focus-visible:ring-ring/70"
        role="application"
        tabIndex={0}
        onKeyDown={handleSurfaceKeyDown}
        onKeyUp={handleSurfaceKeyUp}
        onMouseDown={handleSurfaceMouseDown}
        onMouseMove={handleSurfaceMouseMove}
        onMouseUp={handleSurfaceMouseUp}
        onPaste={handleBrowserPaste}
        onWheel={handleSurfaceWheel}
      >
        <textarea
          ref={textInputRef}
          aria-hidden="true"
          autoCapitalize="off"
          autoCorrect="off"
          className="pointer-events-none absolute top-2 left-2 h-5 w-px resize-none overflow-hidden border-0 bg-transparent p-0 text-[16px] opacity-0 outline-none"
          spellCheck={false}
          tabIndex={-1}
          onCompositionEnd={handleTextCompositionEnd}
          onCompositionStart={handleTextCompositionStart}
          onCompositionUpdate={handleTextCompositionUpdate}
          onInput={handleTextInput}
          onKeyDown={handleTextKeyDown}
          onKeyUp={handleTextKeyUp}
          onPaste={handleBrowserPaste}
        />
        {isExternalBrowser || streamPhase === "external" ? (
          <div className="flex max-w-sm flex-col items-center gap-3 px-6 text-center text-sm text-muted-foreground">
            <ExternalLink className="h-5 w-5" />
            <div className="font-medium text-foreground">{t("browser.externalOpen")}</div>
            <div className="text-xs leading-relaxed">{t("browser.externalHint")}</div>
            <div className="flex items-center gap-2 pt-1">
              <Button
                className="gap-1.5"
                disabled={internalMutation.isPending || !actionTabID}
                size="sm"
                type="button"
                onClick={() => internalMutation.mutate()}
              >
                {internalMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Undo2 className="h-3.5 w-3.5" />}
                {t("browser.returnInternal")}
              </Button>
              <Button
                className="gap-1.5"
                disabled={focusExternalMutation.isPending || !actionTabID}
                size="sm"
                type="button"
                variant="secondary"
                onClick={() => focusExternalMutation.mutate()}
              >
                {focusExternalMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ExternalLink className="h-3.5 w-3.5" />}
                {t("browser.focusExternal")}
              </Button>
            </div>
          </div>
        ) : streamFrame ? (
          <img
            ref={imageRef}
            alt={title}
            className="h-full max-h-full w-full max-w-full cursor-default object-contain"
            draggable={false}
            src={`data:${streamFrame.mime};base64,${streamFrame.data}`}
            onDragStart={(event) => event.preventDefault()}
          />
        ) : (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {busy ? t("browser.loading") : t("browser.empty")}
          </div>
        )}
        {llmCursorStyle ? (
          <div
            aria-hidden="true"
            className="pointer-events-none absolute z-20 -translate-x-1.5 -translate-y-1.5"
            style={{ left: llmCursorStyle.left, top: llmCursorStyle.top }}
          >
            <div className="relative h-5 w-5">
              {llmCursorPulse.visible ? (
                <div key={llmCursorPulse.key} className="absolute inset-0 rounded-full bg-sky-400/35 animate-ping" />
              ) : null}
              <MousePointer2 className="absolute top-0 left-0 h-5 w-5 fill-sky-500 text-white drop-shadow-[0_2px_5px_rgba(0,0,0,0.45)] [filter:drop-shadow(0_0_2px_rgba(14,165,233,0.8))]" />
            </div>
          </div>
        ) : null}
        {streamPhase === "error" && streamError ? (
          <div className="pointer-events-none absolute right-2 bottom-2 max-w-[70%] truncate rounded-md bg-destructive/90 px-2 py-1 text-xs text-destructive-foreground">
            {streamError}
          </div>
        ) : null}
      </div>
    </div>
  );
}
