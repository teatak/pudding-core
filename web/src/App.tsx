import { useSearch } from "@tanstack/react-router";

import { ChatPane } from "@/components/ChatPane";
import { SessionRail } from "@/components/SessionRail";
import { TokenGate } from "@/components/TokenGate";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useToken } from "@/state/tokenStore";

export function App() {
  const token = useToken();
  const selectedSessionID = useSearch({ from: "/" }).session;

  if (!token) {
    return <TokenGate />;
  }

  return (
    <TooltipProvider delayDuration={250}>
      {/* 严格左右分割:无外层 margin/卡片化,折叠触发器以 absolute 悬浮 */}
      <div className="relative flex h-[100svh] overflow-hidden bg-sidebar">
        <SessionRail token={token} selectedSessionID={selectedSessionID} />
        <main className="flex h-full min-w-0 flex-1 overflow-hidden bg-background">
          <ChatPane token={token} selectedSessionID={selectedSessionID} />
        </main>
      </div>
    </TooltipProvider>
  );
}
