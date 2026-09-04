import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { syncBrowserTab, type BrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  cacheElectronBrowserSnapshot,
  electronBrowserBridge,
  electronBrowserSnapshotToTab,
  type ElectronBrowserSnapshot,
  type ElectronWebviewRequiredEvent,
} from "@/browser/electronBridge";

export type ElectronBrowserSurfaceTab = BrowserTab & {
  loading?: boolean;
  webviewRequestID?: string;
};

type BrowserTabsBySession = Record<string, ElectronBrowserSurfaceTab[]>;
const emptyBrowserTabsBySession: BrowserTabsBySession = {};

export function useElectronRequiredBrowserTabs(token: string) {
  const queryClient = useQueryClient();
  const [tabsBySession, setTabsBySession] = useState<BrowserTabsBySession>({});
  const retainedTokenRef = useRef(token);
  const lostRequestIDsRef = useRef<Map<string, string>>(new Map());

  useEffect(() => {
    if (retainedTokenRef.current === token) {
      return;
    }
    retainedTokenRef.current = token;
    lostRequestIDsRef.current.clear();
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
      const key = browserTabKey(tab.sessionID, tab.id);
      const lostRequestID = lostRequestIDsRef.current.get(key);
      if (lostRequestID === "*" || lostRequestID === request.requestID) {
        return;
      }
      lostRequestIDsRef.current.delete(key);
      setTabsBySession((current) => upsertSessionTab(current, tab));
    });
    const syncQueues = new Map<string, Promise<void>>();
    let disposed = false;
    const stopUpdated = bridge.onUpdated((snapshot) => {
      if (snapshot.status === "lost") {
        lostRequestIDsRef.current.set(
          browserTabKey(snapshot.sessionID, snapshot.tabID),
          snapshot.webviewRequestID || "*",
        );
      }
      setTabsBySession((current) => updateRequiredTab(current, snapshot));
      const key = `${snapshot.sessionID}:${snapshot.tabID}`;
      if (snapshot.status === "lost") {
        return;
      }
      if (snapshot.status === "pending" || !token || snapshot.loadError || snapshot.loading) {
        return;
      }
      const queued = (syncQueues.get(key) || Promise.resolve())
        .then(async () => {
          if (disposed) {
            return;
          }
          await syncBrowserTab(token, snapshot.sessionID, snapshot.tabID, {
            targetID: snapshot.runtimeID,
            url: snapshot.url,
            title: snapshot.title,
            faviconURL: snapshot.faviconStale ? "" : snapshot.faviconURL,
            canGoBack: snapshot.canGoBack,
            canGoForward: snapshot.canGoForward,
            historyVisit: snapshot.navigationSettled === true,
          });
          cacheElectronBrowserSnapshot(queryClient, snapshot, snapshot.sessionID);
          void queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() });
        })
        .catch(() => undefined)
        .finally(() => {
          if (syncQueues.get(key) === queued) {
            syncQueues.delete(key);
          }
        });
      syncQueues.set(key, queued);
    });
    return () => {
      disposed = true;
      stopRequired();
      stopUpdated();
    };
  }, [queryClient, token]);

  return retainedTokenRef.current === token ? tabsBySession : emptyBrowserTabsBySession;
}

function browserTabKey(sessionID: string, tabID: string) {
  return `${sessionID.trim()}\u0000${tabID.trim()}`;
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
    loading: snapshot.loading,
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
    left.loading === right.loading &&
    left.canGoBack === right.canGoBack &&
    left.canGoForward === right.canGoForward &&
    left.mode === right.mode &&
    left.createdAt === right.createdAt &&
    left.updatedAt === right.updatedAt
  );
}
