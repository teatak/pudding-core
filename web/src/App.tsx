import { useNavigate, useSearch } from "@tanstack/react-router";
import { PanelRightClose, PanelRightOpen } from "@/components/icons";
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
import { AgentConsoleLayoutControl } from "@/components/AgentConsoleLayoutControl";
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
import { Button } from "@/components/ui/button";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
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
import {
  useAgentConsoleMode,
  type AgentConsoleMode,
} from "@/state/agentConsoleStore";
import { clearFilePreviews } from "@/state/filePreviewStore";
import {
  getRailCollapsedPreference,
  setRailResponsiveCollapsed,
  useRailCollapsed,
} from "@/state/railStore";
import { clearPendingPairingCode, pendingPairingCode } from "@/state/token";
import { setToken, useToken } from "@/state/tokenStore";
import {
  setWorkspaceOpen,
  useActiveWorkspaceSessionID,
  useWorkspaceOpen,
} from "@/state/workspaceStore";

type ConsoleDisplayMode = "full" | AgentConsoleMode;

const floatingInset = 16;
const floatingBottomInset = 4;
const consoleMinimumWidth = 380;
const floatingDefaultWidth = 680;
const floatingDefaultHeight = 420;
const workspaceMinimumWidth = workspaceLayout.minWorkspacePx;
const dockMaximumWidth = 640;
const workspaceTransitionDurationMs = 220;
type WorkspaceTransitionPhase = "idle" | "opening" | "closing";
const centeredLayoutConstraints = {
  dockedMinimumWidth: workspaceLayout.drawerBreakpointPx,
  railChatMinimumWidth: workspaceLayout.railAutoCollapsePx,
  railWorkspaceMinimumWidth:
    sessionRailLayout.expandedWidthPx + workspaceMinimumWidth,
  thirdColumnMinimumWidth: Math.min(
    consoleMinimumWidth,
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

function workspaceTransitionDelay() {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches
    ? 1
    : workspaceTransitionDurationMs;
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
  chatDockSide,
  layoutWidth,
  railCollapsed,
}: {
  chatDockSide: "left" | "right";
  layoutWidth: number;
  railCollapsed: boolean;
}) {
  const railWidth = railCollapsed ? 0 : sessionRailLayout.expandedWidthPx;
  if (chatDockSide === "left") {
    return {
      maximum: Math.min(
        sessionRailLayout.expandedWidthPx + dockMaximumWidth,
        layoutWidth - workspaceMinimumWidth,
      ) / layoutWidth,
      minimum: (railWidth + consoleMinimumWidth) / layoutWidth,
    };
  }
  return {
    maximum: (layoutWidth - consoleMinimumWidth) / layoutWidth,
    minimum: Math.max(
      railWidth + workspaceMinimumWidth,
      layoutWidth - dockMaximumWidth,
    ) / layoutWidth,
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
  const workspaceOpen = useWorkspaceOpen(workspaceSessionID);
  const agentConsoleMode = useAgentConsoleMode();
  const railCollapsed = useRailCollapsed();
  const [projectCreateOpen, setProjectCreateOpen] = useState(false);
  const [pairingCode] = useState(() => pendingPairingCode());
  const [pairingFailed, setPairingFailed] = useState(false);
  const [layoutNode, setLayoutNode] = useState<HTMLDivElement | null>(null);
  const [centeredLayout, setCenteredLayout] = useState(() =>
    resolveCenteredLayoutPresentation({
      chatDockSide: "left",
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
  const workspaceTransitionEnabled = agentConsoleMode !== "floating";
  const [workspacePresent, setWorkspacePresent] = useState(workspaceRequestedOpen);
  const [workspaceTransition, setWorkspaceTransition] = useState<WorkspaceTransitionPhase>("idle");
  const workspaceRequestRef = useRef(workspaceRequestedOpen);
  useLayoutEffect(() => {
    if (!workspaceTransitionEnabled) {
      workspaceRequestRef.current = workspaceRequestedOpen;
      setWorkspaceTransition("idle");
      setWorkspacePresent(workspaceRequestedOpen);
      return;
    }
    if (workspaceRequestRef.current === workspaceRequestedOpen) {
      return;
    }
    workspaceRequestRef.current = workspaceRequestedOpen;
    if (workspaceRequestedOpen) {
      setWorkspacePresent(true);
      setWorkspaceTransition("opening");
    } else {
      setWorkspaceTransition("closing");
    }
    const timer = window.setTimeout(() => {
      setWorkspaceTransition("idle");
      if (!workspaceRequestedOpen) {
        setWorkspacePresent(false);
      }
    }, workspaceTransitionDelay());
    return () => window.clearTimeout(timer);
  }, [workspaceRequestedOpen, workspaceTransitionEnabled]);
  const effectiveWorkspaceOpen = canUseWorkspace && workspacePresent;
  const workspaceVisible = effectiveWorkspaceOpen && workspaceTransition !== "closing";
  const workspaceDockRequested =
    effectiveWorkspaceOpen && agentConsoleMode !== "floating";
  const chatDockSide = agentConsoleMode === "dock-right" ? "right" : "left";
  const updateCenteredLayout = useCallback(
    (layoutWidth: number, nextSplitRatio = dockSplitRatioRef.current) => {
      const expandedBounds = dockSplitRatioBounds({
        chatDockSide,
        layoutWidth,
        railCollapsed: false,
      });
      const feasibleExpandedRatio = clamp(
        nextSplitRatio,
        expandedBounds.minimum,
        expandedBounds.maximum,
      );
      const next = resolveCenteredLayoutPresentation({
        chatDockSide,
        constraints: centeredLayoutConstraints,
        layoutWidth,
        leftGroupRatio: feasibleExpandedRatio,
        workspaceDockRequested,
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
    [chatDockSide, workspaceDockRequested],
  );
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
  const consoleDisplayMode: ConsoleDisplayMode =
    effectiveWorkspaceOpen && !workspaceOverlay ? agentConsoleMode : "full";
  const showConsoleSplit = showSplit && consoleDisplayMode !== "floating";
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
      updateCenteredLayout(nextWidth);
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
  }, [layoutNode, updateCenteredLayout]);

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
      const presentation = updateCenteredLayout(layoutWidth, normalized);
      const bounds = dockSplitRatioBounds({
        chatDockSide,
        layoutWidth,
        railCollapsed:
          getRailCollapsedPreference() ||
          presentation.railResponsiveCollapsed,
      });
      normalized = clamp(normalized, bounds.minimum, bounds.maximum);
      updateCenteredLayout(layoutWidth, normalized);
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
    commitDockSplitRatio(dockSplitRatioRef.current + delta / layoutWidth);
  };

  const startDockResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (
      event.button !== 0 ||
      !layoutNode ||
      !agentConsoleRef.current ||
      (consoleDisplayMode !== "dock-left" && consoleDisplayMode !== "dock-right")
    ) {
      return;
    }
    event.preventDefault();
    const pointerID = event.pointerId;
    const resizeHandle = event.currentTarget;
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

    let liveSplitRatio = dockSplitRatioRef.current;
    let resizeFrame = 0;
    let pendingClientX: number | undefined;
    let cleaned = false;
    const update = (clientX: number) => {
      const layoutRect = layoutNode.getBoundingClientRect();
      const layoutWidth = layoutRect.width;
      const rawRatio = (clientX - layoutRect.left) / layoutWidth;
      const presentation = updateCenteredLayout(layoutWidth, rawRatio);
      const liveRailCollapsed =
        getRailCollapsedPreference() ||
        presentation.railResponsiveCollapsed;
      const bounds = dockSplitRatioBounds({
        chatDockSide,
        layoutWidth,
        railCollapsed: liveRailCollapsed,
      });
      liveSplitRatio = clamp(rawRatio, bounds.minimum, bounds.maximum);
      dockSplitRatioRef.current = liveSplitRatio;
      const railWidth = liveRailCollapsed
        ? 0
        : sessionRailLayout.expandedWidthPx;
      const leftGroupWidth = layoutWidth * liveSplitRatio;
      const chatWidth = chatDockSide === "left"
        ? leftGroupWidth - railWidth
        : layoutWidth - leftGroupWidth;
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
  const consoleNeedsLeftInset =
    consoleDisplayMode === "full" || consoleDisplayMode === "dock-left";
  const workspaceStartsAtStageLeft = consoleDisplayMode !== "dock-left";
  const workspaceToolbarPadding =
    railCollapsed && workspaceStartsAtStageLeft
      ? workspaceOverlay
        ? `max(var(--toolbar-edge-inset), calc(var(--traffic-inset) + var(--toolbar-edge-inset) - (100vw - min(100vw, ${workspaceLayout.drawerWidthPx}px))))`
        : "calc(var(--traffic-inset) + var(--rail-toggle-left) + var(--toolbar-icon-button-size) + var(--rail-title-gap))"
      : "0.75rem";
  const renderedDockSplitRatio = consoleInteracting
    ? dockSplitRatioRef.current
    : dockSplitRatio;
  const renderedChatRatio = chatDockSide === "left"
    ? renderedDockSplitRatio
    : 1 - renderedDockSplitRatio;
  const renderedChatPercent = `${renderedChatRatio * 100}%`;
  const railAdjustment = railCollapsed
    ? 0
    : chatDockSide === "left"
      ? -(1 - renderedDockSplitRatio) * sessionRailLayout.expandedWidthPx
      : renderedChatRatio * sessionRailLayout.expandedWidthPx;
  const preferredDockWidth = railAdjustment === 0
    ? renderedChatPercent
    : `calc(${renderedChatPercent} ${railAdjustment > 0 ? "+" : "-"} ${Math.abs(railAdjustment)}px)`;
  const renderedChatContainerWidth = `${renderedChatRatio * 100}cqw`;
  const preferredDockContainerWidth = railAdjustment === 0
    ? renderedChatContainerWidth
    : `calc(${renderedChatContainerWidth} ${railAdjustment > 0 ? "+" : "-"} ${Math.abs(railAdjustment)}px)`;
  const renderedDockMaximumWidth =
    dockMaximumWidth +
    (railCollapsed && chatDockSide === "left"
      ? sessionRailLayout.expandedWidthPx
      : 0);
  const dockedConsoleWidth = `clamp(min(${consoleMinimumWidth}px, 50%), ${preferredDockWidth}, min(${renderedDockMaximumWidth}px, calc(100% - min(${workspaceMinimumWidth}px, 50%))))`;
  const dockedWorkspaceWidth = `max(0px, calc(100cqw - clamp(min(${consoleMinimumWidth}px, 50cqw), ${preferredDockContainerWidth}, min(${renderedDockMaximumWidth}px, calc(100cqw - min(${workspaceMinimumWidth}px, 50cqw)))) - 1px))`;
  const workspaceSurfaceStyle = {
    "--workspace-toolbar-pl": workspaceToolbarPadding,
    order: consoleDisplayMode === "dock-left" ? 2 : 0,
    width: workspaceOverlay ? `min(100%, ${workspaceLayout.drawerWidthPx}px)` : undefined,
  } as CSSProperties;
  const chatOccupiesStageTopRight =
    consoleDisplayMode === "full" ||
    consoleDisplayMode === "dock-right" ||
    workspaceTransition === "closing";
  const workspaceOccupiesStageTopRight = consoleDisplayMode !== "dock-right";
  const stageToolbarActionCount: 0 | 1 | 2 =
    !canUseWorkspace
      ? 0
      : workspaceVisible && !workspaceOverlay
        ? 2
        : 1;

  const chatArea = (
    <main
      className={cn(
        "flex h-full w-full min-w-0 flex-col",
        consoleDisplayMode === "floating"
          ? "overflow-visible bg-transparent"
          : "overflow-hidden bg-background",
      )}
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
        <ResizablePanel id="primary" className="min-h-0" minSize={splitLayout.minPanePx}>
          <ChatPane
            draftActive={draftActive}
            draftProjectID={draftActive ? draftProjectID : undefined}
            presentation={consoleDisplayMode === "floating" ? "floating" : "default"}
            reserveTopLeftInset={consoleNeedsLeftInset}
            reserveTopRightActions={
              chatOccupiesStageTopRight ? stageToolbarActionCount : 0
            }
            role="primary"
            sessionID={selectedSessionID}
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

  const workspaceToggleLabel = t(
    workspaceRequestedOpen ? "workspace.close" : "workspace.open",
  );
  const stageToolbarActions = canUseWorkspace ? (
    <div className="no-drag-region pointer-events-auto absolute top-0 right-(--workspace-toggle-right) z-[60] flex h-(--toolbar-h) items-center gap-2">
      {workspaceVisible && !workspaceOverlay
        ? <AgentConsoleLayoutControl />
        : null}
      <div className="pudding-workspace-toggle flex items-center">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={workspaceToggleLabel}
              aria-pressed={workspaceRequestedOpen}
              className="no-drag-region pointer-events-auto"
              size="icon-sm"
              tabIndex={-1}
              type="button"
              variant="ghost"
              onClick={() => setWorkspaceOpen(workspaceSessionID, !workspaceRequestedOpen)}
            >
              {workspaceRequestedOpen ? <PanelRightClose /> : <PanelRightOpen />}
            </Button>
          </TooltipTrigger>
          <TooltipContent align="end" side="bottom">
            {workspaceToggleLabel}
          </TooltipContent>
        </Tooltip>
      </div>
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
      data-dock-side={agentConsoleMode === "dock-right" ? "left" : "right"}
      data-transition={workspaceTransition}
      inert={!workspaceVisible}
      style={workspaceSurfaceStyle}
    >
      <WorkspacePane
        activeSessionID={workspaceSessionID}
        reserveTopRightActions={
          workspaceOccupiesStageTopRight ? stageToolbarActionCount : 0
        }
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
        docked && "pudding-workspace-width-transition",
        consoleDisplayMode === "floating"
          ? "pointer-events-none overflow-visible"
          : "overflow-hidden",
      )}
      data-mode={consoleDisplayMode}
      style={{
        flexShrink: 0,
        height: consoleDisplayMode === "floating" ? `min(${floatingDefaultHeight}px, 100%)` : "100%",
        order: consoleDisplayMode === "dock-right" ? 2 : 0,
        position: "relative",
        width: consoleDisplayMode === "floating"
          ? `min(${floatingDefaultWidth}px, 100%)`
          : docked
            ? (workspaceVisible ? dockedConsoleWidth : "100%")
            : "100%",
        zIndex: consoleDisplayMode === "floating" ? 40 : "auto",
      }}
    >
      {chatArea}
    </div>
  );
  const sessionStage = (
    <>
      {/* 始终保留同一父节点，避免 floating 与 docked 互切时重挂 ChatPane。 */}
      <div
        className={cn(
          consoleDisplayMode === "floating"
            ? "pointer-events-none absolute z-40 flex items-end justify-center"
            : "contents",
        )}
        style={
          consoleDisplayMode === "floating"
            ? {
                inset: `calc(var(--toolbar-h) + ${floatingInset}px) ${floatingInset}px ${floatingBottomInset}px`,
              }
            : undefined
        }
      >
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
          aria-valuenow={Math.round(renderedDockSplitRatio * 100)}
          className={cn(
            "pudding-shell-divider group no-drag-region relative z-50 order-1 flex h-full w-px shrink-0 cursor-ew-resize touch-none items-center justify-center outline-none transition-opacity duration-[var(--workspace-transition-duration)] before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 focus-visible:before:bg-muted-foreground/80",
            !workspaceVisible && "pointer-events-none opacity-0",
          )}
          role="separator"
          tabIndex={0}
          onKeyDown={(event) => {
            const increaseKey =
              consoleDisplayMode === "dock-left" ? "ArrowRight" : "ArrowLeft";
            const decreaseKey =
              consoleDisplayMode === "dock-left" ? "ArrowLeft" : "ArrowRight";
            if (event.key !== increaseKey && event.key !== decreaseKey) {
              return;
            }
            event.preventDefault();
            moveDockDivider(
              event.key === increaseKey
                ? consoleDisplayMode === "dock-left" ? 20 : -20
                : consoleDisplayMode === "dock-left" ? -20 : 20,
            );
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
      <BrowserRuntimeProvider token={token}>
        <TooltipProvider delayDuration={250}>
          <OAuthReturnHandler token={token} />
          <div className="relative flex h-full overflow-hidden">
            <div
              aria-hidden="true"
              className="drag-region absolute inset-x-0 top-0 z-20 h-(--toolbar-h)"
            />
            <div
              ref={setLayoutNode}
              className="relative flex h-full min-w-0 flex-1 bg-background"
            >
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
                      : agentConsoleMode === "floating"
                        ? "canvas"
                        : "docked"
                }
                className={cn(
                  "pudding-session-stage relative h-full min-w-0 flex-1 overflow-hidden bg-background",
                  (consoleDisplayMode === "full" || docked) && "flex",
                )}
                style={{
                  "--workspace-inline-content-width": dockedWorkspaceWidth,
                  "--workspace-transition-duration": `${workspaceTransitionDurationMs}ms`,
                } as CSSProperties}
              >
                {standalonePane || sessionStage}
                {stageToolbarActions}
              </div>
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
        </TooltipProvider>
      </BrowserRuntimeProvider>
    </EditorTypographyProvider>
  );
}
