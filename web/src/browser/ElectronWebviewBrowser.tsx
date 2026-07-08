import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileX, RefreshCw } from "lucide-react";
import { createElement, useCallback, useEffect, useRef, useState, type HTMLAttributes } from "react";

import { listBrowserTabs, type BrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { cacheElectronBrowserSnapshot, electronBrowserBridge, type ElectronWebviewCaptureResponse } from "@/browser/electronBridge";
import {
  browserQueryStaleTimeMS,
  browserTargetURL,
  browserTabTitle,
  browserURLIsBlank,
  preferredBrowserTab,
} from "@/browser/helpers";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import type { BrowserCanvasPayload } from "./types";

type CapturedWebviewImage = {
  toDataURL?: () => string;
  toPNG?: () => Uint8Array | ArrayBuffer | number[];
  getSize?: () => { width?: number; height?: number };
};

type WebviewElement = HTMLElement & {
  capturePage?: () => Promise<CapturedWebviewImage>;
  getWebContentsId?: () => number;
  getURL?: () => string;
  loadURL?: (url: string) => Promise<void>;
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

export function ElectronWebviewBrowser({
  activeTab: activeTabProp,
  sessionID,
  token,
}: {
  activeTab?: BrowserTab;
  sessionID: string;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const webviewRef = useRef<WebviewElement | null>(null);
  const payload = browserPayloadFromTab(activeTabProp);
  const webviewReadyRef = useRef(false);
  const webviewReadyCleanupRef = useRef<(() => void) | null>(null);
  const lastRequestedURLRef = useRef("");
  const pendingProgrammaticURLRef = useRef("");
  const pendingTargetURLRef = useRef("");
  const navigationSeqRef = useRef(0);
  const failedNavigationSeqRef = useRef(0);
  const loadErrorRef = useRef<WebviewLoadError | null>(null);
  const [loadError, setLoadError] = useState<WebviewLoadError | null>(null);
  const [navigationLoading, setNavigationLoading] = useState(false);
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

  const updateLoadError = useCallback((error: WebviewLoadError | null) => {
    loadErrorRef.current = error;
    setLoadError(error);
  }, []);

  const setWebviewRef = useCallback((node: WebviewElement | null) => {
    webviewReadyCleanupRef.current?.();
    webviewReadyCleanupRef.current = null;
    webviewRef.current = node;
    webviewReadyRef.current = false;
    lastRequestedURLRef.current = "";
    pendingProgrammaticURLRef.current = "";
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
      loadWebviewURL(node, pendingTargetURLRef.current || "about:blank", lastRequestedURLRef, pendingProgrammaticURLRef);
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
      pendingProgrammaticURLRef.current = "";
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
      const failedURL = normalizeWebviewURL(loadEvent.validatedURL || pendingProgrammaticURLRef.current || pendingTargetURLRef.current || node.getURL?.() || "");
      const error = {
        code: webviewErrorCode(loadEvent),
        description: loadEvent.errorDescription || "",
        url: failedURL,
      };
      setNavigationLoading(false);
      updateLoadError(error);
      const bridge = electronBrowserBridge();
      const webContentsID = node.getWebContentsId?.();
      if (bridge && ownerSessionID && webContentsID && !browserURLIsBlank(failedURL)) {
        void bridge
          .registerWebview({
            sessionID: ownerSessionID,
            tabID,
            url: failedURL,
            webContentsID,
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
    pendingTargetURLRef.current = targetURL;
    const node = webviewRef.current;
    if (!node || !webviewReadyRef.current) {
      return;
    }
    if (
      !browserURLIsBlank(targetURL) &&
      !sameWebviewURL(node.getURL?.() || "", targetURL) &&
      !sameWebviewURL(lastRequestedURLRef.current, targetURL)
    ) {
      setNavigationLoading(true);
    }
    loadWebviewURL(node, targetURL, lastRequestedURLRef, pendingProgrammaticURLRef);
  }, [targetURL]);

  const reloadAfterError = useCallback(() => {
    const node = webviewRef.current;
    const retryURL = loadError?.url || pendingTargetURLRef.current || targetURL || "about:blank";
    const bridge = electronBrowserBridge();
    const webContentsID = node?.getWebContentsId?.();
    if (bridge && ownerSessionID && webContentsID) {
      void bridge
        .reload({ sessionID: ownerSessionID, tabID, url: retryURL })
        .then((snapshot) => cacheElectronBrowserSnapshot(queryClient, snapshot, ownerSessionID))
        .catch((error) => {
          if (!isWebviewNavigationAbortError(error)) {
            console.warn("[browser] webview reload failed", error);
          }
        });
      return;
    }
    if (node) {
      loadWebviewURL(node, retryURL, lastRequestedURLRef, pendingProgrammaticURLRef, { force: true });
    }
  }, [loadError?.url, ownerSessionID, queryClient, tabID, targetURL]);

  useEffect(() => {
    const node = webviewRef.current;
    const bridge = electronBrowserBridge();
    if (!node || !bridge || !ownerSessionID) {
      return;
    }
    let disposed = false;
    const register = () => {
      if (disposed) {
        return;
      }
      const webContentsID = node.getWebContentsId?.();
      if (!webContentsID) {
        return;
      }
      if (loadErrorRef.current) {
        return;
      }
      const currentURL = normalizeWebviewURL(node.getURL?.() || "");
      const pendingProgrammaticURL = pendingProgrammaticURLRef.current;
      if (pendingProgrammaticURL && !sameWebviewURL(currentURL, pendingProgrammaticURL)) {
        return;
      }
      if (browserURLIsBlank(currentURL) && !browserURLIsBlank(targetURL)) {
        return;
      }
      void bridge
        .registerWebview({ sessionID: ownerSessionID, tabID, url: currentURL, webContentsID })
        .then((snapshot) => {
          if (!disposed) {
            cacheElectronBrowserSnapshot(queryClient, snapshot, ownerSessionID);
          }
        })
        .catch(() => undefined);
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
      node.removeEventListener("dom-ready", listener);
      node.removeEventListener("did-finish-load", listener);
      node.removeEventListener("did-navigate", listener);
      node.removeEventListener("did-navigate-in-page", listener);
      node.removeEventListener("page-title-updated", listener);
      node.removeEventListener("page-favicon-updated", listener);
    };
  }, [ownerSessionID, queryClient, targetURL, tabID]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onWebviewCaptureRequest || !bridge.resolveWebviewCapture || !ownerSessionID) {
      return;
    }
    const resolveWebviewCapture = bridge.resolveWebviewCapture;
    return bridge.onWebviewCaptureRequest((request) => {
      if (request.sessionID !== ownerSessionID || request.tabID !== tabID) {
        return;
      }
      void captureCurrentWebview(webviewRef.current, request.captureID, resolveWebviewCapture);
    });
  }, [ownerSessionID, tabID]);

  if (!ownerSessionID) {
    return <div className="p-3 text-sm text-muted-foreground">{t("browser.loadFailed")}</div>;
  }

  return (
    <div className="canvas-window-no-drag relative h-full min-h-0 overflow-hidden bg-[var(--canvas-background)]" aria-label={title} role="application">
      {createElement("webview", {
        ref: setWebviewRef,
        className: "h-full w-full bg-[var(--canvas-background)]",
        src: "about:blank",
        partition: "persist:pudding-default",
        allowpopups: "true",
        webpreferences: "contextIsolation=yes,sandbox=yes",
      } satisfies WebviewProps)}
      {navigationLoading && !loadError ? <BrowserNavigationLoading label={t("browser.loadingPage")} /> : null}
      {loadError ? <BrowserLoadErrorPage error={loadError} onReload={reloadAfterError} /> : null}
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
    <div className="absolute inset-0 z-10 overflow-auto bg-[var(--canvas-background)] text-foreground">
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

function sameWebviewURL(left: string, right: string) {
  return comparableWebviewURL(left) === comparableWebviewURL(right);
}

function comparableWebviewURL(rawURL: string) {
  const value = normalizeWebviewURL(rawURL);
  try {
    return new URL(value).toString();
  } catch {
    return value;
  }
}

function loadWebviewURL(
  node: WebviewElement,
  targetURL: string,
  lastRequestedURLRef: { current: string },
  pendingProgrammaticURLRef: { current: string },
  options: { force?: boolean } = {},
) {
  const currentURL = normalizeWebviewURL(node.getURL?.() || "");
  if (!options.force && (sameWebviewURL(currentURL, targetURL) || sameWebviewURL(lastRequestedURLRef.current, targetURL))) {
    return;
  }
  lastRequestedURLRef.current = targetURL;
  pendingProgrammaticURLRef.current = targetURL;
  const load = node.loadURL?.bind(node);
  if (!load) {
    node.setAttribute("src", targetURL);
    return;
  }
  try {
    void load(targetURL).catch((error) => {
      if (isWebviewNavigationAbortError(error)) {
        return;
      }
      console.warn("[browser] webview navigation failed", error);
    });
  } catch (error) {
    if (isWebviewNavigationAbortError(error) || isWebviewNotReadyError(error)) {
      lastRequestedURLRef.current = "";
      pendingProgrammaticURLRef.current = "";
      return;
    }
    lastRequestedURLRef.current = "";
    pendingProgrammaticURLRef.current = "";
    console.warn("[browser] webview navigation failed", error);
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

async function captureCurrentWebview(
  node: WebviewElement | null,
  captureID: string,
  resolveCapture: (response: ElectronWebviewCaptureResponse) => Promise<void>,
) {
  try {
    if (!node) {
      throw new Error("webview is not mounted");
    }
    const capturePage = node.capturePage?.bind(node);
    if (!capturePage) {
      throw new Error("webview.capturePage is unavailable");
    }
    const image = await capturePage();
    const dataBase64 = dataURLToBase64(image?.toDataURL?.() || "") || pngBytesToBase64(image?.toPNG?.());
    if (!dataBase64) {
      throw new Error("webview.capturePage returned empty image");
    }
    const size = image?.getSize?.();
    await resolveCapture({
      captureID,
      dataBase64,
      width: Math.max(0, Math.round(Number(size?.width) || 0)),
      height: Math.max(0, Math.round(Number(size?.height) || 0)),
    });
  } catch (error) {
    await resolveCapture({
      captureID,
      error: error instanceof Error ? error.message : String(error || "webview capture failed"),
    });
  }
}

function dataURLToBase64(dataURL: string) {
  const marker = "base64,";
  const index = dataURL.indexOf(marker);
  return index >= 0 ? dataURL.slice(index + marker.length) : "";
}

function pngBytesToBase64(value: Uint8Array | ArrayBuffer | number[] | undefined) {
  if (!value) {
    return "";
  }
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}
