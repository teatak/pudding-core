import type { BrowserState, BrowserTab } from "@/api/client";

import type { BrowserCanvasPayload } from "./types";

export const browserQueryStaleTimeMS = 1000;

export function browserAddressToURL(value: string): string {
  const raw = value.trim();
  if (!raw) {
    return "";
  }
  const searchText = raw.startsWith("?") ? raw.slice(1).trim() : "";
  if (searchText) {
    return browserSearchURL(searchText);
  }
  if (hasURLScheme(raw)) {
    return raw;
  }
  if (isBrowserSearchText(raw)) {
    return browserSearchURL(raw);
  }
  if (isLocalBrowserHost(raw)) {
    return `http://${raw}`;
  }
  if (isLikelyBrowserHost(raw)) {
    return `https://${raw}`;
  }
  if (isSingleBrowserLabel(raw)) {
    return `https://${raw}.com`;
  }
  return browserSearchURL(raw);
}

export function browserTabTitle(tab: BrowserTab, fallback: string, blankFallback = fallback): string {
  const title = (tab.title || "").trim();
  const url = (tab.url || "").trim();
  if (browserURLIsBlank(url)) {
    return blankFallback;
  }
  if (title && title !== "about:blank") {
    return title;
  }
  return browserTitleFromURL(url) || url || fallback;
}

export function browserTabFaviconURL(tab: BrowserTab): string {
  if (!browserTabIsReal(tab)) {
    return "";
  }
  return (tab.faviconURL || "").trim() || faviconURLForPage(tab.url);
}

export function preferredBrowserTab(tabs: BrowserTab[], payload: BrowserCanvasPayload | null): BrowserTab | undefined {
  if (payload?.closedAt) {
    return undefined;
  }
  const availableTabs = tabs.filter((tab) => tab.id && tab.sessionID);
  if (availableTabs.length === 0) {
    return undefined;
  }
  const payloadTab = payload?.tabID ? availableTabs.find((tab) => tab.id === payload.tabID) : undefined;
  const latestTab = availableTabs.reduce((latest, tab) => (browserTabTimestamp(tab) > browserTabTimestamp(latest) ? tab : latest), availableTabs[0]!);
  if (!payloadTab) {
    return latestTab;
  }
  return browserTabTimestamp(latestTab) > browserTabTimestamp(payloadTab) ? latestTab : payloadTab;
}

export function browserTabIsReal(tab: BrowserTab): boolean {
  return !browserURLIsBlank(tab.url);
}

export function browserTargetURL(tab: BrowserTab | undefined, payload: BrowserCanvasPayload | null, payloadUpdatedAt?: string): string {
  const payloadURL = (payload?.url || "").trim();
  const tabURL = (tab?.url || "").trim();
  if (!payloadURL) {
    return tabURL;
  }
  if (!tabURL) {
    return payloadURL;
  }
  return browserTabIsNewerThan(tab, payloadUpdatedAt) ? tabURL : payloadURL;
}

export function browserPayloadHasRealState(payload: BrowserCanvasPayload | null): boolean {
  return Boolean(payload && !payload.closedAt && (payload.tabID || payload.url) && !browserURLIsBlank(payload.url));
}

export function browserPayloadHasBlankTabIntent(payload: BrowserCanvasPayload | null): boolean {
  return Boolean(payload?.tabID && !payload.closedAt && browserURLIsBlank(payload.url));
}

export function browserDisplayURL(rawURL?: string): string {
  const url = (rawURL || "").trim();
  return browserURLIsBlank(url) ? "" : url;
}

export function browserURLIsBlank(rawURL?: string): boolean {
  const url = (rawURL || "").trim().toLowerCase();
  return !url || url === "about:blank";
}

export function upsertBrowserTab(tabs: BrowserTab[], tab: BrowserTab): BrowserTab[] {
  const index = tabs.findIndex((item) => item.id === tab.id);
  if (index < 0) {
    return [...tabs, tab];
  }
  const next = [...tabs];
  next[index] = { ...tab, createdAt: tabs[index]!.createdAt || tab.createdAt };
  return next;
}

export function faviconURLForPage(rawURL: string): string {
  try {
    const url = new URL(rawURL);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return "";
    }
    return `${url.origin}/favicon.ico`;
  } catch {
    return "";
  }
}

export function browserPayloadFromState(state: BrowserState | undefined): BrowserCanvasPayload | null {
  if (!state?.hasState || !state.sessionID || !state.url) {
    return null;
  }
  return {
    kind: "browser",
    sessionID: state.sessionID,
    tabID: state.tabID,
    url: state.url,
    title: state.title,
    faviconURL: state.faviconURL,
    mode: state.processMode || state.mode,
  };
}

function browserTabIsNewerThan(tab: BrowserTab | undefined, timestamp?: string): boolean {
  if (!tab) {
    return false;
  }
  const tabTime = browserTabTimestamp(tab);
  const compareTime = browserTimestamp(timestamp);
  return Number.isFinite(tabTime) && (!Number.isFinite(compareTime) || tabTime > compareTime);
}

function browserTabTimestamp(tab: BrowserTab): number {
  const updated = Date.parse(tab.updatedAt);
  if (Number.isFinite(updated)) {
    return updated;
  }
  const created = Date.parse(tab.createdAt);
  return Number.isFinite(created) ? created : 0;
}

function browserTimestamp(value?: string): number {
  const timestamp = Date.parse(value || "");
  return Number.isFinite(timestamp) ? timestamp : NaN;
}

function browserTitleFromURL(rawURL: string): string {
  try {
    const url = new URL(rawURL);
    return url.hostname || rawURL;
  } catch {
    return rawURL;
  }
}

function browserSearchURL(query: string): string {
  return `https://www.google.com/search?q=${encodeURIComponent(query)}`;
}

function hasURLScheme(value: string): boolean {
  return /^[a-z][a-z0-9+.-]*:\/\//i.test(value);
}

function isBrowserSearchText(value: string): boolean {
  return /\s/.test(value) || /[^\x00-\x7f]/.test(value);
}

function isLocalBrowserHost(value: string): boolean {
  const lower = value.toLowerCase();
  return (
    lower === "localhost" ||
    lower.startsWith("localhost:") ||
    lower.startsWith("localhost/") ||
    lower.startsWith("127.") ||
    lower.startsWith("0.0.0.0") ||
    lower.startsWith("192.168.") ||
    lower.startsWith("10.") ||
    /^\[::1\](?::|\/|$)/.test(lower)
  );
}

function isLikelyBrowserHost(value: string): boolean {
  if (/[/?#]/.test(value) && value.includes(".")) {
    return true;
  }
  if (/^\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?(?:\/.*)?$/.test(value)) {
    return true;
  }
  return /^[a-z0-9-]+(?:\.[a-z0-9-]+)+(?:\:\d+)?(?:[/?#].*)?$/i.test(value);
}

function isSingleBrowserLabel(value: string): boolean {
  return /^[a-z0-9][a-z0-9-]{1,62}$/i.test(value);
}
