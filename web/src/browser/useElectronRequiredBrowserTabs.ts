import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { syncBrowserTab, type BrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  electronBrowserBridge,
  electronBrowserSnapshotToTab,
  type ElectronBrowserSnapshot,
  type ElectronWebviewRequiredEvent,
} from "@/browser/electronBridge";
import { updateBrowserWorkspaceActivity } from "@/state/workspaceActivityStore";

export type ElectronBrowserSurfaceTab = BrowserTab & {
  webviewRequestID?: string;
};

type BrowserTabsBySession = Record<string, ElectronBrowserSurfaceTab[]>;
const emptyBrowserTabsBySession: BrowserTabsBySession = {};

export function useElectronRequiredBrowserTabs(token: string) {
  const queryClient = useQueryClient();
  const [tabsBySession, setTabsBySession] = useState<BrowserTabsBySession>({});
  const retainedTokenRef = useRef(token);

  useEffect(() => {
    if (retainedTokenRef.current === token) {
      return;
    }
    retainedTokenRef.current = token;
    setTabsBySession({});
  }, [token]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onWebviewRequired) {
      return;
    }
    const stopRequired = bridge.onWebviewRequired((request) => {
      const tab = requiredEventToTab(request);
      if (!tab) {
        return;
      }
      setTabsBySession((current) => upsertSessionTab(current, tab));
    });
    const syncTimers = new Map<string, number>();
    const pendingSnapshots = new Map<string, ElectronBrowserSnapshot>();
    const stopUpdated = bridge.onUpdated((snapshot) => {
      setTabsBySession((current) => updateRequiredTab(current, snapshot));
      if (snapshot.status !== "lost") {
        const tab = electronBrowserSnapshotToTab(snapshot);
        updateBrowserWorkspaceActivity(snapshot.sessionID, snapshot.tabID, {
          faviconURL: tab.faviconURL,
          title: tab.title,
          url: tab.url,
        });
      }
      const key = `${snapshot.sessionID}:${snapshot.tabID}`;
      window.clearTimeout(syncTimers.get(key));
      if (snapshot.status === "lost") {
        syncTimers.delete(key);
        pendingSnapshots.delete(key);
        return;
      }
      if (snapshot.status === "pending") {
        pendingSnapshots.delete(key);
        return;
      }
      pendingSnapshots.set(key, snapshot);
      syncTimers.set(
        key,
        window.setTimeout(() => {
          syncTimers.delete(key);
          const latest = pendingSnapshots.get(key);
          pendingSnapshots.delete(key);
          if (!latest || !token || latest.loadError) {
            return;
          }
          void syncBrowserTab(token, latest.sessionID, latest.tabID, {
            targetID: latest.runtimeID,
            url: latest.url,
            title: latest.title,
            faviconURL: latest.faviconURL,
            canGoBack: latest.canGoBack,
            canGoForward: latest.canGoForward,
          }).then(() => {
            void queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() });
          }).catch(() => undefined);
        }, 350),
      );
    });
    return () => {
      syncTimers.forEach((timer) => window.clearTimeout(timer));
      stopRequired();
      stopUpdated();
    };
  }, [queryClient, token]);

  return retainedTokenRef.current === token ? tabsBySession : emptyBrowserTabsBySession;
}

function requiredEventToTab(request: ElectronWebviewRequiredEvent): ElectronBrowserSurfaceTab | null {
  const sessionID = request.sessionID.trim();
  const tabID = request.tabID.trim();
  const requestID = request.requestID.trim();
  if (!sessionID || !tabID || !requestID) {
    return null;
  }
  const now = new Date().toISOString();
  const url = request.url.trim() || "about:blank";
  const createdAt = request.createdAt || now;
  return {
    id: tabID,
    sessionID,
    targetID: `pending:${requestID}`,
    url,
    title: url === "about:blank" ? "" : url,
    mode: "webview",
    createdAt,
    updatedAt: createdAt,
    webviewRequestID: requestID,
  };
}

function updateRequiredTab(current: BrowserTabsBySession, snapshot: ElectronBrowserSnapshot) {
  const tabs = current[snapshot.sessionID];
  if (!tabs?.some((tab) => tab.id === snapshot.tabID)) {
    return current;
  }
  if (snapshot.status === "lost") {
    return removeSessionTab(current, snapshot.sessionID, snapshot.tabID);
  }
  const previous = tabs.find((tab) => tab.id === snapshot.tabID);
  const tab: ElectronBrowserSurfaceTab = {
    ...electronBrowserSnapshotToTab(snapshot),
    webviewRequestID: previous?.webviewRequestID,
  };
  return upsertSessionTab(current, tab);
}

function upsertSessionTab(current: BrowserTabsBySession, tab: ElectronBrowserSurfaceTab) {
  const tabs = current[tab.sessionID] || [];
  const index = tabs.findIndex((entry) => entry.id === tab.id);
  if (index >= 0 && sameSurfaceTab(tabs[index], tab)) {
    return current;
  }
  const nextTabs = [...tabs];
  if (index >= 0) {
    nextTabs[index] = tab;
  } else {
    nextTabs.push(tab);
  }
  return { ...current, [tab.sessionID]: nextTabs };
}

function removeSessionTab(current: BrowserTabsBySession, sessionID: string, tabID: string) {
  const tabs = (current[sessionID] || []).filter((tab) => tab.id !== tabID);
  const next = { ...current };
  if (tabs.length > 0) {
    next[sessionID] = tabs;
  } else {
    delete next[sessionID];
  }
  return next;
}

function sameSurfaceTab(left: ElectronBrowserSurfaceTab, right: ElectronBrowserSurfaceTab) {
  return (
    left.id === right.id &&
    left.sessionID === right.sessionID &&
    left.webviewRequestID === right.webviewRequestID &&
    left.targetID === right.targetID &&
    left.url === right.url &&
    left.title === right.title &&
    left.faviconURL === right.faviconURL &&
    left.canGoBack === right.canGoBack &&
    left.canGoForward === right.canGoForward &&
    left.mode === right.mode &&
    left.createdAt === right.createdAt &&
    left.updatedAt === right.updatedAt
  );
}
