import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  Blocks,
  CircleAlert,
  Ellipsis,
  MessageSquareText,
  PanelLeft,
  TextCursorInput,
  MessageCirclePlus,
  Search,
  Rows2,
  Trash,
  Workflow,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { deleteSession, listSessions, updateSession } from "@/api/client";
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
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useBackgroundSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import type { AppSearch } from "@/lib/route";
import { formatRelative } from "@/lib/time";
import { cn } from "@/lib/utils";
import { useOverlayStore } from "@/state/overlayStore";
import { setRailCollapsed, useRailCollapsed, useRailForcedCollapsed } from "@/state/railStore";

const popoverAlignNudgePx = 3;

// 会话栏(rail):展开 = 左侧整栏;折叠 = 悬浮触发器 + hover 浮出 popover 面板。
// 面板内容(RailPanel)两种形态完全复用。
export function SessionRail({
  token,
  selectedSessionID,
  activeSessionIDs = [],
  draftActive,
}: {
  token: string;
  selectedSessionID: string | undefined;
  activeSessionIDs?: string[];
  draftActive: boolean;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const clearSession = useOverlayStore((state) => state.clearSession);
  const runningTurns = useOverlayStore((state) => state.runningTurns);
  const collapsed = useRailCollapsed();
  const forcedCollapsed = useRailForcedCollapsed();
  const hover = useHoverPopover();

  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const activeSessionIDSet = new Set([selectedSessionID, ...activeSessionIDs].filter(Boolean));
  const backgroundSessionIDs = [
    ...sessions.filter((session) => session.running).map((session) => session.id),
    ...Object.entries(runningTurns)
      .filter(([, turnID]) => Boolean(turnID))
      .map(([sessionID]) => sessionID),
  ].filter((sessionID, index, all) => !activeSessionIDSet.has(sessionID) && all.indexOf(sessionID) === index);
  useBackgroundSessionEvents(backgroundSessionIDs, token);

  const renameMutation = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) => updateSession(token, id, { title }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
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
      // 被删会话占用的路由槽位(主 pane / 分屏)就地清理
      await navigate({
        to: "/",
        search: (prev) => {
          const next = { ...(prev as AppSearch) };
          if (next.split === sessionID) {
            delete next.split;
          }
          if (next.session === sessionID) {
            const fallback = remaining.find((session) => session.id !== next.split)?.id || remaining[0]?.id;
            if (fallback) {
              next.session = fallback;
              delete next.draft;
            } else {
              delete next.session;
              next.draft = "1";
            }
          }
          return next;
        },
        replace: true,
      });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
  });

  function collapse(next: boolean) {
    setRailCollapsed(next);
    hover.close();
    if (next) {
      // 收起后鼠标恰好停在触发器原位:压制 hover 弹出,移开一次再恢复
      hover.suppressUntilLeave();
    }
  }

  const panel = (
    <RailPanel
      draftActive={draftActive}
      deletePending={deleteMutation.isPending}
      isError={sessionsQuery.isError}
      isLoading={sessionsQuery.isLoading}
      selectedSessionID={selectedSessionID}
      sessions={sessions}
      token={token}
      onCreate={() => {
        hover.close();
        void navigate({
          to: "/",
          search: (prev) => {
            const next = { ...(prev as AppSearch), draft: "1" };
            delete next.session;
            return next;
          },
        });
      }}
      onDelete={(id) => deleteMutation.mutate(id)}
      onRename={(id, title) => renameMutation.mutate({ id, title })}
      onOpenSplit={(id) => {
        hover.close();
        // 当前主 pane 的会话不重复开分屏
        void navigate({
          to: "/",
          search: (prev) => {
            const search = prev as AppSearch;
            return search.session === id ? search : { ...search, split: id };
          },
        });
      }}
      onRefetch={() => void sessionsQuery.refetch()}
      onSelect={(id) => {
        hover.close();
        void navigate({
          to: "/",
          search: (prev) => {
            const search = prev as AppSearch;
            // 点中已在分屏里的会话:与主 pane 交换,两个都保持可见
            if (search.split === id && search.session) {
              const next = { ...search, session: id, split: search.session };
              delete next.draft;
              return next;
            }
            const next = { ...search, session: id };
            delete next.draft;
            return next;
          },
        });
      }}
    />
  );

  // 统一侧栏按钮:展开/收起都固定在同一个窗口位置。展开态 rail 自身不再放第二个按钮。
  const popoverAlignOffset = collapsed ? -(readTrafficInsetPx() + popoverAlignNudgePx) : 0;
  const railButton = (
    <div
      className="no-drag-region absolute top-0 left-[11px] z-30 flex items-center"
      style={{
        height: "var(--toolbar-h)",
        marginLeft: "var(--traffic-inset)",
      }}
    >
      <Popover open={collapsed && hover.open} onOpenChange={hover.handleOpenChange}>
        <Tooltip>
          <PopoverAnchor asChild>
            <TooltipTrigger asChild>
              <Button
                aria-label={collapsed ? t("rail.expand") : t("rail.collapse")}
                className="text-muted-foreground"
                size="icon"
                tabIndex={-1}
                variant="ghost"
                onClick={() => {
                  if (!collapsed) {
                    collapse(true);
                    return;
                  }
                  // 窄屏强制折叠时展开不可用,点击退化为开合 popover
                  if (forcedCollapsed) {
                    hover.toggle();
                    return;
                  }
                  collapse(false);
                }}
                onMouseEnter={() => {
                  if (collapsed) {
                    hover.openNow();
                  }
                }}
                onMouseLeave={() => {
                  if (collapsed) {
                    hover.scheduleClose();
                  }
                }}
              >
                <PanelLeft />
              </Button>
            </TooltipTrigger>
          </PopoverAnchor>
          {!collapsed ? <TooltipContent side="bottom">{t("rail.collapse")}</TooltipContent> : null}
        </Tooltip>
        {collapsed ? (
          <PopoverContent
            align="start"
            alignOffset={popoverAlignOffset}
            className="flex h-[26rem] max-h-[80vh] w-[260px] flex-col p-0"
            side="bottom"
            sideOffset={11}
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
        ) : null}
      </Popover>
    </div>
  );

  if (collapsed) {
    return railButton;
  }

  return (
    <>
      {railButton}
      <aside className="relative h-full w-[268px] shrink-0 bg-background text-sidebar-foreground">
        <div
          aria-hidden="true"
          className="pointer-events-none absolute top-2 right-0 bottom-2 left-2 z-0 rounded-[var(--rail-radius)] rounded-tl-[var(--rail-left-radius)] rounded-bl-[var(--rail-left-radius)] border border-sidebar-border bg-sidebar"
        />
        <div className="absolute top-0 right-0 bottom-2 left-2 z-10 flex min-h-0 flex-col gap-(--rail-content-align-gap)">
          {/* 顶行是 --toolbar-h 工具条占位;窗口拖拽由 App 根部统一透明拖拽带承载 */}
          <div
            className="h-(--toolbar-h) shrink-0 transition-[padding] duration-200"
            style={{ paddingLeft: "var(--traffic-inset)" }}
          />
          {panel}
        </div>
      </aside>
    </>
  );
}

function readTrafficInsetPx() {
  if (typeof document === "undefined") {
    return 0;
  }
  return Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--traffic-inset")) || 0;
}

// hover 开合 + 点击钉住:面板内一旦发生点击(如打开主题/语言下拉),
// 鼠标离开不再自动关闭,直到 popover 真正关闭。
// suppress:收起边栏的动作刚结束时鼠标恰好停在触发器原位,此时不应
// 立即 hover 弹出 — 压制到鼠标离开触发器一次后恢复。
function useHoverPopover(closeDelay = 160) {
  const [open, setOpen] = useState(false);
  const closeTimer = useRef<number | null>(null);
  const pinnedRef = useRef(false);
  const suppressRef = useRef(false);

  function cancelClose() {
    if (closeTimer.current) {
      window.clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }

  return {
    open,
    openNow() {
      if (suppressRef.current) {
        return;
      }
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
      suppressRef.current = false; // 鼠标离开过一次,恢复 hover 弹出
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
    suppressUntilLeave() {
      suppressRef.current = true;
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
  draftActive: boolean;
  deletePending: boolean;
  onCreate: () => void;
  onSelect: (id: string) => void;
  onOpenSplit: (id: string) => void;
  onDelete: (id: string) => void;
  onRename: (id: string, title: string) => void;
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
  draftActive,
  deletePending,
  onCreate,
  onSelect,
  onOpenSplit,
  onDelete,
  onRename,
  onRefetch,
}: RailPanelProps) {
  const { t } = useI18n();
  return (
    <SidebarProvider className="!contents">
      <Sidebar className="min-h-0 w-full flex-1 bg-transparent" collapsible="none">
        <SidebarHeader>
          <SidebarMenu className="gap-1">
            <SidebarMenuItem>
              <SidebarMenuButton
                isActive={draftActive}
                onClick={onCreate}
              >
                <MessageCirclePlus />
                <span>{t("session.create")}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuItem>
              <SidebarMenuButton>
                <Search />
                <span>{t("rail.search")}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuItem>
              <SidebarMenuButton>
                <Blocks />
                <span>{t("rail.widgets")}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuItem>
              <SidebarMenuButton>
                <Workflow />
                <span>{t("rail.automations")}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>
        <SidebarContent className="py-2 overscroll-contain">
          <SidebarGroup>
            <SidebarGroupLabel>{t("session.recents")}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SessionItems
                deletePending={deletePending}
                isError={isError}
                isLoading={isLoading}
                selectedSessionID={selectedSessionID}
                sessions={sessions}
                onDelete={onDelete}
                onOpenSplit={onOpenSplit}
                onRefetch={onRefetch}
                onRename={onRename}
                onSelect={onSelect}
              />
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
        <SidebarFooter>
          <div className="flex items-center gap-1">
            <ThemeToggle />
            <LanguageToggle />
            <div className="flex-1" />
            <SettingsDialog token={token} />
          </div>
        </SidebarFooter>
      </Sidebar>
    </SidebarProvider>
  );
}

type SessionItemsProps = {
  sessions: Session[];
  selectedSessionID: string | undefined;
  isLoading: boolean;
  isError: boolean;
  deletePending: boolean;
  onSelect: (id: string) => void;
  onOpenSplit: (id: string) => void;
  onDelete: (id: string) => void;
  onRename: (id: string, title: string) => void;
  onRefetch: () => void;
};

function SessionItems({
  sessions,
  selectedSessionID,
  isLoading,
  isError,
  deletePending,
  onSelect,
  onOpenSplit,
  onDelete,
  onRename,
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
    <SidebarMenu className="gap-1">
      {sessions.map((session) => (
        <SessionItem
          key={session.id}
          deletePending={deletePending}
          running={session.running || Boolean(runningTurns[session.id])}
          selected={session.id === selectedSessionID}
          session={session}
          onDelete={() => onDelete(session.id)}
          onOpenSplit={() => onOpenSplit(session.id)}
          onRename={(title) => onRename(session.id, title)}
          onSelect={() => onSelect(session.id)}
        />
      ))}
    </SidebarMenu>
  );
}

type SessionItemProps = {
  session: Session;
  selected: boolean;
  running: boolean;
  deletePending: boolean;
  onSelect: () => void;
  onOpenSplit: () => void;
  onDelete: () => void;
  onRename: (title: string) => void;
};

function SessionItem({
  session,
  selected,
  running,
  deletePending,
  onSelect,
  onOpenSplit,
  onDelete,
  onRename,
}: SessionItemProps) {
  const { t, locale } = useI18n();
  const title = session.title || t("session.untitled");
  const [actionsOpen, setActionsOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const inputRef = useRef<HTMLInputElement>(null);
  const editAfterMenuCloseRef = useRef(false);

  useEffect(() => {
    if (!editing) {
      setDraft(session.title);
    }
  }, [editing, session.title]);

  useEffect(() => {
    if (!editing) {
      return;
    }
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [editing]);

  function startEditing() {
    setDraft(session.title);
    setEditing(true);
  }

  function cancelEditing() {
    setDraft(session.title);
    setEditing(false);
  }

  function saveEditing() {
    const nextTitle = draft.trim();
    if (!nextTitle) {
      cancelEditing();
      return;
    }
    if (nextTitle !== session.title) {
      onRename(nextTitle);
    }
    setEditing(false);
  }

  return (
    <SidebarMenuItem>
      {editing ? (
        <SidebarMenuButton
          asChild
          isActive
        >
          <div className="cursor-text">
            <SessionRunningSlot running={running} />
            <Input
              ref={inputRef}
              aria-label={t("session.rename")}
              className="h-5 min-w-0 flex-1 rounded-none border-0 bg-transparent px-0 py-0 text-sm shadow-none ring-0 focus-visible:border-0 focus-visible:ring-0 md:text-sm dark:bg-transparent"
              placeholder={t("session.untitled")}
              value={draft}
              onBlur={saveEditing}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  event.currentTarget.blur();
                }
                if (event.key === "Escape") {
                  event.preventDefault();
                  cancelEditing();
                }
              }}
            />
          </div>
        </SidebarMenuButton>
      ) : (
        <SidebarMenuButton
          asChild
          className="pr-11 data-active:font-normal group-hover/menu-item:bg-sidebar-accent group-hover/menu-item:text-sidebar-accent-foreground group-has-data-[sidebar=menu-action]/menu-item:pr-11"
          isActive={selected || actionsOpen}
        >
          <button
            type="button"
            onClick={onSelect}
            onDoubleClick={(event) => {
              event.preventDefault();
              startEditing();
            }}
          >
            <SessionRunningSlot running={running} />
            <span className="min-w-0 flex-1 truncate" title={session.title || undefined}>
              {title}
            </span>
          </button>
        </SidebarMenuButton>
      )}
      {!editing ? (
        <>
          <SidebarMenuBadge
            className={cn(
              "right-2 min-w-0 px-0 font-normal text-muted-foreground transition-opacity group-focus-within/menu-item:opacity-0 group-hover/menu-item:opacity-0",
              actionsOpen && "opacity-0",
            )}
          >
            {running ? t("session.generating") : formatRelative(session.lastActivityAt || session.createdAt, locale)}
          </SidebarMenuBadge>
          <DropdownMenu open={actionsOpen} onOpenChange={setActionsOpen}>
            <DropdownMenuTrigger asChild>
              <SidebarMenuAction
                aria-label={t("session.actions")}
                className={cn(
                  "right-1.5 rounded-sm bg-transparent text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-0",
                  actionsOpen && "opacity-100 md:opacity-100",
                )}
                showOnHover
                tabIndex={-1}
              >
                <Ellipsis />
              </SidebarMenuAction>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="end"
              className="min-w-36 w-max"
              onCloseAutoFocus={(event) => {
                event.preventDefault();
                if (!editAfterMenuCloseRef.current) {
                  return;
                }
                editAfterMenuCloseRef.current = false;
                startEditing();
              }}
            >
              <DropdownMenuItem className="whitespace-nowrap" onSelect={onOpenSplit}>
                <Rows2 />
                {t("session.openSplit")}
              </DropdownMenuItem>
              <DropdownMenuItem
                className="whitespace-nowrap"
                onSelect={() => {
                  editAfterMenuCloseRef.current = true;
                }}
              >
                <TextCursorInput />
                {t("session.rename")}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="whitespace-nowrap"
                disabled={deletePending}
                variant="destructive"
                onSelect={() => setDeleteOpen(true)}
              >
                <Trash />
                {t("session.delete")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
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
        </>
      ) : null}
    </SidebarMenuItem>
  );
}

function SessionRunningSlot({ running }: { running: boolean }) {
  return (
    <span aria-hidden="true" className="flex size-3 shrink-0 items-center justify-center">
      {running ? <span className="size-2 animate-pulse rounded-full bg-primary" /> : null}
    </span>
  );
}
