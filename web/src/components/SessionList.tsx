import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Loader2, MessageSquarePlus, Trash2 } from "lucide-react";

import { createSession, deleteSession, listSessions } from "@/api/client";
import type { Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
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
import { useI18n } from "@/i18n";
import { useOverlayStore } from "@/state/overlayStore";

type SessionListProps = {
  token: string;
  selectedSessionID: string | undefined;
};

export function SessionList({ token, selectedSessionID }: SessionListProps) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const clearSession = useOverlayStore((state) => state.clearSession);
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });

  const createMutation = useMutation({
    mutationFn: () => createSession(token, { title: t("session.untitled"), model: t("session.model") }),
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
          await navigate({ to: "/", search: {}, replace: true });
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
          <div className="font-semibold">{t("app.name")}</div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button aria-label={t("session.create")} size="icon" variant="ghost" onClick={() => createMutation.mutate()}>
                {createMutation.isPending ? <Loader2 className="animate-spin" /> : <MessageSquarePlus />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("session.create")}</TooltipContent>
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
                    <AlertDialog>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <AlertDialogTrigger asChild>
                            <SidebarMenuAction
                              aria-label={t("session.delete")}
                              disabled={deleteMutation.isPending}
                              showOnHover
                            >
                              <Trash2 />
                            </SidebarMenuAction>
                          </AlertDialogTrigger>
                        </TooltipTrigger>
                        <TooltipContent>{t("session.delete")}</TooltipContent>
                      </Tooltip>
                      <AlertDialogContent>
                        <AlertDialogHeader>
                          <AlertDialogTitle>{t("deleteSession.title")}</AlertDialogTitle>
                          <AlertDialogDescription>{t("deleteSession.description")}</AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                          <AlertDialogAction
                            variant="destructive"
                            onClick={() => deleteMutation.mutate(session.id)}
                          >
                            {t("common.delete")}
                          </AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
          {sessionsQuery.isLoading ? (
            <div className="flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              {t("common.loading")}
            </div>
          ) : null}
          {!sessionsQuery.isLoading && sessions.length === 0 ? (
            <div className="px-3 py-6 text-sm text-muted-foreground">{t("session.empty")}</div>
          ) : null}
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}
