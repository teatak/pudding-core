import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  adoptBrowserTab,
  createBrowserTab,
  getBrowserState,
  listBrowserTabs,
  releaseBrowserTab,
  syncBrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  allowElectronBrowserTab,
  cacheElectronBrowserSnapshot,
  clearElectronBrowserSessionGate,
  electronBrowserBridge,
} from "@/browser/electronBridge";
import {
  browserPayloadFromState,
  browserPayloadHasBlankTabIntent,
  browserPayloadHasRealState,
  browserQueryStaleTimeMS,
  preferredBrowserTab,
  upsertBrowserTab,
} from "@/browser/helpers";
import type { BrowserTabsData } from "@/browser/types";
import type { WorkspaceSurface } from "@/components/workspace/types";
import { useI18n } from "@/i18n";
import { consumeBrowserReveal, useBrowserRevealEpoch } from "@/state/browserRevealStore";

const SESSION_SURFACE_STORAGE_KEY = "pudding.workspace.sessionSurface.v2";
const LEGACY_WORKSPACE_SURFACE_STORAGE_KEY = "pudding.workspace.sessionSurface.v1";
const LEGACY_CANVAS_SURFACE_STORAGE_KEY = "pudding.canvas.sessionSurface.v1";
const SELECTED_BROWSER_TAB_STORAGE_KEY = "pudding.browser.selectedTab.v1";

type UseWorkspaceBrowserSurfaceArgs = {
  token: string;
  sessionID: string;
  enabled: boolean;
  hasTransientSurface?: boolean;
  itemsLength: number;
  itemsPending: boolean;
};

export function useWorkspaceBrowserSurface({ token, sessionID, enabled, hasTransientSurface = false, itemsLength, itemsPending }: UseWorkspaceBrowserSurfaceArgs) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const currentSessionIDRef = useRef("");
  const surfaceSessionRef = useRef("");
  const sessionSurfaceRef = useRef<Record<string, WorkspaceSurface>>(readSessionSurfaces());
  const selectedBrowserTabRef = useRef<Record<string, string>>(readSelectedBrowserTabs());
  const syncTimersRef = useRef<Record<string, number>>({});
  const [activeSurface, setActiveSurfaceState] = useState<WorkspaceSurface>("workspace");
  const [selectedBrowserTabs, setSelectedBrowserTabs] = useState<Record<string, string>>(selectedBrowserTabRef.current);
  const browserActive = activeSurface === "browser";
  const processModeFallback = electronBrowserBridge() ? "webview" : "headless";

  currentSessionIDRef.current = sessionID;
  selectedBrowserTabRef.current = selectedBrowserTabs;

  const rememberSessionSurface = (targetSessionID: string, surface: WorkspaceSurface) => {
    sessionSurfaceRef.current = { ...sessionSurfaceRef.current, [targetSessionID]: surface };
    writeSessionSurfaces(sessionSurfaceRef.current);
  };

  const rememberSelectedBrowserTab = (targetSessionID: string, tabID?: string) => {
    const next = { ...selectedBrowserTabRef.current };
    if (tabID) {
      next[targetSessionID] = tabID;
    } else {
      delete next[targetSessionID];
    }
    selectedBrowserTabRef.current = next;
    setSelectedBrowserTabs(next);
    writeSelectedBrowserTabs(next);
  };

  const setActiveSurface = (surface: WorkspaceSurface) => {
    if (sessionID) {
      rememberSessionSurface(sessionID, surface);
    }
    setActiveSurfaceState(surface);
  };

  const selectCanvasSurface = () => {
    setActiveSurface("canvas");
  };

  const selectWorkspaceSurface = () => {
    setActiveSurface("workspace");
  };

  const selectTerminalSurface = () => {
    setActiveSurface("terminal");
  };

  const selectProjectSurface = () => {
    setActiveSurface("project");
  };

  useEffect(() => {
    if (!sessionID || surfaceSessionRef.current === sessionID) {
      return;
    }
    surfaceSessionRef.current = sessionID;
    setActiveSurfaceState(sessionSurfaceRef.current[sessionID] || "workspace");
  }, [sessionID]);

  const browserRevealEpoch = useBrowserRevealEpoch(sessionID);
  const browserStateQuery = useQuery({
    enabled,
    queryKey: sessionID ? queryKeys.browserState(sessionID) : ["browser", "missing-session", "state"],
    queryFn: () => {
      if (!sessionID) {
        throw new Error("browser session id missing");
      }
      return getBrowserState(token, sessionID);
    },
    staleTime: browserQueryStaleTimeMS,
  });
  const browserState = browserStateQuery.data?.sessionID === sessionID ? browserStateQuery.data : undefined;
  const browserPayload = browserPayloadFromState(browserState);
  const browserTabsQuery = useQuery({
    enabled,
    queryKey: sessionID ? queryKeys.browserTabs(sessionID) : ["browser", "missing-session"],
    queryFn: () => {
      if (!sessionID) {
        throw new Error("browser session id missing");
      }
      return listBrowserTabs(token, sessionID);
    },
    staleTime: browserQueryStaleTimeMS,
  });
  const browserTabs = (browserTabsQuery.data?.tabs ?? [])
    .filter((tab) => tab.sessionID === sessionID)
    .sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt));
  const selectedBrowserTabID = selectedBrowserTabs[sessionID];
  const activeBrowserTab = browserTabs.find((tab) => tab.id === selectedBrowserTabID) || preferredBrowserTab(browserTabs, browserPayload);
  const hasBrowserState = Boolean(
    activeBrowserTab || browserPayloadHasRealState(browserPayload) || browserPayloadHasBlankTabIntent(browserPayload),
  );
  useEffect(() => {
    if (sessionID && activeBrowserTab && selectedBrowserTabRef.current[sessionID] !== activeBrowserTab.id) {
      rememberSelectedBrowserTab(sessionID, activeBrowserTab.id);
    }
  }, [activeBrowserTab?.id, sessionID]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge || !enabled || !sessionID) {
      return;
    }
    return bridge.onUpdated((snapshot) => {
      if (snapshot.sessionID !== sessionID) {
        return;
      }
      const current = queryClient.getQueryData<BrowserTabsData>(queryKeys.browserTabs(sessionID));
      const isNewTab = !current?.tabs.some((tab) => tab.id === snapshot.tabID);
      const tab = cacheElectronBrowserSnapshot(queryClient, snapshot, sessionID);
      if (!tab) {
        return;
      }
      if (isNewTab) {
        rememberSelectedBrowserTab(sessionID, tab.id);
        rememberSessionSurface(sessionID, "browser");
        setActiveSurfaceState("browser");
        void adoptBrowserTab(token, tab.sessionID, tab.id).catch(() => undefined);
      }
      const key = `${tab.sessionID}:${tab.id}`;
      window.clearTimeout(syncTimersRef.current[key]);
      syncTimersRef.current[key] = window.setTimeout(() => {
        delete syncTimersRef.current[key];
        void syncBrowserTab(token, tab.sessionID, tab.id, {
          targetID: tab.targetID,
          url: tab.url,
          title: tab.title,
          faviconURL: tab.faviconURL,
          canGoBack: tab.canGoBack,
          canGoForward: tab.canGoForward,
        }).catch(() => undefined);
      }, 250);
    });
  }, [enabled, queryClient, sessionID, token]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onAutomationStart || !enabled || !sessionID) {
      return;
    }
    return bridge.onAutomationStart((event) => {
      if (event.sessionID !== sessionID) {
        return;
      }
      rememberSelectedBrowserTab(sessionID, event.tabID);
      rememberSessionSurface(sessionID, "browser");
      setActiveSurfaceState("browser");
    });
  }, [enabled, sessionID]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge || !enabled || !sessionID) {
      return;
    }
    let disposed = false;
    void bridge
      .listTabs({ sessionID })
      .then((result) => {
        if (disposed || currentSessionIDRef.current !== sessionID) {
          return;
        }
        result.tabs
          .filter((snapshot) => snapshot.sessionID === sessionID)
          .forEach((snapshot) => {
            const current = queryClient.getQueryData<BrowserTabsData>(queryKeys.browserTabs(sessionID));
            const known = current?.tabs.some((tab) => tab.id === snapshot.tabID);
            const tab = cacheElectronBrowserSnapshot(queryClient, snapshot, sessionID);
            if (tab && !known) {
              void adoptBrowserTab(token, sessionID, tab.id).catch(() => undefined);
            }
          });
      })
      .catch(() => undefined);
    return () => {
      disposed = true;
    };
  }, [enabled, queryClient, sessionID, token]);

  useEffect(() => {
    return () => {
      Object.values(syncTimersRef.current).forEach((timer) => window.clearTimeout(timer));
      syncTimersRef.current = {};
    };
  }, []);

  const createBrowserTabMutation = useMutation({
    mutationFn: async (targetSessionID: string) => {
      if (!targetSessionID) {
        throw new Error("browser session id missing");
      }
      return { sessionID: targetSessionID, tab: await createBrowserTab(token, targetSessionID) };
    },
    onSuccess: ({ sessionID: targetSessionID, tab }) => {
      allowElectronBrowserTab(targetSessionID, tab.id);
      clearElectronBrowserSessionGate(targetSessionID);
      rememberSelectedBrowserTab(targetSessionID, tab.id);
      rememberSessionSurface(targetSessionID, "browser");
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), (current: BrowserTabsData | undefined) => ({
        tabs: upsertBrowserTab(current?.tabs || [], tab),
        processMode: tab.mode || current?.processMode || processModeFallback,
      }));
      queryClient.setQueryData(queryKeys.browserState(targetSessionID), {
        hasState: true,
        sessionID: targetSessionID,
        tabID: tab.id,
        url: tab.url,
        title: tab.title,
        faviconURL: tab.faviconURL,
        mode: tab.mode,
        processMode: tab.mode || processModeFallback,
        createdAt: tab.createdAt,
        updatedAt: tab.updatedAt,
      });
      if (currentSessionIDRef.current === targetSessionID) {
        setActiveSurfaceState("browser");
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: () => toast.error(t("browser.createFailed")),
  });

  useEffect(() => {
    if (
      !enabled ||
      !sessionID ||
      !browserActive ||
      createBrowserTabMutation.isPending ||
      browserStateQuery.isFetching ||
      browserTabsQuery.isFetching ||
      hasBrowserState
    ) {
      return;
    }
    const fallback = itemsLength > 0 || hasTransientSurface ? "canvas" : "workspace";
    rememberSessionSurface(sessionID, fallback);
    setActiveSurfaceState(fallback);
  }, [
    browserActive,
    browserStateQuery.isFetching,
    browserTabsQuery.isFetching,
    createBrowserTabMutation.isPending,
    enabled,
    hasBrowserState,
    hasTransientSurface,
    itemsLength,
    sessionID,
  ]);

  useEffect(() => {
    if (!enabled || !sessionID || itemsPending || itemsLength === 0 || activeSurface !== "workspace") {
      return;
    }
    rememberSessionSurface(sessionID, "canvas");
    setActiveSurfaceState("canvas");
  }, [activeSurface, enabled, itemsLength, itemsPending, sessionID]);

  useEffect(() => {
    if (
      !enabled ||
      !sessionID ||
      browserActive ||
      !hasBrowserState ||
      hasTransientSurface ||
      itemsLength > 0 ||
      sessionSurfaceRef.current[sessionID] === "canvas" ||
      sessionSurfaceRef.current[sessionID] === "project" ||
      sessionSurfaceRef.current[sessionID] === "terminal"
    ) {
      return;
    }
    rememberSessionSurface(sessionID, "browser");
    setActiveSurfaceState("browser");
  }, [browserActive, enabled, hasBrowserState, hasTransientSurface, itemsLength, sessionID]);

  const createNewBrowserTab = () => {
    if (!sessionID || createBrowserTabMutation.isPending) {
      return;
    }
    setActiveSurface("browser");
    createBrowserTabMutation.mutate(sessionID);
  };

  const selectBrowserTab = (tabID: string) => {
    if (!sessionID || !browserTabs.some((tab) => tab.id === tabID)) {
      return;
    }
    rememberSelectedBrowserTab(sessionID, tabID);
    setActiveSurface("browser");
  };

  useEffect(() => {
    if (!enabled || !sessionID || browserRevealEpoch <= 0) {
      return;
    }
    setActiveSurface("browser");
    consumeBrowserReveal(sessionID, browserRevealEpoch);
  }, [browserRevealEpoch, enabled, sessionID]);

  const closeBrowserTabMutation = useMutation({
    mutationFn: async ({ targetSessionID, tabID }: { targetSessionID: string; tabID: string }) => {
      await releaseBrowserTab(token, targetSessionID, tabID);
      return { sessionID: targetSessionID, tabID };
    },
    onMutate: async ({ targetSessionID, tabID }) => {
      const syncKey = `${targetSessionID}:${tabID}`;
      window.clearTimeout(syncTimersRef.current[syncKey]);
      delete syncTimersRef.current[syncKey];
      await Promise.all([
        queryClient.cancelQueries({ queryKey: queryKeys.browserState(targetSessionID) }),
        queryClient.cancelQueries({ queryKey: queryKeys.browserTabs(targetSessionID) }),
      ]);
      const previousTabs = queryClient.getQueryData<BrowserTabsData>(queryKeys.browserTabs(targetSessionID));
      const previousState = queryClient.getQueryData(queryKeys.browserState(targetSessionID));
      const previousSelectedTabID = selectedBrowserTabRef.current[targetSessionID];
      const previousSurface = sessionSurfaceRef.current[targetSessionID];
      const currentTabs = previousTabs?.tabs || [];
      const closingIndex = currentTabs.findIndex((tab) => tab.id === tabID);
      const remaining = currentTabs.filter((tab) => tab.id !== tabID);
      const replacement = remaining[Math.min(Math.max(closingIndex, 0), Math.max(remaining.length - 1, 0))];
      if (previousSelectedTabID === tabID || !remaining.some((tab) => tab.id === previousSelectedTabID)) {
        rememberSelectedBrowserTab(targetSessionID, replacement?.id);
      }
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), {
        tabs: remaining,
        processMode: previousTabs?.processMode || processModeFallback,
      });
      queryClient.setQueryData(
        queryKeys.browserState(targetSessionID),
        replacement
          ? {
              hasState: true,
              sessionID: targetSessionID,
              tabID: replacement.id,
              url: replacement.url,
              title: replacement.title,
              faviconURL: replacement.faviconURL,
              mode: replacement.mode,
              processMode: replacement.mode || previousTabs?.processMode || processModeFallback,
              createdAt: replacement.createdAt,
              updatedAt: replacement.updatedAt,
            }
          : { hasState: false, sessionID: targetSessionID, processMode: previousTabs?.processMode || processModeFallback },
      );
      if (remaining.length === 0) {
        const fallback = itemsLength > 0 || hasTransientSurface ? "canvas" : "workspace";
        if (sessionSurfaceRef.current[targetSessionID] === "browser") {
          rememberSessionSurface(targetSessionID, fallback);
        }
        if (currentSessionIDRef.current === targetSessionID) {
          setActiveSurfaceState((current) => (current === "browser" ? fallback : current));
        }
      }
      return { previousSelectedTabID, previousState, previousSurface, previousTabs };
    },
    onSuccess: ({ sessionID: targetSessionID }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: (_error, variables, context) => {
      if (context?.previousTabs) {
        queryClient.setQueryData(queryKeys.browserTabs(variables.targetSessionID), context.previousTabs);
      }
      if (context?.previousState) {
        queryClient.setQueryData(queryKeys.browserState(variables.targetSessionID), context.previousState);
      }
      rememberSelectedBrowserTab(variables.targetSessionID, context?.previousSelectedTabID);
      if (context?.previousSurface) {
        rememberSessionSurface(variables.targetSessionID, context.previousSurface);
        if (currentSessionIDRef.current === variables.targetSessionID) {
          setActiveSurfaceState(context.previousSurface);
        }
      }
      toast.error(t("browser.releaseFailed"));
    },
  });

  useEffect(() => {
    if (!enabled || !sessionID || !activeBrowserTab) {
      return;
    }
    clearElectronBrowserSessionGate(sessionID);
  }, [activeBrowserTab, enabled, sessionID]);

  return {
    activeBrowserTab,
    activeBrowserTabID: activeBrowserTab?.id,
    activeSurface,
    browserActive,
    browserTabs,
    closeBrowserTab: (tabID: string) => {
      if (sessionID) {
        closeBrowserTabMutation.mutate({ targetSessionID: sessionID, tabID });
      }
    },
    closingBrowserTabID: closeBrowserTabMutation.isPending ? closeBrowserTabMutation.variables?.tabID : undefined,
    createNewBrowserTab,
    creatingBrowserTab: createBrowserTabMutation.isPending,
    browserSurfacePending: createBrowserTabMutation.isPending || browserStateQuery.isFetching || browserTabsQuery.isFetching,
    browserSurfaceVisible: browserActive || hasBrowserState || createBrowserTabMutation.isPending,
    hasBrowserState,
    selectCanvasSurface,
    selectBrowserTab,
    selectProjectSurface,
    selectTerminalSurface,
    selectWorkspaceSurface,
  };
}

function readSessionSurfaces(): Record<string, WorkspaceSurface> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const currentRaw = window.localStorage.getItem(SESSION_SURFACE_STORAGE_KEY);
    const raw = currentRaw
      || window.localStorage.getItem(LEGACY_WORKSPACE_SURFACE_STORAGE_KEY)
      || window.localStorage.getItem(LEGACY_CANVAS_SURFACE_STORAGE_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const out: Record<string, WorkspaceSurface> = {};
    Object.entries(parsed).forEach(([sessionID, surface]) => {
      if (surface === "workspace" || surface === "canvas" || surface === "browser" || surface === "project" || surface === "terminal") {
        out[sessionID] = !currentRaw && surface === "canvas" ? "workspace" : surface;
      }
    });
    return out;
  } catch {
    return {};
  }
}

function writeSessionSurfaces(surfaces: Record<string, WorkspaceSurface>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(SESSION_SURFACE_STORAGE_KEY, JSON.stringify(surfaces));
    window.localStorage.removeItem(LEGACY_WORKSPACE_SURFACE_STORAGE_KEY);
    window.localStorage.removeItem(LEGACY_CANVAS_SURFACE_STORAGE_KEY);
  } catch {
    // Best-effort UI preference.
  }
}

function readSelectedBrowserTabs(): Record<string, string> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(SELECTED_BROWSER_TAB_STORAGE_KEY) || "{}") as Record<string, unknown>;
    const out: Record<string, string> = {};
    Object.entries(parsed).forEach(([sessionID, tabID]) => {
      if (typeof tabID === "string" && tabID.trim()) {
        out[sessionID] = tabID;
      }
    });
    return out;
  } catch {
    return {};
  }
}

function writeSelectedBrowserTabs(selected: Record<string, string>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(SELECTED_BROWSER_TAB_STORAGE_KEY, JSON.stringify(selected));
  } catch {
    // Best-effort UI preference.
  }
}
