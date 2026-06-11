import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { PanelLeft, RefreshCw } from "lucide-react";
import { useEffect } from "react";

import { listSessions } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Composer } from "@/components/Composer";
import { SettingsDialog } from "@/components/SettingsDialog";
import { Transcript } from "@/components/Transcript";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { replaceSessionSearch } from "@/routes/sessionSearch";

type ChatPaneProps = {
  token: string;
  selectedSessionID: string | undefined;
};

export function ChatPane({ token, selectedSessionID }: ChatPaneProps) {
  const navigate = useNavigate({ from: "/" });
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const selectedSession = sessions.find((session) => session.id === selectedSessionID);
  const activeSessionID = selectedSession?.id;

  useEffect(() => {
    if (!sessionsQuery.isSuccess) {
      return;
    }
    if (!selectedSessionID && sessions[0]) {
      void navigate({ to: "/", search: { session: sessions[0].id } });
      return;
    }
    if (selectedSessionID && !selectedSession) {
      const nextSessionID = sessions[0]?.id;
      if (nextSessionID) {
        void navigate({ to: "/", search: { session: nextSessionID } });
      } else {
        replaceSessionSearch(undefined);
      }
    }
  }, [navigate, selectedSession, selectedSessionID, sessions, sessionsQuery.isSuccess]);

  useSessionEvents(activeSessionID, token);

  return (
    <section className="flex min-w-0 flex-1 flex-col">
      <header className="flex h-14 items-center justify-between border-b bg-background px-4">
        <div className="flex min-w-0 items-center gap-2">
          <PanelLeft className="h-4 w-4 text-muted-foreground" />
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{selectedSession?.title || "No session selected"}</div>
            {selectedSession ? <div className="truncate text-xs text-muted-foreground">{selectedSession.model}</div> : null}
          </div>
        </div>
        <div className="flex items-center gap-1">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                aria-label="Refresh"
                size="icon"
                variant="ghost"
                onClick={() => {
                  void sessionsQuery.refetch();
                }}
              >
                <RefreshCw />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Refresh</TooltipContent>
          </Tooltip>
          <SettingsDialog token={token} />
        </div>
      </header>
      <Separator />
      {activeSessionID ? (
        <>
          <Transcript token={token} sessionID={activeSessionID} />
          <Composer token={token} sessionID={activeSessionID} />
        </>
      ) : (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Create or select a session
        </div>
      )}
    </section>
  );
}
