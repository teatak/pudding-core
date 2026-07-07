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
export type CanvasSurface = "canvas" | "browser";

export type BrowserScreencastMetadata = {
  deviceWidth?: number;
  deviceHeight?: number;
  pageScaleFactor?: number;
  scrollOffsetX?: number;
  scrollOffsetY?: number;
  offsetTop?: number;
};

export type BrowserScreencastFrame = {
  type: "frame";
  mime: string;
  data: string;
  metadata?: BrowserScreencastMetadata;
};

export type BrowserScreencastCaret = {
  type: "caret";
  x: number;
  y: number;
  height?: number;
  visible: boolean;
};

export type BrowserScreencastCursor = {
  type: "cursor";
  x: number;
  y: number;
  action?: string;
  createdAt?: string;
};

export type BrowserScreencastClipboard = {
  type: "clipboard";
  action: "copy" | "cut" | "paste" | "redo" | "selectAll" | "undo";
  ok?: boolean;
  text?: string;
  error?: string;
};

export type BrowserScreencastMessage =
  | BrowserScreencastFrame
  | BrowserScreencastCaret
  | BrowserScreencastCursor
  | BrowserScreencastClipboard
  | { type: "status"; status: string }
  | { type: "error"; error: string };

export type BrowserNavigationAction = "back" | "forward" | "reload";
