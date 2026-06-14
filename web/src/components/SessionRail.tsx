import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CircleAlert, Loader2, MessageSquareText, PanelLeft, Pencil, Plus, SquareSplitVertical, Trash2 } from "lucide-react";
import { useRef, useState } from "react";

import { createSession, deleteSession, listSessions, updateSession } from "@/api/client";
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
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import type { AppSearch } from "@/lib/route";
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
    // title 留空:展示侧 fallback "未命名会话",首条消息提交后由 Composer
    // 自动回填摘要;空 = "未命名"的判据,跨语言稳定且不会覆盖手动命名
    mutationFn: () => createSession(token, { title: "" }),
    onSuccess: async (session) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      await navigate({ to: "/", search: (prev) => ({ ...(prev as AppSearch), session: session.id }) });
    },
  });

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
            } else {
              delete next.session;
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
      createPending={createMutation.isPending}
      deletePending={deleteMutation.isPending}
      isError={sessionsQuery.isError}
      isLoading={sessionsQuery.isLoading}
      selectedSessionID={selectedSessionID}
      sessions={sessionsQuery.data?.sessions || []}
      token={token}
      onCreate={() => createMutation.mutate()}
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
              return { ...search, session: id, split: search.session };
            }
            return { ...search, session: id };
          },
        });
      }}
    />
  );

  // 折叠态:整栏收进 popover(参照 Claude Code 桌面端)。
  // marginLeft 让位 macOS 红绿灯(design.md 2.3)。
  if (collapsed) {
    // popover 面板贴窗口左缘弹出(与展开态 rail 的 8px 内边距对位),
    // 用 alignOffset 抵消触发器被红绿灯 inset 推出去的横向偏移;
    // 浏览器模式 inset=0,无副作用
    const trafficInsetPx =
      Number.parseFloat(
        getComputedStyle(document.documentElement).getPropertyValue("--traffic-inset"),
      ) || 0;
    return (
      // 触发器在 --toolbar-h 工具条带内垂直居中:wrapper 撑满带高 +
      // items-center,与展开态顶行同构 — 不按按钮尺寸硬算偏移
      // (size="icon" 是 32px,按 36px 算过一次,两态错位 2px)
      <div
        className="absolute top-0 left-2 z-30 flex items-center"
        style={{
          height: "var(--toolbar-h)",
          marginLeft: "var(--traffic-inset)",
        }}
      >
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
            alignOffset={-trafficInsetPx}
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
    <aside
      className="flex h-full shrink-0 flex-col gap-2 bg-sidebar px-2 pb-2 text-sidebar-foreground"
    >
      {/* 顶行是 --toolbar-h 工具条(壳 54px,与 InvisibleTitleBarHeight 同值):
          壳模式下整行可拖拽,按钮垂直居中对齐红绿灯 */}
      <div
        className="drag-region flex h-(--toolbar-h) shrink-0 items-center transition-[padding] duration-200"
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
  createPending: boolean;
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
  createPending,
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
          onOpenSplit={onOpenSplit}
          onRefetch={onRefetch}
          onRename={onRename}
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
    <div className="grid gap-0.5">
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
    </div>
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

function SessionItem({ session, selected, running, deletePending, onSelect, onOpenSplit, onDelete, onRename }: SessionItemProps) {
  const { t, locale } = useI18n();
  return (
    <div
      className={cn(
        "group/item relative flex items-center gap-2 rounded-lg px-2.5 py-1.5 text-left transition-colors",
        selected ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/60",
      )}
    >
      <button className="flex min-w-0 flex-1 items-center gap-2 text-left" type="button" onClick={onSelect}>
        {running ? <span className="size-2 shrink-0 animate-pulse rounded-full bg-primary" /> : null}
        <span className="truncate text-[13px] leading-6 font-medium">{session.title || t("session.untitled")}</span>
        <span className="ml-auto shrink-0 pl-2 text-xs text-muted-foreground transition-opacity group-hover/item:opacity-0">
          {running ? t("session.generating") : formatRelative(session.updatedAt, locale)}
        </span>
      </button>
      {/* hover 操作区盖在时间文字位置上 */}
      <div className="absolute top-1/2 right-1.5 flex -translate-y-1/2 items-center gap-0.5 opacity-0 transition-opacity group-hover/item:opacity-100 has-focus-visible:opacity-100">
        <Button
          aria-label={t("session.openSplit")}
          className="size-6"
          size="icon"
          variant="ghost"
          onClick={onOpenSplit}
        >
          <SquareSplitVertical className="size-3.5" />
        </Button>
        <RenameDialog currentTitle={session.title} onRename={onRename} />
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button
              aria-label={t("session.delete")}
              className="size-6"
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
    </div>
  );
}

// 手动命名:Pencil 弹小对话框改名。清空保存 = 恢复"未命名",
// 下一条消息会重新触发自动标题(空标题 = 自动命名判据)。
function RenameDialog({ currentTitle, onRename }: { currentTitle: string; onRename: (title: string) => void }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(currentTitle);

  function submit() {
    onRename(draft.trim());
    setOpen(false);
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) {
          setDraft(currentTitle);
        }
      }}
    >
      <DialogTrigger asChild>
        <Button aria-label={t("session.rename")} className="size-6" size="icon" variant="ghost">
          <Pencil className="size-3.5" />
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t("renameSession.title")}</DialogTitle>
        </DialogHeader>
        <Input
          autoFocus
          placeholder={t("session.untitled")}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              submit();
            }
          }}
        />
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => setOpen(false)}>
            {t("common.cancel")}
          </Button>
          <Button type="button" onClick={submit}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
