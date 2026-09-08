import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { adoptBrowserTab, createBrowserTab, openBrowserTab, releaseBrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  allowElectronBrowserTab,
  cacheElectronBrowserSnapshot,
  clearElectronBrowserSessionGate,
  electronBrowserBridge,
} from "@/browser/electronBridge";
import { upsertBrowserTab } from "@/browser/helpers";
import { useSessionBrowserTabs } from "@/browser/useSessionBrowserTabs";
import type { BrowserTabsData } from "@/browser/types";
import { useI18n } from "@/i18n";
import { consumeBrowserReveal, useBrowserReveal } from "@/state/browserRevealStore";
import {
  browserWorkspaceTabKey,
  mergeWorkspaceTabOrder,
  closeWorkspaceTab,
  openWorkspaceTab,
  setWorkspaceActiveTab,
  useWorkspaceActiveTab,
  useWorkspaceTabOrder,
  workspaceTabResourceID,
} from "@/state/workspaceStore";

type UseWorkspaceBrowserSurfaceArgs = {
  enabled: boolean;
  sessionID: string;
  token: string;
};

export function useWorkspaceBrowserSurface({
  enabled,
  sessionID,
  token,
}: UseWorkspaceBrowserSurfaceArgs) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const storedActiveTab = useWorkspaceActiveTab(sessionID);
  const workspaceTabOrder = useWorkspaceTabOrder(sessionID);
  const readyTokenRef = useRef(token);
  const [browserSelections, setBrowserSelections] = useState<Record<string, string>>({});
  const [releasingBrowserTabID, setReleasingBrowserTabID] = useState("");
  const [readyBrowserSessionIDs, setReadyBrowserSessionIDs] = useState<ReadonlySet<string>>(() => new Set());
  const processModeFallback = electronBrowserBridge() ? "webview" : "headless";

  const browserReveal = useBrowserReveal(sessionID);
  const { query: browserTabsQuery, tabs: resolvedBrowserTabs } = useSessionBrowserTabs(sessionID, token, enabled);
  const browserTabs = resolvedBrowserTabs;
  const visualBrowserTabs = useMemo(() => {
    const tabsByID = new Map(browserTabs.map((tab) => [browserWorkspaceTabKey(tab.id), tab]));
    return mergeWorkspaceTabOrder(workspaceTabOrder, [...tabsByID.keys()]).flatMap((id) => {
      const tab = tabsByID.get(id);
      return tab ? [tab] : [];
    });
  }, [browserTabs, workspaceTabOrder]);
  const browserTabsReady = Boolean(
    readyTokenRef.current === token
    && sessionID
    && readyBrowserSessionIDs.has(sessionID),
  );
  const browserTabsResolved = Boolean(
    enabled
    && sessionID
    && browserTabsQuery.isSuccess
    && !browserTabsQuery.isFetching,
  );

  useEffect(() => {
    if (readyTokenRef.current === token) return;
    readyTokenRef.current = token;
    setReadyBrowserSessionIDs(new Set());
  }, [token]);

  useEffect(() => {
    setReleasingBrowserTabID("");
  }, [sessionID, token]);

  useEffect(() => {
    if (!enabled || !sessionID || !browserTabsQuery.isSuccess || browserTabsQuery.isFetching) return;
    setReadyBrowserSessionIDs((current) => {
      if (current.has(sessionID)) return current;
      const next = new Set(current);
      next.add(sessionID);
      return next;
    });
  }, [browserTabsQuery.isFetching, browserTabsQuery.isSuccess, enabled, sessionID]);

  const storedBrowserTabID = workspaceTabResourceID(storedActiveTab, "browser");
  const requestedBrowserTabID = storedBrowserTabID;
  const activeBrowserTab = browserTabs.find((tab) => tab.id === requestedBrowserTabID)
    || visualBrowserTabs[0];
  const activeBrowserTabID = activeBrowserTab?.id;
  const browserActive = storedActiveTab.startsWith("browser:");
  const activeBrowserSelection = activeBrowserTab
    ? browserSelections[`${sessionID}:${activeBrowserTab.id}`] || ""
    : "";

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge || !enabled || !sessionID) return;
    return bridge.onUpdated((snapshot) => {
      if (snapshot.sessionID !== sessionID) return;
      if (snapshot.tabID === releasingBrowserTabID) return;
      const queryKey = queryKeys.browserTabs(sessionID);
      if (!browserTabsReady && queryClient.getQueryState(queryKey)?.fetchStatus === "fetching") return;
      const current = queryClient.getQueryData<BrowserTabsData>(queryKey);
      const previousTab = current?.tabs.find((tab) => tab.id === snapshot.tabID);
      const isNewTab = !current?.tabs.some((tab) => tab.id === snapshot.tabID);
      if (snapshot.status === "pending" || (previousTab && previousTab.url !== snapshot.url)) {
        const key = `${snapshot.sessionID}:${snapshot.tabID}`;
        setBrowserSelections((selections) => {
          if (!(key in selections)) return selections;
          const next = { ...selections };
          delete next[key];
          return next;
        });
      }
      const tab = cacheElectronBrowserSnapshot(queryClient, snapshot, sessionID);
      if (!tab) return;
      if (isNewTab) {
        if (snapshot.activate !== false) {
          setWorkspaceActiveTab(sessionID, browserWorkspaceTabKey(tab.id));
        }
        void adoptBrowserTab(token, tab.sessionID, tab.id).catch(() => undefined);
      }
    });
  }, [browserTabsReady, enabled, queryClient, releasingBrowserTabID, sessionID, token]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onSelectionChanged || !enabled || !sessionID) return;
    return bridge.onSelectionChanged((event) => {
      if (event.sessionID !== sessionID || !event.tabID) return;
      const key = `${event.sessionID}:${event.tabID}`;
      setBrowserSelections((current) => {
        if (event.selectionText) {
          return current[key] === event.selectionText ? current : { ...current, [key]: event.selectionText };
        }
        if (!(key in current)) return current;
        const next = { ...current };
        delete next[key];
        return next;
      });
    });
  }, [enabled, sessionID]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onAutomationStart || !enabled || !sessionID) return;
    return bridge.onAutomationStart((event) => {
      if (event.sessionID !== sessionID || event.action === "screenshot") return;
      // Automation selects its tab; workspace presentation belongs to the user.
      setWorkspaceActiveTab(sessionID, browserWorkspaceTabKey(event.tabID));
    });
  }, [enabled, sessionID]);

  const createBrowserTabMutation = useMutation({
    mutationFn: async ({ targetSessionID, url }: { targetSessionID: string; url?: string }) => {
      if (!targetSessionID) throw new Error("browser session id missing");
      const tab = await createBrowserTab(token, targetSessionID);
      allowElectronBrowserTab(targetSessionID, tab.id);
      clearElectronBrowserSessionGate(targetSessionID);
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), (current: BrowserTabsData | undefined) => ({ tabs: upsertBrowserTab(current?.tabs || [], tab), processMode: tab.mode || current?.processMode || processModeFallback }));
      openWorkspaceTab(targetSessionID, browserWorkspaceTabKey(tab.id));
      return { sessionID: targetSessionID, tab: url ? await openBrowserTab(token, targetSessionID, tab.id, { url }) : tab };
    },
    onSuccess: ({ sessionID: targetSessionID, tab }) => {
      allowElectronBrowserTab(targetSessionID, tab.id);
      clearElectronBrowserSessionGate(targetSessionID);
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), (current: BrowserTabsData | undefined) => ({
        tabs: upsertBrowserTab(current?.tabs || [], tab),
        processMode: tab.mode || current?.processMode || processModeFallback,
      }));
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: () => toast.error(t("browser.createFailed")),
  });

  const createNewBrowserTab = useCallback(() => {
    if (!sessionID || createBrowserTabMutation.isPending) return;
    createBrowserTabMutation.mutate({ targetSessionID: sessionID });
  }, [createBrowserTabMutation.isPending, createBrowserTabMutation.mutate, sessionID]);

  const openBrowserLink = useCallback((targetSessionID: string, url: string) => {
    const existing = browserTabs.find((tab) => tab.sessionID === targetSessionID && tab.url === url);
    if (existing) { openWorkspaceTab(targetSessionID, browserWorkspaceTabKey(existing.id)); return; }
    if (targetSessionID && !createBrowserTabMutation.isPending) createBrowserTabMutation.mutate({ targetSessionID, url });
  }, [browserTabs, createBrowserTabMutation.isPending, createBrowserTabMutation.mutate]);

  useEffect(() => {
    if (!enabled || !sessionID || !browserReveal) return;
    const tab = browserReveal.tabID
      ? browserTabs.find((entry) => entry.id === browserReveal.tabID)
      : activeBrowserTab;
    if (!tab) {
      if (!browserTabsResolved) return;
      consumeBrowserReveal(sessionID, browserReveal.epoch);
      return;
    }
    setWorkspaceActiveTab(sessionID, browserWorkspaceTabKey(tab.id));
    consumeBrowserReveal(sessionID, browserReveal.epoch);
  }, [activeBrowserTab, browserReveal, browserTabs, browserTabsResolved, enabled, sessionID]);

  const closeBrowserTabsMutation = useMutation({
    mutationFn: async ({ targetSessionID, tabIDs }: { targetSessionID: string; tabIDs: string[] }) => {
      const queryKey = queryKeys.browserTabs(targetSessionID);
      for (const tabID of tabIDs) {
        setReleasingBrowserTabID(tabID);
        await queryClient.cancelQueries({ queryKey });
        await releaseBrowserTab(token, targetSessionID, tabID);
        closeWorkspaceTab(targetSessionID, browserWorkspaceTabKey(tabID));
        queryClient.setQueryData<BrowserTabsData>(queryKey, (current) => ({
          tabs: (current?.tabs || []).filter((tab) => tab.id !== tabID),
          processMode: current?.processMode || processModeFallback,
        }));
      }
    },
    onSettled: async (_result, _error, { targetSessionID }) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
      setReleasingBrowserTabID("");
    },
    onError: () => toast.error(t("browser.releaseFailed")),
  });

  useEffect(() => {
    if (!enabled || !sessionID || !activeBrowserTab) return;
    clearElectronBrowserSessionGate(sessionID);
  }, [activeBrowserTab, enabled, sessionID]);

  const closeBrowserTabs = useCallback(async (targetSessionID: string, tabIDs: string[]) => {
    if (!targetSessionID || closeBrowserTabsMutation.isPending || tabIDs.length === 0) return;
    await closeBrowserTabsMutation.mutateAsync({ targetSessionID, tabIDs: [...new Set(tabIDs)] });
  }, [closeBrowserTabsMutation.isPending, closeBrowserTabsMutation.mutateAsync]);

  return {
    activeBrowserSelection,
    activeBrowserTab,
    activeBrowserTabID,
    browserActive,
    browserSurfacePending: createBrowserTabMutation.isPending || (browserActive && !browserTabsReady && browserTabsQuery.isFetching),
    browserSurfaceError: !browserTabsReady && browserTabsQuery.isError,
    retryBrowserTabs: browserTabsQuery.refetch,
    browserTabs,
    browserTabsReady,
    browserTabsResolved,
    closeBrowserTabs,
    closingBrowserTabIDs: closeBrowserTabsMutation.isPending && closeBrowserTabsMutation.variables?.targetSessionID === sessionID
      ? closeBrowserTabsMutation.variables.tabIDs
      : [],
    createNewBrowserTab,
    openBrowserLink,
    creatingBrowserTab: createBrowserTabMutation.isPending,
  };
}
