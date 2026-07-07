import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createElement, useCallback, useEffect, useRef, type HTMLAttributes } from "react";

import { listBrowserTabs } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { cacheElectronBrowserSnapshot, electronBrowserBridge, type ElectronWebviewCaptureResponse } from "@/browser/electronBridge";
import {
  browserPayloadForItem,
  browserQueryStaleTimeMS,
  browserTabTitle,
  browserURLIsBlank,
  preferredBrowserTab,
} from "@/browser/helpers";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

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

type WebviewProps = HTMLAttributes<HTMLElement> & {
  ref: (node: WebviewElement | null) => void;
  src: string;
  partition: string;
  allowpopups: string;
  webpreferences: string;
};

export function ElectronWebviewBrowser({ token, item }: { token: string; item: CanvasItem }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const webviewRef = useRef<WebviewElement | null>(null);
  const payload = browserPayloadForItem(item);
  const webviewReadyRef = useRef(false);
  const webviewReadyCleanupRef = useRef<(() => void) | null>(null);
  const lastRequestedURLRef = useRef("");
  const pendingTargetURLRef = useRef("");
  const ownerSessionID = payload?.sessionID || item.sourceSessionID;
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
  const activeTab = preferredBrowserTab(tabs, payload);
  const title = activeTab ? browserTabTitle(activeTab, payload?.title || t("browser.newTab"), t("browser.newTab")) : payload?.title || t("browser.newTab");
  const tabID = activeTab?.id || payload?.tabID || "default";
  const targetURL = normalizeWebviewURL(activeTab?.url || payload?.url || "");

  const setWebviewRef = useCallback((node: WebviewElement | null) => {
    webviewReadyCleanupRef.current?.();
    webviewReadyCleanupRef.current = null;
    webviewRef.current = node;
    webviewReadyRef.current = false;
    lastRequestedURLRef.current = "";
    if (!node) {
      return;
    }
    const handleReady = () => {
      webviewReadyRef.current = true;
      loadWebviewURL(node, pendingTargetURLRef.current || "about:blank", lastRequestedURLRef);
    };
    node.addEventListener("dom-ready", handleReady);
    webviewReadyCleanupRef.current = () => {
      node.removeEventListener("dom-ready", handleReady);
      if (webviewRef.current === node) {
        webviewReadyRef.current = false;
      }
    };
  }, []);

  useEffect(() => {
    pendingTargetURLRef.current = targetURL;
    const node = webviewRef.current;
    if (!node || !webviewReadyRef.current) {
      return;
    }
    loadWebviewURL(node, targetURL, lastRequestedURLRef);
  }, [targetURL]);

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
      const currentURL = normalizeWebviewURL(node.getURL?.() || "");
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
    <div className="canvas-window-no-drag relative h-full min-h-0 overflow-hidden bg-card" aria-label={title} role="application">
      {createElement("webview", {
        ref: setWebviewRef,
        className: "h-full w-full bg-card",
        src: "about:blank",
        partition: "persist:pudding-default",
        allowpopups: "true",
        webpreferences: "contextIsolation=yes,sandbox=yes",
      } satisfies WebviewProps)}
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

function loadWebviewURL(node: WebviewElement, targetURL: string, lastRequestedURLRef: { current: string }) {
  const currentURL = normalizeWebviewURL(node.getURL?.() || "");
  if (sameWebviewURL(currentURL, targetURL) || sameWebviewURL(lastRequestedURLRef.current, targetURL)) {
    return;
  }
  lastRequestedURLRef.current = targetURL;
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
      lastRequestedURLRef.current = "";
      console.warn("[browser] webview navigation failed", error);
    });
  } catch (error) {
    if (isWebviewNavigationAbortError(error) || isWebviewNotReadyError(error)) {
      lastRequestedURLRef.current = "";
      return;
    }
    lastRequestedURLRef.current = "";
    console.warn("[browser] webview navigation failed", error);
  }
}

function isWebviewNavigationAbortError(error: unknown) {
  const message = String(error instanceof Error ? error.message : error || "");
  return message.includes("ERR_ABORTED") || message.includes("(-3)");
}

function isWebviewNotReadyError(error: unknown) {
  const message = String(error instanceof Error ? error.message : error || "");
  return message.includes("dom-ready") || message.includes("attached to the DOM");
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
