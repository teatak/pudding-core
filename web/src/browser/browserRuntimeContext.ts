import { createContext, useContext } from "react";

import type { ElectronWebviewRuntimeHandle } from "@/browser/ElectronWebviewBrowser";
import type { ElectronBrowserAutomationEvent } from "@/browser/electronBridge";
import type { ElectronBrowserSurfaceTab } from "@/browser/useElectronRequiredBrowserTabs";

export type BrowserAutomationActivity = {
  action: ElectronBrowserAutomationEvent["action"];
  ok?: boolean;
  phase: "running" | "complete";
  presence: "visible" | "closing";
  sessionID: string;
  tabID: string;
};

export type BrowserRuntimeViewport = {
  clipElement?: HTMLElement;
  element: HTMLDivElement;
  interactive: boolean;
  key: string;
  pip?: boolean;
  pipEmbedded?: boolean;
  priority: number;
};

export type BrowserRuntimeContextValue = {
  automationActivitiesBySession: Record<string, BrowserAutomationActivity>;
  readyRuntimeKeys: Set<string>;
  requiredTabsBySession: Record<string, ElectronBrowserSurfaceTab[]>;
  runtimeTabsBySession: Record<string, ElectronBrowserSurfaceTab[]>;
  registerRuntime: (
    key: string,
    host: HTMLDivElement,
    handle: ElectronWebviewRuntimeHandle,
  ) => () => void;
  registerViewport: (
    viewportID: string,
    viewport: BrowserRuntimeViewport,
  ) => () => void;
  finishAutomationActivity: (
    sessionID: string,
    activity: BrowserAutomationActivity,
  ) => void;
  retainTabs: (sessionID: string, tabs: ElectronBrowserSurfaceTab[]) => void;
};

export const BrowserRuntimeContext = createContext<BrowserRuntimeContextValue | null>(null);

export function useBrowserRuntimeContext() {
  const context = useContext(BrowserRuntimeContext);
  if (!context) {
    throw new Error("BrowserRuntimeProvider is missing");
  }
  return context;
}
