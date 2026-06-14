import { useSearch } from "@tanstack/react-router";
import { useCallback, useMemo, useRef } from "react";
import { usePanelRef } from "react-resizable-panels";

import { CanvasPane } from "@/components/CanvasPane";
import { ChatPane } from "@/components/ChatPane";
import { SessionRail } from "@/components/SessionRail";
import { TokenGate } from "@/components/TokenGate";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Toaster } from "@/components/ui/sonner";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import {
  layoutStorageKeys,
  railLayout,
  resizeTargetMinimumSize,
  splitLayout,
  workspaceLayout,
} from "@/lib/layoutConstants";
import { readPanelLayout, readPanelPixelSize, savePanelLayout, savePanelPixelSize } from "@/lib/panelLayout";
import { useCanvasOpen } from "@/state/canvasStore";
import { setRailCollapsed, useRailCollapsed } from "@/state/railStore";
import { useToken } from "@/state/tokenStore";

const resizeHandleClass =
  "transition-colors [&>div]:h-8 [&>div]:w-1 [&>div]:bg-muted-foreground/55 hover:[&>div]:bg-muted-foreground/80";

export function App() {
  const token = useToken();
  const { session: selectedSessionID, split: splitSessionID } = useSearch({ from: "/" });
  const { t } = useI18n();
  const canvasOpen = useCanvasOpen();
  const railCollapsed = useRailCollapsed();
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
  const railRef = usePanelRef();
  const workspaceRef = usePanelRef();
  const lastWidthRef = useRef(document.documentElement.clientWidth);
  // rail 像素宽持久化:只存"用户拖拽得到的意图宽",不存"窗口变窄时被迫让位的临时宽"
  // (否则 rail 会随窄窗口越缩越窄地漂移)。onLayoutChanged 在 window resize 时
  // 也会触发,还会捕获 resize 中途的瞬态布局,所以两道闸:
  //   1) 整窗宽刚变 → 这次是 resize(或其瞬态)引发的,不是拖拽 → 跳过;
  //   2) workspace 被挤到底线(minSize)→ rail 正让位、当前宽非其意图宽 → 跳过。
  // 只有"整窗宽未变 + workspace 有余量"才是真·用户拖手柄,才落盘。
  const persistRailWidth = useCallback(() => {
    const width = document.documentElement.clientWidth;
    const widthChanged = width !== lastWidthRef.current;
    lastWidthRef.current = width;
    if (widthChanged) {
      return;
    }
    const workspaceSize = workspaceRef.current?.getSize().inPixels ?? Number.POSITIVE_INFINITY;
    if (workspaceSize <= workspaceLayout.persistGuardPx) {
      return;
    }
    if (railRef.current) {
      savePanelPixelSize(layoutStorageKeys.railWidth, railRef.current.getSize().inPixels);
    }
  }, [railRef, workspaceRef]);

  if (!token) {
    return <TokenGate />;
  }

  // 上下分屏(docs/design.md 2.2):pane 三件套整体复用,路由是唯一事实源;
  // split 与主 pane 相同的会话不重复渲染
  const showSplit = Boolean(splitSessionID && splitSessionID !== selectedSessionID);
  const chatArea = (
    <main className="flex h-full min-w-0 flex-col overflow-hidden bg-background">
      {showSplit ? (
        <ResizablePanelGroup
          className="min-h-0 flex-1"
          defaultLayout={savedSplitLayout}
          orientation="vertical"
          resizeTargetMinimumSize={resizeTargetMinimumSize}
          onLayoutChanged={(layout) => savePanelLayout(layoutStorageKeys.splitRatio, layout)}
        >
          <ResizablePanel id="primary" className="min-h-0" minSize={splitLayout.minPanePx}>
            <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
          </ResizablePanel>
          <ResizableHandle aria-label={t("layout.resizeHint")} className={resizeHandleClass} withHandle />
          <ResizablePanel id="split" className="min-h-0" minSize={splitLayout.minPanePx}>
            <ChatPane token={token} sessionID={splitSessionID} role="split" />
          </ResizablePanel>
        </ResizablePanelGroup>
      ) : (
        <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
      )}
    </main>
  );

  return (
    <TooltipProvider delayDuration={250}>
      <div className="relative h-[100svh] overflow-hidden bg-sidebar">
        {railCollapsed ? <SessionRail token={token} selectedSessionID={selectedSessionID} /> : null}
        {/* 外层只处理导航 rail / 核心 workspace:rail 是像素宽偏好;workspace 是核心区。
            workspace 内部再按 tree 分组:chat/canvas 一组比例,primary/split 一组比例。
            这保持 [primary, split] 作为整体再和 canvas 分配空间,避免三 pane 扁平互相抢比例。 */}
        <ResizablePanelGroup
          className="h-full w-full"
          orientation="horizontal"
          resizeTargetMinimumSize={resizeTargetMinimumSize}
          onLayoutChanged={persistRailWidth}
        >
          {!railCollapsed ? (
            <>
              <ResizablePanel
                id="rail"
                defaultSize={`${readPanelPixelSize(
                  layoutStorageKeys.railWidth,
                  railLayout.defaultPx,
                  railLayout.minPx,
                  railLayout.maxPx,
                )}px`}
                groupResizeBehavior="preserve-pixel-size"
                maxSize={railLayout.maxPx}
                minSize={railLayout.minPx}
                panelRef={railRef}
              >
                <SessionRail token={token} selectedSessionID={selectedSessionID} />
              </ResizablePanel>
              <Tooltip>
                <TooltipTrigger asChild>
                  <ResizableHandle
                    aria-label={t("rail.resizeCollapseHint")}
                    className={resizeHandleClass}
                    withHandle
                    onCollapse={() => setRailCollapsed(true)}
                  />
                </TooltipTrigger>
                <TooltipContent side="right">{t("rail.resizeCollapseHint")}</TooltipContent>
              </Tooltip>
            </>
          ) : null}
          <ResizablePanel
            id="workspace"
            className="min-w-0"
            minSize={workspaceLayout.minChatPx}
            panelRef={workspaceRef}
          >
            {canvasOpen ? (
              <ResizablePanelGroup
                className="h-full min-w-0"
                defaultLayout={savedWorkspaceLayout}
                orientation="horizontal"
                resizeTargetMinimumSize={resizeTargetMinimumSize}
                onLayoutChanged={(layout) => savePanelLayout(layoutStorageKeys.workspaceRatio, layout)}
              >
                <ResizablePanel id="chat" className="min-w-0" minSize={workspaceLayout.minChatPx}>
                  {chatArea}
                </ResizablePanel>
                <ResizableHandle aria-label={t("layout.resizeHint")} className={resizeHandleClass} withHandle />
                <ResizablePanel id="canvas" className="min-w-0" minSize={workspaceLayout.minCanvasPx}>
                  <CanvasPane />
                </ResizablePanel>
              </ResizablePanelGroup>
            ) : (
              chatArea
            )}
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>
      <Toaster />
    </TooltipProvider>
  );
}
