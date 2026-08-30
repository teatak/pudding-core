import {
  memo,
  useCallback,
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
  BrowserAutomationActivityContext,
  BrowserRuntimeContext,
  useBrowserRuntimeContext,
  useVisibleBrowserAutomationActivity,
  type BrowserAutomationActivity,
  type BrowserRuntimeContextValue,
  type BrowserRuntimeViewport,
} from "@/browser/browserRuntimeContext";
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
  presentationFrameID?: number;
};

type AutomationLease = {
  action: "click" | "screenshot";
  complete: (ok: boolean) => void;
  completed: boolean;
  frameID?: number;
  key: string;
  presentationFrameReady: boolean;
};

const automationPipCloseDelayMs = 30_000;
const pipViewportPriority = 10;
const workspaceViewportPriority = 20;

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
  const [readyRuntimeKeys, setReadyRuntimeKeys] = useState<Set<string>>(() => new Set());
  const automationActivitiesRef = useRef<Record<string, BrowserAutomationActivity>>({});
  const runtimesRef = useRef(new Map<string, RuntimeEntry>());
  const viewportsRef = useRef(new Map<string, BrowserRuntimeViewport>());
  const automationHideTimersRef = useRef(new Map<string, number>());
  const automationLeasesRef = useRef(new Map<string, AutomationLease>());
  const retainedTokenRef = useRef(token);

  useEffect(() => {
    if (retainedTokenRef.current === token) {
      return;
    }
    retainedTokenRef.current = token;
    setRetainedTabsBySession({});
    automationHideTimersRef.current.forEach((timer) => window.clearTimeout(timer));
    automationHideTimersRef.current.clear();
    automationActivitiesRef.current = {};
    setAutomationActivitiesBySession({});
    setReadyRuntimeKeys(new Set());
  }, [token]);

  const setRuntimeReady = useCallback((key: string, ready: boolean) => {
    setReadyRuntimeKeys((current) => {
      if (current.has(key) === ready) {
        return current;
      }
      const next = new Set(current);
      if (ready) {
        next.add(key);
      } else {
        next.delete(key);
      }
      return next;
    });
  }, []);

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
      setRuntimeReady(runtimeKey(snapshot.sessionID, snapshot.tabID), false);
      const activity = automationActivitiesRef.current[snapshot.sessionID];
      if (activity?.tabID === snapshot.tabID) {
        window.clearTimeout(automationHideTimersRef.current.get(snapshot.sessionID));
        automationHideTimersRef.current.delete(snapshot.sessionID);
        const remainingActivities = { ...automationActivitiesRef.current };
        delete remainingActivities[snapshot.sessionID];
        automationActivitiesRef.current = remainingActivities;
        setAutomationActivitiesBySession(remainingActivities);
      }
    });
  }, [setRuntimeReady]);

  const runtimeTabsBySession = useMemo(() => {
    const next = retainedTokenRef.current === token ? { ...retainedTabsBySession } : {};
    Object.entries(requiredTabsBySession).forEach(([sessionID, requiredTabs]) => {
      next[sessionID] = mergeRuntimeTabs(next[sessionID], requiredTabs);
    });
    return next;
  }, [requiredTabsBySession, retainedTabsBySession, token]);
  const visibleAutomationActivitiesRef = useRef<Record<string, BrowserAutomationActivity>>({});
  const visibleAutomationActivitiesBySession = useMemo(() => {
    const next = Object.fromEntries(Object.entries(automationActivitiesBySession).filter(
      ([sessionID, activity]) => (
        readyRuntimeKeys.has(runtimeKey(sessionID, activity.tabID))
        && runtimeTabsBySession[sessionID]?.some((tab) => tab.id === activity.tabID)
      ),
    ));
    if (sameAutomationActivityRecord(visibleAutomationActivitiesRef.current, next)) {
      return visibleAutomationActivitiesRef.current;
    }
    visibleAutomationActivitiesRef.current = next;
    return next;
  }, [automationActivitiesBySession, readyRuntimeKeys, runtimeTabsBySession]);
  const applyPresentations = useCallback(() => {
    const viewport = selectActiveViewport(viewportsRef.current);

    runtimesRef.current.forEach((runtime, key) => {
      const automation = automationLeasesRef.current.get(key);
      if (automation) {
        const automationViewport = selectActiveViewport(viewportsRef.current, key);
        const anchored = Boolean(
          automationViewport && applyRuntimeGeometry(
            runtime.host,
            automationViewport.element,
            automationViewport.clipElement,
            automationViewport.interactive,
          ),
        );
        if (!anchored) {
          clearRuntimeGeometry(runtime.host);
        }
        applyRuntimePresentation(
          runtime,
          "automation",
          anchored,
          automationViewport?.interactive || false,
          automationViewport?.pip,
          automationViewport?.pipEmbedded,
        );
        return;
      }
      if (viewport?.key === key) {
        if (!applyRuntimeGeometry(
          runtime.host,
          viewport.element,
          viewport.clipElement,
          viewport.interactive,
        )) {
          clearRuntimeGeometry(runtime.host);
          applyRuntimePresentation(runtime, "standby", false, false);
          return;
        }
        applyRuntimePresentation(
          runtime,
          "visible",
          true,
          viewport.interactive,
          viewport.pip,
          viewport.pipEmbedded,
        );
        return;
      }
      clearRuntimeGeometry(runtime.host);
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
      window.cancelAnimationFrame(entry.presentationFrameID || 0);
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

  const finishAutomationActivity = useCallback((
    sessionID: string,
    activity: BrowserAutomationActivity,
  ) => {
    if (
      activity.presence !== "closing"
      || automationActivitiesRef.current[sessionID] !== activity
    ) {
      return;
    }
    const remainingActivities = { ...automationActivitiesRef.current };
    delete remainingActivities[sessionID];
    automationActivitiesRef.current = remainingActivities;
    setAutomationActivitiesBySession(remainingActivities);
  }, []);

  const registerViewport = useCallback((viewportID: string, viewport: BrowserRuntimeViewport) => {
    viewportsRef.current.set(viewportID, viewport);
    const syncGeometry = () => applyPresentations();
    let transitionFrameID = 0;
    const trackTransition = () => {
      transitionFrameID = 0;
      applyPresentations();
      if (workspaceTransitionInProgress(viewport.clipElement)) {
        transitionFrameID = window.requestAnimationFrame(trackTransition);
      }
    };
    const startTransitionTracking = () => {
      if (!transitionFrameID) {
        transitionFrameID = window.requestAnimationFrame(trackTransition);
      }
    };
    const resizeObserver = new ResizeObserver(syncGeometry);
    resizeObserver.observe(viewport.element);
    if (viewport.clipElement) {
      resizeObserver.observe(viewport.clipElement);
    }
    const transitionObserver = viewport.clipElement
      ? new MutationObserver(startTransitionTracking)
      : null;
    transitionObserver?.observe(viewport.clipElement!, {
      attributeFilter: ["data-transition"],
      attributes: true,
    });
    window.addEventListener("resize", syncGeometry);
    window.addEventListener("scroll", syncGeometry, true);
    applyPresentations();
    if (workspaceTransitionInProgress(viewport.clipElement)) {
      startTransitionTracking();
    }
    return () => {
      window.cancelAnimationFrame(transitionFrameID);
      transitionObserver?.disconnect();
      resizeObserver.disconnect();
      window.removeEventListener("resize", syncGeometry);
      window.removeEventListener("scroll", syncGeometry, true);
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
        presence: "visible",
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
      const completedActivity = currentActivity?.tabID === event.tabID
        && currentActivity.action === event.action
        ? {
            ...currentActivity,
            ok: event.ok,
            phase: "complete" as const,
          }
        : undefined;
      if (completedActivity) {
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
          automationHideTimersRef.current.delete(event.sessionID);
          const closingActivity: BrowserAutomationActivity = {
            ...completedActivity,
            presence: "closing",
          };
          const nextActivities = {
            ...automationActivitiesRef.current,
            [event.sessionID]: closingActivity,
          };
          automationActivitiesRef.current = nextActivities;
          setAutomationActivitiesBySession(nextActivities);
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
    finishAutomationActivity,
    readyRuntimeKeys,
    registerRuntime,
    registerViewport,
    requiredTabsBySession,
    retainTabs,
    runtimeTabsBySession,
  }), [
    automationActivitiesBySession,
    finishAutomationActivity,
    readyRuntimeKeys,
    registerRuntime,
    registerViewport,
    requiredTabsBySession,
    retainTabs,
    runtimeTabsBySession,
  ]);

  return (
    <BrowserAutomationActivityContext.Provider value={visibleAutomationActivitiesBySession}>
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
                setRuntimeReady={setRuntimeReady}
                sessionID={sessionID}
                tab={tab}
                token={token}
              />
            )),
          )}
        </div>
      </BrowserRuntimeContext.Provider>
    </BrowserAutomationActivityContext.Provider>
  );
}

export const BrowserViewportPlaceholder = memo(function BrowserViewportPlaceholder({
  active,
  interactive = true,
  pip = false,
  pipEmbedded,
  priority = workspaceViewportPriority,
  sessionID,
  tabID,
}: {
  active: boolean;
  interactive?: boolean;
  pip?: boolean;
  pipEmbedded?: boolean;
  priority?: number;
  sessionID: string;
  tabID?: string;
}) {
  const { registerViewport } = useBrowserRuntimeContext();
  const viewportID = useId();
  const elementRef = useRef<HTMLDivElement | null>(null);
  const key = tabID ? runtimeKey(sessionID, tabID) : "";
  const anchorName = tabID ? runtimeAnchorName(sessionID, tabID) : "--pudding-browser-none";

  useLayoutEffect(() => {
    const element = elementRef.current;
    if (!active || !key || !element) {
      return;
    }
    const clipElement = element.closest(
      ".pudding-browser-pip, .pudding-workspace-stage",
    ) as HTMLElement | null;
    return registerViewport(viewportID, {
      clipElement: clipElement || undefined,
      element,
      interactive,
      key,
      pip,
      pipEmbedded,
      priority,
    });
  }, [active, interactive, key, pip, pipEmbedded, priority, registerViewport, viewportID]);

  return (
    <div
      ref={elementRef}
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
  embedded = false,
  sessionID,
}: {
  embedded?: boolean;
  sessionID: string;
}) {
  const { t } = useI18n();
  const workspaceOpen = useWorkspaceOpen(sessionID);
  const {
    automationActivitiesBySession,
    finishAutomationActivity,
    readyRuntimeKeys,
    runtimeTabsBySession,
  } = useBrowserRuntimeContext();
  const automationActivity = automationActivitiesBySession[sessionID];
  const [entered, setEntered] = useState(false);
  const [surfaceKey, setSurfaceKey] = useState("");
  const entryKey = automationActivity
    ? runtimeKey(automationActivity.sessionID, automationActivity.tabID)
    : "";
  const tab = automationActivity
    ? runtimeTabsBySession[sessionID]?.find((entry) => entry.id === automationActivity.tabID)
    : undefined;
  const ready = Boolean(
    automationActivity
    && readyRuntimeKeys.has(runtimeKey(sessionID, automationActivity.tabID)),
  );
  const canPresent = Boolean(entryKey && !workspaceOpen && tab && ready);
  useLayoutEffect(() => {
    setEntered(false);
    setSurfaceKey("");
    if (!canPresent) {
      return;
    }
    let revealFrame = 0;
    const mountFrame = window.requestAnimationFrame(() => {
      revealFrame = window.requestAnimationFrame(() => setEntered(true));
    });
    return () => {
      window.cancelAnimationFrame(mountFrame);
      window.cancelAnimationFrame(revealFrame);
    };
  }, [canPresent, entryKey]);
  useLayoutEffect(() => {
    if (automationActivity?.presence === "closing") {
      setSurfaceKey("");
    }
  }, [automationActivity?.presence]);

  useEffect(() => {
    if (
      automationActivity?.presence === "closing"
      && (workspaceOpen || !tab || !ready)
    ) {
      finishAutomationActivity(sessionID, automationActivity);
    }
  }, [automationActivity, finishAutomationActivity, ready, sessionID, tab, workspaceOpen]);

  if (
    !automationActivity
    || automationActivity.sessionID !== sessionID
    || workspaceOpen
  ) {
    return null;
  }
  if (!tab || !ready) {
    return null;
  }
  const pageTitle = tab.title?.trim()
    || browserHost(tab.url)
    || t("browser.noTitle");
  const actionLabel = browserAutomationActionLabel(t, automationActivity.action);
  const completed = automationActivity.phase === "complete";
  const failed = completed && automationActivity.ok === false;

  return (
    <div
      className="pudding-browser-pip-presence"
      data-presence={automationActivity.presence === "closing"
        ? "closing"
        : entered ? "visible" : "opening"}
      onTransitionEnd={(event) => {
        if (event.currentTarget !== event.target || event.propertyName !== "grid-template-rows") {
          return;
        }
        if (automationActivity.presence === "closing") {
          finishAutomationActivity(sessionID, automationActivity);
          return;
        }
        setSurfaceKey(entryKey);
      }}
    >
      <aside
        aria-label={`${actionLabel}: ${pageTitle}`}
        aria-live="polite"
        className={embedded
          ? "pudding-browser-pip pointer-events-none relative w-full overflow-hidden border-t border-border/70 bg-popover text-popover-foreground"
          : "pudding-browser-pip pointer-events-none relative w-full overflow-hidden rounded-xl border border-border/80 bg-popover text-popover-foreground shadow-xl"}
        data-phase={automationActivity.phase}
        role="status"
      >
        {embedded ? null : <div className="flex min-h-11 items-center gap-2 border-b border-border/70 bg-popover/95 px-2.5 py-2 backdrop-blur-md">
        <span className="grid size-6 shrink-0 place-items-center overflow-hidden rounded-md bg-muted text-muted-foreground">
          <BrowserFavicon
            className="size-full rounded-sm object-cover"
            fallback={<Globe className="size-3.5" />}
            faviconURL={tab.faviconURL}
            pageURL={tab.url || ""}
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
        </div>}
        <div className="relative aspect-video overflow-hidden bg-muted/50">
          {surfaceKey === entryKey && automationActivity.presence === "visible" ? (
            <BrowserViewportPlaceholder
              active
              interactive={false}
              pip
              pipEmbedded={embedded}
              priority={pipViewportPriority}
              sessionID={sessionID}
              tabID={automationActivity.tabID}
            />
          ) : null}
        </div>
      </aside>
    </div>
  );
});

export function useBrowserAutomationActivity(sessionID: string) {
  return useVisibleBrowserAutomationActivity(sessionID);
}

export function useRetainBrowserRuntimeTabs(
  sessionID: string,
  tabs: ElectronBrowserSurfaceTab[],
  tabsReady: boolean,
) {
  const { retainTabs } = useBrowserRuntimeContext();
  const retainableTabs = useMemo(
    () => tabs.filter((tab) => Boolean(tab.targetID)),
    [tabs],
  );
  useEffect(() => {
    if (tabsReady) {
      retainTabs(sessionID, retainableTabs);
    }
  }, [retainTabs, retainableTabs, sessionID, tabsReady]);
}

const BrowserRuntimeHost = memo(function BrowserRuntimeHost({
  registerRuntime,
  setRuntimeReady,
  sessionID,
  tab,
  token,
}: {
  registerRuntime: BrowserRuntimeContextValue["registerRuntime"];
  setRuntimeReady: (key: string, ready: boolean) => void;
  sessionID: string;
  tab: ElectronBrowserSurfaceTab;
  token: string;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const runtimeRef = useRef<ElectronWebviewRuntimeHandle | null>(null);
  const key = runtimeKey(sessionID, tab.id);
  const handleReadyChange = useCallback((ready: boolean) => {
    setRuntimeReady(key, ready);
  }, [key, setRuntimeReady]);

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
    >
      <div className="pudding-browser-runtime-surface">
        <ElectronWebviewBrowser
          ref={runtimeRef}
          activeTab={tab}
          sessionID={sessionID}
          token={token}
          onReadyChange={handleReadyChange}
        />
      </div>
    </div>
  );
});

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
  viewports: Map<string, BrowserRuntimeViewport>,
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

function applyRuntimeGeometry(
  host: HTMLDivElement,
  element: HTMLDivElement,
  clipElement?: HTMLElement,
  syncSurfaceSize = false,
) {
  const rect = element.getBoundingClientRect();
  const clipRect = clipElement?.getBoundingClientRect();
  const top = clipRect ? Math.max(rect.top, clipRect.top) : rect.top;
  const left = clipRect ? Math.max(rect.left, clipRect.left) : rect.left;
  const right = clipRect ? Math.min(rect.right, clipRect.right) : rect.right;
  const bottom = clipRect ? Math.min(rect.bottom, clipRect.bottom) : rect.bottom;
  const width = right - left;
  const height = bottom - top;
  if (rect.width <= 0 || width <= 0 || height <= 0) {
    return false;
  }
  host.style.top = `${top}px`;
  host.style.left = `${left}px`;
  host.style.width = `${width}px`;
  host.style.height = `${height}px`;
  host.style.setProperty("--pudding-browser-surface-top", `${rect.top - top}px`);
  host.style.setProperty("--pudding-browser-surface-left", `${rect.left - left}px`);
  if (!syncSurfaceSize) {
    const runtimeWidth = Number.parseFloat(
      getComputedStyle(host).getPropertyValue("--browser-runtime-width"),
    );
    if (runtimeWidth > 0) {
      host.style.setProperty(
        "--pudding-browser-runtime-scale",
        String(rect.width / runtimeWidth),
      );
    }
  }
  if (syncSurfaceSize) {
    // Electron 会把 guest surface 尺寸换算为设备像素；向下取整会在右/下边缘
    // 留出一条宿主底色。只放大内部 surface，外层 host 仍负责精确裁剪。
    host.style.setProperty("--pudding-browser-surface-width", `${ceilToDevicePixel(rect.width)}px`);
    host.style.setProperty("--pudding-browser-surface-height", `${ceilToDevicePixel(rect.height)}px`);
  }
  return true;
}

function ceilToDevicePixel(value: number) {
  const scale = window.devicePixelRatio || 1;
  return Math.ceil(value * scale) / scale;
}

function clearRuntimeGeometry(host: HTMLDivElement) {
  host.style.removeProperty("top");
  host.style.removeProperty("left");
  host.style.removeProperty("width");
  host.style.removeProperty("height");
  host.style.removeProperty("--pudding-browser-surface-top");
  host.style.removeProperty("--pudding-browser-surface-left");
  host.style.removeProperty("--pudding-browser-surface-width");
  host.style.removeProperty("--pudding-browser-surface-height");
  host.style.removeProperty("--pudding-browser-runtime-scale");
}

function workspaceTransitionInProgress(element?: HTMLElement) {
  return element?.dataset.transition === "opening"
    || element?.dataset.transition === "closing";
}

function applyRuntimePresentation(
  runtime: RuntimeEntry,
  mode: "standby" | "visible" | "automation",
  anchored: boolean,
  interactive: boolean,
  pip?: boolean,
  pipEmbedded?: boolean,
) {
  const presentation = `${mode}:${anchored ? "anchored" : "standby"}:${interactive ? "interactive" : "passive"}:${pip ? "pip" : "workspace"}:${pipEmbedded ? "embedded" : "standalone"}`;
  if (runtime.presentation === presentation) {
    return;
  }
  const wasVisible = runtime.host.dataset.presentation === "visible"
    || (
      runtime.host.dataset.presentation === "automation"
      && runtime.host.dataset.anchored === "true"
    );
  runtime.presentation = presentation;
  window.cancelAnimationFrame(runtime.presentationFrameID || 0);
  runtime.presentationFrameID = undefined;
  runtime.host.dataset.anchored = anchored ? "true" : "false";
  runtime.host.dataset.interactive = interactive ? "true" : "false";
  if (pip) {
    runtime.host.dataset.pip = "true";
    runtime.host.dataset.pipEmbedded = pipEmbedded ? "true" : "false";
  } else {
    delete runtime.host.dataset.pip;
    delete runtime.host.dataset.pipEmbedded;
  }
  if (mode === "visible" && anchored && !wasVisible) {
    runtime.host.dataset.presentation = "standby";
    runtime.host.setAttribute("aria-hidden", "true");
    runtime.presentationFrameID = window.requestAnimationFrame(() => {
      runtime.presentationFrameID = window.requestAnimationFrame(() => {
        runtime.presentationFrameID = undefined;
        if (runtime.presentation !== presentation) {
          return;
        }
        runtime.host.dataset.presentation = "visible";
        runtime.host.setAttribute("aria-hidden", interactive ? "false" : "true");
      });
    });
    return;
  }
  runtime.host.dataset.presentation = mode;
  runtime.host.setAttribute(
    "aria-hidden",
    mode === "visible" && anchored && interactive ? "false" : "true",
  );
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

function sameAutomationActivityRecord(
  current: Record<string, BrowserAutomationActivity>,
  next: Record<string, BrowserAutomationActivity>,
) {
  const currentKeys = Object.keys(current);
  const nextKeys = Object.keys(next);
  return currentKeys.length === nextKeys.length
    && currentKeys.every((key) => current[key] === next[key]);
}
