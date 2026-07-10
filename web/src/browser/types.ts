import type { BrowserTab } from "@/api/client";

export type BrowserCanvasPayload = {
  kind: "browser";
  sessionID: string;
  tabID?: string;
  url?: string;
  title?: string;
  faviconURL?: string;
  mode?: BrowserProcessMode;
  closedAt?: string;
};

export type BrowserProcessMode = "headless" | "external";
export type BrowserTabsData = { tabs: BrowserTab[]; processMode?: BrowserProcessMode };
export type CanvasSurface = "canvas" | "browser" | "terminal";

export type BrowserNavigationAction = "back" | "forward" | "reload";
