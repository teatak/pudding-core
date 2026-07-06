import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, CornerDownLeft, ExternalLink, Loader2, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import {
  backBrowserTab,
  forwardBrowserTab,
  listBrowserTabs,
  openBrowserTab,
  openBrowserURL,
  putCanvasItem,
  reloadBrowserTab,
  revealBrowserTab,
  type BrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  browserAddressToURL,
  browserDisplayURL,
  browserForegroundRefetchIntervalMS,
  browserPayloadForItem,
  browserPayloadNeedsTabSync,
  browserQueryStaleTimeMS,
  browserTabFaviconURL,
  browserTabTitle,
  preferredBrowserTab,
  upsertBrowserTab,
} from "@/browser/helpers";
import type { BrowserNavigationAction, BrowserTabsData } from "@/browser/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

export function BrowserToolbar({ token, item }: { token: string; item: CanvasItem }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const payload = browserPayloadForItem(item);
  const ownerSessionID = payload?.sessionID || item.sourceSessionID;
  const [urlDraft, setURLDraft] = useState(browserDisplayURL(payload?.url));
  const tabsQuery = useQuery({
    enabled: Boolean(token && ownerSessionID),
    queryKey: ownerSessionID ? queryKeys.browserTabs(ownerSessionID) : ["browser", "missing-session"],
    queryFn: () => {
      if (!ownerSessionID) {
        throw new Error("browser session id missing");
      }
      return listBrowserTabs(token, ownerSessionID);
    },
    refetchInterval: browserForegroundRefetchIntervalMS,
    staleTime: browserQueryStaleTimeMS,
  });
  const tabs = tabsQuery.data?.tabs || [];
  const activeTab = preferredBrowserTab(tabs, payload);
  const processMode = tabsQuery.data?.processMode || activeTab?.mode || payload?.mode;
  const isExternalBrowser = processMode === "external";
  const actionTabID = activeTab?.id || payload?.tabID;

  useEffect(() => {
    if (activeTab?.url) {
      setURLDraft(browserDisplayURL(activeTab.url));
      return;
    }
    setURLDraft(browserDisplayURL(payload?.url));
  }, [activeTab?.id, activeTab?.url, payload?.url]);

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

  useEffect(() => {
    if (!activeTab || !browserPayloadNeedsTabSync(item, payload, activeTab, t("browser.title"))) {
      return;
    }
    void persistTab(activeTab);
  }, [
    activeTab?.id,
    activeTab?.url,
    activeTab?.title,
    activeTab?.faviconURL,
    activeTab?.mode,
    item.id,
    item.title,
    payload?.tabID,
    payload?.url,
    payload?.title,
    payload?.faviconURL,
    payload?.mode,
  ]);

  const openMutation = useMutation({
    mutationFn: async () => {
      if (!ownerSessionID) {
        throw new Error("browser session id missing");
      }
      if (isExternalBrowser) {
        throw new Error("browser is external");
      }
      const url = browserAddressToURL(urlDraft);
      if (activeTab) {
        return openBrowserTab(token, ownerSessionID, activeTab.id, { url });
      }
      return openBrowserURL(token, ownerSessionID, { url });
    },
    onSuccess: (tab) => {
      void persistTab(tab);
    },
    onError: () => toast.error(t("browser.openFailed")),
  });
  const navigationMutation = useMutation({
    mutationFn: async (action: BrowserNavigationAction) => {
      if (!ownerSessionID || !activeTab || isExternalBrowser) {
        throw new Error("browser tab missing");
      }
      switch (action) {
        case "back":
          return backBrowserTab(token, ownerSessionID, activeTab.id);
        case "forward":
          return forwardBrowserTab(token, ownerSessionID, activeTab.id);
        case "reload":
          return reloadBrowserTab(token, ownerSessionID, activeTab.id);
      }
    },
    onSuccess: (tab) => {
      void persistTab(tab);
    },
    onError: () => toast.error(t("browser.navigationFailed")),
  });
  const revealMutation = useMutation({
    mutationFn: async () => {
      if (!ownerSessionID) {
        throw new Error("browser session id missing");
      }
      let tab = activeTab;
      if (!tab) {
        if (actionTabID) {
          return revealBrowserTab(token, ownerSessionID, actionTabID);
        }
        const url = browserAddressToURL(urlDraft);
        tab = await openBrowserURL(token, ownerSessionID, { url });
        await persistTab(tab);
      }
      return revealBrowserTab(token, ownerSessionID, tab.id);
    },
    onSuccess: (tab) => {
      void persistTab(tab);
    },
    onError: () => toast.error(t("browser.revealFailed")),
  });
  const navigationDisabled = !activeTab || isExternalBrowser || tabsQuery.isPending || navigationMutation.isPending;
  const backDisabled = navigationDisabled || !activeTab?.canGoBack;
  const forwardDisabled = navigationDisabled || !activeTab?.canGoForward;
  const revealDisabled = revealMutation.isPending || tabsQuery.isPending || (!actionTabID && !urlDraft.trim());
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
        if (!isExternalBrowser && urlDraft.trim()) {
          openMutation.mutate();
        }
      }}
    >
      <div className="grid shrink-0 grid-cols-[repeat(4,28px)] gap-0.5">
        <Button
          aria-label={t("browser.back")}
          className={navButtonClass}
          disabled={backDisabled}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => navigationMutation.mutate("back")}
        >
          {pendingNavigationAction === "back" ? <Loader2 className={`${navIconClass} animate-spin`} /> : <ArrowLeft className={navIconClass} />}
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
          {pendingNavigationAction === "forward" ? <Loader2 className={`${navIconClass} animate-spin`} /> : <ArrowRight className={navIconClass} />}
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
          {pendingNavigationAction === "reload" ? <Loader2 className={`${navIconClass} animate-spin`} /> : <RefreshCw className={navIconClass} />}
        </Button>
        <Button
          aria-label={t("browser.reveal")}
          className={navButtonClass}
          disabled={revealDisabled}
          size="icon-sm"
          title={isExternalBrowser ? t("browser.focusExternal") : t("browser.reveal")}
          type="button"
          variant="ghost"
          onClick={() => revealMutation.mutate()}
        >
          {revealMutation.isPending ? <Loader2 className={`${navIconClass} animate-spin`} /> : <ExternalLink className={navIconClass} />}
        </Button>
      </div>
      <div className="group relative flex h-8 min-w-0 flex-1 items-center rounded-md border border-transparent bg-transparent transition-[background-color,box-shadow] hover:bg-background/45 focus-within:bg-background/45 focus-within:shadow-[0_0_0_1px_hsl(var(--border)/0.7),0_0_0_3px_hsl(var(--ring)/0.12)]">
        <Input
          className="h-7 min-w-0 flex-1 border-0 bg-transparent pr-8 shadow-none focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent"
          disabled={isExternalBrowser}
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
          disabled={isExternalBrowser || openMutation.isPending || !urlDraft.trim()}
          size="icon-sm"
          type="submit"
          variant="ghost"
        >
          {openMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CornerDownLeft className="h-3.5 w-3.5" />}
        </Button>
      </div>
    </form>
  );
}
