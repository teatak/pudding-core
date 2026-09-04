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
  webviewRequestID?: string;
  version: number;
  loading: boolean;
  faviconStale: boolean;
  navigationSettled?: boolean;
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

export type ElectronBrowserSelectionEvent = {
  selected: boolean;
  selectionText: string;
  sessionID: string;
  tabID: string;
};

export type ElectronBrowserFindResult = {
  sessionID: string;
  tabID: string;
  requestID: number;
  activeMatchOrdinal: number;
  matches: number;
  finalUpdate: boolean;
};

export type ElectronBrowserInteractionEvent = {
  sessionID: string;
  tabID: string;
  kind: "keyboard" | "pointer";
  key?: "Escape";
};

export type ElectronBrowserZoom = {
  factor: number;
  percent: number;
};

export type ElectronBrowserPrintResult = {
  ok: boolean;
  canceled: boolean;
  reason: string;
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
  action: "back" | "click" | "forward" | "observe" | "open" | "reload" | "screenshot" | "scroll" | "type";
  ok?: boolean;
  requestID?: string;
  version?: number;
  createdAt?: string;
};

export type ElectronBrowserCredential = {
  id: string;
  origin: string;
  username: string;
  createdAt: string;
  updatedAt: string;
};

export type ElectronBrowserCredentialPrompt = {
  id: string;
  origin: string;
  username: string;
  kind: "save" | "update";
  createdAt: string;
};

export type ElectronBrowserCredentialState = {
  available: boolean;
  reason: string;
  origin: string;
  formDetected: boolean;
  credentials: ElectronBrowserCredential[];
  prompt: ElectronBrowserCredentialPrompt | null;
  sessionID?: string;
  tabID?: string;
};

export type ElectronBrowserCredentialList = {
  available: boolean;
  reason: string;
  credentials: ElectronBrowserCredential[];
  neverSaveOrigins: string[];
};

export type ElectronBrowserCredentialImportResult = {
  canceled: boolean;
  imported: number;
  updated: number;
  unchanged: number;
  skipped: number;
  sourceDeleted: boolean;
};

export type ElectronBrowserBridge = {
  ensure: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  resolveFavicon?: (request: { url: string; pageURL: string }) => Promise<string>;
  registerWebview: (request: ElectronWebviewRegisterRequest) => Promise<ElectronBrowserSnapshot>;
  loadURL: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  back: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  forward: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  reload: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  stop: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  readSelection?: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSelection>;
  findInPage?: (request: ElectronBrowserRequest & { text: string; forward?: boolean; findNext?: boolean; matchCase?: boolean }) => Promise<{ requestID: number }>;
  stopFindInPage?: (request: ElectronBrowserRequest) => Promise<{ ok: boolean }>;
  getZoom?: (request: ElectronBrowserRequest) => Promise<ElectronBrowserZoom>;
  zoom?: (request: ElectronBrowserRequest & { action: "in" | "out" | "reset" }) => Promise<ElectronBrowserZoom>;
  print?: (request: ElectronBrowserRequest) => Promise<ElectronBrowserPrintResult>;
  listTabs: (request: ElectronBrowserRequest) => Promise<{ tabs: ElectronBrowserSnapshot[]; processMode: "webview" }>;
  closeTab: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  closeSession: (request: ElectronBrowserRequest) => Promise<void>;
  getCredentialState?: (request: ElectronBrowserRequest) => Promise<ElectronBrowserCredentialState>;
  listCredentials?: () => Promise<ElectronBrowserCredentialList>;
  saveCredential?: (request: ElectronBrowserRequest & { pendingID: string }) => Promise<ElectronBrowserCredential>;
  dismissCredential?: (request: ElectronBrowserRequest & { pendingID: string; neverSave?: boolean }) => Promise<void>;
  deleteCredential?: (request: { credentialID: string }) => Promise<void>;
  clearCredentials?: () => Promise<void>;
  allowCredentialOrigin?: (request: { origin: string }) => Promise<void>;
  importChromePasswords?: () => Promise<ElectronBrowserCredentialImportResult>;
  onUpdated: (listener: (snapshot: ElectronBrowserSnapshot) => void) => () => void;
  onCursor?: (listener: (event: ElectronBrowserCursorEvent) => void) => () => void;
  onSelectionChanged?: (listener: (event: ElectronBrowserSelectionEvent) => void) => () => void;
  onFoundInPage?: (listener: (result: ElectronBrowserFindResult) => void) => () => void;
  onInteraction?: (listener: (event: ElectronBrowserInteractionEvent) => void) => () => void;
  onAutomationStart?: (listener: (event: ElectronBrowserAutomationEvent) => void) => () => void;
  onAutomationEnd?: (listener: (event: ElectronBrowserAutomationEvent) => void) => () => void;
  onCredentialState?: (listener: (state: ElectronBrowserCredentialState) => void) => () => void;
  onCredentialsChanged?: (listener: (change: { updatedAt: string }) => void) => () => void;
  onCredentialManage?: (listener: (request: { sessionID: string; tabID: string }) => void) => () => void;
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
    return { selectionText: "" };
  }
  let timeoutID = 0;
  try {
    const result = await Promise.race([
      bridge.readSelection({ sessionID, tabID }),
      new Promise<ElectronBrowserSelection>((resolve) => {
        timeoutID = window.setTimeout(() => resolve({ selectionText: "" }), 2_000);
      }),
    ]);
    return {
      selectionText: String(result.selectionText || "").trim().slice(0, 16 * 1024),
    };
  } catch {
    return { selectionText: "" };
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
  const queryKey = queryKeys.browserTabs(snapshot.sessionID);
  if (queryClient.getQueryState(queryKey)?.fetchStatus === "fetching") {
    return snapshot.status === "lost" ? null : electronBrowserSnapshotToTab(snapshot);
  }
  if (snapshot.status === "lost") {
    queryClient.setQueryData(queryKey, (current: BrowserTabsData | undefined) => {
      const tabs = (current?.tabs || []).filter((tab) => tab.id !== snapshot.tabID);
      return { tabs, processMode: "webview" };
    });
    return null;
  }
  const tab = electronBrowserSnapshotToTab(snapshot);
  queryClient.setQueryData(queryKey, (current: BrowserTabsData | undefined) => ({
    tabs: upsertBrowserTab(current?.tabs || [], tab),
    processMode: "webview",
  }));
  return tab;
}
