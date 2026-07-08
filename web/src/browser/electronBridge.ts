import type { QueryClient } from "@tanstack/react-query";

import type { BrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { browserURLIsBlank, upsertBrowserTab } from "@/browser/helpers";
import type { BrowserTabsData } from "@/browser/types";

export type ElectronBrowserRequest = {
  sessionID: string;
  tabID?: string;
  url?: string;
};

export type ElectronWebviewRegisterRequest = ElectronBrowserRequest & {
  webContentsID: number;
  loadError?: {
    code?: string;
    description?: string;
  };
};

export type ElectronWebviewCaptureRequest = ElectronBrowserRequest & {
  captureID: string;
  fullPage?: boolean;
};

export type ElectronWebviewCaptureResponse = {
  captureID: string;
  dataBase64?: string;
  dataURL?: string;
  width?: number;
  height?: number;
  error?: string;
};

export type ElectronBrowserSnapshot = {
  sessionID: string;
  tabID: string;
  status: "live_internal" | "detached" | "lost";
  url: string;
  title: string;
  canGoBack: boolean;
  canGoForward: boolean;
  profileID: string;
  runtimeID: string;
  version: number;
};

export type ElectronBrowserBridge = {
  ensure: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  registerWebview: (request: ElectronWebviewRegisterRequest) => Promise<ElectronBrowserSnapshot>;
  resolveWebviewCapture?: (response: ElectronWebviewCaptureResponse) => Promise<void>;
  loadURL: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  back: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  forward: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  reload: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  listTabs: (request: ElectronBrowserRequest) => Promise<{ tabs: ElectronBrowserSnapshot[]; processMode: "headless" }>;
  closeTab: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  closeSession: (request: ElectronBrowserRequest) => Promise<void>;
  onUpdated: (listener: (snapshot: ElectronBrowserSnapshot) => void) => () => void;
  onWebviewCaptureRequest?: (listener: (request: ElectronWebviewCaptureRequest) => void) => () => void;
};

declare global {
  interface Window {
    puddingElectronBrowser?: ElectronBrowserBridge;
  }
}

export function hasElectronWebviewBrowser() {
  return typeof window !== "undefined" && Boolean(window.puddingElectronBrowser);
}

export function electronBrowserBridge() {
  return typeof window === "undefined" ? undefined : window.puddingElectronBrowser;
}

export function electronBrowserSnapshotToTab(snapshot: ElectronBrowserSnapshot): BrowserTab {
  const now = new Date().toISOString();
  const url = snapshot.url || "about:blank";
  const title = browserURLIsBlank(url) ? "" : snapshot.title || snapshot.url || "about:blank";
  return {
    id: snapshot.tabID || "default",
    sessionID: snapshot.sessionID,
    targetID: snapshot.runtimeID,
    url,
    title,
    mode: "headless",
    canGoBack: snapshot.canGoBack,
    canGoForward: snapshot.canGoForward,
    createdAt: now,
    updatedAt: now,
  };
}

const electronBrowserSessionGates = new Map<string, Set<string>>();

export function markElectronBrowserSessionClosed(sessionID: string) {
  const key = sessionID.trim();
  if (!key) {
    return;
  }
  electronBrowserSessionGates.set(key, new Set());
}

export function clearElectronBrowserSessionGate(sessionID: string) {
  const key = sessionID.trim();
  if (!key) {
    return;
  }
  electronBrowserSessionGates.delete(key);
}

export function electronBrowserSessionHasGate(sessionID: string) {
  const key = sessionID.trim();
  return key ? electronBrowserSessionGates.has(key) : false;
}

export function allowElectronBrowserTab(sessionID: string, tabID?: string) {
  const key = sessionID.trim();
  const tabKey = (tabID || "").trim();
  if (!key || !tabKey) {
    return;
  }
  const gate = electronBrowserSessionGates.get(key);
  if (!gate) {
    return;
  }
  gate.add(tabKey);
}

function electronBrowserSnapshotAllowed(snapshot: ElectronBrowserSnapshot, expectedSessionID?: string) {
  const snapshotSessionID = snapshot.sessionID.trim();
  const expected = (expectedSessionID || "").trim();
  if (expected && snapshotSessionID !== expected) {
    return false;
  }
  const gate = electronBrowserSessionGates.get(snapshot.sessionID.trim());
  if (!gate) {
    return true;
  }
  return gate.has((snapshot.tabID || "").trim());
}

export function cacheElectronBrowserSnapshot(
  queryClient: QueryClient,
  snapshot: ElectronBrowserSnapshot,
  expectedSessionID?: string,
): BrowserTab | null {
  if (!electronBrowserSnapshotAllowed(snapshot, expectedSessionID)) {
    return null;
  }
  if (snapshot.status === "lost") {
    queryClient.setQueryData(queryKeys.browserTabs(snapshot.sessionID), (current: BrowserTabsData | undefined) => {
      const tabs = (current?.tabs || []).filter((tab) => tab.id !== snapshot.tabID);
      if (tabs.length === 0) {
        queryClient.setQueryData(queryKeys.browserState(snapshot.sessionID), { hasState: false, sessionID: snapshot.sessionID, processMode: "headless" });
      }
      return { tabs, processMode: "headless" };
    });
    return null;
  }
  const tab = electronBrowserSnapshotToTab(snapshot);
  queryClient.setQueryData(queryKeys.browserTabs(snapshot.sessionID), (current: BrowserTabsData | undefined) => ({
    tabs: upsertBrowserTab(current?.tabs || [], tab),
    processMode: "headless",
  }));
  queryClient.setQueryData(queryKeys.browserState(snapshot.sessionID), {
    hasState: true,
    sessionID: snapshot.sessionID,
    tabID: tab.id,
    url: tab.url,
    title: tab.title,
    faviconURL: tab.faviconURL,
    mode: "headless",
    processMode: "headless",
    createdAt: tab.createdAt,
    updatedAt: tab.updatedAt,
  });
  return tab;
}
