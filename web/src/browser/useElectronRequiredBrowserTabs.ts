import { useEffect, useState } from "react";

import type { BrowserTab } from "@/api/client";
import {
  electronBrowserBridge,
  electronBrowserSnapshotToTab,
  type ElectronBrowserSnapshot,
  type ElectronWebviewRequiredEvent,
} from "@/browser/electronBridge";

export type ElectronBrowserSurfaceTab = BrowserTab & {
  webviewRequestID?: string;
};

type BrowserTabsBySession = Record<string, ElectronBrowserSurfaceTab[]>;

export function useElectronRequiredBrowserTabs(token: string) {
  const [tabsBySession, setTabsBySession] = useState<BrowserTabsBySession>({});

  useEffect(() => {
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
    const stopUpdated = bridge.onUpdated((snapshot) => {
      setTabsBySession((current) => updateRequiredTab(current, snapshot));
    });
    return () => {
      stopRequired();
      stopUpdated();
    };
  }, []);

  return tabsBySession;
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
