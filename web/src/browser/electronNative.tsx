import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";

import { queryKeys } from "@/api/queryKeys";
import {
  cacheElectronBrowserSnapshot,
  electronBrowserSnapshotsToTabs,
  electronNativeBrowser,
  hasElectronNativeBrowser,
  type ElectronBrowserBounds,
} from "@/browser/electronBridge";
import {
  browserPayloadForItem,
  browserPayloadHasRealState,
  browserQueryStaleTimeMS,
  browserTabTitle,
  browserURLIsBlank,
  preferredBrowserTab,
} from "@/browser/helpers";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

export function ElectronNativeBrowser({ token, item }: { token: string; item: CanvasItem }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const attachedRef = useRef(false);
  const payload = browserPayloadForItem(item);
  const ownerSessionID = payload?.sessionID || item.sourceSessionID;
  const tabsQuery = useQuery({
    enabled: Boolean(token && ownerSessionID),
    queryKey: ownerSessionID ? queryKeys.browserTabs(ownerSessionID) : ["browser", "missing-session"],
    queryFn: () => {
      if (!ownerSessionID) {
        throw new Error("browser session id missing");
      }
      const bridge = electronNativeBrowser();
      if (!bridge) {
        throw new Error("electron browser bridge missing");
      }
      return bridge.listTabs({ sessionID: ownerSessionID }).then((result) => electronBrowserSnapshotsToTabs(result.tabs));
    },
    staleTime: browserQueryStaleTimeMS,
  });
  const tabs = tabsQuery.data?.tabs || [];
  const activeTab = preferredBrowserTab(tabs, payload);
  const tabID = activeTab?.id || payload?.tabID || "";
  const url = normalizeNativeURL(activeTab?.url || payload?.url || "");
  const title = activeTab?.title?.trim() || (browserPayloadHasRealState(payload) ? payload?.title : "") || t("browser.title");
  const processMode = tabsQuery.data?.processMode || activeTab?.mode || payload?.mode;
  const isExternalBrowser = processMode === "external";
  const bridge = electronNativeBrowser();
  const viewKey = useMemo(() => {
    if (!ownerSessionID) {
      return "";
    }
    return `${ownerSessionID}:${tabID || "default"}`;
  }, [ownerSessionID, tabID]);

  useEffect(() => {
    if (!bridge || !ownerSessionID || isExternalBrowser) {
      return;
    }
    const node = containerRef.current;
    if (!node) {
      return;
    }
    let disposed = false;
    let frame = 0;
    const request = () => ({ sessionID: ownerSessionID, tabID, url, bounds: readBounds(node) });
    const sync = () => {
      if (disposed) {
        return;
      }
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        if (disposed) {
          return;
        }
        const bounds = readBounds(node);
        if (bounds.width <= 0 || bounds.height <= 0) {
          return;
        }
        const call = attachedRef.current ? bridge.updateBounds(request()) : bridge.attach(request());
        void call
          .then((snapshot) => {
            attachedRef.current = true;
            if (snapshot) {
              cacheElectronBrowserSnapshot(queryClient, snapshot);
            }
          })
          .catch(() => {
            attachedRef.current = false;
          });
      });
    };
    const observer = new ResizeObserver(sync);
    observer.observe(node);
    window.addEventListener("resize", sync);
    window.visualViewport?.addEventListener("resize", sync);
    sync();
    return () => {
      disposed = true;
      window.cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", sync);
      window.visualViewport?.removeEventListener("resize", sync);
      attachedRef.current = false;
      void bridge.detach({ sessionID: ownerSessionID, tabID }).catch(() => undefined);
    };
  }, [bridge, isExternalBrowser, ownerSessionID, tabID, url, viewKey]);

  useEffect(() => {
    if (!bridge || !ownerSessionID || isExternalBrowser || !url) {
      return;
    }
    void bridge.loadURL({ sessionID: ownerSessionID, tabID, url }).catch(() => undefined);
  }, [bridge, isExternalBrowser, ownerSessionID, tabID, url]);

  useEffect(() => {
    if (!bridge || !ownerSessionID) {
      return;
    }
    return bridge.onUpdated((snapshot) => {
      if (snapshot.sessionID === ownerSessionID) {
        cacheElectronBrowserSnapshot(queryClient, snapshot);
      }
    });
  }, [bridge, ownerSessionID, queryClient]);

  if (!ownerSessionID) {
    return <div className="p-3 text-sm text-muted-foreground">{t("browser.loadFailed")}</div>;
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-card">
      <div
        ref={containerRef}
        aria-label={activeTab ? browserTabTitle(activeTab, title) : title}
        className="canvas-window-no-drag relative min-h-0 flex-1 overflow-hidden bg-card"
        role="application"
      >
        {tabsQuery.isPending && !url ? (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center gap-2 bg-card text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            {t("browser.loading")}
          </div>
        ) : null}
        {isExternalBrowser ? (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-card text-sm text-muted-foreground">
            {t("browser.externalOpen")}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function readBounds(node: HTMLElement): ElectronBrowserBounds {
  const rect = node.getBoundingClientRect();
  return {
    x: Math.round(rect.left),
    y: Math.round(rect.top),
    width: Math.round(rect.width),
    height: Math.round(rect.height),
  };
}

function normalizeNativeURL(rawURL: string) {
  const value = rawURL.trim();
  if (!value || browserURLIsBlank(value)) {
    return "about:blank";
  }
  return value;
}
