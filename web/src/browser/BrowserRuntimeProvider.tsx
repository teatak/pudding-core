import {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useId,
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
import { BrowserFavicon } from "@/browser/BrowserFavicon";
import {
  electronBrowserBridge,
  type ElectronBrowserAutomationEvent,
} from "@/browser/electronBridge";
import { activateBrowserPageFindRegion } from "@/browser/pageFindTarget";
import {
  useElectronRequiredBrowserTabs,
  type ElectronBrowserSurfaceTab,
} from "@/browser/useElectronRequiredBrowserTabs";
import { Check, Globe, X } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import { useWorkspaceOpen } from "@/state/workspaceStore";

type RuntimeEntry = {
  handle: ElectronWebviewRuntimeHandle;
  host: HTMLDivElement;
  presentation: string;
};

type ActiveViewport = {
  interactive: boolean;
  key: string;
  pipPhase?: BrowserAutomationActivity["phase"];
  priority: number;
};

type AutomationLease = {
  action: "click" | "screenshot";
  complete: (ok: boolean) => void;
  completed: boolean;
  frameID?: number;
  key: string;
  presentationFrameReady: boolean;
};

type BrowserAutomationActivity = {
  action: ElectronBrowserAutomationEvent["action"];
  ok?: boolean;
  phase: "running" | "complete";
  sessionID: string;
  tabID: string;
};

type BrowserRuntimeContextValue = {
  automationActivitiesBySession: Record<string, BrowserAutomationActivity>;
  requiredTabsBySession: Record<string, ElectronBrowserSurfaceTab[]>;
  runtimeTabsBySession: Record<string, ElectronBrowserSurfaceTab[]>;
  registerRuntime: (
    key: string,
    host: HTMLDivElement,
    handle: ElectronWebviewRuntimeHandle,
  ) => () => void;
  registerViewport: (
    viewportID: string,
    viewport: ActiveViewport,
  ) => () => void;
  retainTabs: (sessionID: string, tabs: ElectronBrowserSurfaceTab[]) => void;
};

const emptyRuntimeTabs: ElectronBrowserSurfaceTab[] = [];
const automationPipCloseDelayMs = 30_000;
const pipViewportPriority = 10;
const workspaceViewportPriority = 20;

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
  const [automationActivitiesBySession, setAutomationActivitiesBySession] = useState<
    Record<string, BrowserAutomationActivity>
  >({});
  const automationActivitiesRef = useRef<Record<string, BrowserAutomationActivity>>({});
  const runtimesRef = useRef(new Map<string, RuntimeEntry>());
  const viewportsRef = useRef(new Map<string, ActiveViewport>());
  const automationHideTimersRef = useRef(new Map<string, number>());
  const automationLeasesRef = useRef(new Map<string, AutomationLease>());

  useEffect(() => {
    setRetainedTabsBySession({});
    automationHideTimersRef.current.forEach((timer) => window.clearTimeout(timer));
    automationHideTimersRef.current.clear();
    automationActivitiesRef.current = {};
    setAutomationActivitiesBySession({});
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
    const viewport = selectActiveViewport(viewportsRef.current);

    runtimesRef.current.forEach((runtime, key) => {
      const automation = automationLeasesRef.current.get(key);
      if (automation) {
        const automationViewport = selectActiveViewport(viewportsRef.current, key);
        applyRuntimePresentation(
          runtime,
          "automation",
          Boolean(automationViewport),
          false,
          automationViewport?.pipPhase,
        );
        return;
      }
      if (viewport?.key === key) {
        applyRuntimePresentation(
          runtime,
          "visible",
          true,
          viewport.interactive,
          viewport.pipPhase,
        );
        return;
      }
      applyRuntimePresentation(runtime, "standby", false, false);
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
      const lease = automationLeasesRef.current.get(key);
      if (lease) {
        window.cancelAnimationFrame(lease.frameID || 0);
        completeAutomationLease(lease, false);
        automationLeasesRef.current.delete(key);
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

  const registerViewport = useCallback((viewportID: string, viewport: ActiveViewport) => {
    viewportsRef.current.set(viewportID, viewport);
    applyPresentations();
    return () => {
      if (viewportsRef.current.get(viewportID) !== viewport) {
        return;
      }
      viewportsRef.current.delete(viewportID);
      applyPresentations();
    };
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
      window.clearTimeout(automationHideTimersRef.current.get(event.sessionID));
      automationHideTimersRef.current.delete(event.sessionID);
      const activity: BrowserAutomationActivity = {
        action: event.action,
        phase: "running",
        sessionID: event.sessionID,
        tabID: event.tabID,
      };
      const nextActivities = {
        ...automationActivitiesRef.current,
        [event.sessionID]: activity,
      };
      automationActivitiesRef.current = nextActivities;
      setAutomationActivitiesBySession(nextActivities);

      if (
        !event.requestID
        || (event.action !== "click" && event.action !== "screenshot")
      ) {
        return;
      }
      const key = runtimeKey(event.sessionID, event.tabID);
      const previousLease = automationLeasesRef.current.get(key);
      if (previousLease) {
        window.cancelAnimationFrame(previousLease.frameID || 0);
        runtimesRef.current.get(previousLease.key)?.handle.releaseAutomationFocus();
        completeAutomationLease(previousLease, false);
      }
      const lease: AutomationLease = {
        action: event.action,
        complete: (ok: boolean) => complete(event.requestID || "", ok),
        completed: false,
        key,
        presentationFrameReady: false,
      };
      automationLeasesRef.current.set(key, lease);
      applyPresentations();
      const deadline = performance.now() + 1_200;
      const focusWhenReady = () => {
        if (automationLeasesRef.current.get(key) !== lease) {
          return;
        }
        const runtime = runtimesRef.current.get(lease.key);
        const rect = runtime?.host.getBoundingClientRect();
        if (runtime && rect && rect.width > 0 && rect.height > 0) {
          if (!lease.presentationFrameReady) {
            lease.presentationFrameReady = true;
          } else if (lease.action === "screenshot" && runtime.handle.readyForCapture()) {
            completeAutomationLease(lease, true);
            return;
          } else if (lease.action === "click" && runtime.handle.focusForAutomation()) {
            completeAutomationLease(lease, true);
            return;
          }
        }
        if (performance.now() >= deadline) {
          automationLeasesRef.current.delete(key);
          runtime?.handle.releaseAutomationFocus();
          applyPresentations();
          completeAutomationLease(lease, false);
          return;
        }
        lease.frameID = window.requestAnimationFrame(focusWhenReady);
      };
      lease.frameID = window.requestAnimationFrame(focusWhenReady);
    });
    const stopEnd = bridge.onAutomationEnd((event) => {
      const currentActivity = automationActivitiesRef.current[event.sessionID];
      if (
        currentActivity?.tabID === event.tabID
        && currentActivity.action === event.action
      ) {
        const completedActivity: BrowserAutomationActivity = {
          ...currentActivity,
          ok: event.ok,
          phase: "complete",
        };
        const nextActivities = {
          ...automationActivitiesRef.current,
          [event.sessionID]: completedActivity,
        };
        automationActivitiesRef.current = nextActivities;
        setAutomationActivitiesBySession(nextActivities);
        window.clearTimeout(automationHideTimersRef.current.get(event.sessionID));
        const timer = window.setTimeout(() => {
          if (automationActivitiesRef.current[event.sessionID] !== completedActivity) {
            return;
          }
          const remainingActivities = { ...automationActivitiesRef.current };
          delete remainingActivities[event.sessionID];
          automationActivitiesRef.current = remainingActivities;
          automationHideTimersRef.current.delete(event.sessionID);
          setAutomationActivitiesBySession(remainingActivities);
        }, automationPipCloseDelayMs);
        automationHideTimersRef.current.set(event.sessionID, timer);
      }
      const key = runtimeKey(event.sessionID, event.tabID);
      const lease = automationLeasesRef.current.get(key);
      if (
        lease?.action === event.action
      ) {
        window.cancelAnimationFrame(lease.frameID || 0);
        runtimesRef.current.get(lease.key)?.handle.releaseAutomationFocus();
        automationLeasesRef.current.delete(key);
        applyPresentations();
      }
      complete(event.requestID || "", true);
    });
    return () => {
      stopStart();
      stopEnd();
      automationHideTimersRef.current.forEach((timer) => window.clearTimeout(timer));
      automationHideTimersRef.current.clear();
      automationLeasesRef.current.forEach((lease) => {
        window.cancelAnimationFrame(lease.frameID || 0);
        runtimesRef.current.get(lease.key)?.handle.releaseAutomationFocus();
        completeAutomationLease(lease, false);
      });
      automationLeasesRef.current.clear();
    };
  }, [applyPresentations]);

  const context = useMemo<BrowserRuntimeContextValue>(() => ({
    automationActivitiesBySession,
    registerRuntime,
    registerViewport,
    requiredTabsBySession,
    retainTabs,
    runtimeTabsBySession,
  }), [
    automationActivitiesBySession,
    registerRuntime,
    registerViewport,
    requiredTabsBySession,
    retainTabs,
    runtimeTabsBySession,
  ]);

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
  interactive = true,
  pipPhase,
  priority = workspaceViewportPriority,
  sessionID,
  tabID,
}: {
  active: boolean;
  interactive?: boolean;
  pipPhase?: BrowserAutomationActivity["phase"];
  priority?: number;
  sessionID: string;
  tabID?: string;
}) {
  const { registerViewport } = useBrowserRuntimeContext();
  const viewportID = useId();
  const key = tabID ? runtimeKey(sessionID, tabID) : "";
  const anchorName = tabID ? runtimeAnchorName(sessionID, tabID) : "--pudding-browser-none";

  useLayoutEffect(() => {
    if (!active || !key) {
      return;
    }
    return registerViewport(viewportID, { interactive, key, pipPhase, priority });
  }, [active, interactive, key, pipPhase, priority, registerViewport, viewportID]);

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

export const BrowserAutomationPip = memo(function BrowserAutomationPip({
  sessionID,
}: {
  sessionID: string;
}) {
  const { t } = useI18n();
  const workspaceOpen = useWorkspaceOpen(sessionID);
  const { automationActivitiesBySession, runtimeTabsBySession } = useBrowserRuntimeContext();
  const automationActivity = automationActivitiesBySession[sessionID];
  if (!automationActivity || automationActivity.sessionID !== sessionID || workspaceOpen) {
    return null;
  }

  const tab = runtimeTabsBySession[sessionID]?.find((entry) => entry.id === automationActivity.tabID);
  const pageTitle = tab?.title?.trim()
    || browserHost(tab?.url)
    || t("browser.noTitle");
  const actionLabel = browserAutomationActionLabel(t, automationActivity.action);
  const completed = automationActivity.phase === "complete";
  const failed = completed && automationActivity.ok === false;

  return (
    <aside
      aria-label={`${actionLabel}: ${pageTitle}`}
      aria-live="polite"
      className="pudding-browser-pip pointer-events-none absolute right-4 z-10 w-[min(20rem,calc(100%-2rem))] overflow-hidden rounded-xl border border-border/80 bg-popover text-popover-foreground shadow-xl"
      data-phase={automationActivity.phase}
      role="status"
      style={{ bottom: "calc(var(--pudding-composer-overlay-height) + 0.75rem)" }}
    >
      <div className="flex h-10 items-center gap-2 border-b border-border/70 bg-popover/95 px-2.5 backdrop-blur-md">
        <span className="grid size-6 shrink-0 place-items-center overflow-hidden rounded-md bg-muted text-muted-foreground">
          <BrowserFavicon
            className="size-full rounded-sm object-cover"
            fallback={<Globe className="size-3.5" />}
            faviconURL={tab?.faviconURL}
            pageURL={tab?.url || ""}
          />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-xs font-medium">{actionLabel}</span>
          <span className="block truncate text-[11px] text-muted-foreground">{pageTitle}</span>
        </span>
        {completed ? (
          <span className={failed
            ? "grid size-5 shrink-0 place-items-center rounded-full bg-destructive text-white"
            : "grid size-5 shrink-0 place-items-center rounded-full bg-emerald-500 text-white"}
          >
            {failed
              ? <X aria-hidden="true" className="size-3" strokeWidth={3} />
              : <Check aria-hidden="true" className="size-3" strokeWidth={3} />}
          </span>
        ) : (
          <Spinner className="size-3.5 shrink-0 text-muted-foreground" />
        )}
      </div>
      <div className="relative aspect-video overflow-hidden bg-muted/50">
        <BrowserViewportPlaceholder
          active
          interactive={false}
          pipPhase={automationActivity.phase}
          priority={pipViewportPriority}
          sessionID={sessionID}
          tabID={automationActivity.tabID}
        />
      </div>
    </aside>
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

  useLayoutEffect(() => {
    const host = hostRef.current;
    if (!host) {
      return;
    }
    const observer = new ResizeObserver(([entry]) => {
      if (!entry) {
        return;
      }
      const styles = getComputedStyle(host);
      const runtimeWidth = Number.parseFloat(styles.getPropertyValue("--browser-runtime-width"));
      const runtimeHeight = Number.parseFloat(styles.getPropertyValue("--browser-runtime-height"));
      if (runtimeWidth <= 0 || runtimeHeight <= 0) {
        return;
      }
      const scale = Math.min(
        entry.contentRect.width / runtimeWidth,
        entry.contentRect.height / runtimeHeight,
      );
      host.style.setProperty("--pudding-browser-runtime-scale", String(scale));
    });
    observer.observe(host);
    return () => observer.disconnect();
  }, []);

  return (
    <div
      ref={hostRef}
      className="pudding-browser-runtime-host pointer-events-none invisible fixed top-0 left-0 overflow-hidden opacity-0"
      onFocusCapture={activateBrowserPageFindRegion}
      onPointerDownCapture={activateBrowserPageFindRegion}
      style={{ "--pudding-browser-anchor": anchorName } as CSSProperties}
    >
      <div className="pudding-browser-runtime-surface">
        <ElectronWebviewBrowser ref={runtimeRef} activeTab={tab} sessionID={sessionID} token={token} />
      </div>
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

function selectActiveViewport(
  viewports: Map<string, ActiveViewport>,
  key?: string,
) {
  return [...viewports.values()]
    .filter((viewport) => !key || viewport.key === key)
    .sort((left, right) => right.priority - left.priority)[0] || null;
}

function browserAutomationActionLabel(
  t: ReturnType<typeof useI18n>["t"],
  action: ElectronBrowserAutomationEvent["action"],
) {
  switch (action) {
    case "back":
      return t("transcript.toolBrowserBack");
    case "click":
      return t("transcript.toolBrowserClick");
    case "forward":
      return t("transcript.toolBrowserForward");
    case "observe":
      return t("transcript.toolBrowserObserve");
    case "open":
      return t("transcript.toolBrowserOpen");
    case "reload":
      return t("transcript.toolBrowserReload");
    case "screenshot":
      return t("transcript.toolBrowserScreenshot");
    case "scroll":
      return t("transcript.toolBrowserScroll");
    case "type":
      return t("transcript.toolBrowserType");
  }
}

function browserHost(url: string | undefined) {
  if (!url || url === "about:blank") {
    return "";
  }
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
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
  mode: "standby" | "visible" | "automation",
  anchored: boolean,
  interactive: boolean,
  pipPhase?: BrowserAutomationActivity["phase"],
) {
  const presentation = `${mode}:${anchored ? "anchored" : "standby"}:${interactive ? "interactive" : "passive"}:${pipPhase || "workspace"}`;
  if (runtime.presentation === presentation) {
    return;
  }
  runtime.presentation = presentation;
  runtime.host.dataset.anchored = anchored ? "true" : "false";
  runtime.host.dataset.interactive = interactive ? "true" : "false";
  runtime.host.dataset.presentation = mode;
  if (pipPhase) {
    runtime.host.dataset.pipPhase = pipPhase;
  } else {
    delete runtime.host.dataset.pipPhase;
  }
  runtime.host.setAttribute("aria-hidden", mode === "visible" && interactive ? "false" : "true");
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
