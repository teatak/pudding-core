import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, CornerDownLeft, Globe2, History, RefreshCw, Trash2 } from "lucide-react";
import { useDeferredValue, useEffect, useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  APIError,
  backBrowserTab,
  clearBrowserHistory,
  deleteBrowserHistoryEntry,
  forwardBrowserTab,
  listBrowserHistory,
  listBrowserTabs,
  openBrowserTab,
  openBrowserURL,
  reloadBrowserTab,
  syncBrowserTab,
  type BrowserHistoryEntry,
  type BrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { allowElectronBrowserTab, hasElectronWebviewBrowser } from "@/browser/electronBridge";
import {
  browserAddressToURL,
  browserCompactURL,
  browserDisplayURL,
  browserQueryStaleTimeMS,
  browserTabFaviconURL,
  browserTargetURL,
  browserTabTitle,
  preferredBrowserTab,
  uniqueBrowserHistoryBySite,
  upsertBrowserTab,
} from "@/browser/helpers";
import type { BrowserCanvasPayload, BrowserNavigationAction, BrowserTabsData } from "@/browser/types";
import { Spinner } from "@/components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Command, CommandGroup, CommandItem, CommandList } from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

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
  active = true,
  activeTab: activeTabProp,
  sessionID,
  token,
}: {
  active?: boolean;
  activeTab?: BrowserTab;
  sessionID: string;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const payload = browserPayloadFromTab(activeTabProp);
  const [urlDraft, setURLDraft] = useState(browserDisplayURL(payload?.url));
  const [pendingSubmittedURL, setPendingSubmittedURL] = useState("");
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historySearch, setHistorySearch] = useState("");
  const [selectedHistoryIndex, setSelectedHistoryIndex] = useState(-1);
  const [clearHistoryOpen, setClearHistoryOpen] = useState(false);
  const deferredHistorySearch = useDeferredValue(historySearch.trim());
  const addressInputRef = useRef<HTMLInputElement>(null);
  const addressBlurTimerRef = useRef<number | undefined>(undefined);
  const lastOpenAttemptRef = useRef<BrowserOpenAttempt | null>(null);
  const embeddedBrowser = hasElectronWebviewBrowser();
  const processModeFallback = embeddedBrowser ? "webview" : "headless";
  const tabsQuery = useQuery({
    enabled: Boolean(token && sessionID),
    queryKey: queryKeys.browserTabs(sessionID),
    queryFn: () => listBrowserTabs(token, sessionID),
    staleTime: browserQueryStaleTimeMS,
  });
  const historyQuery = useQuery({
    enabled: Boolean(active && historyOpen && token && sessionID),
    queryKey: queryKeys.browserHistory(deferredHistorySearch),
    queryFn: () => listBrowserHistory(token, sessionID, deferredHistorySearch, deferredHistorySearch ? 10 : 64),
    staleTime: 0,
  });
  const historyCandidates = historyQuery.data?.history || [];
  const historyEntries = deferredHistorySearch ? historyCandidates.slice(0, 10) : uniqueBrowserHistoryBySite(historyCandidates, 10);
  const tabs = (tabsQuery.data?.tabs || []).filter((tab) => tab.sessionID === sessionID);
  const activeTab = activeTabProp || preferredBrowserTab(tabs, payload);
  const targetURL = browserTargetURL(activeTab, payload, payload?.updatedAt);
  const refreshBrowserQueries = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(sessionID) });
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

  useEffect(() => {
    setSelectedHistoryIndex((current) => Math.min(current, historyEntries.length - 1));
  }, [historyEntries.length]);

  useEffect(() => () => window.clearTimeout(addressBlurTimerRef.current), []);

  useEffect(() => {
    if (!active) {
      setHistoryOpen(false);
      setClearHistoryOpen(false);
      return;
    }
    const focusAddressInput = () => {
      const input = addressInputRef.current;
      input?.focus();
      input?.select();
      setHistorySearch("");
      setSelectedHistoryIndex(-1);
      setHistoryOpen(true);
    };
    const focusAddressBar = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== "l" || (!event.metaKey && !event.ctrlKey) || event.altKey) {
        return;
      }
      event.preventDefault();
      focusAddressInput();
    };
    window.addEventListener("keydown", focusAddressBar);
    return () => window.removeEventListener("keydown", focusAddressBar);
  }, [active]);

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
      setPendingSubmittedURL("");
      setURLDraft(browserDisplayURL(tab.url));
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() });
      void persistTab(tab, { refreshAfterPersist: !embeddedBrowser, syncBrowserTab: embeddedBrowser });
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
  const deleteHistoryMutation = useMutation({
    mutationFn: (historyID: string) => deleteBrowserHistoryEntry(token, sessionID, historyID),
    onSuccess: () => {
      setSelectedHistoryIndex(-1);
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() });
    },
    onError: (error) => {
      toast.error(t("browser.historyDeleteFailed"), { description: browserOpenErrorDescription(null, error) });
    },
  });
  const clearHistoryMutation = useMutation({
    mutationFn: () => clearBrowserHistory(token, sessionID),
    onSuccess: () => {
      setClearHistoryOpen(false);
      setHistoryOpen(false);
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() });
    },
    onError: (error) => {
      toast.error(t("browser.historyClearFailed"), { description: browserOpenErrorDescription(null, error) });
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

  const navigateToAddress = (draft: string) => {
    if (!draft.trim()) {
      return;
    }
    const url = browserAddressToURL(draft);
    setHistoryOpen(false);
    setPendingSubmittedURL(url);
    setURLDraft(browserDisplayURL(url));
    openMutation.mutate(url);
  };

  const selectHistoryEntry = (entry: BrowserHistoryEntry) => {
    navigateToAddress(entry.url);
  };

  return (
    <form
      className="canvas-window-no-drag flex min-w-0 flex-1 items-center gap-2"
      onDoubleClick={(event) => event.stopPropagation()}
      onPointerDown={(event) => event.stopPropagation()}
      onSubmit={(event) => {
        event.preventDefault();
        navigateToAddress(urlDraft);
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
      <Popover open={historyOpen && historyEntries.length > 0} onOpenChange={setHistoryOpen}>
        <PopoverAnchor asChild>
          <div className="group relative flex h-8 min-w-0 flex-1 items-center rounded-md border border-transparent bg-transparent transition-[background-color,box-shadow] hover:bg-background/45 focus-within:bg-background/45 focus-within:shadow-[0_0_0_1px_hsl(var(--border)/0.7),0_0_0_3px_hsl(var(--ring)/0.12)]">
            <Input
              ref={addressInputRef}
              aria-autocomplete="list"
              aria-expanded={historyOpen}
              className="h-7 min-w-0 flex-1 border-0 bg-transparent pr-8 shadow-none focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent"
              placeholder={t("browser.urlPlaceholder")}
              value={urlDraft}
              onBlur={() => {
                window.clearTimeout(addressBlurTimerRef.current);
                addressBlurTimerRef.current = window.setTimeout(() => {
                  const focused = document.activeElement;
                  if (focused === addressInputRef.current || (focused instanceof Element && focused.closest('[data-slot="popover-content"]'))) {
                    return;
                  }
                  setHistoryOpen(false);
                }, 0);
              }}
              onChange={(event) => {
                setURLDraft(event.target.value);
                setHistorySearch(event.target.value);
                setSelectedHistoryIndex(-1);
                setHistoryOpen(true);
              }}
              onFocus={(event) => {
                window.clearTimeout(addressBlurTimerRef.current);
                const input = event.currentTarget;
                setHistorySearch("");
                setSelectedHistoryIndex(-1);
                setHistoryOpen(true);
                window.setTimeout(() => {
                  if (document.activeElement === input) {
                    input.select();
                  }
                }, 0);
              }}
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  setHistoryOpen(false);
                  return;
                }
                if (event.key === "ArrowDown") {
                  event.preventDefault();
                  setHistoryOpen(true);
                  setSelectedHistoryIndex((current) => Math.min(current + 1, historyEntries.length - 1));
                  return;
                }
                if (event.key === "ArrowUp") {
                  event.preventDefault();
                  setHistoryOpen(true);
                  setSelectedHistoryIndex((current) => (current <= 0 ? historyEntries.length - 1 : current - 1));
                  return;
                }
                if (event.key === "Enter" && historyOpen && selectedHistoryIndex >= 0) {
                  const entry = historyEntries[selectedHistoryIndex];
                  if (entry) {
                    event.preventDefault();
                    selectHistoryEntry(entry);
                  }
                }
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
        </PopoverAnchor>
        <PopoverContent
          align="start"
          avoidCollisions={false}
          className="gap-0 rounded-xl border border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] p-1 shadow-[0_12px_32px_rgba(0,0,0,0.18)] ring-0"
          sideOffset={6}
          style={{ width: "var(--radix-popover-trigger-width)" }}
          onCloseAutoFocus={(event) => event.preventDefault()}
          onInteractOutside={(event) => {
            if (event.target === addressInputRef.current) {
              event.preventDefault();
            }
          }}
          onOpenAutoFocus={(event) => event.preventDefault()}
        >
          <Command
            className="rounded-md p-0"
            shouldFilter={false}
            value={selectedHistoryIndex >= 0 ? historyEntries[selectedHistoryIndex]?.id || "__none__" : "__none__"}
            onValueChange={(value) => setSelectedHistoryIndex(historyEntries.findIndex((entry) => entry.id === value))}
          >
            <CommandList className="max-h-80">
              {historyEntries.length > 0 ? (
                <CommandGroup heading={deferredHistorySearch ? t("browser.historyResults") : undefined}>
                  {historyEntries.map((entry, index) => (
                    <CommandItem
                      key={entry.id}
                      className={cn(
                        "min-w-0 py-1.5 pr-1 [&>svg:last-child]:hidden",
                        selectedHistoryIndex === index && "bg-muted text-foreground",
                      )}
                      value={entry.id}
                      onMouseEnter={() => setSelectedHistoryIndex(index)}
                      onSelect={() => selectHistoryEntry(entry)}
                    >
                      <div className="flex size-5 shrink-0 items-center justify-center overflow-hidden rounded-sm text-muted-foreground">
                        <HistoryFavicon url={entry.faviconURL} />
                      </div>
                      <div className="flex min-w-0 flex-1 items-baseline gap-1.5 overflow-hidden text-sm">
                        <span className="min-w-0 truncate">{entry.title || historyURLLabel(entry.url)}</span>
                        <span aria-hidden="true" className="shrink-0 text-muted-foreground/45">·</span>
                        <span className="max-w-[45%] shrink-0 truncate text-xs text-muted-foreground">{browserCompactURL(entry.url)}</span>
                      </div>
                      <Button
                        aria-label={t("browser.historyDelete")}
                        className="size-7 shrink-0 text-muted-foreground opacity-0 group-data-selected/command-item:opacity-100 hover:text-destructive group-hover/command-item:opacity-100"
                        disabled={deleteHistoryMutation.isPending && deleteHistoryMutation.variables === entry.id}
                        size="icon-sm"
                        type="button"
                        variant="ghost"
                        onClick={(event) => {
                          event.stopPropagation();
                          deleteHistoryMutation.mutate(entry.id);
                        }}
                        onPointerDown={(event) => {
                          event.preventDefault();
                          event.stopPropagation();
                        }}
                      >
                        {deleteHistoryMutation.isPending && deleteHistoryMutation.variables === entry.id ? (
                          <Spinner className="size-3.5" />
                        ) : (
                          <Trash2 className="size-3.5" />
                        )}
                      </Button>
                    </CommandItem>
                  ))}
                </CommandGroup>
              ) : null}
            </CommandList>
          </Command>
          {historyEntries.length > 0 ? (
            <div className="mt-1 flex items-center justify-between border-t px-1 pt-1">
              <div className="flex items-center gap-1.5 px-2 text-xs text-muted-foreground">
                <History className="size-3.5" />
                {t("browser.historyGlobal")}
              </div>
              <Button
                className="h-7 px-2 text-xs text-muted-foreground hover:text-destructive"
                size="sm"
                type="button"
                variant="ghost"
                onClick={() => {
                  setHistoryOpen(false);
                  setClearHistoryOpen(true);
                }}
              >
                {t("browser.historyClear")}
              </Button>
            </div>
          ) : null}
        </PopoverContent>
      </Popover>
      <AlertDialog open={clearHistoryOpen} onOpenChange={setClearHistoryOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("browser.historyClearTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("browser.historyClearDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={clearHistoryMutation.isPending}
              variant="destructive"
              onClick={() => clearHistoryMutation.mutate()}
            >
              {t("browser.historyClear")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  );
}

function historyURLLabel(rawURL: string) {
  try {
    return new URL(rawURL).hostname || rawURL;
  } catch {
    return rawURL;
  }
}

function HistoryFavicon({ url }: { url?: string }) {
  const [failed, setFailed] = useState(false);

  useEffect(() => setFailed(false), [url]);

  if (!url || failed) {
    return <Globe2 className="size-4" />;
  }
  return <img alt="" className="size-4 object-contain" draggable={false} src={url} onError={() => setFailed(true)} />;
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
