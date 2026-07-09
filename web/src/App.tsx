import { useSearch } from "@tanstack/react-router";
import { PanelRight } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent } from "react";
import { useGroupRef } from "react-resizable-panels";

import { CanvasPane } from "@/components/CanvasPane";
import { ChatPane } from "@/components/ChatPane";
import { AppsPane } from "@/components/AppsPane";
import { SessionRail } from "@/components/SessionRail";
import { SettingsDialog } from "@/components/SettingsDialog";
import { PairingGate, TokenGate } from "@/components/TokenGate";
import { claimMobilePairing } from "@/api/client";
import { Button } from "@/components/ui/button";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Toaster } from "@/components/ui/sonner";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { WorkspaceResizableHandle } from "@/components/WorkspaceResizableHandle";
import { useIsMobile } from "@/hooks/use-mobile";
import { useVisibleSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import {
  layoutStorageKeys,
  resizeTargetMinimumSize,
  splitLayout,
  workspaceLayout,
} from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { cn } from "@/lib/utils";
import { useCanvasMCP } from "@/mcp/canvasTools";
import { setCanvasOpen, useCanvasOpen } from "@/state/canvasStore";
import { setRailLayoutForcedCollapsed } from "@/state/railStore";
import { clearPendingPairingCode, pendingPairingCode } from "@/state/token";
import { setToken, useToken } from "@/state/tokenStore";

function readSavedSplitLayout() {
  return readPanelLayout(layoutStorageKeys.splitRatio, splitLayout.fallback, {
    minPercent: splitLayout.minPercent,
    maxPercent: splitLayout.maxPercent,
  });
}

function readSavedWorkspaceLayout() {
  return readPanelLayout(layoutStorageKeys.workspaceRatio, workspaceLayout.fallback, {
    minPercent: workspaceLayout.minPercent,
    maxPercent: workspaceLayout.maxPercent,
  });
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function clampWorkspaceCanvasRatio(workspaceWidth: number, canvasRatio: number) {
  if (workspaceWidth <= 0) {
    return clamp(canvasRatio, workspaceLayout.minPercent, workspaceLayout.maxPercent);
  }
  const maxCanvas = Math.max(0, workspaceWidth - workspaceLayout.minChatPx);
  const minCanvas = Math.min(
    maxCanvas,
    Math.max(workspaceLayout.minCanvasPx, workspaceWidth - workspaceLayout.maxChatPx),
  );
  const canvasWidth = clamp((workspaceWidth * canvasRatio) / 100, minCanvas, maxCanvas);
  return clamp((canvasWidth / workspaceWidth) * 100, workspaceLayout.minPercent, workspaceLayout.maxPercent);
}

function readSavedWorkspaceCanvasRatio() {
  return readSavedWorkspaceLayout().canvas;
}

function saveWorkspaceCanvasRatio(canvasRatio: number) {
  savePanelLayout(layoutStorageKeys.workspaceRatio, {
    chat: 100 - canvasRatio,
    canvas: canvasRatio,
  });
}

export function App() {
  const token = useToken();
  const { session: selectedSessionID, draft, split: splitSessionID, view } = useSearch({ from: "/" });
  const { t } = useI18n();
  const isMobile = useIsMobile();
  const canvasOpen = useCanvasOpen();
  const [pairingCode] = useState(() => pendingPairingCode());
  const [pairingFailed, setPairingFailed] = useState(false);
  const [leftWorkspaceNode, setLeftWorkspaceNode] = useState<HTMLDivElement | null>(null);
  const [workspaceNode, setWorkspaceNode] = useState<HTMLDivElement | null>(null);
  const [canvasRatio, setCanvasRatio] = useState(() => readSavedWorkspaceCanvasRatio());
  const [workspaceResizing, setWorkspaceResizing] = useState(false);
  const splitGroupRef = useGroupRef();
  const canvasRatioRef = useRef(canvasRatio);
  const workspaceResizeCleanupRef = useRef<(() => void) | null>(null);
  // 上下分屏(docs/design.md 2.2):pane 三件套整体复用,路由是唯一事实源;
  // split 与主 pane 相同的会话不重复渲染
  const appsActive = view === "apps";
  const showSplit = !appsActive && Boolean(splitSessionID && splitSessionID !== selectedSessionID);
  const draftActive = !appsActive && draft === "1" && !selectedSessionID;
  const canUseCanvas = !appsActive && Boolean(selectedSessionID);
  const effectiveCanvasOpen = canUseCanvas && canvasOpen;
  const activeSessionIDs = (appsActive ? [] : [selectedSessionID, showSplit ? splitSessionID : undefined]).filter(
    (sessionID): sessionID is string => Boolean(sessionID),
  );

  // SSE 是 session-scoped,不是 pane-scoped。visible sessions 在 App 层统一去重订阅,
  // ChatPane 只负责 pane-local UI/滚动状态。
  useVisibleSessionEvents(activeSessionIDs, token);
  useCanvasMCP(token);

  useLayoutEffect(() => {
    if (!leftWorkspaceNode) {
      setRailLayoutForcedCollapsed(false);
      return;
    }
    const update = () => {
      setRailLayoutForcedCollapsed(leftWorkspaceNode.getBoundingClientRect().width < workspaceLayout.railAutoCollapsePx);
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(leftWorkspaceNode);
    return () => {
      observer.disconnect();
    };
  }, [leftWorkspaceNode]);

  useEffect(() => {
    canvasRatioRef.current = canvasRatio;
  }, [canvasRatio]);

  useEffect(() => {
    return () => {
      workspaceResizeCleanupRef.current?.();
    };
  }, []);

  useEffect(() => {
    if (token) {
      return;
    }
    if (!pairingCode) {
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
    const showSplit = Boolean(splitSessionID && splitSessionID !== selectedSessionID);
    if (!showSplit) {
      group.setLayout(splitLayout.closed);
      return;
    }
    group.setLayout(readSavedSplitLayout());
  }, [selectedSessionID, splitGroupRef, splitSessionID]);

  if (!token) {
    if (pairingCode) {
      return <PairingGate failed={pairingFailed} />;
    }
    return <TokenGate />;
  }

  const startWorkspaceResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!effectiveCanvasOpen || event.button !== 0) {
      return;
    }
    event.preventDefault();
    const resizeNode = workspaceNode;
    if (!resizeNode) {
      return;
    }
    const workspaceRect = resizeNode.getBoundingClientRect();
    if (!workspaceRect || workspaceRect.width <= 0) {
      return;
    }
    workspaceResizeCleanupRef.current?.();
    const workspaceWidth = Math.round(workspaceRect.width);
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    const previousWorkspaceResizeAttr = document.documentElement.getAttribute("data-workspace-resizing");
    setWorkspaceResizing(true);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    document.documentElement.setAttribute("data-workspace-resizing", "true");

    let cleaned = false;
    const cleanup = () => {
      if (cleaned) {
        return;
      }
      cleaned = true;
      document.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
      window.removeEventListener("blur", handlePointerUp);
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      if (previousWorkspaceResizeAttr === null) {
        document.documentElement.removeAttribute("data-workspace-resizing");
      } else {
        document.documentElement.setAttribute("data-workspace-resizing", previousWorkspaceResizeAttr);
      }
      setWorkspaceResizing(false);
      setCanvasRatio(canvasRatioRef.current);
      saveWorkspaceCanvasRatio(canvasRatioRef.current);
      if (workspaceResizeCleanupRef.current === cleanup) {
        workspaceResizeCleanupRef.current = null;
      }
    };
    const handlePointerMove = (moveEvent: PointerEvent) => {
      const nextCanvasRatio = ((workspaceRect.right - moveEvent.clientX) / workspaceWidth) * 100;
      const next = clampWorkspaceCanvasRatio(workspaceWidth, nextCanvasRatio);
      canvasRatioRef.current = next;
      resizeNode.style.setProperty("--workspace-canvas-ratio", `${next}%`);
    };
    const handlePointerUp = () => {
      cleanup();
    };

    document.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerUp);
    window.addEventListener("blur", handlePointerUp);
    workspaceResizeCleanupRef.current = cleanup;
  };

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
        <ResizablePanel
          id="primary"
          className="min-h-0"
          minSize={splitLayout.minPanePx}
        >
          <ChatPane
            reserveTopRightAction={canUseCanvas && !effectiveCanvasOpen}
            token={token}
            sessionID={selectedSessionID}
            draftActive={draftActive}
            role="primary"
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

  const canvasToggle = canUseCanvas ? (
    <div className="pudding-canvas-toggle no-drag-region pointer-events-auto absolute top-0 right-(--canvas-toggle-right) z-[100] flex h-(--toolbar-h) items-center">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-label={t("canvas.toggle")}
            aria-pressed={effectiveCanvasOpen}
            className="no-drag-region pointer-events-auto aria-pressed:bg-muted aria-pressed:text-foreground dark:aria-pressed:bg-muted/50"
            size="icon-sm"
            tabIndex={-1}
            type="button"
            variant="ghost"
            onClick={() => setCanvasOpen(!canvasOpen)}
          >
            <PanelRight />
          </Button>
        </TooltipTrigger>
        <TooltipContent align="end" side="bottom">
          {t("canvas.toggle")}
        </TooltipContent>
      </Tooltip>
    </div>
  ) : null;

  const mainPane = appsActive ? (
    <AppsPane token={token} />
  ) : !canUseCanvas ? (
    chatArea
  ) : isMobile ? (
    <>
      {chatArea}
      <Sheet open={effectiveCanvasOpen} onOpenChange={setCanvasOpen}>
        <SheetContent
          className="w-[min(28rem,92vw)] max-w-none gap-0 p-0"
          side="right"
        >
          <SheetHeader className="sr-only">
            <SheetTitle>{t("canvas.title")}</SheetTitle>
            <SheetDescription>{t("canvas.empty")}</SheetDescription>
          </SheetHeader>
          <CanvasPane key={selectedSessionID || "canvas"} token={token} sessionID={selectedSessionID} />
        </SheetContent>
      </Sheet>
    </>
  ) : (
    chatArea
  );

  const leftWorkspace = (
    <div ref={setLeftWorkspaceNode} className="relative flex h-full min-w-0 bg-background">
      <SessionRail
        activeSessionIDs={activeSessionIDs}
        draftActive={draftActive}
        selectedSessionID={appsActive ? undefined : selectedSessionID}
        token={token}
      />
      <div className="relative h-full min-w-0 flex-1 bg-background">{mainPane}</div>
    </div>
  );

  const workspaceCanvasStyle = {
    "--workspace-canvas-ratio": `${canvasRatio}%`,
    "--workspace-canvas-max-width": `max(0px, calc(100% - ${workspaceLayout.minChatPx}px))`,
    "--workspace-canvas-min-width": `min(var(--workspace-canvas-max-width), max(${workspaceLayout.minCanvasPx}px, calc(100% - ${workspaceLayout.maxChatPx}px)))`,
    "--workspace-canvas-width": "clamp(var(--workspace-canvas-min-width), var(--workspace-canvas-ratio), var(--workspace-canvas-max-width))",
  } as CSSProperties;

  const workspaceContent =
    isMobile || !canUseCanvas ? (
      leftWorkspace
    ) : (
      <div ref={setWorkspaceNode} className="relative h-full min-w-0 overflow-hidden bg-background" style={workspaceCanvasStyle}>
        <div
          className={cn(
            "workspace-split-pane absolute inset-y-0 left-0 min-w-0 transition-[right] duration-200 ease-out",
            workspaceResizing && "transition-none",
          )}
          style={{ right: effectiveCanvasOpen ? "var(--workspace-canvas-width)" : 0 }}
        >
          {leftWorkspace}
        </div>
        <div
          aria-hidden={!effectiveCanvasOpen}
          className={cn(
            "workspace-split-pane absolute inset-y-0 min-w-0 overflow-visible border-l border-border transition-[right] duration-200 ease-out",
            workspaceResizing && "transition-none",
            !effectiveCanvasOpen && "pointer-events-none",
          )}
          style={{
            right: effectiveCanvasOpen ? 0 : "calc(0px - var(--workspace-canvas-width) - 32px)",
            width: "var(--workspace-canvas-width)",
          }}
        >
          <div
            aria-label={t("layout.resizeHint")}
            aria-orientation="vertical"
            className="group absolute inset-y-0 left-0 z-40 w-3 -translate-x-1/2 cursor-col-resize bg-transparent"
            role="separator"
            tabIndex={effectiveCanvasOpen ? 0 : -1}
            onPointerDown={startWorkspaceResize}
          >
            <div
              className={cn(
                "absolute top-1/2 left-1/2 h-8 w-[3px] -translate-x-[calc(50%+1px)] -translate-y-1/2 rounded-lg bg-muted-foreground/55 opacity-0 transition-opacity group-hover:opacity-100",
                workspaceResizing && "opacity-100",
              )}
            />
          </div>
          <CanvasPane key={selectedSessionID || "canvas"} token={token} sessionID={selectedSessionID} />
        </div>
        {workspaceResizing ? (
          <div
            aria-hidden="true"
            className="no-drag-region fixed inset-0 z-[1000] cursor-col-resize touch-none select-none bg-transparent"
          />
        ) : null}
      </div>
    );

  return (
    <TooltipProvider delayDuration={250}>
      <div className="relative flex h-full overflow-hidden">
        <div aria-hidden="true" className="drag-region absolute inset-x-0 top-0 z-20 h-(--toolbar-h)" />
        <div className="relative h-full min-w-0 flex-1 bg-background">
          {workspaceContent}
          {canvasToggle}
        </div>
      </div>
      <SettingsDialog token={token} showTrigger={false} />
      <Toaster />
    </TooltipProvider>
  );
}
