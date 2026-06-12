import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CircleAlert, Loader2, MessageSquareText, PanelLeftClose, PanelLeftOpen, Plus, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";

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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { formatRelative } from "@/lib/time";
import { cn } from "@/lib/utils";
import { useOverlayStore } from "@/state/overlayStore";

const COLLAPSED_KEY = "pudding.railCollapsed";
const LAST_SEEN_KEY = "pudding.lastSeen";

function readLastSeen(): Record<string, string> {
  try {
    return JSON.parse(localStorage.getItem(LAST_SEEN_KEY) || "{}");
  } catch {
    return {};
  }
}

function markSeen(sessionID: string) {
  const map = readLastSeen();
  map[sessionID] = new Date().toISOString();
  localStorage.setItem(LAST_SEEN_KEY, JSON.stringify(map));
}

type SessionListProps = {
  token: string;
  selectedSessionID: string | undefined;
};

export function SessionList({ token, selectedSessionID }: SessionListProps) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const clearSession = useOverlayStore((state) => state.clearSession);
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSED_KEY) === "1");
  const [hoverOpen, setHoverOpen] = useState(false);
  const closeTimer = useRef<number | null>(null);

  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
    refetchInterval: 15_000, // 非选中 session 的运行态兜底刷新
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const selectedSession = sessions.find((session) => session.id === selectedSessionID);

  // 选中即视为已读:lastSeen 推进到该 session 最新一次更新之后
  useEffect(() => {
    if (selectedSessionID) {
      markSeen(selectedSessionID);
    }
  }, [selectedSessionID, selectedSession?.updatedAt]);

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

  function toggleCollapsed(next: boolean) {
    setCollapsed(next);
    localStorage.setItem(COLLAPSED_KEY, next ? "1" : "0");
    setHoverOpen(false);
  }

  function scheduleClose() {
    if (closeTimer.current) {
      window.clearTimeout(closeTimer.current);
    }
    closeTimer.current = window.setTimeout(() => setHoverOpen(false), 160);
  }

  function cancelClose() {
    if (closeTimer.current) {
      window.clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }

  const panel = (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <Button
        className="mx-2 justify-start gap-2 rounded-lg"
        disabled={createMutation.isPending}
        size="sm"
        variant="outline"
        onClick={() => createMutation.mutate()}
      >
        {createMutation.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
        {t("session.create")}
      </Button>
      <ScrollArea className="min-h-0 flex-1 px-2">
        <SessionItems
          deletePending={deleteMutation.isPending}
          isError={sessionsQuery.isError}
          isLoading={sessionsQuery.isLoading}
          selectedSessionID={selectedSessionID}
          sessions={sessions}
          onDelete={(id) => deleteMutation.mutate(id)}
          onRefetch={() => void sessionsQuery.refetch()}
          onSelect={(id) => {
            setHoverOpen(false);
            void navigate({ to: "/", search: { session: id } });
          }}
        />
      </ScrollArea>
      <div className="flex items-center gap-1 px-2 pb-1">
        <ThemeToggle />
        <LanguageToggle />
        <div className="flex-1" />
        <SettingsDialog token={token} />
      </div>
    </div>
  );

  if (collapsed) {
    return (
      <div
        className="flex h-full w-12 shrink-0 flex-col items-center gap-1 bg-sidebar pb-3 transition-[padding] duration-200"
        style={{ paddingTop: "calc(var(--traffic-inset-y) + 8px)" }}
      >
        <Popover open={hoverOpen} onOpenChange={setHoverOpen}>
          <PopoverTrigger asChild>
            <Button
              aria-label={t("rail.expand")}
              size="icon"
              variant="ghost"
              onClick={() => toggleCollapsed(false)}
              onMouseEnter={() => {
                cancelClose();
                setHoverOpen(true);
              }}
              onMouseLeave={scheduleClose}
            >
              <PanelLeftOpen />
            </Button>
          </PopoverTrigger>
          <PopoverContent
            align="start"
            className="h-105 w-72 p-0 py-2"
            side="right"
            sideOffset={6}
            onMouseEnter={cancelClose}
            onMouseLeave={scheduleClose}
          >
            {panel}
          </PopoverContent>
        </Popover>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={t("session.create")}
              disabled={createMutation.isPending}
              size="icon"
              variant="ghost"
              onClick={() => createMutation.mutate()}
            >
              {createMutation.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
            </Button>
          </TooltipTrigger>
          <TooltipContent side="right">{t("session.create")}</TooltipContent>
        </Tooltip>
        <div className="flex-1" />
        <ThemeToggle />
        <SettingsDialog token={token} />
      </div>
    );
  }

  return (
    <aside className="flex h-full w-[268px] shrink-0 flex-col gap-2 bg-sidebar pb-2 text-sidebar-foreground">
      <div
        className="flex items-center gap-1 px-2 pt-2 transition-[padding] duration-200"
        style={{ paddingLeft: "calc(var(--traffic-inset) + 8px)" }}
      >
        <Tooltip>
          <TooltipTrigger asChild>
            <Button aria-label={t("rail.collapse")} size="icon" variant="ghost" onClick={() => toggleCollapsed(true)}>
              <PanelLeftClose />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{t("rail.collapse")}</TooltipContent>
        </Tooltip>
      </div>
      {panel}
    </aside>
  );
}

function SessionItems({
  sessions,
  selectedSessionID,
  isLoading,
  isError,
  deletePending,
  onSelect,
  onDelete,
  onRefetch,
}: {
  sessions: Session[];
  selectedSessionID: string | undefined;
  isLoading: boolean;
  isError: boolean;
  deletePending: boolean;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
  onRefetch: () => void;
}) {
  const { t, locale } = useI18n();
  const lastSeen = readLastSeen();
  // 实时运行态:sessions 快照(15s 兜底)与 SSE overlay 双源取或
  const runningTurns = useOverlayStore((state) => state.runningTurns);

  if (isLoading) {
    return (
      <div className="grid gap-2 py-1">
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
      </div>
    );
  }
  if (isError) {
    return (
      <Alert className="mt-1" variant="destructive">
        <CircleAlert className="h-3.5 w-3.5" />
        <AlertDescription className="grid gap-2">
          <span>{t("session.loadFailed")}</span>
          <Button size="sm" type="button" variant="outline" onClick={onRefetch}>
            {t("common.refresh")}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }
  if (sessions.length === 0) {
    return (
      <div className="grid justify-items-center gap-2 px-3 py-10 text-center text-sm text-muted-foreground">
        <MessageSquareText className="h-5 w-5" />
        <div>{t("session.empty")}</div>
      </div>
    );
  }

  return (
    <div className="grid gap-0.5 py-1">
      {sessions.map((session) => {
        const selected = session.id === selectedSessionID;
        const running = session.running || Boolean(runningTurns[session.id]);
        const seenAt = lastSeen[session.id];
        const unseenDone = !selected && !running && Boolean(seenAt) && session.updatedAt > seenAt;
        return (
          <div
            key={session.id}
            className={cn(
              "group/item relative flex items-start gap-2 rounded-lg px-2.5 py-2 text-left transition-colors",
              selected ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/60",
            )}
          >
            <button className="min-w-0 flex-1 text-left" type="button" onClick={() => onSelect(session.id)}>
              <span className="flex items-center gap-2">
                <StatusDot running={running} unseenDone={unseenDone} />
                <span className="truncate text-[13px] leading-5 font-medium">{session.title || session.id}</span>
              </span>
              <span className="block truncate pl-4 text-xs leading-5 text-muted-foreground">
                {running ? t("session.generating") : formatRelative(session.updatedAt, locale)}
              </span>
            </button>
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
                  aria-label={t("session.delete")}
                  className="absolute top-1.5 right-1.5 size-6 opacity-0 transition-opacity group-hover/item:opacity-100 focus-visible:opacity-100"
                  disabled={deletePending}
                  size="icon"
                  variant="ghost"
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>{t("deleteSession.title")}</AlertDialogTitle>
                  <AlertDialogDescription>{t("deleteSession.description")}</AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                  <AlertDialogAction variant="destructive" onClick={() => onDelete(session.id)}>
                    {t("common.delete")}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        );
      })}
    </div>
  );
}

function StatusDot({ running, unseenDone }: { running: boolean; unseenDone: boolean }) {
  return (
    <span
      className={cn(
        "size-2 shrink-0 rounded-full",
        running ? "animate-pulse bg-primary" : unseenDone ? "bg-success" : "bg-transparent",
      )}
    />
  );
}
