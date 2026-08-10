import { useNavigate, useSearch } from "@tanstack/react-router";
import { PanelRightClose, PanelRightOpen } from "@/components/icons";
import {
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
import { PairingGate, TokenGate } from "@/components/TokenGate";
import { Button } from "@/components/ui/button";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { WorkspacePane } from "@/components/workspace/WorkspacePane";
import { WorkspaceResizableHandle } from "@/components/WorkspaceResizableHandle";
import { useVisibleSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import { emitAgentConsoleResizePhase } from "@/lib/agentConsoleResize";
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
  setRailResponsiveCollapsed,
  useRailCollapsed,
} from "@/state/railStore";
import { clearPendingPairingCode, pendingPairingCode } from "@/state/token";
import { setToken, useToken } from "@/state/tokenStore";
import { setWorkspaceOpen, useWorkspaceOpen } from "@/state/workspaceStore";

type StageSize = {
  width: number;
  height: number;
};

type ConsoleDisplayMode = "full" | AgentConsoleMode;

const floatingInset = 16;
const floatingBottomInset = 4;
const consoleMinimumWidth = 380;
const consoleMinimumHeight = 320;
const floatingMinimumWidth = 360;
const floatingDefaultWidth = 640;
const floatingDefaultHeight = 420;
const workspaceMinimumWidth = workspaceLayout.minWorkspacePx;
const dockMaximumWidth = 640;
const dockWidthStorageKey = "pudding.agentConsoleDockWidth";
const dockWidthFallback = 480;

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
  emitAgentConsoleResizePhase("start");
  return () => {
    if (previousValue) {
      root.style.setProperty(property, previousValue, previousPriority);
    } else {
      root.style.removeProperty(property);
    }
    delete root.dataset.agentConsoleResizing;
    emitAgentConsoleResizePhase("end");
  };
}

function readDockWidth() {
  const saved = Number.parseFloat(localStorage.getItem(dockWidthStorageKey) || "");
  return Number.isFinite(saved)
    ? clamp(saved, consoleMinimumWidth, dockMaximumWidth)
    : dockWidthFallback;
}

function readToolbarHeightPx() {
  return Number.parseFloat(
    getComputedStyle(document.documentElement).getPropertyValue("--toolbar-h"),
  ) || 54;
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
  const workspaceOpen = useWorkspaceOpen();
  const agentConsoleMode = useAgentConsoleMode();
  const railCollapsed = useRailCollapsed();
  const [projectCreateOpen, setProjectCreateOpen] = useState(false);
  const [pairingCode] = useState(() => pendingPairingCode());
  const [pairingFailed, setPairingFailed] = useState(false);
  const [layoutNode, setLayoutNode] = useState<HTMLDivElement | null>(null);
  const [layoutWidth, setLayoutWidth] = useState(0);
  const [stageNode, setStageNode] = useState<HTMLDivElement | null>(null);
  const [stageSize, setStageSize] = useState<StageSize>({
    width: 0,
    height: 0,
  });
  const [consoleInteracting, setConsoleInteracting] = useState(false);
  const [dockWidth, setDockWidth] = useState(readDockWidth);
  const splitGroupRef = useGroupRef();
  const agentConsoleRef = useRef<HTMLDivElement | null>(null);
  const dockWidthRef = useRef(dockWidth);
  const dockResizeCleanupRef = useRef<(() => void) | null>(null);
  const previewTokenRef = useRef(token);

  const appsActive = view === "apps";
  const projectsActive = view === "projects";
  const standaloneViewActive = appsActive || projectsActive;
  const showSplit = !standaloneViewActive && Boolean(splitSessionID && splitSessionID !== selectedSessionID);
  const draftActive = !standaloneViewActive && draft === "1" && !selectedSessionID;
  const canUseWorkspace = !standaloneViewActive && Boolean(selectedSessionID);
  const effectiveWorkspaceOpen = canUseWorkspace && workspaceOpen;
  const workspaceDockRequested =
    effectiveWorkspaceOpen && agentConsoleMode !== "floating";
  const centeredLayout = resolveCenteredLayoutPresentation({
    constraints: {
      chatDockMaximumWidth: dockMaximumWidth,
      chatDockMinimumWidth: consoleMinimumWidth,
      leftPairMinimumWidth: workspaceLayout.railAutoCollapsePx,
      railWidth: sessionRailLayout.expandedWidthPx,
      rightPairMinimumWidth: workspaceLayout.drawerBreakpointPx,
      workspaceDockMinimumWidth: workspaceMinimumWidth,
    },
    dockWidth,
    layoutWidth,
    workspaceDockRequested,
  });
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

  useLayoutEffect(() => {
    setRailResponsiveCollapsed(centeredLayout.railResponsiveCollapsed);
  }, [centeredLayout.railResponsiveCollapsed]);

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
    let frame = 0;
    const measure = () => {
      const width = Math.round(layoutNode.getBoundingClientRect().width);
      setLayoutWidth((current) => current === width ? current : width);
    };
    const scheduleMeasure = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    measure();
    const observer = new ResizeObserver(scheduleMeasure);
    observer.observe(layoutNode);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [layoutNode]);

  useLayoutEffect(() => {
    if (!stageNode || consoleDisplayMode !== "floating") {
      return;
    }
    let frame = 0;
    const measure = () => {
      const rect = stageNode.getBoundingClientRect();
      const width = Math.round(rect.width);
      const height = Math.round(rect.height);
      setStageSize((current) => (
        current.width === width && current.height === height
          ? current
          : { width, height }
      ));
    };
    const scheduleMeasure = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    measure();
    const observer = new ResizeObserver(scheduleMeasure);
    observer.observe(stageNode);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [consoleDisplayMode, stageNode]);

  useEffect(() => {
    dockWidthRef.current = dockWidth;
  }, [dockWidth]);

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

  const commitDockWidth = (next: number) => {
    const normalized = clamp(next, consoleMinimumWidth, dockMaximumWidth);
    dockWidthRef.current = normalized;
    setDockWidth(normalized);
    localStorage.setItem(dockWidthStorageKey, String(normalized));
  };

  const startDockResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (
      event.button !== 0 ||
      !stageNode ||
      !agentConsoleRef.current ||
      (consoleDisplayMode !== "dock-left" && consoleDisplayMode !== "dock-right")
    ) {
      return;
    }
    event.preventDefault();
    const pointerID = event.pointerId;
    const resizeHandle = event.currentTarget;
    const agentConsoleNode = agentConsoleRef.current;
    const stageRect = stageNode.getBoundingClientRect();
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    const resizeFromRight = consoleDisplayMode === "dock-right";
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

    let liveWidth = dockWidthRef.current;
    let resizeFrame = 0;
    let pendingClientX: number | undefined;
    let cleaned = false;
    const update = (clientX: number) => {
      const rawWidth = resizeFromRight ? stageRect.right - clientX : clientX - stageRect.left;
      const consoleMin = Math.min(consoleMinimumWidth, stageRect.width / 2);
      const workspaceMin = Math.min(workspaceMinimumWidth, stageRect.width / 2);
      const consoleMax = Math.max(
        consoleMin,
        Math.min(dockMaximumWidth, stageRect.width - workspaceMin),
      );
      const width = clamp(rawWidth, consoleMin, consoleMax);
      liveWidth = width;
      agentConsoleNode.style.width = `${width}px`;
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
      commitDockWidth(liveWidth);
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

  const resolvedStageWidth = stageSize.width || floatingDefaultWidth + floatingInset * 2;
  const resolvedStageHeight = stageSize.height || floatingDefaultHeight + floatingInset * 2;
  const preferredFloatingTopInset = readToolbarHeightPx() + floatingInset;
  const floatingTopInset = Math.min(
    preferredFloatingTopInset,
    Math.max(0, resolvedStageHeight - floatingInset - 1),
  );
  const maximumFloatingWidth = Math.max(1, resolvedStageWidth - floatingInset * 2);
  const maximumFloatingHeight = Math.max(
    1,
    resolvedStageHeight - floatingTopInset - floatingBottomInset,
  );
  const minimumFloatingWidth = Math.min(floatingMinimumWidth, maximumFloatingWidth);
  const minimumFloatingHeight = Math.min(consoleMinimumHeight, maximumFloatingHeight);
  const floatingWidth = clamp(
    floatingDefaultWidth,
    minimumFloatingWidth,
    maximumFloatingWidth,
  );
  const floatingHeight = clamp(
    floatingDefaultHeight,
    minimumFloatingHeight,
    maximumFloatingHeight,
  );
  const floatingXInset = resolvedStageWidth - floatingWidth >= floatingInset * 2 ? floatingInset : 0;
  const maximumFloatingY = Math.max(
    floatingTopInset,
    resolvedStageHeight - floatingHeight - floatingBottomInset,
  );
  const floatingX = Math.max(floatingXInset, (resolvedStageWidth - floatingWidth) / 2);
  // 浮动控制台始终以底部输入栏为锚点；展开只向上增长。
  const floatingY = maximumFloatingY;
  const docked = workspaceDocked;
  const consoleAtTrafficLights = consoleDisplayMode === "floating" && floatingX < 120 && floatingY < 54;
  const consoleNeedsLeftInset =
    consoleDisplayMode === "full" || consoleDisplayMode === "dock-left" || consoleAtTrafficLights;
  const workspaceStartsAtStageLeft = consoleDisplayMode !== "dock-left";
  const workspaceToolbarPadding =
    railCollapsed && workspaceStartsAtStageLeft
      ? "calc(var(--traffic-inset) + var(--rail-toggle-left) + var(--toolbar-icon-button-size) + var(--rail-title-gap))"
      : "0.75rem";
  const dockedConsoleWidth = `clamp(min(380px, 50%), ${dockWidth}px, min(640px, calc(100% - min(380px, 50%))))`;
  const workspaceSurfaceStyle = {
    "--workspace-toolbar-pl": workspaceToolbarPadding,
    order: consoleDisplayMode === "dock-left" ? 2 : 0,
    width: workspaceOverlay ? `min(100%, ${workspaceLayout.drawerWidthPx}px)` : undefined,
  } as CSSProperties;
  const chatOccupiesStageTopRight =
    consoleDisplayMode === "full" ||
    consoleDisplayMode === "dock-right";
  const workspaceOccupiesStageTopRight = consoleDisplayMode !== "dock-right";
  const stageToolbarActionCount: 0 | 1 | 2 =
    !canUseWorkspace
      ? 0
      : effectiveWorkspaceOpen && !workspaceOverlay
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
    effectiveWorkspaceOpen ? "workspace.close" : "workspace.open",
  );
  const stageToolbarActions = canUseWorkspace ? (
    <div className="no-drag-region pointer-events-auto absolute top-0 right-(--workspace-toggle-right) z-[60] flex h-(--toolbar-h) items-center gap-2">
      {effectiveWorkspaceOpen && !workspaceOverlay
        ? <AgentConsoleLayoutControl />
        : null}
      <div className="pudding-workspace-toggle flex items-center">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={workspaceToggleLabel}
              aria-pressed={effectiveWorkspaceOpen}
              className="no-drag-region pointer-events-auto"
              size="icon-sm"
              tabIndex={-1}
              type="button"
              variant="ghost"
              onClick={() => setWorkspaceOpen(!effectiveWorkspaceOpen)}
            >
              {effectiveWorkspaceOpen ? <PanelRightClose /> : <PanelRightOpen />}
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
      aria-hidden={!effectiveWorkspaceOpen}
      className={cn(
        "pudding-workspace-stage h-full min-h-0 min-w-0 flex-1 overflow-hidden bg-background",
        !effectiveWorkspaceOpen && "hidden",
        workspaceOverlay &&
          "absolute inset-y-0 right-0 flex-none border-l border-[var(--workspace-border)] shadow-[-8px_0_24px_-16px_rgb(0_0_0/0.28)]",
      )}
      inert={!effectiveWorkspaceOpen}
      style={workspaceSurfaceStyle}
    >
      <WorkspacePane
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
        consoleDisplayMode === "floating"
          ? "pointer-events-none overflow-visible"
          : "overflow-hidden",
      )}
      data-mode={consoleDisplayMode}
      style={{
        flexShrink: 0,
        height: consoleDisplayMode === "floating" ? floatingHeight : "100%",
        order: consoleDisplayMode === "dock-right" ? 2 : 0,
        position: "relative",
        width: consoleDisplayMode === "floating" ? floatingWidth : docked ? dockedConsoleWidth : "100%",
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
            ? { inset: `${floatingTopInset}px ${floatingXInset}px ${floatingBottomInset}px` }
            : undefined
        }
      >
        {agentConsole}
      </div>
      {workspaceOverlay ? (
        <button
          aria-label={t("workspace.close")}
          className="no-drag-region absolute inset-0 bg-overlay"
          tabIndex={-1}
          type="button"
          onClick={() => setWorkspaceOpen(false)}
        />
      ) : null}
      {docked ? (
        <div
          key="dock-resize-handle"
          aria-label={t("layout.resizeHint")}
          aria-orientation="vertical"
          aria-valuemax={dockMaximumWidth}
          aria-valuemin={consoleMinimumWidth}
          aria-valuenow={Math.round(dockWidth)}
          className="group no-drag-region relative z-50 order-1 flex h-full w-px shrink-0 cursor-ew-resize touch-none items-center justify-center outline-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-border focus-visible:before:bg-muted-foreground/80"
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
            commitDockWidth(dockWidth + (event.key === increaseKey ? 20 : -20));
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
                ref={setStageNode}
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
                  "relative h-full min-w-0 flex-1 overflow-hidden bg-background",
                  (consoleDisplayMode === "full" || docked) && "flex",
                )}
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
          <AppToaster />
        </TooltipProvider>
      </BrowserRuntimeProvider>
    </EditorTypographyProvider>
  );
}
