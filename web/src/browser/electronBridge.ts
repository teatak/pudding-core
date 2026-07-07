import type { QueryClient } from "@tanstack/react-query";

import type { BrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { upsertBrowserTab } from "@/browser/helpers";
import type { BrowserTabsData } from "@/browser/types";

export type ElectronBrowserBounds = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export type ElectronBrowserRequest = {
  sessionID: string;
  tabID?: string;
  url?: string;
  bounds?: ElectronBrowserBounds;
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
  attach: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  updateBounds: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot | null>;
  detach: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot | null>;
  loadURL: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  back: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  forward: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  reload: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  listTabs: (request: ElectronBrowserRequest) => Promise<{ tabs: ElectronBrowserSnapshot[]; processMode: "headless" }>;
  closeTab: (request: ElectronBrowserRequest) => Promise<ElectronBrowserSnapshot>;
  closeSession: (request: ElectronBrowserRequest) => Promise<void>;
  onUpdated: (listener: (snapshot: ElectronBrowserSnapshot) => void) => () => void;
};

declare global {
  interface Window {
    puddingElectronBrowser?: ElectronBrowserBridge;
  }
}

export function hasElectronNativeBrowser() {
  return typeof window !== "undefined" && Boolean(window.puddingElectronBrowser);
}

export function electronNativeBrowser() {
  return typeof window === "undefined" ? undefined : window.puddingElectronBrowser;
}

export function electronBrowserSnapshotToTab(snapshot: ElectronBrowserSnapshot): BrowserTab {
  const now = new Date().toISOString();
  return {
    id: snapshot.tabID || "default",
    sessionID: snapshot.sessionID,
    targetID: snapshot.runtimeID,
    url: snapshot.url || "about:blank",
    title: snapshot.title || snapshot.url || "about:blank",
    mode: "headless",
    canGoBack: snapshot.canGoBack,
    canGoForward: snapshot.canGoForward,
    createdAt: now,
    updatedAt: now,
  };
}

export function electronBrowserSnapshotsToTabs(snapshots: ElectronBrowserSnapshot[]): BrowserTabsData {
  return {
    tabs: snapshots.filter((snapshot) => snapshot.status !== "lost").map(electronBrowserSnapshotToTab),
    processMode: "headless",
  };
}

export function cacheElectronBrowserSnapshot(queryClient: QueryClient, snapshot: ElectronBrowserSnapshot) {
  if (snapshot.status === "lost") {
    queryClient.setQueryData(queryKeys.browserTabs(snapshot.sessionID), (current: BrowserTabsData | undefined) => ({
      tabs: (current?.tabs || []).filter((tab) => tab.id !== snapshot.tabID),
      processMode: "headless",
    }));
    return;
  }
  const tab = electronBrowserSnapshotToTab(snapshot);
  queryClient.setQueryData(queryKeys.browserTabs(snapshot.sessionID), (current: BrowserTabsData | undefined) => ({
    tabs: upsertBrowserTab(current?.tabs || [], tab),
    processMode: "headless",
  }));
}
