import { useSearch } from "@tanstack/react-router";

import { ChatPane } from "@/components/ChatPane";
import { SessionRail } from "@/components/SessionRail";
import { TokenGate } from "@/components/TokenGate";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useToken } from "@/state/tokenStore";

export function App() {
  const token = useToken();
  const { session: selectedSessionID, split: splitSessionID } = useSearch({ from: "/" });

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
          <ChatPane token={token} sessionID={selectedSessionID} role="primary" />
          {showSplit ? (
            <>
              <div className="h-px shrink-0 bg-border" />
              <ChatPane token={token} sessionID={splitSessionID} role="split" />
            </>
          ) : null}
        </main>
      </div>
    </TooltipProvider>
  );
}
