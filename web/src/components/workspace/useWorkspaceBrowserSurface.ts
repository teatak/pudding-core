import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { adoptBrowserTab, createBrowserTab, releaseBrowserTab } from "@/api/client";
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
  getWorkspaceSessionUI,
  mergeWorkspaceTabOrder,
  nextWorkspaceTabAfterClose,
  openWorkspaceTab,
  replaceWorkspaceSessionUI,
  setWorkspaceActiveTab,
  updateWorkspaceSessionUI,
  useWorkspaceActiveTab,
  useWorkspaceTabOrder,
  workspaceTabResourceID,
  type WorkspaceTabKey,
} from "@/state/workspaceStore";

type UseWorkspaceBrowserSurfaceArgs = {
  availableNonBrowserTabs: WorkspaceTabKey[];
  enabled: boolean;
  sessionID: string;
  token: string;
};

export function useWorkspaceBrowserSurface({
  availableNonBrowserTabs,
  enabled,
  sessionID,
  token,
}: UseWorkspaceBrowserSurfaceArgs) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const activeTab = useWorkspaceActiveTab(sessionID);
  const workspaceTabOrder = useWorkspaceTabOrder(sessionID);
  const closingBrowserTabRef = useRef("");
  const readyTokenRef = useRef(token);
  const [browserSelections, setBrowserSelections] = useState<Record<string, string>>({});
  const [readyBrowserSessionIDs, setReadyBrowserSessionIDs] = useState<ReadonlySet<string>>(() => new Set());
  const processModeFallback = electronBrowserBridge() ? "webview" : "headless";

  const browserReveal = useBrowserReveal(sessionID);
  const { query: browserTabsQuery, tabs: browserTabs } = useSessionBrowserTabs(sessionID, token, enabled);
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
    if (!enabled || !sessionID || !browserTabsQuery.isSuccess || browserTabsQuery.isFetching) return;
    setReadyBrowserSessionIDs((current) => {
      if (current.has(sessionID)) return current;
      const next = new Set(current);
      next.add(sessionID);
      return next;
    });
  }, [browserTabsQuery.isFetching, browserTabsQuery.isSuccess, enabled, sessionID]);

  const requestedBrowserTabID = workspaceTabResourceID(activeTab, "browser");
  const activeBrowserTab = browserTabs.find((tab) => tab.id === requestedBrowserTabID)
    || visualBrowserTabs[0];
  const activeBrowserTabID = activeBrowserTab?.id;
  const browserActive = Boolean(requestedBrowserTabID);
  const activeBrowserSelection = activeBrowserTab
    ? browserSelections[`${sessionID}:${activeBrowserTab.id}`] || ""
    : "";
  const hasBrowserState = browserTabs.length > 0;

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge || !enabled || !sessionID) return;
    return bridge.onUpdated((snapshot) => {
      if (snapshot.sessionID !== sessionID) return;
      if (snapshot.tabID === closingBrowserTabRef.current) return;
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
          openWorkspaceTab(sessionID, browserWorkspaceTabKey(tab.id));
        }
        void adoptBrowserTab(token, tab.sessionID, tab.id).catch(() => undefined);
      }
    });
  }, [browserTabsReady, enabled, queryClient, sessionID, token]);

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
      openWorkspaceTab(sessionID, browserWorkspaceTabKey(event.tabID));
    });
  }, [enabled, sessionID]);

  const createBrowserTabMutation = useMutation({
    mutationFn: async (targetSessionID: string) => {
      if (!targetSessionID) throw new Error("browser session id missing");
      return { sessionID: targetSessionID, tab: await createBrowserTab(token, targetSessionID) };
    },
    onSuccess: ({ sessionID: targetSessionID, tab }) => {
      allowElectronBrowserTab(targetSessionID, tab.id);
      clearElectronBrowserSessionGate(targetSessionID);
      openWorkspaceTab(targetSessionID, browserWorkspaceTabKey(tab.id));
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
    createBrowserTabMutation.mutate(sessionID);
  }, [createBrowserTabMutation.isPending, createBrowserTabMutation.mutate, sessionID]);

  const selectBrowserTab = useCallback((tabID: string) => {
    if (!sessionID || !browserTabs.some((tab) => tab.id === tabID)) return;
    setWorkspaceActiveTab(sessionID, browserWorkspaceTabKey(tabID));
  }, [browserTabs, sessionID]);

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
    openWorkspaceTab(sessionID, browserWorkspaceTabKey(tab.id));
    consumeBrowserReveal(sessionID, browserReveal.epoch);
  }, [activeBrowserTab, browserReveal, browserTabs, browserTabsResolved, enabled, sessionID]);

  const closeBrowserTabMutation = useMutation({
    mutationFn: async ({ targetSessionID, tabID }: { targetSessionID: string; tabID: string }) => {
      await releaseBrowserTab(token, targetSessionID, tabID);
      return { sessionID: targetSessionID, tabID };
    },
    onMutate: async ({ targetSessionID, tabID }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
      const previousTabs = queryClient.getQueryData<BrowserTabsData>(queryKeys.browserTabs(targetSessionID));
      const previousUI = getWorkspaceSessionUI(targetSessionID);
      const closingKey = browserWorkspaceTabKey(tabID);
      const remainingTabs = (previousTabs?.tabs || []).filter((tab) => tab.id !== tabID);
      const availableTabs = [
        ...availableNonBrowserTabs,
        ...(previousTabs?.tabs || []).map((tab) => browserWorkspaceTabKey(tab.id)),
      ];
      const fallback = nextWorkspaceTabAfterClose(closingKey, availableTabs, previousUI.tabOrder);
      updateWorkspaceSessionUI(targetSessionID, (current) => ({
        ...current,
        activeTab: current.activeTab === closingKey ? fallback : current.activeTab,
        tabOrder: current.tabOrder.filter((tab) => tab !== closingKey),
      }));
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), {
        tabs: remainingTabs,
        processMode: previousTabs?.processMode || processModeFallback,
      });
      return { previousTabs, previousUI };
    },
    onSuccess: async ({ sessionID: targetSessionID }) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: (_error, variables, context) => {
      if (context?.previousTabs) {
        queryClient.setQueryData(queryKeys.browserTabs(variables.targetSessionID), context.previousTabs);
      }
      if (context?.previousUI) {
        replaceWorkspaceSessionUI(variables.targetSessionID, context.previousUI);
      }
      toast.error(t("browser.releaseFailed"));
    },
    onSettled: () => {
      closingBrowserTabRef.current = "";
    },
  });

  useEffect(() => {
    if (!enabled || !sessionID || !activeBrowserTab) return;
    clearElectronBrowserSessionGate(sessionID);
  }, [activeBrowserTab, enabled, sessionID]);

  const closeBrowserTab = useCallback((tabID: string) => {
    if (!sessionID || closingBrowserTabRef.current) return;
    closingBrowserTabRef.current = tabID;
    closeBrowserTabMutation.mutate({ targetSessionID: sessionID, tabID });
  }, [closeBrowserTabMutation.mutate, sessionID]);

  return {
    activeBrowserSelection,
    activeBrowserTab,
    activeBrowserTabID,
    activeTab,
    browserActive,
    browserSurfacePending: createBrowserTabMutation.isPending || (browserActive && !browserTabsReady),
    browserSurfaceVisible: browserActive || hasBrowserState || createBrowserTabMutation.isPending,
    browserTabs,
    browserTabsReady,
    browserTabsResolved,
    closeBrowserTab,
    closingBrowserTabID: closeBrowserTabMutation.isPending
      ? closeBrowserTabMutation.variables?.tabID
      : undefined,
    createNewBrowserTab,
    creatingBrowserTab: createBrowserTabMutation.isPending,
    selectBrowserTab,
  };
}
