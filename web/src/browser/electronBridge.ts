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
  requestID?: string;
  createdAt?: string;
  webContentsID: number;
  loadError?: {
    code?: string;
    description?: string;
  };
};

export type ElectronWebviewRequiredEvent = Required<Pick<ElectronBrowserRequest, "sessionID" | "tabID" | "url">> & {
  requestID: string;
  createdAt?: string;
};

export type ElectronBrowserSnapshot = {
  sessionID: string;
  tabID: string;
  status: "live_internal" | "pending" | "detached" | "lost";
  url: string;
  title: string;
  faviconURL?: string;
  canGoBack: boolean;
  canGoForward: boolean;
  profileID: string;
  runtimeID: string;
  version: number;
  activate?: boolean;
  createdAt?: string;
  updatedAt?: string;
  loadError?: {
    code: string;
    description: string;
  };
};

export type ElectronBrowserSelection = {
  selectionText: string;
};

export type ElectronBrowserCursorEvent = {
  sessionID: string;
  tabID: string;
  action: "click" | "type" | "scroll";
  x: number;
  y: number;
  version?: number;
  createdAt?: string;
};

export type ElectronBrowserAutomationEvent = {
  sessionID: string;
  tabID: string;
  action: "back" | "click" | "forward" | "open" | "reload" | "scroll" | "type";
  requestID?: string;
  version?: number;
  createdAt?: string;
};

export type ElectronBrowserBridge = {
  ensure: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  resolveFavicon?: (request: { url: string; pageURL: string }) => Promise<string>;
  registerWebview: (request: ElectronWebviewRegisterRequest) => Promise<ElectronBrowserSnapshot>;
  loadURL: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  back: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  forward: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  reload: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  readSelection?: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSelection>;
  listTabs: (request: ElectronBrowserRequest) => Promise<{ tabs: ElectronBrowserSnapshot[]; processMode: "webview" }>;
  closeTab: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  closeSession: (request: ElectronBrowserRequest) => Promise<void>;
  onUpdated: (listener: (snapshot: ElectronBrowserSnapshot) => void) => () => void;
  onCursor?: (listener: (event: ElectronBrowserCursorEvent) => void) => () => void;
  onAutomationStart?: (listener: (event: ElectronBrowserAutomationEvent) => void) => () => void;
  onAutomationEnd?: (listener: (event: ElectronBrowserAutomationEvent) => void) => () => void;
  completeAutomationLifecycle?: (request: { requestID: string; ok: boolean }) => Promise<boolean>;
  onWebviewRequired?: (listener: (request: ElectronWebviewRequiredEvent) => void) => () => void;
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

export async function readElectronBrowserSelection(sessionID: string, tabID: string) {
  const bridge = electronBrowserBridge();
  if (!bridge?.readSelection) {
    return "";
  }
  let timeoutID = 0;
  try {
    const result = await Promise.race([
      bridge.readSelection({ sessionID, tabID }),
      new Promise<ElectronBrowserSelection>((resolve) => {
        timeoutID = window.setTimeout(() => resolve({ selectionText: "" }), 500);
      }),
    ]);
    return String(result.selectionText || "").trim().slice(0, 16 * 1024);
  } catch {
    return "";
  } finally {
    window.clearTimeout(timeoutID);
  }
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
    faviconURL: snapshot.faviconURL || undefined,
    mode: "webview",
    canGoBack: snapshot.canGoBack,
    canGoForward: snapshot.canGoForward,
    createdAt: snapshot.createdAt || now,
    updatedAt: snapshot.updatedAt || now,
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
        queryClient.setQueryData(queryKeys.browserState(snapshot.sessionID), { hasState: false, sessionID: snapshot.sessionID, processMode: "webview" });
      }
      return { tabs, processMode: "webview" };
    });
    return null;
  }
  const tab = electronBrowserSnapshotToTab(snapshot);
  queryClient.setQueryData(queryKeys.browserTabs(snapshot.sessionID), (current: BrowserTabsData | undefined) => ({
    tabs: upsertBrowserTab(current?.tabs || [], tab),
    processMode: "webview",
  }));
  queryClient.setQueryData(queryKeys.browserState(snapshot.sessionID), {
    hasState: true,
    sessionID: snapshot.sessionID,
    tabID: tab.id,
    url: tab.url,
    title: tab.title,
    faviconURL: tab.faviconURL,
    mode: "webview",
    processMode: "webview",
    createdAt: tab.createdAt,
    updatedAt: tab.updatedAt,
  });
  return tab;
}
