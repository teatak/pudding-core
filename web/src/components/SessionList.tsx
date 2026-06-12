import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CircleAlert, Loader2, MessageSquareText, Plus, Trash2 } from "lucide-react";

import { createSession, deleteSession, listSessions } from "@/api/client";
import type { Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { LanguageToggle } from "@/components/LanguageToggle";
import { SettingsDialog } from "@/components/SettingsDialog";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
import { Skeleton } from "@/components/ui/skeleton";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
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
    mutationFn: () => createSession(token, { title: t("session.untitled") }),
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
    <Sidebar className="border-r-0" collapsible="none">
      <SidebarHeader className="gap-3 px-3 pt-4 pb-1">
        <div className="px-1 text-base font-semibold tracking-tight">{t("app.name")}</div>
        <Button
          className="w-full justify-start gap-2 rounded-lg"
          disabled={createMutation.isPending}
          size="sm"
          variant="outline"
          onClick={() => createMutation.mutate()}
        >
          {createMutation.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
          {t("session.create")}
        </Button>
      </SidebarHeader>
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
            <div className="grid gap-2 px-3 py-2">
              <Skeleton className="h-10" />
              <Skeleton className="h-10" />
              <Skeleton className="h-10" />
            </div>
          ) : null}
          {sessionsQuery.isError ? (
            <Alert className="mx-3 mt-2" variant="destructive">
              <CircleAlert className="h-3.5 w-3.5" />
              <AlertDescription className="grid gap-2">
                <span>{t("session.loadFailed")}</span>
                <Button size="sm" type="button" variant="outline" onClick={() => void sessionsQuery.refetch()}>
                  {t("common.refresh")}
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          {!sessionsQuery.isLoading && sessions.length === 0 ? (
            <div className="grid justify-items-center gap-2 px-3 py-10 text-center text-sm text-muted-foreground">
              <MessageSquareText className="h-5 w-5" />
              <div>{t("session.empty")}</div>
            </div>
          ) : null}
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter className="px-3 pb-3">
        <div className="flex items-center gap-1">
          <ThemeToggle />
          <LanguageToggle />
          <div className="flex-1" />
          <SettingsDialog token={token} />
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
