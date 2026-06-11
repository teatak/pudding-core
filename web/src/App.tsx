import { useLocation } from "@tanstack/react-router";

import { ChatPane } from "@/components/ChatPane";
import { SessionList } from "@/components/SessionList";
import { TokenGate } from "@/components/TokenGate";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useToken } from "@/state/tokenStore";

export function App() {
  const token = useToken();
  useLocation({ select: (location) => location.href });
  const selectedSessionID =
    typeof window === "undefined" ? undefined : new URLSearchParams(window.location.search).get("session") || undefined;

  if (!token) {
    return <TokenGate />;
  }

  return (
    <TooltipProvider delayDuration={250}>
      <SidebarProvider>
        <SessionList token={token} selectedSessionID={selectedSessionID} />
        <SidebarInset>
          <ChatPane token={token} selectedSessionID={selectedSessionID} />
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}
