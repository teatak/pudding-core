import { useSearch } from "@tanstack/react-router";
import { PanelRight } from "lucide-react";
import { useEffect, useMemo } from "react";
import { useGroupRef } from "react-resizable-panels";

import { CanvasPane } from "@/components/CanvasPane";
import { ChatPane } from "@/components/ChatPane";
import { SessionRail } from "@/components/SessionRail";
import { TokenGate } from "@/components/TokenGate";
import { Button } from "@/components/ui/button";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Toaster } from "@/components/ui/sonner";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { WorkspaceResizableHandle } from "@/components/WorkspaceResizableHandle";
import { useI18n } from "@/i18n";
import {
  layoutStorageKeys,
  resizeTargetMinimumSize,
  splitLayout,
  workspaceLayout,
} from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { setCanvasOpen, useCanvasOpen } from "@/state/canvasStore";
import { useToken } from "@/state/tokenStore";

export function App() {
  const token = useToken();
  const { session: selectedSessionID, split: splitSessionID } = useSearch({ from: "/" });
  const { t } = useI18n();
  const canvasOpen = useCanvasOpen();
  const workspaceGroupRef = useGroupRef();
  const splitGroupRef = useGroupRef();
  const savedSplitLayout = useMemo(
    () =>
      readPanelLayout(layoutStorageKeys.splitRatio, splitLayout.fallback, {
        minPercent: splitLayout.minPercent,
        maxPercent: splitLayout.maxPercent,
      }),
    [],
  );
  const savedWorkspaceLayout = useMemo(
    () =>
      readPanelLayout(layoutStorageKeys.workspaceRatio, workspaceLayout.fallback, {
        minPercent: workspaceLayout.minPercent,
        maxPercent: workspaceLayout.maxPercent,
      }),
    [],
  );

  useEffect(() => {
    const group = workspaceGroupRef.current;
    if (!group) {
      return;
    }
    if (!canvasOpen) {
      group.setLayout(workspaceLayout.closed);
      return;
    }
    group.setLayout(
      readPanelLayout(layoutStorageKeys.workspaceRatio, workspaceLayout.fallback, {
        minPercent: workspaceLayout.minPercent,
        maxPercent: workspaceLayout.maxPercent,
      }),
    );
  }, [canvasOpen, workspaceGroupRef]);

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
    group.setLayout(
      readPanelLayout(layoutStorageKeys.splitRatio, splitLayout.fallback, {
        minPercent: splitLayout.minPercent,
        maxPercent: splitLayout.maxPercent,
      }),
    );
  }, [selectedSessionID, splitGroupRef, splitSessionID]);

  if (!token) {
    return <TokenGate />;
  }

  // 上下分屏(docs/design.md 2.2):pane 三件套整体复用,路由是唯一事实源;
  // split 与主 pane 相同的会话不重复渲染
  const showSplit = Boolean(splitSessionID && splitSessionID !== selectedSessionID);
  const chatArea = (
    <main className="flex h-full min-w-0 flex-col overflow-hidden bg-background">
      <ResizablePanelGroup
        className="min-h-0 flex-1"
        defaultLayout={showSplit ? savedSplitLayout : splitLayout.closed}
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
          <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
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

  return (
    <TooltipProvider delayDuration={250}>
      <div className="relative flex h-[100svh] overflow-hidden">
        <div aria-hidden="true" className="drag-region absolute inset-x-0 top-0 z-20 h-(--toolbar-h)" />
        <SessionRail token={token} selectedSessionID={selectedSessionID} />
        {/* rail 固定宽度,不参与 resize。核心工作区只在 chat/canvas 之间分配空间。 */}
        <div className="relative h-full min-w-0 flex-1 bg-background">
          <div className="no-drag-region absolute top-0 right-[13px] z-30 flex h-(--toolbar-h) items-center">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  aria-label={t("canvas.toggle")}
                  aria-pressed={canvasOpen}
                  className={
                    canvasOpen
                      ? "bg-muted text-muted-foreground hover:bg-muted/80 hover:text-muted-foreground"
                      : "text-muted-foreground hover:text-muted-foreground"
                  }
                  size="icon-sm"
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
          <ResizablePanelGroup
            className="h-full min-w-0"
            defaultLayout={canvasOpen ? savedWorkspaceLayout : workspaceLayout.closed}
            groupRef={workspaceGroupRef}
            id="workspace"
            orientation="horizontal"
            resizeTargetMinimumSize={resizeTargetMinimumSize}
            onLayoutChanged={(layout) => {
              if (canvasOpen && typeof layout.canvas === "number" && layout.canvas > 0) {
                savePanelLayout(layoutStorageKeys.workspaceRatio, layout);
              }
            }}
          >
            <ResizablePanel
              id="chat"
              className="min-w-0"
              minSize={workspaceLayout.minChatPx}
            >
              {chatArea}
            </ResizablePanel>
            <WorkspaceResizableHandle
              id="chat-canvas"
              aria-label={t("layout.resizeHint")}
              className={canvasOpen ? undefined : "hidden"}
              disabled={!canvasOpen}
            />
            <ResizablePanel
              id="canvas"
              className="min-w-0"
              collapsedSize="0%"
              collapsible
              minSize={workspaceLayout.minCanvasPx}
            >
              {canvasOpen ? <CanvasPane /> : null}
            </ResizablePanel>
          </ResizablePanelGroup>
        </div>
      </div>
      <Toaster />
    </TooltipProvider>
  );
}
