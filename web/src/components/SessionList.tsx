import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Loader2, MessageSquarePlus, Trash2 } from "lucide-react";

import { createSession, deleteSession, listSessions } from "@/api/client";
import type { Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/button";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarSeparator,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { replaceSessionSearch } from "@/routes/sessionSearch";
import { useOverlayStore } from "@/state/overlayStore";

type SessionListProps = {
  token: string;
  selectedSessionID: string | undefined;
};

export function SessionList({ token, selectedSessionID }: SessionListProps) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const clearSession = useOverlayStore((state) => state.clearSession);
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });

  const createMutation = useMutation({
    mutationFn: () => createSession(token, { title: "Untitled session", model: "mock-model" }),
    onSuccess: async (session) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      await navigate({ to: "/", search: { session: session.id } });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (sessionID: string) => deleteSession(token, sessionID),
    onSuccess: async (_, sessionID) => {
      const previous = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      const remaining = previous?.sessions.filter((session) => session.id !== sessionID) || [];

      if (previous) {
        queryClient.setQueryData(queryKeys.sessions(), { sessions: remaining });
      }
      clearSession(sessionID);
      if (selectedSessionID === sessionID) {
        const nextSessionID = remaining[0]?.id;
        if (nextSessionID) {
          await navigate({ to: "/", search: { session: nextSessionID } });
        } else {
          replaceSessionSearch(undefined);
        }
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
  });

  const sessions = sessionsQuery.data?.sessions || [];

  return (
    <Sidebar collapsible="none">
      <SidebarHeader>
        <div className="flex h-10 items-center justify-between px-2">
          <div className="font-semibold">Pudding</div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button aria-label="Create session" size="icon" variant="ghost" onClick={() => createMutation.mutate()}>
                {createMutation.isPending ? <Loader2 className="animate-spin" /> : <MessageSquarePlus />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>Create session</TooltipContent>
          </Tooltip>
        </div>
      </SidebarHeader>
      <SidebarSeparator />
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {sessions.map((session) => {
                const selected = session.id === selectedSessionID;
                return (
                  <SidebarMenuItem key={session.id}>
                    <SidebarMenuButton
                      type="button"
                      isActive={selected}
                      onClick={() => navigate({ to: "/", search: { session: session.id } })}
                    >
                      <span className="truncate">{session.title || session.id}</span>
                    </SidebarMenuButton>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <SidebarMenuAction
                          aria-label="Delete session"
                          disabled={deleteMutation.isPending}
                          showOnHover
                          onClick={() => deleteMutation.mutate(session.id)}
                        >
                          <Trash2 />
                        </SidebarMenuAction>
                      </TooltipTrigger>
                      <TooltipContent>Delete session</TooltipContent>
                    </Tooltip>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
          {sessionsQuery.isLoading ? (
            <div className="flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              Loading
            </div>
          ) : null}
          {!sessionsQuery.isLoading && sessions.length === 0 ? (
            <div className="px-3 py-6 text-sm text-muted-foreground">No sessions</div>
          ) : null}
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}
