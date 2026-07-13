import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, CornerDownLeft, RefreshCw } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  APIError,
  backBrowserTab,
  forwardBrowserTab,
  listBrowserTabs,
  openBrowserTab,
  openBrowserURL,
  reloadBrowserTab,
  syncBrowserTab,
  type BrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { allowElectronBrowserTab, hasElectronWebviewBrowser } from "@/browser/electronBridge";
import {
  browserAddressToURL,
  browserDisplayURL,
  browserQueryStaleTimeMS,
  browserTabFaviconURL,
  browserTargetURL,
  browserTabTitle,
  preferredBrowserTab,
  upsertBrowserTab,
} from "@/browser/helpers";
import type { BrowserCanvasPayload, BrowserNavigationAction, BrowserTabsData } from "@/browser/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";

type BrowserOpenAttempt = {
  url: string;
};

type PersistTabOptions = {
  refreshAfterPersist?: boolean;
  syncBrowserTab?: boolean;
};

function browserOpenErrorDescription(attempt: BrowserOpenAttempt | null, error: unknown): string {
  const lines: string[] = [];
  if (attempt?.url) {
    lines.push(`URL: ${attempt.url}`);
  }
  const message =
    error instanceof APIError
      ? `${error.status} ${error.code}`
      : error instanceof Error
        ? error.message
        : typeof error === "string"
          ? error
          : "";
  if (message) {
    lines.push(`Error: ${message}`);
  }
  return lines.join("\n");
}

function isBrowserNavigationAbortError(error: unknown): boolean {
  const message =
    error instanceof APIError
      ? error.code
      : error instanceof Error
        ? error.message
        : typeof error === "string"
          ? error
          : "";
  return message.includes("ERR_ABORTED") || message.includes("(-3)");
}

function sameToolbarURL(left: string, right: string): boolean {
  const leftURL = browserDisplayURL(left);
  const rightURL = browserDisplayURL(right);
  if (leftURL === rightURL) {
    return true;
  }
  try {
    return new URL(leftURL).toString() === new URL(rightURL).toString();
  } catch {
    return false;
  }
}

export function BrowserToolbar({
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
  const payload = browserPayloadFromTab(activeTabProp);
  const [urlDraft, setURLDraft] = useState(browserDisplayURL(payload?.url));
  const lastOpenAttemptRef = useRef<BrowserOpenAttempt | null>(null);
  const pendingSubmittedURLRef = useRef("");
  const embeddedBrowser = hasElectronWebviewBrowser();
  const processModeFallback = embeddedBrowser ? "webview" : "headless";
  const tabsQuery = useQuery({
    enabled: Boolean(token && sessionID),
    queryKey: queryKeys.browserTabs(sessionID),
    queryFn: () => listBrowserTabs(token, sessionID),
    staleTime: browserQueryStaleTimeMS,
  });
  const tabs = (tabsQuery.data?.tabs || []).filter((tab) => tab.sessionID === sessionID);
  const activeTab = activeTabProp || preferredBrowserTab(tabs, payload);
  const targetURL = browserTargetURL(activeTab, payload, payload?.updatedAt);
  const refreshBrowserQueries = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(sessionID) });
    void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(sessionID) });
  };

  useLayoutEffect(() => {
    if (pendingSubmittedURLRef.current && !sameToolbarURL(pendingSubmittedURLRef.current, targetURL)) {
      return;
    }
    pendingSubmittedURLRef.current = "";
    setURLDraft(browserDisplayURL(targetURL));
  }, [targetURL]);

  const persistTab = async (tab: BrowserTab, options: PersistTabOptions = {}) => {
    if (tab.sessionID !== sessionID) {
      return;
    }
    const refreshAfterPersist = options.refreshAfterPersist ?? true;
    const shouldSyncBrowserTab = options.syncBrowserTab ?? false;
    const title = browserTabTitle(tab, t("browser.newTab"), t("browser.newTab"));
    const faviconURL = browserTabFaviconURL(tab);
    allowElectronBrowserTab(sessionID, tab.id);
    queryClient.setQueryData(queryKeys.browserTabs(sessionID), (current: BrowserTabsData | undefined) => ({
      tabs: upsertBrowserTab(current?.tabs || [], tab),
      processMode: tab.mode || current?.processMode,
    }));
    queryClient.setQueryData(queryKeys.browserState(sessionID), {
      hasState: true,
      sessionID,
      tabID: tab.id,
      url: tab.url,
      title,
      faviconURL,
      mode: tab.mode,
      processMode: tab.mode || processModeFallback,
      createdAt: tab.createdAt,
      updatedAt: tab.updatedAt,
    });
    const syncedTab = shouldSyncBrowserTab
      ? await syncBrowserTab(token, sessionID, tab.id, {
          targetID: tab.targetID,
          url: tab.url,
          title,
          faviconURL,
          canGoBack: tab.canGoBack,
          canGoForward: tab.canGoForward,
        }).catch(() => null)
      : null;
    if (syncedTab) {
      queryClient.setQueryData(queryKeys.browserTabs(sessionID), (current: BrowserTabsData | undefined) => ({
        tabs: upsertBrowserTab(current?.tabs || [], syncedTab),
        processMode: syncedTab.mode || current?.processMode,
      }));
      queryClient.setQueryData(queryKeys.browserState(sessionID), {
        hasState: true,
        sessionID,
        tabID: syncedTab.id,
        url: syncedTab.url,
        title,
        faviconURL,
        mode: syncedTab.mode,
        processMode: syncedTab.mode || processModeFallback,
        createdAt: syncedTab.createdAt,
        updatedAt: syncedTab.updatedAt,
      });
    }
    if (refreshAfterPersist) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(sessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(sessionID) });
    }
  };

  useEffect(() => {
    if (!activeTab) {
      return;
    }
    allowElectronBrowserTab(sessionID, activeTab.id);
  }, [
    activeTab?.id,
    activeTab?.url,
    activeTab?.title,
    activeTab?.faviconURL,
    activeTab?.mode,
    sessionID,
  ]);

  const openMutation = useMutation({
    mutationFn: async (url: string) => {
      lastOpenAttemptRef.current = { url };
      if (activeTab) {
        return openBrowserTab(token, sessionID, activeTab.id, { url });
      }
      return openBrowserURL(token, sessionID, { url });
    },
    onSuccess: (tab) => {
      void persistTab(tab, { refreshAfterPersist: !embeddedBrowser, syncBrowserTab: embeddedBrowser });
    },
    onError: (error) => {
      if (isBrowserNavigationAbortError(error)) {
        refreshBrowserQueries();
        return;
      }
      toast.error(t("browser.openFailed"), { description: browserOpenErrorDescription(lastOpenAttemptRef.current, error) });
    },
  });
  const navigationMutation = useMutation({
    mutationFn: async (action: BrowserNavigationAction) => {
      if (!activeTab) {
        throw new Error("browser tab missing");
      }
      switch (action) {
        case "back":
          return backBrowserTab(token, sessionID, activeTab.id);
        case "forward":
          return forwardBrowserTab(token, sessionID, activeTab.id);
        case "reload":
          return reloadBrowserTab(token, sessionID, activeTab.id);
      }
    },
    onSuccess: (tab) => {
      void persistTab(tab);
    },
    onError: (error) => {
      if (isBrowserNavigationAbortError(error)) {
        refreshBrowserQueries();
        return;
      }
      toast.error(t("browser.navigationFailed"), { description: browserOpenErrorDescription(null, error) });
    },
  });
  const navigationDisabled = !activeTab || tabsQuery.isPending || navigationMutation.isPending;
  const backDisabled = navigationDisabled || !activeTab?.canGoBack;
  const forwardDisabled = navigationDisabled || !activeTab?.canGoForward;
  const pendingNavigationAction = navigationMutation.isPending ? navigationMutation.variables : undefined;
  const navButtonClass =
    "h-7 w-7 rounded-md text-muted-foreground [backface-visibility:hidden] [transform:translateZ(0)] [transition-duration:120ms] [transition-property:background-color,color] hover:text-foreground active:translate-y-0";
  const navIconClass = "h-3.5 w-3.5 [backface-visibility:hidden] [transform:translateZ(0)]";

  return (
    <form
      className="canvas-window-no-drag flex min-w-0 flex-1 items-center gap-2"
      onDoubleClick={(event) => event.stopPropagation()}
      onPointerDown={(event) => event.stopPropagation()}
      onSubmit={(event) => {
        event.preventDefault();
        if (urlDraft.trim()) {
          const url = browserAddressToURL(urlDraft);
          pendingSubmittedURLRef.current = url;
          setURLDraft(browserDisplayURL(url));
          openMutation.mutate(url);
        }
      }}
    >
      <div className="grid shrink-0 grid-cols-3 gap-0.5">
        <Button
          aria-label={t("browser.back")}
          className={navButtonClass}
          disabled={backDisabled}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => navigationMutation.mutate("back")}
        >
          {pendingNavigationAction === "back" ? <Spinner className={`${navIconClass}`} /> : <ArrowLeft className={navIconClass} />}
        </Button>
        <Button
          aria-label={t("browser.forward")}
          className={navButtonClass}
          disabled={forwardDisabled}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => navigationMutation.mutate("forward")}
        >
          {pendingNavigationAction === "forward" ? <Spinner className={`${navIconClass}`} /> : <ArrowRight className={navIconClass} />}
        </Button>
        <Button
          aria-label={t("browser.reload")}
          className={navButtonClass}
          disabled={navigationDisabled}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => navigationMutation.mutate("reload")}
        >
          {pendingNavigationAction === "reload" ? <Spinner className={`${navIconClass}`} /> : <RefreshCw className={navIconClass} />}
        </Button>
      </div>
      <div className="group relative flex h-8 min-w-0 flex-1 items-center rounded-md border border-transparent bg-transparent transition-[background-color,box-shadow] hover:bg-background/45 focus-within:bg-background/45 focus-within:shadow-[0_0_0_1px_hsl(var(--border)/0.7),0_0_0_3px_hsl(var(--ring)/0.12)]">
        <Input
          className="h-7 min-w-0 flex-1 border-0 bg-transparent pr-8 shadow-none focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent"
          placeholder={t("browser.urlPlaceholder")}
          value={urlDraft}
          onChange={(event) => setURLDraft(event.target.value)}
          onFocus={(event) => {
            const input = event.currentTarget;
            window.setTimeout(() => {
              if (document.activeElement === input) {
                input.select();
              }
            }, 0);
          }}
        />
        <Button
          aria-label={t("browser.openURL")}
          className="absolute top-1/2 right-1 h-5 w-5 -translate-y-1/2 rounded-[5px] text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 hover:bg-muted/70 hover:text-foreground focus-visible:opacity-100 disabled:opacity-0 group-focus-within:opacity-100"
          disabled={openMutation.isPending || !urlDraft.trim()}
          size="icon-sm"
          type="submit"
          variant="ghost"
        >
          {openMutation.isPending ? <Spinner className="h-3.5 w-3.5" /> : <CornerDownLeft className="h-3.5 w-3.5" />}
        </Button>
      </div>
    </form>
  );
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
