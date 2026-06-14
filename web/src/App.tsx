import { useSearch } from "@tanstack/react-router";
import { useCallback, useMemo, useRef } from "react";
import { usePanelRef } from "react-resizable-panels";

import { CanvasPane } from "@/components/CanvasPane";
import { ChatPane } from "@/components/ChatPane";
import { SessionRail } from "@/components/SessionRail";
import { TokenGate } from "@/components/TokenGate";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { readPanelLayout, readPanelPixelSize, savePanelLayout, savePanelPixelSize } from "@/lib/panelLayout";
import { useCanvasOpen } from "@/state/canvasStore";
import { setRailCollapsed, useRailCollapsed } from "@/state/railStore";
import { useToken } from "@/state/tokenStore";

export function App() {
  const token = useToken();
  const { session: selectedSessionID, split: splitSessionID } = useSearch({ from: "/" });
  const canvasOpen = useCanvasOpen();
  const railCollapsed = useRailCollapsed();
  const splitLayout = useMemo(() => readPanelLayout("pudding.splitRatio", { primary: 50, split: 50 }), []);
  const railRef = usePanelRef();
  const canvasRef = usePanelRef();
  const mainRef = usePanelRef();
  const lastWidthRef = useRef(document.documentElement.clientWidth);
  // 侧栏像素宽持久化:只存"用户拖拽得到的意图宽",不存"窗口变窄时被迫让位的临时宽"
  // (否则 rail / canvas 会随窄窗口越缩越窄地漂移)。onLayoutChanged 在 window resize 时
  // 也会触发,还会捕获 resize 中途的瞬态布局,所以两道闸:
  //   1) 整窗宽刚变 → 这次是 resize(或其瞬态)引发的,不是拖拽 → 跳过;
  //   2) 会话被挤到底线(minSize)→ 侧栏正让位、当前宽非其意图宽 → 跳过。
  // 只有"整窗宽未变 + 会话有余量"才是真·用户拖手柄,才落盘。
  const persistSideWidths = useCallback(() => {
    const width = document.documentElement.clientWidth;
    const widthChanged = width !== lastWidthRef.current;
    lastWidthRef.current = width;
    if (widthChanged) {
      return;
    }
    const mainSize = mainRef.current?.getSize().inPixels ?? Number.POSITIVE_INFINITY;
    if (mainSize <= 362) {
      return;
    }
    if (railRef.current) {
      savePanelPixelSize("pudding.railWidth", railRef.current.getSize().inPixels);
    }
    if (canvasRef.current) {
      savePanelPixelSize("pudding.canvasWidth", canvasRef.current.getSize().inPixels);
    }
  }, [railRef, canvasRef, mainRef]);

  if (!token) {
    return <TokenGate />;
  }

  // 上下分屏(docs/design.md 2.2):pane 三件套整体复用,路由是唯一事实源;
  // split 与主 pane 相同的会话不重复渲染
  const showSplit = Boolean(splitSessionID && splitSessionID !== selectedSessionID);

  return (
    <TooltipProvider delayDuration={250}>
      <div className="relative h-[100svh] overflow-hidden bg-sidebar">
        {railCollapsed ? <SessionRail token={token} selectedSessionID={selectedSessionID} /> : null}
        {/* 三列横向布局:会话(main)是唯一弹性列(preserve-relative,默认),吸收一切伸缩;
            rail / canvas 定宽(preserve-pixel),各自按像素宽持久化(见 panel 的 onResize)。
            故不存整组百分比 layout——百分比会在换窗口宽 / 换组合时把侧栏还原成错的像素宽。
            任何变化(开关 canvas、收展 rail、缩放窗口)只让会话伸缩,会话 minSize 是它不被
            侧栏压破的底线。 */}
        <ResizablePanelGroup
          className="h-full w-full"
          orientation="horizontal"
          resizeTargetMinimumSize={{ coarse: 32, fine: 8 }}
          onLayoutChanged={persistSideWidths}
        >
          {!railCollapsed ? (
            <>
              <ResizablePanel
                id="rail"
                defaultSize={`${readPanelPixelSize("pudding.railWidth", 268, 220, 420)}px`}
                groupResizeBehavior="preserve-pixel-size"
                maxSize={420}
                minSize={220}
                panelRef={railRef}
              >
                <SessionRail token={token} selectedSessionID={selectedSessionID} />
              </ResizablePanel>
              <ResizableHandle withHandle onCollapse={() => setRailCollapsed(true)} />
            </>
          ) : null}
          <ResizablePanel id="main" className="min-w-0" minSize={360} panelRef={mainRef}>
            <main className="flex h-full min-w-0 flex-col overflow-hidden bg-background">
              {showSplit ? (
                <ResizablePanelGroup
                  className="min-h-0 flex-1"
                  defaultLayout={splitLayout}
                  orientation="vertical"
                  onLayoutChanged={(layout) => savePanelLayout("pudding.splitRatio", layout)}
                >
                  <ResizablePanel id="primary" className="min-h-0" maxSize="80%" minSize="20%">
                    <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
                  </ResizablePanel>
                  <ResizableHandle withHandle />
                  <ResizablePanel id="split" className="min-h-0" maxSize="80%" minSize="20%">
                    <ChatPane token={token} sessionID={splitSessionID} role="split" />
                  </ResizablePanel>
                </ResizablePanelGroup>
              ) : (
                <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
              )}
            </main>
          </ResizablePanel>
          {canvasOpen ? (
            <>
              <ResizableHandle withHandle />
              <ResizablePanel
                id="canvas"
                defaultSize={`${readPanelPixelSize("pudding.canvasWidth", 320, 240, Number.POSITIVE_INFINITY)}px`}
                groupResizeBehavior="preserve-pixel-size"
                minSize={240}
                panelRef={canvasRef}
              >
                <CanvasPane />
              </ResizablePanel>
            </>
          ) : null}
        </ResizablePanelGroup>
      </div>
      <Toaster />
    </TooltipProvider>
  );
}
