import {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import {
  ElectronWebviewBrowser,
  type ElectronWebviewRuntimeHandle,
} from "@/browser/ElectronWebviewBrowser";
import { electronBrowserBridge } from "@/browser/electronBridge";
import { activateBrowserPageFindRegion } from "@/browser/pageFindTarget";
import {
  useElectronRequiredBrowserTabs,
  type ElectronBrowserSurfaceTab,
} from "@/browser/useElectronRequiredBrowserTabs";
import { Camera, Check } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";

type RuntimeEntry = {
  handle: ElectronWebviewRuntimeHandle;
  host: HTMLDivElement;
  presentation: string;
};

type ActiveViewport = {
  key: string;
};

type AutomationLease = {
  action: "click" | "screenshot";
  captureStatus: "running" | "success";
  complete: (ok: boolean) => void;
  completed: boolean;
  key: string;
  previewDataURL: string;
  presentationFrameReady: boolean;
};

type BrowserRuntimeContextValue = {
  requiredTabsBySession: Record<string, ElectronBrowserSurfaceTab[]>;
  registerRuntime: (
    key: string,
    host: HTMLDivElement,
    handle: ElectronWebviewRuntimeHandle,
  ) => () => void;
  retainTabs: (sessionID: string, tabs: ElectronBrowserSurfaceTab[]) => void;
  setViewport: (viewport: ActiveViewport | null) => void;
};

const emptyRuntimeTabs: ElectronBrowserSurfaceTab[] = [];
const screenshotPreviewExitDelayMs = 2_000;

const BrowserRuntimeContext = createContext<BrowserRuntimeContextValue | null>(null);

export function BrowserRuntimeProvider({
  children,
  token,
}: {
  children: ReactNode;
  token: string;
}) {
  const requiredTabsBySession = useElectronRequiredBrowserTabs(token);
  const [retainedTabsBySession, setRetainedTabsBySession] = useState<
    Record<string, ElectronBrowserSurfaceTab[]>
  >({});
  const runtimesRef = useRef(new Map<string, RuntimeEntry>());
  const viewportRef = useRef<ActiveViewport | null>(null);
  const automationFrameRef = useRef<number | undefined>(undefined);
  const automationHideTimerRef = useRef<number | undefined>(undefined);
  const automationLeaseRef = useRef<AutomationLease | null>(null);

  useEffect(() => {
    setRetainedTabsBySession({});
  }, [token]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onUpdated) {
      return;
    }
    return bridge.onUpdated((snapshot) => {
      if (snapshot.status !== "lost") {
        return;
      }
      setRetainedTabsBySession((current) => removeRuntimeTab(current, snapshot.sessionID, snapshot.tabID));
    });
  }, []);

  const runtimeTabsBySession = useMemo(() => {
    const next = { ...retainedTabsBySession };
    Object.entries(requiredTabsBySession).forEach(([sessionID, requiredTabs]) => {
      next[sessionID] = mergeRuntimeTabs(next[sessionID], requiredTabs);
    });
    return next;
  }, [requiredTabsBySession, retainedTabsBySession]);

  const applyPresentations = useCallback(() => {
    const automation = automationLeaseRef.current;
    const viewport = viewportRef.current;

    runtimesRef.current.forEach((runtime, key) => {
      if (automation?.key === key) {
        if (automation.action === "screenshot") {
          runtime.host.dataset.captureStatus = automation.captureStatus;
          applyRuntimeCapturePreview(runtime, automation.previewDataURL);
          applyRuntimePresentation(runtime, "preview", false);
        } else {
          delete runtime.host.dataset.captureStatus;
          applyRuntimeCapturePreview(runtime, "");
          applyRuntimePresentation(runtime, "automation", viewport?.key === key);
        }
        return;
      }
      delete runtime.host.dataset.captureStatus;
      applyRuntimeCapturePreview(runtime, "");
      if (!automation && viewport?.key === key) {
        applyRuntimePresentation(runtime, "visible", true);
        return;
      }
      applyRuntimePresentation(runtime, "standby", false);
    });
  }, []);

  const registerRuntime = useCallback((
    key: string,
    host: HTMLDivElement,
    handle: ElectronWebviewRuntimeHandle,
  ) => {
    const entry: RuntimeEntry = {
      handle,
      host,
      presentation: "",
    };
    runtimesRef.current.set(key, entry);
    applyPresentations();
    return () => {
      if (runtimesRef.current.get(key) !== entry) {
        return;
      }
      entry.handle.releaseAutomationFocus();
      runtimesRef.current.delete(key);
      const lease = automationLeaseRef.current;
      if (lease?.key === key) {
        completeAutomationLease(lease, false);
        automationLeaseRef.current = null;
        applyPresentations();
      }
    };
  }, [applyPresentations]);

  const retainTabs = useCallback((sessionID: string, tabs: ElectronBrowserSurfaceTab[]) => {
    if (!sessionID) {
      return;
    }
    setRetainedTabsBySession((current) => {
      if (sameRuntimeTabs(current[sessionID], tabs)) {
        return current;
      }
      const next = { ...current };
      if (tabs.length > 0) {
        next[sessionID] = tabs;
      } else {
        delete next[sessionID];
      }
      return next;
    });
  }, []);

  const setViewport = useCallback((viewport: ActiveViewport | null) => {
    viewportRef.current = viewport;
    applyPresentations();
  }, [applyPresentations]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge?.onAutomationStart || !bridge.onAutomationEnd || !bridge.completeAutomationLifecycle) {
      return;
    }

    const complete = (requestID: string, ok: boolean) => {
      if (!requestID) {
        return;
      }
      void bridge.completeAutomationLifecycle?.({ requestID, ok }).catch(() => undefined);
    };
    const stopStart = bridge.onAutomationStart((event) => {
      if ((event.action !== "click" && event.action !== "screenshot") || !event.requestID) {
        return;
      }
      window.clearTimeout(automationHideTimerRef.current);
      window.cancelAnimationFrame(automationFrameRef.current || 0);
      const previousLease = automationLeaseRef.current;
      if (previousLease) {
        runtimesRef.current.get(previousLease.key)?.handle.releaseAutomationFocus();
        completeAutomationLease(previousLease, false);
      }
      const lease: AutomationLease = {
        action: event.action,
        captureStatus: "running",
        complete: (ok: boolean) => complete(event.requestID || "", ok),
        completed: false,
        key: runtimeKey(event.sessionID, event.tabID),
        previewDataURL: "",
        presentationFrameReady: false,
      };
      automationLeaseRef.current = lease;
      applyPresentations();
      const deadline = performance.now() + 1_200;
      const focusWhenReady = () => {
        if (automationLeaseRef.current !== lease) {
          return;
        }
        const runtime = runtimesRef.current.get(lease.key);
        const rect = runtime?.host.getBoundingClientRect();
        if (runtime && rect && rect.width > 0 && rect.height > 0) {
          if (
            lease.action === "screenshot"
            && runtime.handle.readyForCapture()
          ) {
            if (lease.presentationFrameReady) {
              completeAutomationLease(lease, true);
              return;
            }
            lease.presentationFrameReady = true;
          } else if (lease.action === "click" && runtime.handle.focusForAutomation()) {
            completeAutomationLease(lease, true);
            return;
          }
        }
        if (performance.now() >= deadline) {
          automationLeaseRef.current = null;
          runtime?.handle.releaseAutomationFocus();
          applyPresentations();
          completeAutomationLease(lease, false);
          return;
        }
        automationFrameRef.current = window.requestAnimationFrame(focusWhenReady);
      };
      automationFrameRef.current = window.requestAnimationFrame(focusWhenReady);
    });
    const stopEnd = bridge.onAutomationEnd((event) => {
      if ((event.action !== "click" && event.action !== "screenshot") || !event.requestID) {
        return;
      }
      const lease = automationLeaseRef.current;
      if (lease?.key === runtimeKey(event.sessionID, event.tabID)) {
        window.cancelAnimationFrame(automationFrameRef.current || 0);
        runtimesRef.current.get(lease.key)?.handle.releaseAutomationFocus();
        if (lease.action === "screenshot" && event.ok === true) {
          lease.captureStatus = "success";
          lease.previewDataURL = event.previewDataURL || "";
          applyPresentations();
          automationHideTimerRef.current = window.setTimeout(() => {
            if (automationLeaseRef.current !== lease) {
              return;
            }
            automationLeaseRef.current = null;
            applyPresentations();
          }, screenshotPreviewExitDelayMs);
        } else {
          automationLeaseRef.current = null;
          applyPresentations();
        }
      }
      complete(event.requestID, true);
    });
    return () => {
      stopStart();
      stopEnd();
      window.clearTimeout(automationHideTimerRef.current);
      window.cancelAnimationFrame(automationFrameRef.current || 0);
      const lease = automationLeaseRef.current;
      if (lease) {
        runtimesRef.current.get(lease.key)?.handle.releaseAutomationFocus();
        completeAutomationLease(lease, false);
        automationLeaseRef.current = null;
      }
    };
  }, [applyPresentations]);

  const context = useMemo<BrowserRuntimeContextValue>(() => ({
    registerRuntime,
    requiredTabsBySession,
    retainTabs,
    setViewport,
  }), [registerRuntime, requiredTabsBySession, retainTabs, setViewport]);

  return (
    <BrowserRuntimeContext.Provider value={context}>
      {children}
      <div
        className="pudding-browser-keepalive-layer pointer-events-none fixed inset-0 z-20 overflow-hidden"
      >
        {Object.entries(runtimeTabsBySession).flatMap(([sessionID, tabs]) =>
          tabs.map((tab) => (
            <BrowserRuntimeHost
              key={runtimeKey(sessionID, tab.id)}
              registerRuntime={registerRuntime}
              sessionID={sessionID}
              tab={tab}
              token={token}
            />
          )),
        )}
      </div>
    </BrowserRuntimeContext.Provider>
  );
}

export const BrowserViewportPlaceholder = memo(function BrowserViewportPlaceholder({
  active,
  sessionID,
  tabID,
}: {
  active: boolean;
  sessionID: string;
  tabID?: string;
}) {
  const { setViewport } = useBrowserRuntimeContext();
  const key = tabID ? runtimeKey(sessionID, tabID) : "";
  const anchorName = tabID ? runtimeAnchorName(sessionID, tabID) : "--pudding-browser-none";

  useLayoutEffect(() => {
    if (!active || !key) {
      return;
    }
    setViewport({ key });
    return () => setViewport(null);
  }, [active, key, setViewport]);

  return (
    <div
      aria-hidden="true"
      className="pudding-browser-viewport-placeholder absolute inset-0"
      style={{ "--pudding-browser-anchor": anchorName } as CSSProperties}
    />
  );
});

export const BrowserViewportOverlay = memo(function BrowserViewportOverlay({
  active,
  children,
  sessionID,
  tabID,
}: {
  active: boolean;
  children: ReactNode;
  sessionID: string;
  tabID?: string;
}) {
  if (!active || !tabID) {
    return null;
  }
  const anchorName = runtimeAnchorName(sessionID, tabID);
  return createPortal(
    <div
      className="pudding-browser-viewport-overlay pointer-events-none fixed z-40 overflow-visible"
      style={{ "--pudding-browser-anchor": anchorName } as CSSProperties}
    >
      {children}
    </div>,
    document.body,
  );
});

export function useBrowserRuntimeTabs(sessionID: string, tabs: ElectronBrowserSurfaceTab[]) {
  const { requiredTabsBySession, retainTabs } = useBrowserRuntimeContext();
  useEffect(() => {
    retainTabs(sessionID, tabs);
  }, [retainTabs, sessionID, tabs]);
  return requiredTabsBySession[sessionID] || emptyRuntimeTabs;
}

const BrowserRuntimeHost = memo(function BrowserRuntimeHost({
  registerRuntime,
  sessionID,
  tab,
  token,
}: {
  registerRuntime: BrowserRuntimeContextValue["registerRuntime"];
  sessionID: string;
  tab: ElectronBrowserSurfaceTab;
  token: string;
}) {
  const { t } = useI18n();
  const hostRef = useRef<HTMLDivElement | null>(null);
  const runtimeRef = useRef<ElectronWebviewRuntimeHandle | null>(null);
  const key = runtimeKey(sessionID, tab.id);
  const anchorName = runtimeAnchorName(sessionID, tab.id);

  useLayoutEffect(() => {
    const host = hostRef.current;
    const handle = runtimeRef.current;
    if (!host || !handle) {
      return;
    }
    return registerRuntime(key, host, handle);
  }, [key, registerRuntime]);

  return (
    <div
      ref={hostRef}
      className="pudding-browser-runtime-host pointer-events-none invisible fixed top-0 left-0 overflow-hidden opacity-0"
      onFocusCapture={activateBrowserPageFindRegion}
      onPointerDownCapture={activateBrowserPageFindRegion}
      style={{ "--pudding-browser-anchor": anchorName } as CSSProperties}
    >
      <div className="pudding-browser-capture-chrome" role="status">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          <Camera className="size-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-xs font-medium text-foreground">{t("transcript.toolBrowserScreenshot")}</span>
          <span className="block truncate text-[11px] text-muted-foreground">{tab.title || tab.url}</span>
        </span>
        <Spinner className="pudding-browser-capture-progress size-3.5 shrink-0 text-muted-foreground" />
      </div>
      <div className="pudding-browser-runtime-surface">
        <ElectronWebviewBrowser ref={runtimeRef} activeTab={tab} sessionID={sessionID} token={token} />
      </div>
      <div className="pudding-browser-capture-preview" aria-hidden="true">
        <img
          alt=""
          className="pudding-browser-capture-preview-image h-full w-full object-contain"
          data-browser-capture-preview="true"
          hidden
        />
        <span className="pudding-browser-capture-placeholder flex size-10 items-center justify-center rounded-xl bg-muted text-muted-foreground">
          <Camera className="size-5" />
        </span>
      </div>
      <span className="pudding-browser-capture-success hidden items-center justify-center rounded-full bg-emerald-500 text-white shadow-lg ring-2 ring-white/80">
        <Check aria-hidden="true" className="size-5" strokeWidth={3} />
      </span>
    </div>
  );
});

function useBrowserRuntimeContext() {
  const context = useContext(BrowserRuntimeContext);
  if (!context) {
    throw new Error("BrowserRuntimeProvider is missing");
  }
  return context;
}

function runtimeKey(sessionID: string, tabID: string) {
  return `${sessionID}\u0000${tabID}`;
}

function runtimeAnchorName(sessionID: string, tabID: string) {
  return `--pudding-browser-${Array.from(
    runtimeKey(sessionID, tabID),
    (character) => character.codePointAt(0)?.toString(16) || "0",
  ).join("-")}`;
}

function completeAutomationLease(lease: AutomationLease, ok: boolean) {
  if (lease.completed) {
    return;
  }
  lease.completed = true;
  lease.complete(ok);
}

function applyRuntimePresentation(
  runtime: RuntimeEntry,
  mode: "standby" | "visible" | "automation" | "preview",
  anchored: boolean,
) {
  const presentation = `${mode}:${anchored ? "anchored" : "standby"}`;
  if (runtime.presentation === presentation) {
    return;
  }
  runtime.presentation = presentation;
  runtime.host.dataset.anchored = anchored ? "true" : "false";
  runtime.host.dataset.presentation = mode;
  runtime.host.setAttribute("aria-hidden", mode === "visible" || mode === "preview" ? "false" : "true");
}

function applyRuntimeCapturePreview(runtime: RuntimeEntry, source: string) {
  const image = runtime.host.querySelector<HTMLImageElement>("[data-browser-capture-preview]");
  if (!image) {
    return;
  }
  if (!source) {
    image.removeAttribute("src");
    image.hidden = true;
    return;
  }
  image.src = source;
  image.hidden = false;
}

function mergeRuntimeTabs(
  current: ElectronBrowserSurfaceTab[] | undefined,
  incoming: ElectronBrowserSurfaceTab[],
) {
  const next = [...(current || [])];
  incoming.forEach((tab) => {
    const index = next.findIndex((entry) => entry.id === tab.id);
    if (index >= 0) {
      next[index] = tab;
    } else {
      next.push(tab);
    }
  });
  return next;
}

function removeRuntimeTab(
  current: Record<string, ElectronBrowserSurfaceTab[]>,
  sessionID: string,
  tabID: string,
) {
  const tabs = current[sessionID];
  if (!tabs?.some((tab) => tab.id === tabID)) {
    return current;
  }
  const next = { ...current };
  const remaining = tabs.filter((tab) => tab.id !== tabID);
  if (remaining.length > 0) {
    next[sessionID] = remaining;
  } else {
    delete next[sessionID];
  }
  return next;
}

function sameRuntimeTabs(
  current: ElectronBrowserSurfaceTab[] | undefined,
  next: ElectronBrowserSurfaceTab[],
) {
  return Boolean(
    current &&
      current.length === next.length &&
      current.every((tab, index) => tab === next[index]),
  );
}
