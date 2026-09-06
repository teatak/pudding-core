import { LibraryFavoriteButton } from "@/components/workspace/LibraryFavoriteButton";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, RefreshCw, X } from "@/components/icons";
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
  type BrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrowserAddressField } from "./BrowserAddressField";
import { browserOpenErrorDescription, type BrowserOpenAttempt } from "./browserErrors";
import { BrowserOptionsMenu } from "@/browser/BrowserOptionsMenu";
import {
  allowElectronBrowserTab,
  cacheElectronBrowserSnapshot,
  electronBrowserBridge,
  hasElectronWebviewBrowser,
} from "@/browser/electronBridge";
import {
  browserAddressToURL,
  browserDisplayURL,
  browserQueryStaleTimeMS,
  browserTargetURL,
  preferredBrowserTab,
  upsertBrowserTab,
} from "@/browser/helpers";
import type { BrowserCanvasPayload, BrowserNavigationAction, BrowserTabsData } from "@/browser/types";
import type { ElectronBrowserSurfaceTab } from "@/browser/useElectronRequiredBrowserTabs";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";

type PersistTabOptions = {
  refreshAfterPersist?: boolean;
};

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
  active = true,
  activeTab: activeTabProp,
  onOpenFind,
  sessionID,
  token,
}: {
  active?: boolean;
  activeTab?: ElectronBrowserSurfaceTab;
  onOpenFind: () => void;
  sessionID: string;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const payload = browserPayloadFromTab(activeTabProp);
  const [urlDraft, setURLDraft] = useState(browserDisplayURL(payload?.url));
  const [pendingSubmittedURL, setPendingSubmittedURL] = useState("");
  const lastOpenAttemptRef = useRef<BrowserOpenAttempt | null>(null);
  const embeddedBrowser = hasElectronWebviewBrowser();
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
    void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(sessionID) });
  };

  useLayoutEffect(() => {
    if (pendingSubmittedURL && !sameToolbarURL(pendingSubmittedURL, targetURL)) {
      return;
    }
    if (pendingSubmittedURL) {
      setPendingSubmittedURL("");
    }
    setURLDraft(browserDisplayURL(targetURL));
  }, [pendingSubmittedURL, targetURL]);

  const persistTab = async (tab: BrowserTab, options: PersistTabOptions = {}) => {
    if (tab.sessionID !== sessionID) {
      return;
    }
    const refreshAfterPersist = options.refreshAfterPersist ?? true;
    allowElectronBrowserTab(sessionID, tab.id);
    queryClient.setQueryData(queryKeys.browserTabs(sessionID), (current: BrowserTabsData | undefined) => ({
      tabs: upsertBrowserTab(current?.tabs || [], tab),
      processMode: tab.mode || current?.processMode,
    }));
    if (refreshAfterPersist) {
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
      setPendingSubmittedURL("");
      setURLDraft(browserDisplayURL(tab.url));
      void persistTab(tab, { refreshAfterPersist: !embeddedBrowser });
    },
    onError: (error) => {
      setPendingSubmittedURL("");
      setURLDraft(browserDisplayURL(activeTab?.url || targetURL));
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
  const stopLoadingMutation = useMutation({
    mutationFn: async () => {
      if (!activeTab) {
        throw new Error("browser tab missing");
      }
      const bridge = electronBrowserBridge();
      if (!bridge) {
        throw new Error("browser bridge unavailable");
      }
      return bridge.stop({ sessionID, tabID: activeTab.id });
    },
    onSuccess: (snapshot) => {
      cacheElectronBrowserSnapshot(queryClient, snapshot, sessionID);
    },
    onError: (error) => {
      toast.error(t("browser.navigationFailed"), { description: browserOpenErrorDescription(null, error) });
    },
  });
  const pageLoading = activeTabProp?.loading === true;
  const navigationDisabled = !activeTab || tabsQuery.isPending || navigationMutation.isPending;
  const backDisabled = navigationDisabled || !activeTab?.canGoBack;
  const forwardDisabled = navigationDisabled || !activeTab?.canGoForward;
  const pendingNavigationAction = navigationMutation.isPending ? navigationMutation.variables : undefined;
  const navButtonClass =
    "h-7 w-7 rounded-md text-muted-foreground [backface-visibility:hidden] [transform:translateZ(0)] hover:text-foreground active:translate-y-0";
  const navIconClass = "h-3.5 w-3.5 [backface-visibility:hidden] [transform:translateZ(0)]";

  const navigateToAddress = (draft: string) => {
    if (!draft.trim()) {
      return;
    }
    const url = browserAddressToURL(draft);
    setPendingSubmittedURL(url);
    setURLDraft(browserDisplayURL(url));
    openMutation.mutate(url);
  };

  return (
    <div
      className="canvas-window-no-drag flex min-w-0 flex-1 items-center gap-2"
      onDoubleClick={(event) => event.stopPropagation()}
      onPointerDown={(event) => event.stopPropagation()}
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
          aria-label={pageLoading ? t("browser.stopLoading") : t("browser.reload")}
          className={navButtonClass}
          disabled={pageLoading ? stopLoadingMutation.isPending : navigationDisabled}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => pageLoading ? stopLoadingMutation.mutate() : navigationMutation.mutate("reload")}
        >
          {pageLoading ? <X className={navIconClass} /> : <RefreshCw className={navIconClass} />}
        </Button>
      </div>
      <BrowserAddressField
        active={active}
        sessionID={sessionID}
        token={token}
        value={urlDraft}
        onChange={setURLDraft}
        onSubmit={navigateToAddress}
        pending={openMutation.isPending}
      />
      {activeTab && /^https?:\/\//.test(activeTab.url || "") ? <LibraryFavoriteButton token={token} sessionID={sessionID} target={{kind:"web", url:activeTab.url!, title:activeTab.title}} /> : null}
      <BrowserOptionsMenu
        active={active}
        activeTab={activeTab}
        sessionID={sessionID}
        onOpenFind={onOpenFind}
      />
    </div>
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
