import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  closeBrowserSession,
  createBrowserTab,
  getBrowserState,
  listBrowserTabs,
  syncBrowserTab,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  allowElectronBrowserTab,
  cacheElectronBrowserSnapshot,
  clearElectronBrowserSessionGate,
  electronBrowserBridge,
  markElectronBrowserSessionClosed,
} from "@/browser/electronBridge";
import {
  browserPayloadFromState,
  browserPayloadHasBlankTabIntent,
  browserPayloadHasRealState,
  browserQueryStaleTimeMS,
  browserTabFaviconURL,
  browserTabTitle,
  faviconURLForPage,
  preferredBrowserTab,
  upsertBrowserTab,
} from "@/browser/helpers";
import type { BrowserTabsData, CanvasSurface } from "@/browser/types";
import { useI18n } from "@/i18n";
import { setCanvasOpen } from "@/state/canvasStore";
import { consumeBrowserReveal, useBrowserRevealEpoch } from "@/state/browserRevealStore";

const SESSION_SURFACE_STORAGE_KEY = "pudding.canvas.sessionSurface.v1";

type UseCanvasBrowserSurfaceArgs = {
  token: string;
  sessionID: string;
  enabled: boolean;
  itemsLength: number;
};

export function useCanvasBrowserSurface({ token, sessionID, enabled, itemsLength }: UseCanvasBrowserSurfaceArgs) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const currentSessionIDRef = useRef("");
  const surfaceSessionRef = useRef("");
  const sessionSurfaceRef = useRef<Record<string, CanvasSurface>>(readSessionSurfaces());
  const closeEpochRef = useRef<Record<string, number>>({});
  const closingSessionsRef = useRef<Record<string, boolean>>({});
  const syncTimersRef = useRef<Record<string, number>>({});
  const [browserActive, setBrowserActive] = useState(false);
  const [closingSessions, setClosingSessions] = useState<Record<string, boolean>>({});

  closingSessionsRef.current = closingSessions;
  currentSessionIDRef.current = sessionID;

  const rememberSessionSurface = (targetSessionID: string, surface: CanvasSurface) => {
    sessionSurfaceRef.current = { ...sessionSurfaceRef.current, [targetSessionID]: surface };
    writeSessionSurfaces(sessionSurfaceRef.current);
  };

  const clearSyncTimers = (targetSessionID: string) => {
    const prefix = `${targetSessionID}:`;
    Object.entries(syncTimersRef.current).forEach(([key, timer]) => {
      if (key.startsWith(prefix)) {
        window.clearTimeout(timer);
        delete syncTimersRef.current[key];
      }
    });
  };

  const setActiveSurface = (surface: CanvasSurface) => {
    if (sessionID) {
      rememberSessionSurface(sessionID, surface);
    }
    setBrowserActive(surface === "browser");
  };

  const selectCanvasSurface = () => {
    setActiveSurface("canvas");
  };

  useEffect(() => {
    if (!sessionID || surfaceSessionRef.current === sessionID) {
      return;
    }
    surfaceSessionRef.current = sessionID;
    setBrowserActive(sessionSurfaceRef.current[sessionID] === "browser");
  }, [sessionID]);

  const browserRevealEpoch = useBrowserRevealEpoch(sessionID);
  const browserClosing = Boolean(sessionID && closingSessions[sessionID]);
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
  const browserPayload = browserClosing ? null : browserPayloadFromState(browserState);
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
  const browserTabs = browserClosing ? [] : (browserTabsQuery.data?.tabs ?? []).filter((tab) => tab.sessionID === sessionID);
  const activeBrowserTab = preferredBrowserTab(browserTabs, browserPayload);
  const hasBrowserState = Boolean(
    activeBrowserTab || browserPayloadHasRealState(browserPayload) || browserPayloadHasBlankTabIntent(browserPayload),
  );
  const hasOpenBrowserWindow = browserActive || hasBrowserState;
  const browserTabTitleText = activeBrowserTab
    ? browserTabTitle(activeBrowserTab, browserPayload?.title || t("browser.newTab"), t("browser.newTab"))
    : hasBrowserState
      ? browserPayload?.title || t("browser.newTab")
      : "";
  const browserTabFaviconURLText = activeBrowserTab
    ? browserTabFaviconURL(activeBrowserTab)
    : hasBrowserState
      ? browserPayload?.faviconURL || (browserPayload?.url ? faviconURLForPage(browserPayload.url) : "")
      : "";

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge || !enabled || !sessionID) {
      return;
    }
    return bridge.onUpdated((snapshot) => {
      if (snapshot.sessionID !== sessionID) {
        return;
      }
      if (closingSessionsRef.current[snapshot.sessionID]) {
        return;
      }
      const tab = cacheElectronBrowserSnapshot(queryClient, snapshot, sessionID);
      if (!tab) {
        return;
      }
      const key = `${tab.sessionID}:${tab.id}`;
      window.clearTimeout(syncTimersRef.current[key]);
      syncTimersRef.current[key] = window.setTimeout(() => {
        delete syncTimersRef.current[key];
        if (closingSessionsRef.current[tab.sessionID]) {
          return;
        }
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
    if (!bridge || !enabled || !sessionID || browserClosing) {
      return;
    }
    let disposed = false;
    void bridge
      .listTabs({ sessionID })
      .then((result) => {
        if (disposed || currentSessionIDRef.current !== sessionID || closingSessionsRef.current[sessionID]) {
          return;
        }
        result.tabs
          .filter((snapshot) => snapshot.sessionID === sessionID)
          .forEach((snapshot) => cacheElectronBrowserSnapshot(queryClient, snapshot, sessionID));
      })
      .catch(() => undefined);
    return () => {
      disposed = true;
    };
  }, [browserClosing, enabled, queryClient, sessionID]);

  useEffect(() => {
    return () => {
      Object.values(syncTimersRef.current).forEach((timer) => window.clearTimeout(timer));
      syncTimersRef.current = {};
    };
  }, []);

  const createBrowserTabMutation = useMutation({
    mutationFn: async ({ targetSessionID, fresh }: { targetSessionID: string; fresh?: boolean }) => {
      if (!targetSessionID) {
        throw new Error("browser session id missing");
      }
      let startCloseEpoch = closeEpochRef.current[targetSessionID] || 0;
      if (fresh) {
        startCloseEpoch += 1;
        closeEpochRef.current = {
          ...closeEpochRef.current,
          [targetSessionID]: startCloseEpoch,
        };
      }
      const tab = await createBrowserTab(token, targetSessionID);
      return { fresh: Boolean(fresh), sessionID: targetSessionID, startCloseEpoch, tab };
    },
    onSuccess: ({ fresh, sessionID: targetSessionID, startCloseEpoch, tab }) => {
      const closedAfterRequest = (closeEpochRef.current[targetSessionID] || 0) > startCloseEpoch;
      if (closedAfterRequest || (closingSessionsRef.current[targetSessionID] && !fresh)) {
        queryClient.setQueryData(queryKeys.browserState(targetSessionID), { hasState: false, sessionID: targetSessionID });
        queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), { tabs: [], processMode: "headless" });
        return;
      }
      const title = browserTabTitle(tab, t("browser.newTab"), t("browser.newTab"));
      const faviconURL = browserTabFaviconURL(tab);
      allowElectronBrowserTab(targetSessionID, tab.id);
      clearElectronBrowserSessionGate(targetSessionID);
      setClosingSessions((prev) => withoutKey(prev, targetSessionID));
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), (current: BrowserTabsData | undefined) => ({
        tabs: upsertBrowserTab(current?.tabs || [], tab),
        processMode: tab.mode || current?.processMode || "headless",
      }));
      queryClient.setQueryData(queryKeys.browserState(targetSessionID), {
        hasState: true,
        sessionID: targetSessionID,
        tabID: tab.id,
        url: tab.url,
        title,
        faviconURL,
        mode: tab.mode,
        processMode: tab.mode || "headless",
        createdAt: tab.createdAt,
        updatedAt: tab.updatedAt,
      });
      if (currentSessionIDRef.current === targetSessionID && sessionSurfaceRef.current[targetSessionID] === "browser") {
        setBrowserActive(true);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: () => {
      toast.error(t("browser.createFailed"));
    },
  });

  useEffect(() => {
    if (
      !enabled ||
      !sessionID ||
      !browserActive ||
      browserClosing ||
      createBrowserTabMutation.isPending ||
      browserStateQuery.isFetching ||
      browserTabsQuery.isFetching ||
      hasBrowserState
    ) {
      return;
    }
    rememberSessionSurface(sessionID, "canvas");
    setBrowserActive(false);
  }, [
    browserActive,
    browserClosing,
    browserStateQuery.isFetching,
    browserTabsQuery.isFetching,
    createBrowserTabMutation.isPending,
    enabled,
    hasBrowserState,
    sessionID,
  ]);

  useEffect(() => {
    if (!enabled || !sessionID || browserActive || browserClosing || !hasBrowserState || itemsLength > 0) {
      return;
    }
    rememberSessionSurface(sessionID, "browser");
    setBrowserActive(true);
  }, [browserActive, browserClosing, enabled, hasBrowserState, itemsLength, sessionID]);

  const activateBrowserSurface = () => {
    if (!sessionID) {
      return;
    }
    const fresh = Boolean(closingSessionsRef.current[sessionID]);
    setActiveSurface("browser");
    if (!activeBrowserTab && !hasBrowserState && !createBrowserTabMutation.isPending) {
      createBrowserTabMutation.mutate({ targetSessionID: sessionID, fresh });
    }
  };

  useEffect(() => {
    if (!enabled || !sessionID || browserClosing || browserRevealEpoch <= 0) {
      return;
    }
    setActiveSurface("browser");
    consumeBrowserReveal(sessionID, browserRevealEpoch);
  }, [browserClosing, browserRevealEpoch, enabled, sessionID]);

  const browserCloseMutation = useMutation({
    mutationFn: async (targetSessionID: string) => {
      if (!targetSessionID) {
        throw new Error("browser session id missing");
      }
      await closeBrowserSession(token, targetSessionID);
      return { sessionID: targetSessionID };
    },
    onMutate: async (targetSessionID: string) => {
      if (!targetSessionID) {
        return;
      }
      const closeEpoch = (closeEpochRef.current[targetSessionID] || 0) + 1;
      closeEpochRef.current = {
        ...closeEpochRef.current,
        [targetSessionID]: closeEpoch,
      };
      clearSyncTimers(targetSessionID);
      await Promise.all([
        queryClient.cancelQueries({ queryKey: queryKeys.browserState(targetSessionID) }),
        queryClient.cancelQueries({ queryKey: queryKeys.browserTabs(targetSessionID) }),
      ]);
      markElectronBrowserSessionClosed(targetSessionID);
      setClosingSessions((prev) => ({ ...prev, [targetSessionID]: true }));
      rememberSessionSurface(targetSessionID, "canvas");
      if (currentSessionIDRef.current === targetSessionID) {
        setBrowserActive(false);
      }
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), { tabs: [], processMode: "headless" });
      queryClient.setQueryData(queryKeys.browserState(targetSessionID), { hasState: false, sessionID: targetSessionID });
      return { closeEpoch, sessionID: targetSessionID };
    },
    onSuccess: ({ sessionID: targetSessionID }, _targetSessionID, context) => {
      const closeEpoch = context?.closeEpoch || 0;
      if ((closeEpochRef.current[targetSessionID] || 0) > closeEpoch) {
        return;
      }
      rememberSessionSurface(targetSessionID, "canvas");
      if (currentSessionIDRef.current === targetSessionID) {
        setBrowserActive(false);
      }
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), { tabs: [], processMode: "headless" });
      queryClient.setQueryData(queryKeys.browserState(targetSessionID), { hasState: false, sessionID: targetSessionID });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
      setClosingSessions((prev) => withoutKey(prev, targetSessionID));
      if (itemsLength === 0) {
        setCanvasOpen(false);
      }
    },
    onError: (_error, targetSessionID) => {
      if (targetSessionID) {
        clearElectronBrowserSessionGate(targetSessionID);
        setClosingSessions((prev) => withoutKey(prev, targetSessionID));
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(targetSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
      }
      toast.error(t("browser.releaseFailed"));
    },
  });

  useEffect(() => {
    if (!enabled || !sessionID || browserClosing || !activeBrowserTab) {
      return;
    }
    clearElectronBrowserSessionGate(sessionID);
  }, [activeBrowserTab, browserClosing, enabled, sessionID]);

  return {
    activeBrowserTab,
    activateBrowserSurface,
    browserActive,
    browserButtonPending:
      !activeBrowserTab && (createBrowserTabMutation.isPending || browserStateQuery.isFetching || browserTabsQuery.isFetching),
    browserClosePending: browserCloseMutation.isPending,
    browserSurfacePending: createBrowserTabMutation.isPending || browserStateQuery.isFetching || browserTabsQuery.isFetching,
    browserSurfaceVisible: browserActive || hasBrowserState || createBrowserTabMutation.isPending,
    browserTabFaviconURLText,
    browserTabTitleText,
    closeBrowserSurface: () => {
      if (sessionID) {
        browserCloseMutation.mutate(sessionID);
      }
    },
    hasBrowserState,
    hasOpenBrowserWindow,
    selectCanvasSurface,
  };
}

function readSessionSurfaces(): Record<string, CanvasSurface> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(SESSION_SURFACE_STORAGE_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const out: Record<string, CanvasSurface> = {};
    Object.entries(parsed).forEach(([sessionID, surface]) => {
      if (surface === "canvas" || surface === "browser") {
        out[sessionID] = surface;
      }
    });
    return out;
  } catch {
    return {};
  }
}

function writeSessionSurfaces(surfaces: Record<string, CanvasSurface>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(SESSION_SURFACE_STORAGE_KEY, JSON.stringify(surfaces));
  } catch {
    // Best-effort UI preference.
  }
}

function withoutKey<T>(record: Record<string, T>, key: string): Record<string, T> {
  const next = { ...record };
  delete next[key];
  return next;
}
