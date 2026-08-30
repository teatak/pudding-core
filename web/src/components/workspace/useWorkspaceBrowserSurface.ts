import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  adoptBrowserTab,
  createBrowserTab,
  listBrowserTabs,
  releaseBrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  allowElectronBrowserTab,
  cacheElectronBrowserSnapshot,
  clearElectronBrowserSessionGate,
  electronBrowserBridge,
} from "@/browser/electronBridge";
import {
  browserQueryStaleTimeMS,
  preferredBrowserTab,
  upsertBrowserTab,
} from "@/browser/helpers";
import type { BrowserTabsData } from "@/browser/types";
import type { WorkspaceSurface } from "@/components/workspace/types";
import { useI18n } from "@/i18n";
import {
  consumeBrowserReveal,
  useBrowserReveal,
} from "@/state/browserRevealStore";

const SESSION_SURFACE_STORAGE_KEY = "pudding.workspace.sessionSurface.v2";
const LEGACY_WORKSPACE_SURFACE_STORAGE_KEY = "pudding.workspace.sessionSurface.v1";
const LEGACY_CANVAS_SURFACE_STORAGE_KEY = "pudding.canvas.sessionSurface.v1";
const SELECTED_BROWSER_TAB_STORAGE_KEY = "pudding.browser.selectedTab.v1";

type UseWorkspaceBrowserSurfaceArgs = {
  token: string;
  sessionID: string;
  enabled: boolean;
  hasProjectSurface?: boolean;
  hasTransientSurface?: boolean;
  itemsLength: number;
  itemsPending: boolean;
};

export function useWorkspaceBrowserSurface({
  token,
  sessionID,
  enabled,
  hasProjectSurface = false,
  hasTransientSurface = false,
  itemsLength,
  itemsPending,
}: UseWorkspaceBrowserSurfaceArgs) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [initialSessionSurfaces] = useState(readSessionSurfaces);
  const [initialSelectedBrowserTabs] = useState(readSelectedBrowserTabs);
  const currentSessionIDRef = useRef("");
  const sessionSurfaceRef = useRef<Record<string, WorkspaceSurface>>(initialSessionSurfaces);
  const surfaceSessionRef = useRef(sessionID);
  const selectedBrowserTabRef = useRef<Record<string, string>>(initialSelectedBrowserTabs);
  const readyTokenRef = useRef(token);
  const [activeSurface, setActiveSurfaceState] = useState<WorkspaceSurface>(
    () => sessionSurfaceRef.current[sessionID] || "workspace",
  );
  const [browserSelections, setBrowserSelections] = useState<Record<string, string>>({});
  const [selectedBrowserTabs, setSelectedBrowserTabs] = useState<Record<string, string>>(initialSelectedBrowserTabs);
  const [readyBrowserSessionIDs, setReadyBrowserSessionIDs] = useState<ReadonlySet<string>>(() => new Set());
  const browserActive = activeSurface === "browser";
  const processModeFallback = electronBrowserBridge() ? "webview" : "headless";

  currentSessionIDRef.current = sessionID;
  selectedBrowserTabRef.current = selectedBrowserTabs;

  const rememberSessionSurface = useCallback((targetSessionID: string, surface: WorkspaceSurface) => {
    sessionSurfaceRef.current = { ...sessionSurfaceRef.current, [targetSessionID]: surface };
    writeSessionSurfaces(sessionSurfaceRef.current);
  }, []);

  const rememberSelectedBrowserTab = useCallback((targetSessionID: string, tabID?: string) => {
    const next = { ...selectedBrowserTabRef.current };
    if (tabID) {
      next[targetSessionID] = tabID;
    } else {
      delete next[targetSessionID];
    }
    selectedBrowserTabRef.current = next;
    setSelectedBrowserTabs(next);
    writeSelectedBrowserTabs(next);
  }, []);

  const setActiveSurface = useCallback((surface: WorkspaceSurface) => {
    if (sessionID) {
      rememberSessionSurface(sessionID, surface);
    }
    setActiveSurfaceState(surface);
  }, [rememberSessionSurface, sessionID]);

  const selectCanvasSurface = useCallback(() => {
    setActiveSurface("canvas");
  }, [setActiveSurface]);

  const selectWorkspaceSurface = useCallback(() => {
    setActiveSurface("workspace");
  }, [setActiveSurface]);

  const selectProjectSurface = useCallback(() => {
    setActiveSurface("project");
  }, [setActiveSurface]);

  useEffect(() => {
    if (!sessionID || surfaceSessionRef.current === sessionID) {
      return;
    }
    surfaceSessionRef.current = sessionID;
    setActiveSurfaceState(sessionSurfaceRef.current[sessionID] || "workspace");
  }, [sessionID]);

  const browserReveal = useBrowserReveal(sessionID);
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
  const browserTabs = useMemo(
    () => (browserTabsQuery.data?.tabs ?? [])
      .filter((tab) => tab.sessionID === sessionID)
      .sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt)),
    [browserTabsQuery.data?.tabs, sessionID],
  );
  const browserTabsReady = Boolean(
    readyTokenRef.current === token
    && sessionID
    && readyBrowserSessionIDs.has(sessionID),
  );
  useEffect(() => {
    if (readyTokenRef.current === token) {
      return;
    }
    readyTokenRef.current = token;
    setReadyBrowserSessionIDs(new Set());
  }, [token]);
  useEffect(() => {
    if (!enabled || !sessionID || !browserTabsQuery.isSuccess || browserTabsQuery.isFetching) {
      return;
    }
    setReadyBrowserSessionIDs((current) => {
      if (current.has(sessionID)) {
        return current;
      }
      const next = new Set(current);
      next.add(sessionID);
      return next;
    });
  }, [browserTabsQuery.isFetching, browserTabsQuery.isSuccess, enabled, sessionID]);
  const selectedBrowserTabID = selectedBrowserTabs[sessionID];
  const activeBrowserTab = browserTabs.find((tab) => tab.id === selectedBrowserTabID) || preferredBrowserTab(browserTabs, null);
  const activeBrowserSelection = activeBrowserTab
    ? browserSelections[`${sessionID}:${activeBrowserTab.id}`] || ""
    : "";
  const hasBrowserState = Boolean(activeBrowserTab);
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
      const queryKey = queryKeys.browserTabs(sessionID);
      if (
        !browserTabsReady
        && queryClient.getQueryState(queryKey)?.fetchStatus === "fetching"
      ) {
        return;
      }
      const current = queryClient.getQueryData<BrowserTabsData>(queryKey);
      const previousTab = current?.tabs.find((tab) => tab.id === snapshot.tabID);
      const isNewTab = !current?.tabs.some((tab) => tab.id === snapshot.tabID);
      if (snapshot.status === "pending" || (previousTab && previousTab.url !== snapshot.url)) {
        const key = `${snapshot.sessionID}:${snapshot.tabID}`;
        setBrowserSelections((selections) => {
          if (!(key in selections)) {
            return selections;
          }
          const next = { ...selections };
          delete next[key];
          return next;
        });
      }
      const tab = cacheElectronBrowserSnapshot(queryClient, snapshot, sessionID);
      if (!tab) {
        return;
      }
      if (isNewTab) {
        if (snapshot.activate !== false) {
          rememberSelectedBrowserTab(sessionID, tab.id);
          rememberSessionSurface(sessionID, "browser");
          setActiveSurfaceState("browser");
        }
        void adoptBrowserTab(token, tab.sessionID, tab.id).catch(() => undefined);
      }
    });
  }, [browserTabsReady, enabled, queryClient, sessionID, token]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onSelectionChanged || !enabled || !sessionID) {
      return;
    }
    return bridge.onSelectionChanged((event) => {
      if (event.sessionID !== sessionID || !event.tabID) {
        return;
      }
      const key = `${event.sessionID}:${event.tabID}`;
      setBrowserSelections((current) => {
        if (event.selectionText) {
          return current[key] === event.selectionText
            ? current
            : { ...current, [key]: event.selectionText };
        }
        if (!(key in current)) {
          return current;
        }
        const next = { ...current };
        delete next[key];
        return next;
      });
    });
  }, [enabled, sessionID]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onAutomationStart || !enabled || !sessionID) {
      return;
    }
    return bridge.onAutomationStart((event) => {
      if (event.sessionID !== sessionID || event.action === "screenshot") {
        return;
      }
      rememberSelectedBrowserTab(sessionID, event.tabID);
      rememberSessionSurface(sessionID, "browser");
      setActiveSurfaceState("browser");
    });
  }, [enabled, sessionID]);

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
      if (currentSessionIDRef.current === targetSessionID) {
        setActiveSurfaceState("browser");
      }
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
    browserTabsQuery.isFetching,
    createBrowserTabMutation.isPending,
    enabled,
    hasBrowserState,
    hasTransientSurface,
    itemsLength,
    sessionID,
  ]);

  useEffect(() => {
    if (
      !enabled ||
      !sessionID ||
      itemsPending ||
      itemsLength === 0 ||
      activeSurface !== "workspace" ||
      sessionSurfaceRef.current[sessionID]
    ) {
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
      sessionSurfaceRef.current[sessionID] === "project"
    ) {
      return;
    }
    rememberSessionSurface(sessionID, "browser");
    setActiveSurfaceState("browser");
  }, [browserActive, enabled, hasBrowserState, hasTransientSurface, itemsLength, sessionID]);

  const createNewBrowserTab = useCallback(() => {
    if (!sessionID || createBrowserTabMutation.isPending) {
      return;
    }
    createBrowserTabMutation.mutate(sessionID);
  }, [createBrowserTabMutation.isPending, createBrowserTabMutation.mutate, sessionID]);

  const selectBrowserTab = useCallback((tabID: string) => {
    if (!sessionID || !browserTabs.some((tab) => tab.id === tabID)) {
      return;
    }
    rememberSelectedBrowserTab(sessionID, tabID);
    setActiveSurface("browser");
  }, [browserTabs, rememberSelectedBrowserTab, sessionID, setActiveSurface]);

  useEffect(() => {
    if (!enabled || !sessionID || !browserReveal) {
      return;
    }
    if (browserReveal.tabID) {
      if (!browserTabs.some((tab) => tab.id === browserReveal.tabID)) {
        return;
      }
      rememberSelectedBrowserTab(sessionID, browserReveal.tabID);
    }
    setActiveSurface("browser");
    consumeBrowserReveal(sessionID, browserReveal.epoch);
  }, [browserReveal, browserTabs, enabled, rememberSelectedBrowserTab, sessionID, setActiveSurface]);

  const closeBrowserTabMutation = useMutation({
    mutationFn: async ({ targetSessionID, tabID }: { targetSessionID: string; tabID: string }) => {
      await releaseBrowserTab(token, targetSessionID, tabID);
      return { sessionID: targetSessionID, tabID };
    },
    onMutate: async ({ targetSessionID, tabID }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
      const previousTabs = queryClient.getQueryData<BrowserTabsData>(queryKeys.browserTabs(targetSessionID));
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
      if (remaining.length === 0) {
        const fallback = itemsLength > 0 || hasTransientSurface
          ? "canvas"
          : hasProjectSurface
            ? "project"
            : "workspace";
        if (sessionSurfaceRef.current[targetSessionID] === "browser") {
          rememberSessionSurface(targetSessionID, fallback);
        }
        if (currentSessionIDRef.current === targetSessionID) {
          setActiveSurfaceState((current) => (current === "browser" ? fallback : current));
        }
      }
      return { previousSelectedTabID, previousSurface, previousTabs };
    },
    onSuccess: ({ sessionID: targetSessionID }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: (_error, variables, context) => {
      if (context?.previousTabs) {
        queryClient.setQueryData(queryKeys.browserTabs(variables.targetSessionID), context.previousTabs);
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

  const closeBrowserTab = useCallback((tabID: string) => {
    if (sessionID) {
      closeBrowserTabMutation.mutate({ targetSessionID: sessionID, tabID });
    }
  }, [closeBrowserTabMutation.mutate, sessionID]);

  return {
    activeBrowserTab,
    activeBrowserTabID: activeBrowserTab?.id,
    activeBrowserSelection,
    activeSurface,
    browserActive,
    browserTabsReady,
    browserTabs,
    closeBrowserTab,
    closingBrowserTabID: closeBrowserTabMutation.isPending ? closeBrowserTabMutation.variables?.tabID : undefined,
    createNewBrowserTab,
    creatingBrowserTab: createBrowserTabMutation.isPending,
    browserSurfacePending: createBrowserTabMutation.isPending || (!browserTabsReady && browserTabsQuery.isFetching),
    browserSurfaceVisible: browserActive || hasBrowserState || createBrowserTabMutation.isPending,
    hasBrowserState,
    selectCanvasSurface,
    selectBrowserTab,
    selectProjectSurface,
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
      if (surface === "workspace" || surface === "canvas" || surface === "browser" || surface === "project") {
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
