import { useSearch } from "@tanstack/react-router";

import { ChatPane } from "@/components/ChatPane";
import { SessionList } from "@/components/SessionList";
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
      <div className="flex h-[100svh] overflow-hidden bg-sidebar">
        <SessionList token={token} selectedSessionID={selectedSessionID} />
        <main className="flex h-full min-w-0 flex-1 overflow-hidden bg-background md:my-2 md:mr-2 md:rounded-xl md:border">
          <ChatPane token={token} selectedSessionID={selectedSessionID} />
        </main>
      </div>
    </TooltipProvider>
  );
}
