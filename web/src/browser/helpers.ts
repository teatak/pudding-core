import type { BrowserState, BrowserTab } from "@/api/client";
import type { CanvasItem } from "@/contracts/api";
import { apiURL } from "@/state/apiBase";

import type { BrowserCanvasPayload } from "./types";

export const browserForegroundRefetchIntervalMS = 5000;
export const browserBackgroundRefetchIntervalMS = 10000;
export const browserQueryStaleTimeMS = 1000;

export function browserCanvasItemID(sessionID: string): string {
  return `browser_${sessionID.replace(/[^a-zA-Z0-9_-]/g, "_")}`;
}

export function browserWindowKey(item: CanvasItem): string {
  const payload = browserPayloadForItem(item);
  const ownerSessionID = payload?.sessionID || item.sourceSessionID || item.id;
  return `${ownerSessionID}:${payload?.tabID || payload?.url || item.id}`;
}

export function browserScreencastURL(token: string, sessionID: string, tabID: string): string {
  const url = new URL(
    apiURL(`/sessions/${encodeURIComponent(sessionID)}/browser/tabs/${encodeURIComponent(tabID)}/screencast`),
    window.location.href,
  );
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.searchParams.set("token", token);
  return url.toString();
}

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

export function browserTabTitle(tab: BrowserTab, fallback: string): string {
  const title = (tab.title || "").trim();
  const url = (tab.url || "").trim();
  if (title && !(title === "about:blank" && !browserURLIsBlank(url))) {
    return title;
  }
  if (!browserURLIsBlank(url)) {
    return browserTitleFromURL(url) || url;
  }
  return fallback;
}

export function browserTabFaviconURL(tab: BrowserTab): string {
  if (!browserTabIsReal(tab)) {
    return "";
  }
  return (tab.faviconURL || "").trim() || faviconURLForPage(tab.url);
}

export function preferredBrowserTab(tabs: BrowserTab[], payload: BrowserCanvasPayload | null): BrowserTab | undefined {
  const realTabs = tabs.filter(browserTabIsReal);
  if (realTabs.length === 0) {
    return undefined;
  }
  const payloadTab = payload?.tabID ? realTabs.find((tab) => tab.id === payload.tabID) : undefined;
  const latestTab = realTabs.reduce((latest, tab) => (browserTabTimestamp(tab) > browserTabTimestamp(latest) ? tab : latest), realTabs[0]!);
  if (!payloadTab) {
    return latestTab;
  }
  return browserTabTimestamp(latestTab) > browserTabTimestamp(payloadTab) ? latestTab : payloadTab;
}

export function browserTabIsReal(tab: BrowserTab): boolean {
  return !browserURLIsBlank(tab.url);
}

export function browserTabSwitchKey(tab: BrowserTab | undefined): string {
  return tab ? `${tab.id}:${tab.url}` : "";
}

export function browserPayloadHasRealState(payload: BrowserCanvasPayload | null): boolean {
  return Boolean(payload && (payload.tabID || payload.url) && !browserURLIsBlank(payload.url));
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
  const next = tabs.filter((item) => item.id !== tab.id);
  next.push(tab);
  return next;
}

export function browserPayloadNeedsTabSync(
  item: CanvasItem,
  payload: BrowserCanvasPayload | null,
  tab: BrowserTab,
  fallbackTitle: string,
): boolean {
  const title = browserTabTitle(tab, fallbackTitle);
  const faviconURL = browserTabFaviconURL(tab);
  return (
    item.title !== title ||
    payload?.tabID !== tab.id ||
    payload?.url !== tab.url ||
    payload?.title !== title ||
    (payload?.faviconURL || "") !== faviconURL ||
    (payload?.mode || "") !== (tab.mode || "")
  );
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

export function renderedBrowserImageRect(image: HTMLImageElement, fallbackWidth: number, fallbackHeight: number) {
  const rect = image.getBoundingClientRect();
  const mediaWidth = image.naturalWidth || fallbackWidth;
  const mediaHeight = image.naturalHeight || fallbackHeight;
  if (rect.width <= 0 || rect.height <= 0 || mediaWidth <= 0 || mediaHeight <= 0) {
    return null;
  }
  const mediaRatio = mediaWidth / mediaHeight;
  const rectRatio = rect.width / rect.height;
  if (rectRatio > mediaRatio) {
    const width = rect.height * mediaRatio;
    return {
      left: rect.left + (rect.width - width) / 2,
      top: rect.top,
      width,
      height: rect.height,
    };
  }
  const height = rect.width / mediaRatio;
  return {
    left: rect.left,
    top: rect.top,
    width: rect.width,
    height,
  };
}

export function browserMouseButton(button: number): string {
  switch (button) {
    case 1:
      return "middle";
    case 2:
      return "right";
    case 3:
      return "back";
    case 4:
      return "forward";
    default:
      return "left";
  }
}

export function browserMouseButtonMask(button: number): number {
  switch (button) {
    case 1:
      return 4;
    case 2:
      return 2;
    case 3:
      return 8;
    case 4:
      return 16;
    default:
      return 1;
  }
}

export function browserModifiers(event: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): number {
  return (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0);
}

export function isPlainTextKey(event: { altKey: boolean; ctrlKey: boolean; key: string; metaKey: boolean }): boolean {
  return event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey;
}

export function browserClipboardShortcut(event: {
  altKey: boolean;
  ctrlKey: boolean;
  key: string;
  metaKey: boolean;
  shiftKey: boolean;
}): "copy" | "cut" | "paste" | "redo" | "selectAll" | "undo" | null {
  if (event.altKey || (!event.metaKey && !event.ctrlKey)) {
    return null;
  }
  switch (event.key.toLowerCase()) {
    case "a":
      return "selectAll";
    case "c":
      return "copy";
    case "x":
      return "cut";
    case "v":
      return "paste";
    case "y":
      return event.ctrlKey && !event.metaKey ? "redo" : null;
    case "z":
      return event.shiftKey ? "redo" : "undo";
    default:
      return null;
  }
}

export function browserKeyMessage(
  event: {
    altKey: boolean;
    code: string;
    ctrlKey: boolean;
    key: string;
    metaKey: boolean;
    shiftKey: boolean;
  },
  eventType: "keyDown" | "keyUp",
): Record<string, unknown> {
  const text = eventType === "keyDown" && event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey ? event.key : "";
  const virtualKeyCode = browserVirtualKeyCode(event);
  return {
    type: "key",
    eventType,
    key: event.key,
    code: event.code,
    text,
    unmodifiedText: text,
    windowsVirtualKeyCode: virtualKeyCode,
    nativeVirtualKeyCode: virtualKeyCode,
    modifiers: browserModifiers(event),
  };
}

export function browserPayloadForItem(item: CanvasItem): BrowserCanvasPayload | null {
  const payload = asRecord(item.item);
  const kind = stringValue(payload?.kind) || item.kind;
  if (kind !== "browser") {
    return null;
  }
  const sessionID = stringValue(payload?.sessionID) || item.sourceSessionID;
  if (!sessionID) {
    return null;
  }
  return {
    kind: "browser",
    sessionID,
    tabID: stringValue(payload?.tabID) || undefined,
    url: stringValue(payload?.url) || undefined,
    title: stringValue(payload?.title) || item.title || undefined,
    faviconURL: stringValue(payload?.faviconURL) || undefined,
    mode: payload?.mode === "external" ? "external" : payload?.mode === "headless" ? "headless" : undefined,
  };
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

function browserTabTimestamp(tab: BrowserTab): number {
  const updated = Date.parse(tab.updatedAt);
  if (Number.isFinite(updated)) {
    return updated;
  }
  const created = Date.parse(tab.createdAt);
  return Number.isFinite(created) ? created : 0;
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

function browserVirtualKeyCode(event: { code: string; key: string }): number {
  if (event.code.startsWith("Key") && event.code.length === 4) {
    return event.code.charCodeAt(3);
  }
  if (event.code.startsWith("Digit") && event.code.length === 6) {
    return event.code.charCodeAt(5);
  }
  const map: Record<string, number> = {
    Backspace: 8,
    Tab: 9,
    Enter: 13,
    Shift: 16,
    Control: 17,
    Alt: 18,
    Escape: 27,
    " ": 32,
    PageUp: 33,
    PageDown: 34,
    End: 35,
    Home: 36,
    ArrowLeft: 37,
    ArrowUp: 38,
    ArrowRight: 39,
    ArrowDown: 40,
    Delete: 46,
  };
  return map[event.key] || 0;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}
