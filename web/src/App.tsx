import { useSearch } from "@tanstack/react-router";

import { ChatPane } from "@/components/ChatPane";
import { SessionList } from "@/components/SessionList";
import { TokenGate } from "@/components/TokenGate";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
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
      <div className="h-[100svh] overflow-hidden">
        <SidebarProvider className="h-full min-h-0 overflow-hidden">
          <SessionList token={token} selectedSessionID={selectedSessionID} />
          <SidebarInset className="h-full min-h-0 overflow-hidden">
            <ChatPane token={token} selectedSessionID={selectedSessionID} />
          </SidebarInset>
        </SidebarProvider>
      </div>
    </TooltipProvider>
  );
}
