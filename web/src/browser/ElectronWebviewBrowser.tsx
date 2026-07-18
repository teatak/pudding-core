import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Compass, FileX, Globe2, MousePointer2, RefreshCw } from "lucide-react";
import { createElement, useCallback, useEffect, useRef, useState, type CSSProperties, type HTMLAttributes } from "react";

import { listBrowserHistory, listBrowserTabs, type BrowserHistoryEntry, type BrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrowserFavicon } from "@/browser/BrowserFavicon";
import {
  cacheElectronBrowserSnapshot,
  electronBrowserBridge,
  type ElectronBrowserCursorEvent,
} from "@/browser/electronBridge";
import type { ElectronBrowserSurfaceTab } from "@/browser/useElectronRequiredBrowserTabs";
import {
  browserQueryStaleTimeMS,
  browserCompactURL,
  browserTargetURL,
  browserTabTitle,
  browserURLIsBlank,
  preferredBrowserTab,
  uniqueBrowserHistoryBySite,
} from "@/browser/helpers";
import { Button } from "@/components/ui/button";
import { Item, ItemContent, ItemGroup, ItemHeader, ItemTitle } from "@/components/ui/item";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import type { BrowserCanvasPayload } from "./types";

type WebviewElement = HTMLElement & {
  getWebContentsId?: () => number;
  getURL?: () => string;
};

type WebviewLoadError = {
  code: string;
  description: string;
  url: string;
};

type WebviewLoadErrorEvent = Event & {
  errorCode?: number;
  errorDescription?: string;
  isMainFrame?: boolean;
  validatedURL?: string;
};

type WebviewProps = HTMLAttributes<HTMLElement> & {
  ref: (node: WebviewElement | null) => void;
  src: string;
  partition: string;
  allowpopups: string;
  webpreferences: string;
};

type BrowserAutomationCursorState = {
  effectVisible: boolean;
  id: string;
  action: ElectronBrowserCursorEvent["action"];
  x: number;
  y: number;
};

export function ElectronWebviewBrowser({
  activeTab: activeTabProp,
  sessionID,
  token,
}: {
  activeTab?: ElectronBrowserSurfaceTab;
  sessionID: string;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const webviewRef = useRef<WebviewElement | null>(null);
  const payload = browserPayloadFromTab(activeTabProp);
  const webviewReadyRef = useRef(false);
  const webviewReadyCleanupRef = useRef<(() => void) | null>(null);
  const pendingTargetURLRef = useRef("");
  const webviewRequestIDRef = useRef("");
  const tabCreatedAtRef = useRef<string | undefined>(undefined);
  const navigationSeqRef = useRef(0);
  const failedNavigationSeqRef = useRef(0);
  const loadErrorRef = useRef<WebviewLoadError | null>(null);
  const cursorEffectTimerRef = useRef<number | undefined>(undefined);
  const [loadError, setLoadError] = useState<WebviewLoadError | null>(null);
  const [navigationLoading, setNavigationLoading] = useState(false);
  const [automationCursor, setAutomationCursor] = useState<BrowserAutomationCursorState | null>(null);
  const ownerSessionID = sessionID;
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
  const tabs = (tabsQuery.data?.tabs || []).filter((tab) => tab.sessionID === ownerSessionID);
  const activeTab = activeTabProp || preferredBrowserTab(tabs, payload);
  const title = activeTab ? browserTabTitle(activeTab, payload?.title || t("browser.newTab"), t("browser.newTab")) : payload?.title || t("browser.newTab");
  const tabID = activeTab?.id || payload?.tabID || "default";
  const targetURL = normalizeWebviewURL(browserTargetURL(activeTab, payload, payload?.updatedAt));
  const webviewRequestID = activeTabProp?.webviewRequestID || "";
  const recentHistoryQuery = useQuery({
    enabled: Boolean(token && ownerSessionID && browserURLIsBlank(targetURL)),
    queryKey: queryKeys.browserHistoryRecent(16),
    queryFn: () => listBrowserHistory(token, ownerSessionID, "", 64),
    staleTime: 0,
  });
  pendingTargetURLRef.current = targetURL;
  webviewRequestIDRef.current = webviewRequestID;
  tabCreatedAtRef.current = activeTab?.createdAt;

  const updateLoadError = useCallback((error: WebviewLoadError | null) => {
    loadErrorRef.current = error;
    setLoadError(error);
  }, []);

  const openRecentMutation = useMutation({
    mutationFn: async (url: string) => {
      const bridge = electronBrowserBridge();
      if (!bridge) {
        throw new Error("browser bridge unavailable");
      }
      setNavigationLoading(true);
      updateLoadError(null);
      return bridge.loadURL({ sessionID: ownerSessionID, tabID, url });
    },
    onSuccess: (snapshot) => {
      cacheElectronBrowserSnapshot(queryClient, snapshot, ownerSessionID);
    },
    onError: (error) => {
      setNavigationLoading(false);
      console.warn("[browser] recent history navigation failed", error);
    },
  });

  const setWebviewRef = useCallback((node: WebviewElement | null) => {
    webviewReadyCleanupRef.current?.();
    webviewReadyCleanupRef.current = null;
    webviewRef.current = node;
    webviewReadyRef.current = false;
    navigationSeqRef.current = 0;
    failedNavigationSeqRef.current = 0;
    setNavigationLoading(false);
    updateLoadError(null);
    if (!node) {
      return;
    }
    const handleReady = () => {
      webviewReadyRef.current = true;
      if (!browserURLIsBlank(pendingTargetURLRef.current)) {
        setNavigationLoading(true);
      }
    };
    const handleStartLoading = () => {
      if (!browserURLIsBlank(pendingTargetURLRef.current)) {
        setNavigationLoading(true);
      }
    };
    const handleStopLoading = () => {
      if (!loadErrorRef.current) {
        setNavigationLoading(false);
      }
    };
    const handleStartNavigation = (event: Event) => {
      const navigationEvent = event as WebviewLoadErrorEvent;
      if (navigationEvent.isMainFrame === false) {
        return;
      }
      navigationSeqRef.current += 1;
      if (!browserURLIsBlank(pendingTargetURLRef.current)) {
        setNavigationLoading(true);
      }
      updateLoadError(null);
    };
    const handleFinishLoad = () => {
      setNavigationLoading(false);
      if (navigationSeqRef.current > failedNavigationSeqRef.current) {
        updateLoadError(null);
      }
    };
    const handleFailLoad = (event: Event) => {
      const loadEvent = event as WebviewLoadErrorEvent;
      if (loadEvent.isMainFrame === false || isWebviewNavigationAbortCode(loadEvent.errorCode)) {
        return;
      }
      failedNavigationSeqRef.current = navigationSeqRef.current;
      const failedURL = normalizeWebviewURL(
        loadEvent.validatedURL || pendingTargetURLRef.current || webviewCurrentURL(node),
      );
      const error = {
        code: webviewErrorCode(loadEvent),
        description: loadEvent.errorDescription || "",
        url: failedURL,
      };
      setNavigationLoading(false);
      updateLoadError(error);
      const bridge = electronBrowserBridge();
      const webContentsID = webviewContentsID(node);
      if (bridge && ownerSessionID && webContentsID && !browserURLIsBlank(failedURL)) {
        void bridge
          .registerWebview({
            sessionID: ownerSessionID,
            tabID,
            url: failedURL,
            webContentsID,
            requestID: webviewRequestIDRef.current || undefined,
            createdAt: tabCreatedAtRef.current,
            loadError: { code: error.code, description: error.description },
          })
          .then((snapshot) => cacheElectronBrowserSnapshot(queryClient, snapshot, ownerSessionID))
          .catch(() => undefined);
      }
    };
    node.addEventListener("dom-ready", handleReady);
    node.addEventListener("did-start-loading", handleStartLoading);
    node.addEventListener("did-stop-loading", handleStopLoading);
    node.addEventListener("did-start-navigation", handleStartNavigation);
    node.addEventListener("did-finish-load", handleFinishLoad);
    node.addEventListener("did-fail-load", handleFailLoad);
    webviewReadyCleanupRef.current = () => {
      node.removeEventListener("dom-ready", handleReady);
      node.removeEventListener("did-start-loading", handleStartLoading);
      node.removeEventListener("did-stop-loading", handleStopLoading);
      node.removeEventListener("did-start-navigation", handleStartNavigation);
      node.removeEventListener("did-finish-load", handleFinishLoad);
      node.removeEventListener("did-fail-load", handleFailLoad);
      if (webviewRef.current === node) {
        webviewReadyRef.current = false;
      }
    };
  }, [ownerSessionID, queryClient, tabID, updateLoadError]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onCursor || !ownerSessionID || !tabID) {
      return;
    }
    return bridge.onCursor((event) => {
      if (event.sessionID !== ownerSessionID || event.tabID !== tabID) {
        return;
      }
      const x = Number(event.x);
      const y = Number(event.y);
      if (!Number.isFinite(x) || !Number.isFinite(y)) {
        return;
      }
      window.clearTimeout(cursorEffectTimerRef.current);
      setAutomationCursor({
        effectVisible: event.action !== "scroll",
        id: `${event.createdAt || Date.now()}:${event.version || 0}:${event.action}`,
        action: event.action,
        x: Math.max(0, Math.round(x)),
        y: Math.max(0, Math.round(y)),
      });
      if (event.action !== "scroll") {
        cursorEffectTimerRef.current = window.setTimeout(() => {
          setAutomationCursor((current) => (current ? { ...current, effectVisible: false } : current));
        }, 800);
      }
    });
  }, [ownerSessionID, tabID]);

  useEffect(() => {
    return () => {
      window.clearTimeout(cursorEffectTimerRef.current);
    };
  }, []);

  useEffect(() => {
    window.clearTimeout(cursorEffectTimerRef.current);
    setAutomationCursor(null);
  }, [ownerSessionID, tabID]);

  const reloadAfterError = useCallback(() => {
    const retryURL = loadError?.url || pendingTargetURLRef.current || targetURL || "about:blank";
    const bridge = electronBrowserBridge();
    if (bridge && ownerSessionID) {
      void bridge
        .reload({ sessionID: ownerSessionID, tabID, url: retryURL })
        .then((snapshot) => cacheElectronBrowserSnapshot(queryClient, snapshot, ownerSessionID))
        .catch((error) => {
          if (!isWebviewNavigationAbortError(error)) {
            console.warn("[browser] webview reload failed", error);
          }
        });
    }
  }, [loadError?.url, ownerSessionID, queryClient, tabID, targetURL]);

  useEffect(() => {
    const node = webviewRef.current;
    const bridge = electronBrowserBridge();
    if (!node || !bridge || !ownerSessionID) {
      return;
    }
    let disposed = false;
    let registered = false;
    let registrationInFlight = false;
    let retryDelayMS = 100;
    let retryTimer: number | undefined;
    const scheduleRetry = () => {
      if (disposed || registered || retryTimer !== undefined) {
        return;
      }
      retryTimer = window.setTimeout(() => {
        retryTimer = undefined;
        register();
      }, retryDelayMS);
      retryDelayMS = Math.min(retryDelayMS * 2, 1_500);
    };
    const register = () => {
      if (disposed || registered || registrationInFlight) {
        return;
      }
      if (!webviewReadyRef.current || !node.isConnected) {
        return;
      }
      const webContentsID = webviewContentsID(node);
      if (!webContentsID) {
        scheduleRetry();
        return;
      }
      if (loadErrorRef.current) {
        return;
      }
      const currentURL = normalizeWebviewURL(webviewCurrentURL(node));
      const registrationURL = browserURLIsBlank(currentURL) && !browserURLIsBlank(targetURL) ? targetURL : currentURL;
      registrationInFlight = true;
      void bridge
        .registerWebview({
          sessionID: ownerSessionID,
          tabID,
          url: registrationURL,
          webContentsID,
          requestID: webviewRequestID || undefined,
          createdAt: activeTab?.createdAt,
        })
        .then((snapshot) => {
          if (!disposed) {
            registered = true;
            window.clearTimeout(retryTimer);
            retryTimer = undefined;
            cacheElectronBrowserSnapshot(queryClient, snapshot, ownerSessionID);
          }
        })
        .catch(() => {
          scheduleRetry();
        })
        .finally(() => {
          registrationInFlight = false;
        });
    };
    const listener = () => register();
    node.addEventListener("dom-ready", listener);
    node.addEventListener("did-finish-load", listener);
    node.addEventListener("did-navigate", listener);
    node.addEventListener("did-navigate-in-page", listener);
    node.addEventListener("page-title-updated", listener);
    node.addEventListener("page-favicon-updated", listener);
    if (webviewReadyRef.current) {
      register();
    }
    return () => {
      disposed = true;
      window.clearTimeout(retryTimer);
      node.removeEventListener("dom-ready", listener);
      node.removeEventListener("did-finish-load", listener);
      node.removeEventListener("did-navigate", listener);
      node.removeEventListener("did-navigate-in-page", listener);
      node.removeEventListener("page-title-updated", listener);
      node.removeEventListener("page-favicon-updated", listener);
    };
  }, [activeTab?.createdAt, ownerSessionID, queryClient, targetURL, tabID, webviewRequestID]);

  if (!ownerSessionID) {
    return <div className="p-3 text-sm text-muted-foreground">{t("browser.loadFailed")}</div>;
  }

  return (
    <div className="canvas-window-no-drag relative h-full min-h-0 overflow-hidden bg-[var(--workspace-chrome-background)]" aria-label={title} role="application">
      {createElement("webview", {
        ref: setWebviewRef,
        className: "h-full w-full bg-[var(--workspace-chrome-background)]",
        src: "about:blank",
        partition: "persist:pudding-default",
        allowpopups: "true",
        webpreferences: "contextIsolation=yes,sandbox=yes",
      } satisfies WebviewProps)}
      {browserURLIsBlank(targetURL) && !loadError ? (
        <BrowserEmptyState
          history={uniqueBrowserHistoryBySite(recentHistoryQuery.data?.history || [], 16)}
          openingURL={openRecentMutation.isPending ? openRecentMutation.variables : undefined}
          onOpen={(url) => openRecentMutation.mutate(url)}
        />
      ) : null}
      {navigationLoading && !loadError ? <BrowserNavigationLoading label={t("browser.loadingPage")} /> : null}
      {loadError ? <BrowserLoadErrorPage error={loadError} onReload={reloadAfterError} /> : null}
      {automationCursor ? <BrowserAutomationCursor cursor={automationCursor} /> : null}
    </div>
  );
}

function BrowserEmptyState({
  history,
  openingURL,
  onOpen,
}: {
  history: BrowserHistoryEntry[];
  openingURL?: string;
  onOpen: (url: string) => void;
}) {
  const { t } = useI18n();

  return (
    <div className="absolute inset-0 z-[1] overflow-y-auto bg-[var(--workspace-chrome-background)]">
      <div className="flex min-h-full items-center justify-center px-6 py-8">
        <div className="w-full max-w-5xl text-center">
          <div className="mx-auto max-w-sm">
            <Compass className="mx-auto mb-4 size-8 text-muted-foreground" strokeWidth={1.6} />
            <h2 className="text-base font-semibold text-foreground">{t("browser.emptyTitle")}</h2>
          </div>
          {history.length > 0 ? (
            <ItemGroup className="mx-auto mt-6 flex max-w-[55rem] flex-row flex-wrap justify-center gap-4">
              {history.map((entry) => (
                <Item
                  key={entry.id}
                  asChild
                  className="w-24 min-w-0 flex-none flex-col flex-nowrap gap-2 px-2 py-2 text-center text-[13px] font-normal text-foreground/85 transition-none hover:bg-accent disabled:cursor-wait disabled:opacity-70"
                >
                  <button
                    disabled={Boolean(openingURL)}
                    title={`${entry.title || browserCompactURL(entry.url)} · ${browserCompactURL(entry.url)}`}
                    type="button"
                    onClick={() => onOpen(entry.url)}
                  >
                    <ItemHeader className="justify-center">
                      <span className="flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted">
                        {openingURL === entry.url ? <Spinner className="size-5" /> : <BrowserRecentFavicon entry={entry} />}
                      </span>
                    </ItemHeader>
                    <ItemContent className="w-full flex-none gap-0">
                      <ItemTitle className="block w-full truncate text-center text-[13px] font-normal">
                        {entry.title || browserCompactURL(entry.url)}
                      </ItemTitle>
                    </ItemContent>
                  </button>
                </Item>
              ))}
            </ItemGroup>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function BrowserRecentFavicon({ entry }: { entry: BrowserHistoryEntry }) {
  return (
    <BrowserFavicon
      className="size-6 object-contain"
      fallback={<Globe2 className="size-6 text-muted-foreground" />}
      faviconURL={entry.faviconURL}
      pageURL={entry.url}
    />
  );
}

function BrowserAutomationCursor({ cursor }: { cursor: BrowserAutomationCursorState }) {
  const style = {
    transform: `translate3d(${cursor.x}px, ${cursor.y}px, 0)`,
  } satisfies CSSProperties;
  const iconStyle = {
    filter: "drop-shadow(0 2px 2px rgba(0,0,0,0.5))",
  } satisfies CSSProperties;
  return (
    <div
      aria-hidden="true"
      className="pointer-events-none absolute top-0 left-0 z-20 transition-transform duration-200 ease-out will-change-transform"
      style={style}
    >
      {cursor.effectVisible && cursor.action === "click" ? (
        <span
          key={cursor.id}
          className="absolute top-0 left-0 h-8 w-8 -translate-x-1/2 -translate-y-1/2 animate-ping rounded-full border border-primary/70"
        />
      ) : null}
      {cursor.effectVisible && cursor.action === "type" ? (
        <span key={cursor.id} className="absolute top-0 left-1 h-5 w-0.5 animate-pulse rounded-full bg-primary/80" />
      ) : null}
      <MousePointer2
        className="relative h-5 w-5 -translate-x-0.5 -translate-y-0.5 text-neutral-950"
        fill="currentColor"
        stroke="white"
        strokeLinejoin="round"
        strokeWidth={1.35}
        style={iconStyle}
      />
    </div>
  );
}

function BrowserNavigationLoading({ label }: { label: string }) {
  return (
    <div aria-label={label} className="pointer-events-none absolute inset-x-0 top-0 z-10">
      <div className="absolute inset-x-0 top-0 h-0.5 overflow-hidden bg-primary/10">
        <div className="h-full w-1/2 animate-pulse bg-primary/80" />
      </div>
    </div>
  );
}

function BrowserLoadErrorPage({ error, onReload }: { error: WebviewLoadError; onReload: () => void }) {
  const { t } = useI18n();
  const host = webviewErrorHost(error.url);
  const message = host ? t("browser.errorHostNotResolved").replace("{host}", host) : t("browser.errorGeneric");

  return (
    <div className="absolute inset-0 z-10 overflow-auto bg-[var(--workspace-background)] text-foreground">
      <div className="mx-auto w-full max-w-[520px] px-8 pt-[18vh] pb-12">
        <FileX className="mb-8 h-12 w-12 text-muted-foreground" strokeWidth={1.75} />
        <h2 className="text-2xl leading-8 font-semibold text-foreground">{t("browser.errorTitle")}</h2>
        <p className="mt-4 text-[15px] leading-6 text-muted-foreground">{message}</p>
        <div className="mt-6 text-[15px] leading-6 text-muted-foreground">
            <p>{t("browser.errorTry")}</p>
          <ul className="mt-1 list-disc space-y-1 pl-7">
            <li>{t("browser.errorCheckNetwork")}</li>
            <li>{t("browser.errorCheckProxy")}</li>
          </ul>
        </div>
        {error.code ? <p className="mt-7 text-[13px] font-medium tracking-wide text-muted-foreground uppercase">{error.code}</p> : null}
        <Button className="mt-12 gap-2 rounded-full px-5" type="button" onClick={onReload}>
          <RefreshCw className="h-4 w-4" />
          {t("browser.errorReload")}
        </Button>
      </div>
    </div>
  );
}

function normalizeWebviewURL(rawURL: string) {
  const value = rawURL.trim();
  return !value || browserURLIsBlank(value) ? "about:blank" : value;
}

function webviewCurrentURL(node: WebviewElement | null | undefined) {
  if (!node) {
    return "";
  }
  try {
    return node.getURL?.() || "";
  } catch (error) {
    if (!isWebviewNotReadyError(error)) {
      console.warn("[browser] webview URL read failed", error);
    }
    return "";
  }
}

function webviewContentsID(node: WebviewElement | null | undefined) {
  if (!node || !node.isConnected) {
    return undefined;
  }
  try {
    return node.getWebContentsId?.();
  } catch (error) {
    if (!isWebviewNotReadyError(error)) {
      console.warn("[browser] webview contents id read failed", error);
    }
    return undefined;
  }
}

function isWebviewNavigationAbortError(error: unknown) {
  const message = String(error instanceof Error ? error.message : error || "");
  return message.includes("ERR_ABORTED") || message.includes("(-3)");
}

function isWebviewNavigationAbortCode(errorCode: number | undefined) {
  return errorCode === -3;
}

function isWebviewNotReadyError(error: unknown) {
  const message = String(error instanceof Error ? error.message : error || "");
  return message.includes("dom-ready") || message.includes("attached to the DOM");
}

function webviewErrorCode(event: WebviewLoadErrorEvent) {
  const description = (event.errorDescription || "").trim();
  if (/^ERR_/i.test(description)) {
    return description.toUpperCase();
  }
  if (typeof event.errorCode === "number" && Number.isFinite(event.errorCode)) {
    return `ERR_${event.errorCode}`;
  }
  return description || "ERR_FAILED";
}

function webviewErrorHost(rawURL: string) {
  try {
    const url = new URL(rawURL);
    return url.hostname || "";
  } catch {
    return "";
  }
}

function browserPayloadFromTab(tab: BrowserTab | undefined): (BrowserCanvasPayload & { updatedAt?: string }) | null {
  if (!tab) {
    return null;
  }
  return {
    kind: "browser",
    sessionID: tab.sessionID,
    tabID: tab.id,
    url: tab.url,
    title: tab.title,
    faviconURL: tab.faviconURL,
    mode: tab.mode,
    updatedAt: tab.updatedAt,
  };
}
