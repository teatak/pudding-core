import { useSearch } from "@tanstack/react-router";
import { PanelRightOpen } from "lucide-react";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { Rnd } from "react-rnd";
import { useGroupRef } from "react-resizable-panels";

import { claimMobilePairing } from "@/api/client";
import { AgentConsoleLayoutControl } from "@/components/AgentConsoleLayoutControl";
import { AppsPane } from "@/components/AppsPane";
import { AppToaster } from "@/components/AppToaster";
import { ChatPane } from "@/components/ChatPane";
import { EditorTypographyProvider } from "@/components/EditorTypographyProvider";
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
import {
  layoutStorageKeys,
  resizeTargetMinimumSize,
  splitLayout,
} from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { saveLastAppRoute } from "@/lib/route";
import { cn } from "@/lib/utils";
import { useCanvasMCP } from "@/mcp/canvasTools";
import { useAgentConsoleMode, type AgentConsoleMode } from "@/state/agentConsoleStore";
import { clearFilePreviews } from "@/state/filePreviewStore";
import { useRailCollapsed } from "@/state/railStore";
import { clearPendingPairingCode, pendingPairingCode } from "@/state/token";
import { setToken, useToken } from "@/state/tokenStore";
import { setWorkspaceOpen, useWorkspaceOpen } from "@/state/workspaceStore";

type StageSize = {
  width: number;
  height: number;
};

type FloatingFrame = StageSize & {
  x: number;
  y: number;
};

type ConsoleDisplayMode = "full" | AgentConsoleMode;

const floatingInset = 16;
const floatingDefaultWidth = 560;
const floatingDefaultHeight = 560;
const consoleMinimumWidth = 380;
const consoleMinimumHeight = 320;
const workspaceMinimumWidth = 320;
const dockMaximumWidth = 640;
const dockRatioStorageKey = "pudding.agentConsoleDockRatio";
const dockRatioFallback = 0.4;

function readSavedSplitLayout() {
  return readPanelLayout(layoutStorageKeys.splitRatio, splitLayout.fallback, {
    minPercent: splitLayout.minPercent,
    maxPercent: splitLayout.maxPercent,
  });
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function finiteOr(value: number, fallback: number) {
  return Number.isFinite(value) ? value : fallback;
}

function readDockRatio() {
  const saved = Number.parseFloat(localStorage.getItem(dockRatioStorageKey) || "");
  return Number.isFinite(saved) ? clamp(saved, 0.2, 0.8) : dockRatioFallback;
}

function readToolbarHeightPx() {
  return Number.parseFloat(
    getComputedStyle(document.documentElement).getPropertyValue("--toolbar-h"),
  ) || 54;
}

export function App() {
  const token = useToken();
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
  const [pairingCode] = useState(() => pendingPairingCode());
  const [pairingFailed, setPairingFailed] = useState(false);
  const [stageNode, setStageNode] = useState<HTMLDivElement | null>(null);
  const [stageSize, setStageSize] = useState<StageSize>({ width: 0, height: 0 });
  const [consoleInteracting, setConsoleInteracting] = useState(false);
  const [dockRatio, setDockRatio] = useState(readDockRatio);
  const [floatingFrame, setFloatingFrame] = useState<FloatingFrame>({
    x: -1,
    y: floatingInset,
    width: floatingDefaultWidth,
    height: floatingDefaultHeight,
  });
  const splitGroupRef = useGroupRef();
  const dockRatioRef = useRef(dockRatio);
  const dockResizeCleanupRef = useRef<(() => void) | null>(null);
  const floatingDragCleanupRef = useRef<(() => void) | null>(null);
  const previewTokenRef = useRef(token);

  const appsActive = view === "apps";
  const projectsActive = view === "projects";
  const standaloneViewActive = appsActive || projectsActive;
  const showSplit = !standaloneViewActive && Boolean(splitSessionID && splitSessionID !== selectedSessionID);
  const draftActive = !standaloneViewActive && draft === "1" && !selectedSessionID;
  const canUseWorkspace = !standaloneViewActive && Boolean(selectedSessionID);
  const effectiveWorkspaceOpen = canUseWorkspace && workspaceOpen;
  const consoleDisplayMode: ConsoleDisplayMode = effectiveWorkspaceOpen ? agentConsoleMode : "full";
  const activeSessionIDs = (
    standaloneViewActive
      ? []
      : [selectedSessionID, showSplit ? splitSessionID : undefined]
  ).filter((sessionID): sessionID is string => Boolean(sessionID));

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
    if (!stageNode || (consoleDisplayMode !== "floating" && consoleDisplayMode !== "collapsed")) {
      return;
    }
    const update = () => {
      const rect = stageNode.getBoundingClientRect();
      setStageSize({
        width: Math.round(rect.width),
        height: Math.round(rect.height),
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(stageNode);
    return () => observer.disconnect();
  }, [consoleDisplayMode, stageNode]);

  useEffect(() => {
    dockRatioRef.current = dockRatio;
  }, [dockRatio]);

  useEffect(
    () => () => {
      dockResizeCleanupRef.current?.();
      floatingDragCleanupRef.current?.();
    },
    [],
  );

  useEffect(() => {
    if (consoleDisplayMode !== "floating") {
      floatingDragCleanupRef.current?.();
    }
  }, [consoleDisplayMode]);

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
    if (!showSplit) {
      group.setLayout(splitLayout.closed);
      return;
    }
    group.setLayout(readSavedSplitLayout());
  }, [showSplit, splitGroupRef]);

  if (!token) {
    if (pairingCode) {
      return <PairingGate failed={pairingFailed} />;
    }
    return <TokenGate />;
  }

  const commitDockRatio = (next: number) => {
    const normalized = clamp(next, 0.2, 0.8);
    dockRatioRef.current = normalized;
    setDockRatio(normalized);
    localStorage.setItem(dockRatioStorageKey, String(normalized));
  };

  const startDockResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (
      event.button !== 0 ||
      !stageNode ||
      (consoleDisplayMode !== "dock-left" && consoleDisplayMode !== "dock-right")
    ) {
      return;
    }
    event.preventDefault();
    const pointerID = event.pointerId;
    const stageRect = stageNode.getBoundingClientRect();
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    const resizeFromRight = consoleDisplayMode === "dock-right";

    dockResizeCleanupRef.current?.();
    setConsoleInteracting(true);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";

    const update = (clientX: number) => {
      const rawWidth = resizeFromRight ? stageRect.right - clientX : clientX - stageRect.left;
      const consoleMin = Math.min(consoleMinimumWidth, stageRect.width / 2);
      const workspaceMin = Math.min(workspaceMinimumWidth, stageRect.width / 2);
      const consoleMax = Math.max(
        consoleMin,
        Math.min(dockMaximumWidth, stageRect.width - workspaceMin),
      );
      const width = clamp(rawWidth, consoleMin, consoleMax);
      const nextRatio = stageRect.width > 0 ? width / stageRect.width : dockRatioFallback;
      dockRatioRef.current = nextRatio;
      stageNode.style.setProperty("--agent-console-dock-width", `${width}px`);
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerCancel);
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      setConsoleInteracting(false);
      dockResizeCleanupRef.current = null;
    };
    const handlePointerMove = (moveEvent: PointerEvent) => {
      if (moveEvent.pointerId === pointerID) {
        moveEvent.preventDefault();
        update(moveEvent.clientX);
      }
    };
    const handlePointerUp = (upEvent: PointerEvent) => {
      if (upEvent.pointerId !== pointerID) {
        return;
      }
      update(upEvent.clientX);
      const nextRatio = dockRatioRef.current;
      cleanup();
      commitDockRatio(nextRatio);
    };
    const handlePointerCancel = (cancelEvent: PointerEvent) => {
      if (cancelEvent.pointerId === pointerID) {
        cleanup();
      }
    };

    window.addEventListener("pointermove", handlePointerMove, { passive: false });
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerCancel);
    dockResizeCleanupRef.current = cleanup;
  };

  const clampFloatingPosition = (
    x: number,
    y: number,
    width: number,
    height: number,
  ) => {
    const stageRect = stageNode?.getBoundingClientRect();
    const stageWidth = stageRect?.width || floatingDefaultWidth + floatingInset * 2;
    const stageHeight = stageRect?.height || floatingDefaultHeight + floatingInset * 2;
    const minimumX = stageWidth - width >= floatingInset * 2 ? floatingInset : 0;
    const minimumY = Math.min(
      readToolbarHeightPx() + floatingInset,
      Math.max(0, stageHeight - floatingInset - 1),
    );
    return {
      x: clamp(
        x,
        minimumX,
        Math.max(minimumX, stageWidth - width - minimumX),
      ),
      y: clamp(
        y,
        minimumY,
        Math.max(minimumY, stageHeight - height - floatingInset),
      ),
    };
  };

  const startFloatingDrag = (consoleNode: HTMLElement) => {
    floatingDragCleanupRef.current?.();
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    const dragShield = document.createElement("div");
    dragShield.className = "pudding-agent-console-drag-shield no-drag-region";
    dragShield.setAttribute("aria-hidden", "true");
    document.body.appendChild(dragShield);
    document.body.style.cursor = "grabbing";
    document.body.style.userSelect = "none";
    consoleNode.dataset.dragging = "true";
    const cleanup = () => {
      dragShield.remove();
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      delete consoleNode.dataset.dragging;
      floatingDragCleanupRef.current = null;
    };

    floatingDragCleanupRef.current = cleanup;
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
    resolvedStageHeight - floatingTopInset - floatingInset,
  );
  const minimumFloatingWidth = Math.min(consoleMinimumWidth, maximumFloatingWidth);
  const minimumFloatingHeight = Math.min(consoleMinimumHeight, maximumFloatingHeight);
  const floatingWidth = clamp(
    finiteOr(floatingFrame.width, floatingDefaultWidth),
    minimumFloatingWidth,
    maximumFloatingWidth,
  );
  const floatingHeight = clamp(
    finiteOr(floatingFrame.height, floatingDefaultHeight),
    minimumFloatingHeight,
    maximumFloatingHeight,
  );
  const floatingXInset = resolvedStageWidth - floatingWidth >= floatingInset * 2 ? floatingInset : 0;
  const requestedFloatingX = finiteOr(
    floatingFrame.x,
    resolvedStageWidth - floatingWidth - floatingXInset,
  );
  const floatingX = clamp(
    requestedFloatingX < 0
      ? resolvedStageWidth - floatingWidth - floatingXInset
      : requestedFloatingX,
    floatingXInset,
    Math.max(floatingXInset, resolvedStageWidth - floatingWidth - floatingXInset),
  );
  const floatingY = clamp(
    finiteOr(floatingFrame.y, floatingTopInset),
    floatingTopInset,
    Math.max(floatingTopInset, resolvedStageHeight - floatingHeight - floatingInset),
  );
  const collapsedWidth = Math.min(720, Math.max(1, resolvedStageWidth - floatingInset * 2));
  const collapsedPosition = {
    x: Math.max(0, Math.round((resolvedStageWidth - collapsedWidth) / 2)),
    y: Math.max(0, resolvedStageHeight - 54 - floatingInset),
  };
  const docked =
    consoleDisplayMode === "dock-left" || consoleDisplayMode === "dock-right";
  const consolePosition =
    consoleDisplayMode === "floating"
      ? { x: floatingX, y: floatingY }
      : consoleDisplayMode === "collapsed"
        ? collapsedPosition
        : { x: 0, y: 0 };
  const consoleSize =
    consoleDisplayMode === "floating"
      ? { width: floatingWidth, height: floatingHeight }
      : consoleDisplayMode === "collapsed"
        ? { width: collapsedWidth, height: 54 }
        : { width: "100%", height: "100%" };

  const consoleAtTrafficLights = consoleDisplayMode === "floating" && floatingX < 120 && floatingY < 54;
  const consoleNeedsLeftInset =
    consoleDisplayMode === "full" || consoleDisplayMode === "dock-left" || consoleAtTrafficLights;
  const workspaceStartsAtStageLeft = consoleDisplayMode !== "dock-left";
  const workspaceToolbarPadding =
    railCollapsed && workspaceStartsAtStageLeft
      ? "calc(var(--traffic-inset) + var(--rail-toggle-left) + var(--toolbar-icon-button-size) + var(--rail-title-gap))"
      : "0.75rem";
  const workspaceSurfaceStyle = {
    "--workspace-toolbar-pl": workspaceToolbarPadding,
    order: consoleDisplayMode === "dock-left" ? 2 : 0,
  } as CSSProperties;
  const stageStyle = {
    "--agent-console-dock-width": `${dockRatio * 100}%`,
  } as CSSProperties;

  const chatArea = (
    <main className="flex h-full min-w-0 flex-col overflow-hidden bg-background">
      <ResizablePanelGroup
        className="min-h-0 flex-1"
        defaultLayout={showSplit ? readSavedSplitLayout() : splitLayout.closed}
        groupRef={splitGroupRef}
        id="split-workspace"
        orientation="vertical"
        resizeTargetMinimumSize={resizeTargetMinimumSize}
        onLayoutChanged={(layout) => {
          if (showSplit && typeof layout.split === "number" && layout.split > 0) {
            savePanelLayout(layoutStorageKeys.splitRatio, layout);
          }
        }}
      >
        <ResizablePanel id="primary" className="min-h-0" minSize={splitLayout.minPanePx}>
          <ChatPane
            compact={consoleDisplayMode === "collapsed"}
            draftActive={draftActive}
            draftProjectID={draftActive ? draftProjectID : undefined}
            headerActions={effectiveWorkspaceOpen ? <AgentConsoleLayoutControl /> : undefined}
            headerDragHandle={consoleDisplayMode === "floating"}
            reserveTopLeftInset={consoleNeedsLeftInset}
            reserveTopRightAction={canUseWorkspace && !effectiveWorkspaceOpen}
            role="primary"
            sessionID={selectedSessionID}
            token={token}
          />
        </ResizablePanel>
        <WorkspaceResizableHandle
          aria-label={t("layout.resizeHint")}
          className={showSplit ? undefined : "hidden"}
          disabled={!showSplit}
        />
        <ResizablePanel
          id="split"
          className="min-h-0"
          collapsedSize="0%"
          collapsible
          minSize={splitLayout.minPanePx}
        >
          {showSplit ? <ChatPane token={token} sessionID={splitSessionID} role="split" /> : null}
        </ResizablePanel>
      </ResizablePanelGroup>
    </main>
  );

  const workspaceToggleLabel = t("workspace.open");
  const workspaceToggle = canUseWorkspace && !effectiveWorkspaceOpen ? (
    <div className="pudding-workspace-toggle no-drag-region pointer-events-auto absolute top-0 right-(--workspace-toggle-right) z-[100] flex h-(--toolbar-h) items-center">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-label={workspaceToggleLabel}
            className="no-drag-region pointer-events-auto"
            size="icon-sm"
            tabIndex={-1}
            type="button"
            variant="ghost"
            onClick={() => setWorkspaceOpen(true)}
          >
            <PanelRightOpen />
          </Button>
        </TooltipTrigger>
        <TooltipContent align="end" side="bottom">
          {workspaceToggleLabel}
        </TooltipContent>
      </Tooltip>
    </div>
  ) : null;

  const standalonePane = appsActive
    ? <AppsPane token={token} />
    : projectsActive
      ? <ProjectsPane token={token} />
      : null;

  const workspaceSurface = (
    <div
      key="workspace"
      className="pudding-workspace-stage h-full min-h-0 min-w-0 flex-1 overflow-hidden bg-background"
      style={workspaceSurfaceStyle}
    >
      <WorkspacePane
        secondarySessionID={showSplit ? splitSessionID : undefined}
        sessionID={selectedSessionID}
        token={token}
      />
    </div>
  );
  const sessionStage = (
    <>
      <Rnd
        key="agent-console-native-drag"
        bounds={consoleDisplayMode === "floating" ? "parent" : undefined}
        cancel=".no-drag-region,button,input,textarea,select,a,[role='button']"
        className={cn(
          "pudding-agent-console flex min-h-0 min-w-0 overflow-hidden",
          consoleDisplayMode === "floating" && "rounded-xl border",
          consoleDisplayMode === "collapsed" && "rounded-xl border",
        )}
        data-mode={consoleDisplayMode}
        disableDragging={consoleDisplayMode !== "floating"}
        dragHandleClassName="pudding-agent-console-drag-handle"
        enableResizing={consoleDisplayMode === "floating"}
        maxHeight={consoleDisplayMode === "floating" ? maximumFloatingHeight : undefined}
        maxWidth={consoleDisplayMode === "floating" ? maximumFloatingWidth : undefined}
        minHeight={consoleDisplayMode === "floating" ? minimumFloatingHeight : 0}
        minWidth={consoleDisplayMode === "floating" ? minimumFloatingWidth : 0}
        position={consolePosition}
        size={consoleSize}
        style={{
          flexShrink: 0,
          order: consoleDisplayMode === "dock-right" ? 2 : 0,
          position:
            consoleDisplayMode === "floating" || consoleDisplayMode === "collapsed"
              ? "absolute"
              : "relative",
          zIndex:
            consoleDisplayMode === "floating" || consoleDisplayMode === "collapsed" ? 40 : "auto",
          transition:
            consoleInteracting || docked
              ? "none"
              : "transform 180ms ease-out, width 180ms ease-out, height 180ms ease-out, border-radius 180ms ease-out",
        }}
        onDragStart={(_event, data) => {
          setConsoleInteracting(true);
          startFloatingDrag(data.node);
        }}
        onDragStop={(_event, data) => {
          const nextPosition = clampFloatingPosition(
            data.x,
            data.y,
            data.node.offsetWidth,
            data.node.offsetHeight,
          );
          floatingDragCleanupRef.current?.();
          setConsoleInteracting(false);
          setFloatingFrame((current) => ({
            ...current,
            ...nextPosition,
          }));
        }}
        onResizeStart={() => setConsoleInteracting(true)}
        onResizeStop={(_event, _direction, ref, _delta, position) => {
          const nextPosition = clampFloatingPosition(
            position.x,
            position.y,
            ref.offsetWidth,
            ref.offsetHeight,
          );
          setConsoleInteracting(false);
          setFloatingFrame({
            x: finiteOr(nextPosition.x, floatingX),
            y: finiteOr(nextPosition.y, floatingY),
            width: finiteOr(ref.offsetWidth, floatingWidth),
            height: finiteOr(ref.offsetHeight, floatingHeight),
          });
        }}
      >
        {chatArea}
      </Rnd>
      {docked ? (
        <div
          key="dock-resize-handle"
          aria-label={t("layout.resizeHint")}
          aria-orientation="vertical"
          aria-valuemax={80}
          aria-valuemin={20}
          aria-valuenow={Math.round(dockRatio * 100)}
          className="group no-drag-region relative z-50 order-1 flex h-full w-px shrink-0 cursor-col-resize touch-none items-center justify-center outline-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-border focus-visible:before:bg-muted-foreground/80"
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
            commitDockRatio(dockRatio + (event.key === increaseKey ? 0.02 : -0.02));
          }}
          onPointerDown={startDockResize}
        >
          <div
            aria-hidden="true"
            className="absolute inset-y-0 left-1/2 z-20 w-3 -translate-x-1/2 cursor-col-resize touch-none"
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
      {effectiveWorkspaceOpen ? workspaceSurface : null}
    </>
  );

  return (
    <EditorTypographyProvider token={token}>
      <TooltipProvider delayDuration={250}>
        <div className="relative flex h-full overflow-hidden">
          <div aria-hidden="true" className="drag-region absolute inset-x-0 top-0 z-20 h-(--toolbar-h)" />
          <div className="relative flex h-full min-w-0 flex-1 bg-background">
            <SessionRail
              activeSessionIDs={activeSessionIDs}
              draftActive={draftActive}
              selectedSessionID={standaloneViewActive ? undefined : selectedSessionID}
              token={token}
            />
            <div
              ref={setStageNode}
              className={cn(
                "relative h-full min-w-0 flex-1 overflow-hidden bg-background",
                (consoleDisplayMode === "full" || docked) && "flex",
              )}
              style={stageStyle}
            >
              {standalonePane || sessionStage}
              {workspaceToggle}
            </div>
          </div>
        </div>
        <SettingsDialog token={token} showTrigger={false} />
        <AppToaster />
      </TooltipProvider>
    </EditorTypographyProvider>
  );
}
