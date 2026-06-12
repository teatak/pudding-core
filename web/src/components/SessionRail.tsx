import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CircleAlert, Loader2, MessageSquareText, PanelLeft, Plus, Trash2 } from "lucide-react";
import { useRef, useState } from "react";

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
import { setRailCollapsed, useRailCollapsed, useRailForcedCollapsed } from "@/state/railStore";

// 会话栏(rail):展开 = 左侧整栏;折叠 = 悬浮触发器 + hover 浮出 popover 面板。
// 面板内容(RailPanel)两种形态完全复用。
export function SessionRail({ token, selectedSessionID }: { token: string; selectedSessionID: string | undefined }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const clearSession = useOverlayStore((state) => state.clearSession);
  const collapsed = useRailCollapsed();
  const forcedCollapsed = useRailForcedCollapsed();
  const hover = useHoverPopover();

  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
    refetchInterval: 15_000, // 非选中 session 的运行态兜底刷新
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

  function collapse(next: boolean) {
    setRailCollapsed(next);
    hover.close();
  }

  const panel = (
    <RailPanel
      createPending={createMutation.isPending}
      deletePending={deleteMutation.isPending}
      isError={sessionsQuery.isError}
      isLoading={sessionsQuery.isLoading}
      selectedSessionID={selectedSessionID}
      sessions={sessionsQuery.data?.sessions || []}
      token={token}
      onCreate={() => createMutation.mutate()}
      onDelete={(id) => deleteMutation.mutate(id)}
      onRefetch={() => void sessionsQuery.refetch()}
      onSelect={(id) => {
        hover.close();
        void navigate({ to: "/", search: { session: id } });
      }}
    />
  );

  // 折叠态:整栏收进 popover(参照 Claude Code 桌面端)。
  // marginLeft 让位 macOS 红绿灯(design.md 2.3)。
  if (collapsed) {
    return (
      <div className="absolute top-2 left-2 z-30" style={{ marginLeft: "var(--traffic-inset)" }}>
        <Popover open={hover.open} onOpenChange={hover.handleOpenChange}>
          <PopoverTrigger asChild>
            <Button
              aria-label={t("rail.expand")}
              size="icon"
              variant="ghost"
              onClick={() => {
                // 窄屏强制折叠时展开不可用,点击退化为开合 popover
                if (forcedCollapsed) {
                  hover.toggle();
                  return;
                }
                collapse(false);
              }}
              onMouseEnter={hover.openNow}
              onMouseLeave={hover.scheduleClose}
            >
              <PanelLeft />
            </Button>
          </PopoverTrigger>
          <PopoverContent
            align="start"
            className="flex h-[26rem] max-h-[80vh] w-72 flex-col p-2"
            side="bottom"
            sideOffset={6}
            onMouseEnter={hover.cancelClose}
            onMouseLeave={hover.scheduleClose}
            onPointerDownCapture={hover.pin}
            onFocusOutside={(event) => event.preventDefault()}
            onInteractOutside={(event) => {
              // 主题/语言下拉的菜单 portal 在 popover DOM 之外,
              // Radix 会误判为"点击外部";命中 popper 容器时拦下关闭
              const target = event.target as HTMLElement | null;
              if (target?.closest("[data-radix-popper-content-wrapper]")) {
                event.preventDefault();
              }
            }}
          >
            {panel}
          </PopoverContent>
        </Popover>
      </div>
    );
  }

  return (
    <aside className="flex h-full w-[268px] shrink-0 flex-col gap-2 bg-sidebar p-2 text-sidebar-foreground">
      <div
        className="flex items-center transition-[padding] duration-200"
        style={{ paddingLeft: "var(--traffic-inset)" }}
      >
        <Tooltip>
          <TooltipTrigger asChild>
            <Button aria-label={t("rail.collapse")} size="icon" variant="ghost" onClick={() => collapse(true)}>
              <PanelLeft />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{t("rail.collapse")}</TooltipContent>
        </Tooltip>
      </div>
      {panel}
    </aside>
  );
}

// hover 开合 + 点击钉住:面板内一旦发生点击(如打开主题/语言下拉),
// 鼠标离开不再自动关闭,直到 popover 真正关闭。
function useHoverPopover(closeDelay = 160) {
  const [open, setOpen] = useState(false);
  const closeTimer = useRef<number | null>(null);
  const pinnedRef = useRef(false);

  function cancelClose() {
    if (closeTimer.current) {
      window.clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }

  return {
    open,
    openNow() {
      cancelClose();
      setOpen(true);
    },
    close() {
      cancelClose();
      setOpen(false);
    },
    toggle() {
      setOpen((value) => !value);
    },
    scheduleClose() {
      if (pinnedRef.current) {
        return;
      }
      cancelClose();
      closeTimer.current = window.setTimeout(() => setOpen(false), closeDelay);
    },
    cancelClose,
    pin() {
      pinnedRef.current = true;
      cancelClose();
    },
    handleOpenChange(next: boolean) {
      setOpen(next);
      if (!next) {
        pinnedRef.current = false;
      }
    },
  };
}

type RailPanelProps = {
  token: string;
  sessions: Session[];
  selectedSessionID: string | undefined;
  isLoading: boolean;
  isError: boolean;
  createPending: boolean;
  deletePending: boolean;
  onCreate: () => void;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
  onRefetch: () => void;
};

// 面板三段:新建 / 列表 / 脚部。四边间距由外层容器(aside / popover)统一给 8px,
// 内部不再叠加水平 margin,保证两种形态边缘视觉一致。
function RailPanel({
  token,
  sessions,
  selectedSessionID,
  isLoading,
  isError,
  createPending,
  deletePending,
  onCreate,
  onSelect,
  onDelete,
  onRefetch,
}: RailPanelProps) {
  const { t } = useI18n();
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <Button
        className="w-full justify-start gap-2 rounded-lg"
        disabled={createPending}
        size="sm"
        variant="outline"
        onClick={onCreate}
      >
        {createPending ? <Loader2 className="animate-spin" /> : <Plus />}
        {t("session.create")}
      </Button>
      <ScrollArea className="min-h-0 flex-1">
        <SessionItems
          deletePending={deletePending}
          isError={isError}
          isLoading={isLoading}
          selectedSessionID={selectedSessionID}
          sessions={sessions}
          onDelete={onDelete}
          onRefetch={onRefetch}
          onSelect={onSelect}
        />
      </ScrollArea>
      <div className="flex items-center gap-1">
        <ThemeToggle />
        <LanguageToggle />
        <div className="flex-1" />
        <SettingsDialog token={token} />
      </div>
    </div>
  );
}

type SessionItemsProps = {
  sessions: Session[];
  selectedSessionID: string | undefined;
  isLoading: boolean;
  isError: boolean;
  deletePending: boolean;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
  onRefetch: () => void;
};

function SessionItems({
  sessions,
  selectedSessionID,
  isLoading,
  isError,
  deletePending,
  onSelect,
  onDelete,
  onRefetch,
}: SessionItemsProps) {
  const { t } = useI18n();
  // 实时运行态:sessions 快照(15s 兜底)与 SSE overlay 双源取或
  const runningTurns = useOverlayStore((state) => state.runningTurns);

  if (isLoading) {
    return (
      <div className="grid gap-2">
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
      </div>
    );
  }
  if (isError) {
    return (
      <Alert variant="destructive">
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
      <div className="grid justify-items-center gap-2 px-2 py-10 text-center text-sm text-muted-foreground">
        <MessageSquareText className="h-5 w-5" />
        <div>{t("session.empty")}</div>
      </div>
    );
  }

  return (
    <div className="grid gap-0.5">
      {sessions.map((session) => (
        <SessionItem
          key={session.id}
          deletePending={deletePending}
          running={session.running || Boolean(runningTurns[session.id])}
          selected={session.id === selectedSessionID}
          session={session}
          onDelete={() => onDelete(session.id)}
          onSelect={() => onSelect(session.id)}
        />
      ))}
    </div>
  );
}

type SessionItemProps = {
  session: Session;
  selected: boolean;
  running: boolean;
  deletePending: boolean;
  onSelect: () => void;
  onDelete: () => void;
};

function SessionItem({ session, selected, running, deletePending, onSelect, onDelete }: SessionItemProps) {
  const { t, locale } = useI18n();
  return (
    <div
      className={cn(
        "group/item relative flex items-start gap-2 rounded-lg px-2.5 py-2 text-left transition-colors",
        selected ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/60",
      )}
    >
      <button className="min-w-0 flex-1 text-left" type="button" onClick={onSelect}>
        <span className="flex items-center gap-2">
          <span
            className={cn("size-2 shrink-0 rounded-full", running ? "animate-pulse bg-primary" : "bg-transparent")}
          />
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
            <AlertDialogAction variant="destructive" onClick={onDelete}>
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
