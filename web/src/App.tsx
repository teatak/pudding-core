import { useSearch } from "@tanstack/react-router";

import { CanvasPane } from "@/components/CanvasPane";
import { ChatPane } from "@/components/ChatPane";
import { SplitHandle, useResizableRatio } from "@/components/ResizeHandle";
import { SessionRail } from "@/components/SessionRail";
import { TokenGate } from "@/components/TokenGate";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useCanvasOpen } from "@/state/canvasStore";
import { useToken } from "@/state/tokenStore";

export function App() {
  const token = useToken();
  const { session: selectedSessionID, split: splitSessionID } = useSearch({ from: "/" });
  const canvasOpen = useCanvasOpen();
  const { ratio: splitRatio, startDrag: startSplitDrag } = useResizableRatio({
    key: "pudding.splitRatio",
    fallback: 0.5,
    min: 0.2,
    max: 0.8,
  });

  if (!token) {
    return <TokenGate />;
  }

  // 上下分屏(docs/design.md 2.2):pane 三件套整体复用,路由是唯一事实源;
  // split 与主 pane 相同的会话不重复渲染
  const showSplit = Boolean(splitSessionID && splitSessionID !== selectedSessionID);

  return (
    <TooltipProvider delayDuration={250}>
      {/* 严格左右分割:无外层 margin/卡片化,折叠触发器以 absolute 悬浮 */}
      <div className="relative flex h-[100svh] overflow-hidden bg-sidebar">
        <SessionRail token={token} selectedSessionID={selectedSessionID} />
        <main className="flex h-full min-w-0 flex-1 flex-col overflow-hidden bg-background">
          {showSplit ? (
            <>
              {/* 比例分屏:两 pane 按 splitRatio 分配高度,分隔条可拖拽 */}
              <div className="flex min-h-0 flex-col" style={{ flexBasis: 0, flexGrow: splitRatio }}>
                <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
              </div>
              <SplitHandle onPointerDown={startSplitDrag} />
              <div className="flex min-h-0 flex-col" style={{ flexBasis: 0, flexGrow: 1 - splitRatio }}>
                <ChatPane token={token} sessionID={splitSessionID} role="split" />
              </div>
            </>
          ) : (
            <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
          )}
        </main>
        {canvasOpen ? <CanvasPane /> : null}
      </div>
      <Toaster />
    </TooltipProvider>
  );
}
