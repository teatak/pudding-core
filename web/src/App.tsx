import { useNavigate, useSearch } from "@tanstack/react-router";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { useGroupRef } from "react-resizable-panels";

import { claimMobilePairing } from "@/api/client";
import { BrowserRuntimeProvider } from "@/browser/BrowserRuntimeProvider";
import { WorkspaceFocusControl } from "@/components/WorkspaceFocusControl";
import { AppsPane } from "@/components/AppsPane";
import { AppToaster } from "@/components/AppToaster";
import { ChatPane } from "@/components/ChatPane";
import { EditorTypographyProvider } from "@/components/EditorTypographyProvider";
import { ProjectCreateDialog } from "@/components/ProjectCreateDialog";
import { OAuthReturnHandler } from "@/components/OAuthReturnHandler";
import { ProjectsPane } from "@/components/ProjectsPane";
import { SessionRail } from "@/components/SessionRail";
import { SettingsDialog } from "@/components/SettingsDialog";
import { ComputerUsePermissionGuide } from "@/components/ComputerUsePermissionGuide";
import { PairingGate, TokenGate } from "@/components/TokenGate";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { TooltipProvider } from "@/components/ui/tooltip";
import { WorkspacePane } from "@/components/workspace/WorkspacePane";
import { WorkspaceResizableHandle } from "@/components/WorkspaceResizableHandle";
import { useVisibleSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import {
  layoutStorageKeys,
  resizeTargetMinimumSize,
  sessionRailLayout,
  splitLayout,
  workspaceLayout,
} from "@/lib/layoutConstants";
import { resolveCenteredLayoutPresentation } from "@/lib/centeredLayout";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { saveLastAppRoute, type AppSearch } from "@/lib/route";
import { cn } from "@/lib/utils";
import { useCanvasMCP } from "@/mcp/canvasTools";
import { clearFilePreviews } from "@/state/filePreviewStore";
import {
  getRailCollapsedPreference,
  setRailResponsiveCollapsed,
  useRailCollapsed,
} from "@/state/railStore";
import { clearPendingPairingCode, pendingPairingCode } from "@/state/token";
import { setToken, useToken } from "@/state/tokenStore";
import {
  minimumChatPaneWidth,
  setWorkspaceFocusChatWidth,
  setWorkspaceOpen,
  useActiveWorkspaceSessionID,
  useWorkspaceFocusChatWidth,
  useWorkspacePresentation,
} from "@/state/workspaceStore";

const focusWorkspaceMinimumWidth = 220;
const workspaceMinimumWidth = workspaceLayout.minWorkspacePx;
const workspaceTransitionDurationMs = 220;
type WorkspaceTransitionPhase = "idle" | "opening" | "closing";
const centeredLayoutConstraints = {
  dockedMinimumWidth: workspaceLayout.drawerBreakpointPx,
  railChatMinimumWidth: workspaceLayout.railAutoCollapsePx,
  thirdColumnMinimumWidth: Math.min(
    minimumChatPaneWidth,
    workspaceMinimumWidth,
  ),
};

function readSavedSplitLayout() {
  return readPanelLayout(layoutStorageKeys.splitRatio, splitLayout.fallback, {
    minPercent: splitLayout.minPercent,
    maxPercent: splitLayout.maxPercent,
  });
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function lockAgentConsoleResizeCursor(cursor: string) {
  const root = document.documentElement;
  const property = "--agent-console-resize-cursor";
  const previousValue = root.style.getPropertyValue(property);
  const previousPriority = root.style.getPropertyPriority(property);
  root.style.setProperty(property, cursor);
  root.dataset.agentConsoleResizing = "true";
  return () => {
    if (previousValue) {
      root.style.setProperty(property, previousValue, previousPriority);
    } else {
      root.style.removeProperty(property);
    }
    delete root.dataset.agentConsoleResizing;
  };
}

function readDockSplitRatio() {
  const saved = Number.parseFloat(
    localStorage.getItem(layoutStorageKeys.agentConsoleDockSplitRatio) || "",
  );
  return Number.isFinite(saved)
    ? clamp(saved, 0, 1)
    : workspaceLayout.defaultLeftGroupRatio;
}

function dockSplitRatioBounds({
  layoutWidth,
  railCollapsed,
  minimumChatWidth = minimumChatPaneWidth,
  minimumWorkspaceWidth = workspaceMinimumWidth,
}: {
  layoutWidth: number;
  railCollapsed: boolean;
  minimumChatWidth?: number;
  minimumWorkspaceWidth?: number;
}) {
  const railWidth = railCollapsed ? 0 : sessionRailLayout.expandedWidthPx;
  return {
    maximum: (layoutWidth - minimumWorkspaceWidth) / layoutWidth,
    minimum: (railWidth + minimumChatWidth) / layoutWidth,
  };
}

export function App() {
  const token = useToken();
  const navigate = useNavigate();
  const {
    session: selectedSessionID,
    draft,
    project: draftProjectID,
    split: splitSessionID,
    view,
  } = useSearch({ from: "/" });
  const { t } = useI18n();
  const appsActive = view === "apps";
  const projectsActive = view === "projects";
  const standaloneViewActive = appsActive || projectsActive;
  const showSplit = !standaloneViewActive && Boolean(splitSessionID && splitSessionID !== selectedSessionID);
  const workspaceSessionID = useActiveWorkspaceSessionID(
    standaloneViewActive ? undefined : selectedSessionID,
    showSplit ? splitSessionID : undefined,
  );
  const workspacePresentation = useWorkspacePresentation(workspaceSessionID);
  const workspaceOpen = workspacePresentation !== "hidden";
  const focused = workspacePresentation === "focused" && !standaloneViewActive;
  const focusChatWidth = useWorkspaceFocusChatWidth(workspaceSessionID);
  const railCollapsed = useRailCollapsed();
  const [projectCreateOpen, setProjectCreateOpen] = useState(false);
  const [pairingCode] = useState(() => pendingPairingCode());
  const [pairingFailed, setPairingFailed] = useState(false);
  const [layoutNode, setLayoutNode] = useState<HTMLDivElement | null>(null);
  const [centeredLayout, setCenteredLayout] = useState(() =>
    resolveCenteredLayoutPresentation({
      focused: false,
      constraints: centeredLayoutConstraints,
      layoutWidth: 0,
      leftGroupRatio: workspaceLayout.defaultLeftGroupRatio,
      workspaceDockRequested: false,
    }),
  );
  const [consoleInteracting, setConsoleInteracting] = useState(false);
  const [dockSplitRatio, setDockSplitRatio] = useState(readDockSplitRatio);
  const splitGroupRef = useGroupRef();
  const agentConsoleRef = useRef<HTMLDivElement | null>(null);
  const dockSplitRatioRef = useRef(dockSplitRatio);
  const dockResizeCleanupRef = useRef<(() => void) | null>(null);
  const centeredLayoutRef = useRef(centeredLayout);
  const previewTokenRef = useRef(token);

  const draftActive = !standaloneViewActive && draft === "1" && !selectedSessionID;
  const canUseWorkspace = !standaloneViewActive && Boolean(selectedSessionID);
  const workspaceRequestedOpen = canUseWorkspace && workspaceOpen;
  const [workspacePresent, setWorkspacePresent] = useState(workspaceRequestedOpen);
  const [workspaceTransition, setWorkspaceTransition] = useState<WorkspaceTransitionPhase>("idle");
  const workspaceRequestRef = useRef(workspaceRequestedOpen);
  const workspaceSessionRef = useRef(workspaceSessionID);
  const effectiveWorkspaceOpen = canUseWorkspace && workspacePresent;
  const workspaceVisible = effectiveWorkspaceOpen && workspaceTransition !== "closing";
  const workspaceDockRequested = effectiveWorkspaceOpen;
  const updateCenteredLayout = useCallback(
    (
      layoutWidth: number,
      nextWorkspaceDockRequested: boolean,
      nextSplitRatio = dockSplitRatio,
    ) => {
      const expandedBounds = dockSplitRatioBounds({
        layoutWidth,
        minimumChatWidth: minimumChatPaneWidth,
        minimumWorkspaceWidth: focused ? focusWorkspaceMinimumWidth : workspaceMinimumWidth,
        railCollapsed: false,
      });
      const feasibleExpandedRatio = clamp(
        nextSplitRatio,
        expandedBounds.minimum,
        expandedBounds.maximum,
      );
      const next = resolveCenteredLayoutPresentation({
        focused,
        constraints: centeredLayoutConstraints,
        layoutWidth,
        leftGroupRatio: feasibleExpandedRatio,
        workspaceDockRequested: nextWorkspaceDockRequested,
      });
      const current = centeredLayoutRef.current;
      if (
        current.railResponsiveCollapsed === next.railResponsiveCollapsed &&
        current.workspaceOverlay === next.workspaceOverlay
      ) {
        return current;
      }
      centeredLayoutRef.current = next;
      setRailResponsiveCollapsed(next.railResponsiveCollapsed);
      setCenteredLayout(next);
      return next;
    },
    [dockSplitRatio, focused],
  );
  const finishWorkspaceTransition = useCallback(() => {
    if (workspaceTransition === "idle") {
      return;
    }
    setWorkspaceTransition("idle");
    if (workspaceTransition === "closing") {
      updateCenteredLayout(layoutNode?.clientWidth || 0, false);
      setWorkspacePresent(false);
    }
  }, [layoutNode, updateCenteredLayout, workspaceTransition]);
  useLayoutEffect(() => {
    const sessionChanged = workspaceSessionRef.current !== workspaceSessionID;
    workspaceSessionRef.current = workspaceSessionID;
    if (sessionChanged && !workspaceRequestedOpen) {
      workspaceRequestRef.current = workspaceRequestedOpen;
      updateCenteredLayout(
        layoutNode?.clientWidth || 0,
        workspaceRequestedOpen,
      );
      setWorkspaceTransition("idle");
      setWorkspacePresent(workspaceRequestedOpen);
      return;
    }
    if (workspaceRequestRef.current === workspaceRequestedOpen) {
      return;
    }
    workspaceRequestRef.current = workspaceRequestedOpen;
    if (workspaceRequestedOpen) {
      // 先提交最终响应式形态，再让工作区入场，避免 rail 收起与分栏宽度
      // 在相邻两帧分别生效，造成工作区横向越界后回弹。
      updateCenteredLayout(layoutNode?.clientWidth || 0, true);
      setWorkspacePresent(true);
      setWorkspaceTransition("opening");
    } else {
      setWorkspaceTransition("closing");
    }
  }, [
    layoutNode,
    updateCenteredLayout,
    workspaceRequestedOpen,
    workspaceSessionID,
  ]);
  // workspaceOpen 只表达用户意图。停靠/抽屉以及 rail 的响应式展示由
  // 两个共享 Chat 的组合区域统一求解，不写回用户偏好。
  const workspaceOverlay =
    workspaceDockRequested && centeredLayout.workspaceOverlay;
  const workspaceDocked =
    workspaceDockRequested && !workspaceOverlay;

  function openProjectDraft(projectID: string) {
    void navigate({
      to: "/",
      search: (prev) => {
        const next = { ...(prev as AppSearch), draft: "1", project: projectID };
        delete next.session;
        delete next.split;
        delete next.view;
        return next;
      },
    });
  }

  function openProjectCreate() {
    setProjectCreateOpen(true);
  }
  const showConsoleSplit = showSplit && !focused;
  const activeSessionIDs = (
    standaloneViewActive
      ? []
      : [selectedSessionID, showSplit ? splitSessionID : undefined]
  ).filter((sessionID): sessionID is string => Boolean(sessionID));

  useEffect(
    () => () => {
      setRailResponsiveCollapsed(false);
    },
    [],
  );

  useEffect(() => {
    saveLastAppRoute({
      session: selectedSessionID,
      draft,
      project: draftProjectID,
      split: splitSessionID,
      view,
    });
  }, [draft, draftProjectID, selectedSessionID, splitSessionID, view]);

  // SSE 是 session-scoped,不是 pane-scoped。visible sessions 在 App 层统一去重订阅。
  useVisibleSessionEvents(activeSessionIDs, token);
  useCanvasMCP(token);

  useEffect(() => {
    if (previewTokenRef.current && previewTokenRef.current !== token) {
      clearFilePreviews();
    }
    previewTokenRef.current = token;
  }, [token]);

  useLayoutEffect(() => {
    if (!layoutNode) {
      return;
    }
    let previousWidth = layoutNode.clientWidth;
    let resizeSettledTimer = 0;
    const updatePresentation = () => {
      const nextWidth = layoutNode.clientWidth;
      if (nextWidth !== previousWidth) {
        document.documentElement.dataset.shellResizing = "true";
        window.clearTimeout(resizeSettledTimer);
        resizeSettledTimer = window.setTimeout(() => {
          delete document.documentElement.dataset.shellResizing;
        }, 80);
        previousWidth = nextWidth;
      }
      updateCenteredLayout(nextWidth, workspaceDockRequested);
    };
    updatePresentation();
    const observer = new ResizeObserver(updatePresentation);
    observer.observe(layoutNode);
    window.addEventListener("resize", updatePresentation, { passive: true });
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", updatePresentation);
      window.clearTimeout(resizeSettledTimer);
      delete document.documentElement.dataset.shellResizing;
    };
  }, [layoutNode, updateCenteredLayout, workspaceDockRequested]);

  useEffect(() => {
    dockSplitRatioRef.current = dockSplitRatio;
  }, [dockSplitRatio]);

  useEffect(
    () => () => {
      dockResizeCleanupRef.current?.();
    },
    [],
  );

  useEffect(() => {
    if (token || !pairingCode) {
      return;
    }
    let cancelled = false;
    void claimMobilePairing(pairingCode, { deviceName: navigator.userAgent || "Mobile device" })
      .then((result) => {
        if (cancelled) {
          return;
        }
        clearPendingPairingCode();
        setToken(result.token);
      })
      .catch(() => {
        if (!cancelled) {
          clearPendingPairingCode();
          setPairingFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [pairingCode, token]);

  useEffect(() => {
    const group = splitGroupRef.current;
    if (!group) {
      return;
    }
    if (!showConsoleSplit) {
      group.setLayout(splitLayout.closed);
      return;
    }
    group.setLayout(readSavedSplitLayout());
  }, [showConsoleSplit, splitGroupRef]);

  if (!token) {
    if (pairingCode) {
      return <PairingGate failed={pairingFailed} />;
    }
    return <TokenGate />;
  }

  const commitDockSplitRatio = (next: number) => {
    const layoutWidth = layoutNode?.clientWidth || 0;
    let normalized = clamp(next, 0, 1);
    if (layoutWidth > 0 && workspaceDocked) {
      const presentation = updateCenteredLayout(
        layoutWidth,
        workspaceDockRequested,
        normalized,
      );
      const bounds = dockSplitRatioBounds({
        layoutWidth,
        minimumChatWidth: minimumChatPaneWidth,
        minimumWorkspaceWidth: focused ? focusWorkspaceMinimumWidth : workspaceMinimumWidth,
        railCollapsed:
          getRailCollapsedPreference() ||
          presentation.railResponsiveCollapsed,
      });
      normalized = clamp(normalized, bounds.minimum, bounds.maximum);
      updateCenteredLayout(layoutWidth, workspaceDockRequested, normalized);
    }
    if (focused) {
      setWorkspaceFocusChatWidth(workspaceSessionID, layoutWidth * normalized);
      return;
    }
    dockSplitRatioRef.current = normalized;
    setDockSplitRatio(normalized);
    localStorage.setItem(
      layoutStorageKeys.agentConsoleDockSplitRatio,
      String(normalized),
    );
  };

  const moveDockDivider = (delta: number) => {
    const layoutWidth = layoutNode?.clientWidth || 0;
    if (layoutWidth <= 0) {
      return;
    }
    const ratio = focused
      ? focusChatWidth / layoutWidth
      : dockSplitRatioRef.current;
    commitDockSplitRatio(ratio + delta / layoutWidth);
  };

  const startDockResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (
      event.button !== 0 ||
      !layoutNode ||
      !agentConsoleRef.current ||
      !workspaceDocked
    ) {
      return;
    }
    event.preventDefault();
    const pointerID = event.pointerId;
    const resizeHandle = event.currentTarget;
    resizeHandle.focus();
    const agentConsoleNode = agentConsoleRef.current;
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    const resizeShield = document.createElement("div");
    resizeShield.className = "pudding-agent-console-resize-shield no-drag-region";
    resizeShield.setAttribute("aria-hidden", "true");
    resizeShield.style.cursor = "ew-resize";

    dockResizeCleanupRef.current?.();
    try {
      resizeHandle.setPointerCapture(pointerID);
    } catch {
      // The full-screen shield below still keeps host-side pointer events alive.
    }
    document.body.appendChild(resizeShield);
    const restoreResizeCursor = lockAgentConsoleResizeCursor("ew-resize");
    setConsoleInteracting(true);
    document.body.style.cursor = "ew-resize";
    document.body.style.userSelect = "none";

    let liveSplitRatio = focused
      ? agentConsoleNode.getBoundingClientRect().width / layoutNode.clientWidth
      : dockSplitRatioRef.current;
    let resizeFrame = 0;
    let pendingClientX: number | undefined;
    let cleaned = false;
    const update = (clientX: number) => {
      const layoutRect = layoutNode.getBoundingClientRect();
      const layoutWidth = layoutRect.width;
      const rawRatio = (clientX - layoutRect.left) / layoutWidth;
      const presentation = updateCenteredLayout(
        layoutWidth,
        workspaceDockRequested,
        rawRatio,
      );
      const liveRailCollapsed =
        getRailCollapsedPreference() ||
        presentation.railResponsiveCollapsed;
      const bounds = dockSplitRatioBounds({
        layoutWidth,
        minimumChatWidth: minimumChatPaneWidth,
        minimumWorkspaceWidth: focused ? focusWorkspaceMinimumWidth : workspaceMinimumWidth,
        railCollapsed: liveRailCollapsed,
      });
      liveSplitRatio = clamp(rawRatio, bounds.minimum, bounds.maximum);
      if (!focused) dockSplitRatioRef.current = liveSplitRatio;
      const railWidth = liveRailCollapsed
        ? 0
        : sessionRailLayout.expandedWidthPx;
      const leftGroupWidth = layoutWidth * liveSplitRatio;
      const chatWidth = leftGroupWidth - railWidth;
      agentConsoleNode.style.width = `${chatWidth}px`;
    };
    const scheduleUpdate = (clientX: number) => {
      pendingClientX = clientX;
      if (resizeFrame) {
        return;
      }
      resizeFrame = window.requestAnimationFrame(() => {
        resizeFrame = 0;
        if (pendingClientX !== undefined) {
          update(pendingClientX);
          pendingClientX = undefined;
        }
      });
    };
    const cleanup = () => {
      if (cleaned) {
        return;
      }
      cleaned = true;
      window.cancelAnimationFrame(resizeFrame);
      resizeShield.remove();
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerCancel);
      window.removeEventListener("blur", handleWindowBlur);
      if (resizeHandle.hasPointerCapture(pointerID)) {
        resizeHandle.releasePointerCapture(pointerID);
      }
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      restoreResizeCursor();
      setConsoleInteracting(false);
      dockResizeCleanupRef.current = null;
    };
    const finish = () => {
      if (pendingClientX !== undefined) {
        update(pendingClientX);
        pendingClientX = undefined;
      }
      cleanup();
      commitDockSplitRatio(liveSplitRatio);
    };
    const handlePointerMove = (moveEvent: PointerEvent) => {
      if (moveEvent.pointerId === pointerID) {
        moveEvent.preventDefault();
        scheduleUpdate(moveEvent.clientX);
      }
    };
    const handlePointerUp = (upEvent: PointerEvent) => {
      if (upEvent.pointerId !== pointerID) {
        return;
      }
      window.cancelAnimationFrame(resizeFrame);
      resizeFrame = 0;
      pendingClientX = undefined;
      update(upEvent.clientX);
      finish();
    };
    const handlePointerCancel = (cancelEvent: PointerEvent) => {
      if (cancelEvent.pointerId === pointerID) {
        finish();
      }
    };
    const handleWindowBlur = () => finish();

    window.addEventListener("pointermove", handlePointerMove, { passive: false });
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerCancel);
    window.addEventListener("blur", handleWindowBlur);
    dockResizeCleanupRef.current = cleanup;
  };

  const docked = workspaceDocked;
  const workspaceToolbarPadding = "0.75rem";
  const renderedDockSplitRatio = consoleInteracting
    ? dockSplitRatioRef.current
    : dockSplitRatio;
  const renderedChatRatio = renderedDockSplitRatio;
  const renderedChatPercent = `${renderedChatRatio * 100}%`;
  const railAdjustment = railCollapsed ? 0 : -(1 - renderedDockSplitRatio) * sessionRailLayout.expandedWidthPx;
  const preferredDockWidth = railAdjustment === 0
    ? renderedChatPercent
    : `calc(${renderedChatPercent} ${railAdjustment > 0 ? "+" : "-"} ${Math.abs(railAdjustment)}px)`;
  const renderedChatContainerWidth = `${renderedChatRatio * 100}cqw`;
  const preferredDockContainerWidth = railAdjustment === 0
    ? renderedChatContainerWidth
    : `calc(${renderedChatContainerWidth} ${railAdjustment > 0 ? "+" : "-"} ${Math.abs(railAdjustment)}px)`;
  const minimumWorkspaceWidth = focused ? focusWorkspaceMinimumWidth : workspaceMinimumWidth;
  const dockedConsoleWidth = `clamp(${minimumChatPaneWidth}px, ${focused ? `${focusChatWidth}px` : preferredDockWidth}, calc(100% - min(${minimumWorkspaceWidth}px, 50%)))`;
  const dockedWorkspaceWidth = `max(0px, calc(100cqw - clamp(${minimumChatPaneWidth}px, ${focused ? `${focusChatWidth}px` : preferredDockContainerWidth}, calc(100cqw - min(${minimumWorkspaceWidth}px, 50cqw))) - 1px))`;
  const workspaceSurfaceStyle = {
    "--workspace-toolbar-pl": workspaceToolbarPadding,
    order: 2,
    width: workspaceOverlay ? `min(100%, ${workspaceLayout.drawerWidthPx}px)` : undefined,
  } as CSSProperties;
  const chatOccupiesStageTopRight = !docked || workspaceTransition === "closing";
  const reserveWorkspaceControl = canUseWorkspace;

  const chatArea = (
    <main
      className="flex h-full w-full min-w-0 flex-col overflow-hidden bg-background"
    >
      <ResizablePanelGroup
        className="min-h-0 flex-1"
        defaultLayout={showConsoleSplit ? readSavedSplitLayout() : splitLayout.closed}
        groupRef={splitGroupRef}
        id="split-workspace"
        orientation="vertical"
        resizeTargetMinimumSize={resizeTargetMinimumSize}
        onLayoutChanged={(layout) => {
          if (showConsoleSplit && typeof layout.split === "number" && layout.split > 0) {
            savePanelLayout(layoutStorageKeys.splitRatio, layout);
          }
        }}
      >
        <ResizablePanel id="primary" className="min-h-0" minSize={focused ? 0 : splitLayout.minPanePx}>
          <ChatPane
            draftActive={draftActive}
            draftProjectID={draftActive ? draftProjectID : undefined}
            reserveTopLeftInset
            reserveWorkspaceControl={chatOccupiesStageTopRight && reserveWorkspaceControl}
            role="primary"
            sessionID={focused ? workspaceSessionID : selectedSessionID}
            token={token}
          />
        </ResizablePanel>
        <WorkspaceResizableHandle
          aria-label={t("layout.resizeHint")}
          className={showConsoleSplit ? undefined : "hidden"}
          disabled={!showConsoleSplit}
        />
        <ResizablePanel
          id="split"
          className="min-h-0"
          collapsedSize="0%"
          collapsible
          minSize={splitLayout.minPanePx}
        >
          {showConsoleSplit ? <ChatPane token={token} sessionID={splitSessionID} role="split" /> : null}
        </ResizablePanel>
      </ResizablePanelGroup>
    </main>
  );

  const workspaceToolbarActions = canUseWorkspace ? (
    <div className="no-drag-region pointer-events-auto absolute top-0 right-(--workspace-toggle-right) z-[60] flex h-(--toolbar-h) items-center">
      <WorkspaceFocusControl sessionID={workspaceSessionID} />
    </div>
  ) : null;

  const standalonePane = appsActive
    ? <AppsPane token={token} />
    : projectsActive
      ? (
          <ProjectsPane
            token={token}
            onOpenProjectDraft={openProjectDraft}
          />
        )
      : null;

  const workspaceSurface = (
    <div
      key="workspace"
      aria-hidden={!workspaceVisible}
      className={cn(
        "pudding-workspace-stage h-full min-h-0 min-w-0 flex-1 overflow-hidden bg-background",
        !effectiveWorkspaceOpen && "invisible pointer-events-none !w-0 !flex-none opacity-0",
        workspaceOverlay &&
          "absolute inset-y-0 right-0 z-50 flex-none border-l border-[var(--workspace-border)] shadow-[-8px_0_24px_-16px_rgb(0_0_0/0.28)]",
      )}
      data-presentation={workspaceOverlay ? "overlay" : docked ? "docked" : "inline"}
      data-transition={workspaceTransition}
      inert={!workspaceVisible}
      style={workspaceSurfaceStyle}
      onAnimationEnd={(event) => {
        if (event.currentTarget === event.target && workspaceOverlay) {
          finishWorkspaceTransition();
        }
      }}
    >
      <WorkspacePane
        activeSessionID={workspaceSessionID}
        presented={effectiveWorkspaceOpen}
        reserveWorkspaceControl={reserveWorkspaceControl}
        secondarySessionID={showSplit ? splitSessionID : undefined}
        sessionID={selectedSessionID}
        token={token}
      />
    </div>
  );
  const agentConsole = (
    <div
      ref={agentConsoleRef}
      key="agent-console"
      className={cn(
        "pudding-agent-console min-h-0 min-w-0",
        docked && workspaceTransition !== "idle" && "pudding-workspace-width-transition",
        "overflow-hidden",
      )}
      style={{
        flexShrink: 0,
        height: "100%",
        order: 0,
        position: "relative",
        width: docked
            ? (workspaceVisible ? dockedConsoleWidth : "100%")
            : "100%",
      }}
      onTransitionEnd={(event) => {
        if (
          event.currentTarget === event.target &&
          event.propertyName === "width" &&
          docked
        ) {
          finishWorkspaceTransition();
        }
      }}
    >
      {chatArea}
    </div>
  );
  const sessionStage = (
    <>
      {/* 保留同一父节点，切换专注布局时不重建对话和输入区。 */}
      <div className="contents">
        {agentConsole}
      </div>
      {workspaceOverlay ? (
        <button
          aria-label={t("workspace.close")}
          className="pudding-workspace-backdrop no-drag-region absolute inset-0 z-40 bg-overlay"
          data-transition={workspaceTransition}
          tabIndex={-1}
          type="button"
          onClick={() => setWorkspaceOpen(workspaceSessionID, false)}
        />
      ) : null}
      {docked ? (
        <div
          key="dock-resize-handle"
          aria-label={t("layout.resizeHint")}
          aria-orientation="vertical"
          aria-valuemax={100}
          aria-valuemin={0}
          aria-valuenow={Math.round((focused ? focusChatWidth / (layoutNode?.clientWidth || focusChatWidth) : renderedDockSplitRatio) * 100)}
          className={cn(
            "pudding-shell-divider group no-drag-region relative z-50 order-1 flex h-full w-px shrink-0 cursor-ew-resize touch-none items-center justify-center outline-none transition-opacity duration-[var(--workspace-transition-duration)] before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 focus-visible:before:bg-muted-foreground/80",
            !workspaceVisible && "pointer-events-none opacity-0",
          )}
          role="separator"
          tabIndex={0}
          onKeyDown={(event) => {
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
            event.preventDefault();
            moveDockDivider(event.key === "ArrowRight" ? 20 : -20);
          }}
          onPointerDown={startDockResize}
        >
          <div
            aria-hidden="true"
            className="absolute inset-y-0 left-1/2 z-20 w-3 -translate-x-1/2 cursor-ew-resize touch-none"
          />
          <div
            aria-hidden="true"
            className={cn(
              "pointer-events-none relative z-10 h-8 w-[3px] shrink-0 rounded-full bg-muted-foreground/70 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100",
              consoleInteracting && "opacity-100",
            )}
          />
        </div>
      ) : null}
      {workspaceSurface}
    </>
  );

  return (
    <EditorTypographyProvider token={token}>
      <TooltipProvider delayDuration={250}>
        <BrowserRuntimeProvider token={token}>
          <OAuthReturnHandler token={token} />
          <div className="relative flex h-full overflow-hidden">
            <div
              ref={setLayoutNode}
              className="isolate relative flex h-full min-w-0 flex-1 bg-background"
            >
              <div
                aria-hidden="true"
                className="drag-region absolute inset-x-0 top-0 z-20 h-(--toolbar-h)"
              />
              <SessionRail
                activeSessionIDs={activeSessionIDs}
                draftActive={draftActive}
                selectedSessionID={standaloneViewActive ? undefined : selectedSessionID}
                token={token}
                onCreateProject={openProjectCreate}
              />
              <div
                data-workspace-presentation={
                  !effectiveWorkspaceOpen
                    ? "hidden"
                    : workspaceOverlay
                      ? "overlay"
                      : "docked"
                }
                className={cn(
                  "pudding-session-stage relative flex h-full min-w-0 flex-1 overflow-hidden bg-background",
                )}
                style={{
                  "--workspace-control-width": workspaceOpen
                    ? "calc(var(--toolbar-icon-button-size) * 2 + 0.25rem)"
                    : "var(--toolbar-icon-button-size)",
                  "--workspace-inline-content-width": dockedWorkspaceWidth,
                  "--workspace-transition-duration": `${workspaceTransitionDurationMs}ms`,
                } as CSSProperties}
              >
                {standalonePane || sessionStage}
              </div>
              {workspaceToolbarActions}
            </div>
          </div>
          <ProjectCreateDialog
            open={projectCreateOpen}
            token={token}
            onCreated={openProjectDraft}
            onOpenChange={setProjectCreateOpen}
          />
          <SettingsDialog token={token} showTrigger={false} />
          <ComputerUsePermissionGuide />
          <AppToaster />
        </BrowserRuntimeProvider>
      </TooltipProvider>
    </EditorTypographyProvider>
  );
}
