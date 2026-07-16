import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import {
  ArrowRight,
  ChevronRight,
  Ellipsis,
  FolderClosed,
  FolderOpen,
  FolderPlus,
  MessageSquareText,
  Package,
  PanelLeft,
  Pin,
  MessageCirclePlus,
  Search,
  Settings,
  SquareTerminal,
  Trash,
} from "lucide-react";
import {
  createContext,
  Fragment,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { toast } from "sonner";

import {
  createProject,
  deleteProject,
  deleteSession,
  getAudioBindings,
  listProjects,
  listSessions,
  revealDesktopPath,
  updateProject,
  updateSession,
} from "@/api/client";
import type { Project, Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { LanguageToggle } from "@/components/LanguageToggle";
import { SessionSearchDialog } from "@/components/SessionSearchDialog";
import { Spinner } from "@/components/Spinner";
import { ThemeToggle } from "@/components/ThemeToggle";
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
import { Collapsible, CollapsibleContent } from "@/components/ui/collapsible";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
  AppDropdownMenuSubContent as DropdownMenuSubContent,
  AppDropdownMenuSubTrigger as DropdownMenuSubTrigger,
} from "@/components/AppMenu";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import {
  DropdownMenu,
  DropdownMenuSub,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Popover, PopoverAnchor } from "@/components/ui/popover";
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
import { useIsMobile } from "@/hooks/use-mobile";
import { useBackgroundSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import {
  type DesktopUpdateState,
  activateDesktopUpdate,
  getDesktopUpdateState,
  onDesktopMenuCommand,
  onDesktopUpdateState,
} from "@/lib/desktopBridge";
import type { AppSearch } from "@/lib/route";
import { pickDirectories } from "@/lib/desktopBridge";
import { openSettingsDialog } from "@/lib/settingsDialog";
import { formatRelative } from "@/lib/time";
import { cn } from "@/lib/utils";
import { isTurnPhaseActive, useOverlayStore } from "@/state/overlayStore";
import { setRailCollapsed, useRailCollapsed, useRailForcedCollapsed } from "@/state/railStore";

const popoverAlignNudgePx = 3;
const dragAutoScrollEdgePx = 44;
const dragAutoScrollMaxStepPx = 14;
const collapsedSessionGroupsStorageKey = "pudding.sessionRail.collapsedGroups";
const sessionCollapseThreshold = 6;
const collapsedSessionDisplayLimit = 5;
const RailOverlayHoldContext = createContext<((id: string, open: boolean) => void) | null>(null);

function useRailOverlayHold(open: boolean) {
  const setOverlayHold = useContext(RailOverlayHoldContext);
  const id = useId();

  useEffect(() => {
    if (!setOverlayHold) {
      return;
    }
    setOverlayHold(id, open);
    return () => setOverlayHold(id, false);
  }, [id, open, setOverlayHold]);
}

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
  const { project: draftProjectID, view } = useSearch({ from: "/" });
  const { t } = useI18n();
  const clearSession = useOverlayStore((state) => state.clearSession);
  const clearSessionCompletion = useOverlayStore((state) => state.clearSessionCompletion);
  const completedSessions = useOverlayStore((state) => state.completedSessions);
  const runningTurns = useOverlayStore((state) => state.runningTurns);
  const turnPhases = useOverlayStore((state) => state.turnPhases);
  const collapsed = useRailCollapsed();
  const forcedCollapsed = useRailForcedCollapsed();
  const isMobile = useIsMobile();
  const hover = useHoverPopover();
  const [searchOpen, setSearchOpen] = useState(false);

  useEffect(
    () =>
      onDesktopMenuCommand((command) => {
        if (command === "new-session") {
          openNewSession();
          return;
        }
        if (command === "search-sessions") {
          openSessionSearch();
          return;
        }
        if (command === "settings") {
          hover.close();
          openSettingsDialog();
        }
      }),
    [hover, isMobile, navigate],
  );

  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects(),
    queryFn: () => listProjects(token),
    enabled: Boolean(token),
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const projects = projectsQuery.data?.projects || [];
  useEffect(() => {
    if (!draftActive || !draftProjectID || !projectsQuery.isSuccess) {
      return;
    }
    if (projects.some((project) => project.id === draftProjectID)) {
      return;
    }
    void navigate({
      to: "/",
      search: (previous) => {
        const next = { ...(previous as AppSearch) };
        delete next.project;
        return next;
      },
      replace: true,
    });
  }, [draftActive, draftProjectID, navigate, projects, projectsQuery.isSuccess]);
  const audioBindingsSessionID = selectedSessionID || sessions[0]?.id;
  const audioBindingsQuery = useQuery({
    queryKey: queryKeys.audioBindings(),
    queryFn: () => getAudioBindings(token, audioBindingsSessionID || ""),
    enabled: Boolean(token && audioBindingsSessionID),
  });
  const appsActive = view === "apps";
  const activeSessionIDSet = new Set<string>(
    [selectedSessionID, ...activeSessionIDs].filter((sessionID): sessionID is string => Boolean(sessionID)),
  );
  const activeSessionIDsKey = Array.from(activeSessionIDSet).sort().join("\n");
  useEffect(() => {
    for (const sessionID of activeSessionIDSet) {
      if (completedSessions[sessionID]) {
        clearSessionCompletion(sessionID);
      }
    }
  }, [activeSessionIDsKey, clearSessionCompletion, completedSessions]);
  const audioInputOwner = audioBindingsQuery.data?.bindings.inputOwner;
  const backgroundSessionIDs = [
    ...sessions.filter((session) => session.running || session.backgroundProcessCount > 0).map((session) => session.id),
    ...(audioInputOwner ? [audioInputOwner] : []),
    ...Object.entries(runningTurns)
      .filter(([, turnID]) => Boolean(turnID))
      .map(([sessionID]) => sessionID),
    ...Object.entries(turnPhases)
      .filter(([, phase]) => isTurnPhaseActive(phase))
      .map(([sessionID]) => sessionID),
  ].filter((sessionID, index, all) => !activeSessionIDSet.has(sessionID) && all.indexOf(sessionID) === index);
  useBackgroundSessionEvents(backgroundSessionIDs, token);

  const renameMutation = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) => updateSession(token, id, { title }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
  });

  const pinMutation = useMutation({
    mutationFn: ({ id, pinned, pinnedOrder }: { id: string; pinned: boolean; pinnedOrder: number }) =>
      updateSession(token, id, { pinned, pinnedOrder }),
    onMutate: async ({ id, pinned, pinnedOrder }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.sessions() });
      const previous = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      if (previous) {
        queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), {
          sessions: previous.sessions.map((session) =>
            session.id === id ? { ...session, pinned, pinnedOrder } : session,
          ),
        });
      }
      return { previous };
    },
    onError: (_error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.sessions(), context.previous);
      }
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
  });

  function nextPinnedOrder(sessionID: string) {
    return Math.max(0, ...sessions.filter((session) => session.id !== sessionID).map((session) => session.pinnedOrder)) + 1;
  }

  function changePinned(id: string, pinned: boolean, pinnedOrder?: number) {
    pinMutation.mutate({
      id,
      pinned,
      pinnedOrder: pinned ? pinnedOrder ?? nextPinnedOrder(id) : 0,
    });
  }

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

  const createProjectMutation = useMutation({
    mutationFn: (rootDirs: string[]) => createProject(token, { rootDirs }),
    onSuccess: (created) => {
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) => ({
        projects: [...(previous?.projects.filter((project) => project.id !== created.id) || []), created],
      }));
      openProjectDraft(created.id);
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects() });
    },
    onError: () => toast.error(t("project.createFailed")),
  });

  function collapse(next: boolean) {
    setRailCollapsed(next);
    hover.close();
    if (next) {
      // 收起后鼠标恰好停在触发器原位:压制 hover 弹出,移开一次再恢复
      hover.suppressUntilLeave();
    }
  }

  function openSessionSearch() {
    setSearchOpen(true);
    hover.close();
  }

  function openNewSession() {
    void navigate({
      to: "/",
      search: (prev) => {
        const next = { ...(prev as AppSearch), draft: "1" };
        delete next.session;
        delete next.split;
        delete next.view;
        delete next.project;
        return next;
      },
    });
    if (isMobile) {
      hover.close();
    }
  }

  function openProjectDraft(projectID: string) {
    void navigate({
      to: "/",
      search: (prev) => {
        const next = { ...(prev as AppSearch), draft: "1", project: projectID };
        delete next.session;
        delete next.split;
        delete next.view;
        return next;
      },
    });
    if (isMobile) {
      hover.close();
    }
  }

  async function createNewProject() {
    const rootDirs = Array.from(
      new Set(
        (await pickDirectories({
          buttonLabel: t("project.createPickButton"),
          message: t("project.createPickMessage"),
          title: t("project.create"),
        }))
          .map((path) => path.trim())
          .filter(Boolean),
      ),
    );
    if (rootDirs.length === 0) {
      return;
    }
    const existing = projects.find(
      (project) => project.rootDirs.length === rootDirs.length && project.rootDirs.every((path, index) => path === rootDirs[index]),
    );
    if (existing) {
      openProjectDraft(existing.id);
      return;
    }
    createProjectMutation.mutate(rootDirs);
  }

  function selectSession(id: string) {
    void navigate({
      to: "/",
      search: (prev) => {
        const search = prev as AppSearch;
        // 点中已在分屏里的会话:与主 pane 交换,两个都保持可见
        if (search.split === id && search.session) {
          const next = { ...search, session: id, split: search.session };
          delete next.draft;
          delete next.view;
          return next;
        }
        const next = { ...search, session: id };
        delete next.draft;
        delete next.view;
        delete next.project;
        return next;
      },
    });
    if (isMobile) {
      hover.close();
    }
  }

  function renderPanel() {
    return (
      <RailPanel
        appsActive={appsActive}
        draftActive={draftActive}
        deletePending={deleteMutation.isPending}
        createProjectPending={createProjectMutation.isPending}
        draftProjectID={draftProjectID}
        isError={sessionsQuery.isError}
        isLoading={sessionsQuery.isLoading}
        projects={projects}
        selectedSessionID={selectedSessionID}
        sessions={sessions}
        token={token}
        onSearch={openSessionSearch}
        onCreate={openNewSession}
        onCreateProject={() => void createNewProject()}
        onDelete={(id) => deleteMutation.mutate(id)}
        onCreateProjectSession={openProjectDraft}
        onRename={(id, title) => renameMutation.mutate({ id, title })}
        onOverlayOpenChange={hover.setClosePaused}
        onOpenSplit={(id) => {
          // 当前主 pane 的会话不重复开分屏
          void navigate({
            to: "/",
            search: (prev) => {
              const search = prev as AppSearch;
              if (search.session === id) {
                return search;
              }
              const next = { ...search, split: id };
              delete next.view;
              return next;
            },
          });
          if (isMobile) {
            hover.close();
          }
        }}
        onPinChange={changePinned}
        onRefetch={() => {
          void sessionsQuery.refetch();
          void projectsQuery.refetch();
        }}
        onSelect={selectSession}
      />
    );
  }

  // 统一侧栏按钮:展开/收起都固定在同一个窗口位置。展开态 rail 自身不再放第二个按钮。
  const popoverAlignOffset = collapsed ? -(readTrafficInsetPx() + popoverAlignNudgePx) : 0;
  const railButton = (
    <div
      className="pudding-rail-toggle no-drag-region absolute top-0 left-(--rail-toggle-left) z-40 flex items-center"
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
                size="icon-sm"
                tabIndex={-1}
                variant="ghost"
                onClick={() => {
                  if (isMobile) {
                    hover.toggle();
                    return;
                  }
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
                  if (!isMobile && collapsed) {
                    hover.openNow();
                  }
                }}
                onMouseLeave={() => {
                  if (!isMobile && collapsed) {
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
            className={cn(
              "flex w-[260px] flex-col p-0",
              isMobile
                ? "h-[calc(100svh-var(--toolbar-h)-1rem)] max-h-[calc(100svh-var(--toolbar-h)-1rem)] w-[min(20rem,calc(100vw-1rem))]"
                : "h-[min(48rem,calc(100vh-var(--toolbar-h)-1.5rem))] max-h-[calc(100vh-var(--toolbar-h)-1.5rem)]",
            )}
            side="bottom"
            sideOffset={isMobile ? 8 : 11}
            onMouseEnter={isMobile ? undefined : hover.cancelClose}
            onMouseLeave={isMobile ? undefined : hover.scheduleClose}
            onFocusOutside={(event) => event.preventDefault()}
            onInteractOutside={(event) => {
              const target = event.target as HTMLElement | null;
              if (
                hover.shouldIgnoreOutsideInteraction() ||
                target?.closest(".pudding-rail-toggle") ||
                isRailPopoverPortalTarget(target)
              ) {
                event.preventDefault();
              }
            }}
          >
            {renderPanel()}
          </PopoverContent>
        ) : null}
      </Popover>
    </div>
  );
  const searchDialog = (
    <SessionSearchDialog
      open={searchOpen}
      projects={projects}
      sessions={sessions}
      token={token}
      onOpenChange={setSearchOpen}
      onSelect={selectSession}
    />
  );

  if (collapsed) {
    return (
      <>
        {railButton}
        {searchDialog}
      </>
    );
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
          {renderPanel()}
        </div>
      </aside>
      {searchDialog}
    </>
  );
}

function readTrafficInsetPx() {
  if (typeof document === "undefined") {
    return 0;
  }
  return Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--traffic-inset")) || 0;
}

function isRailPopoverPortalTarget(target: HTMLElement | null) {
  return Boolean(
    target?.closest(
      [
        "[data-radix-popper-content-wrapper]", // dropdown/select/tooltip portal
        "[role=dialog]", // settings/delete dialogs
        "[data-slot=dialog-content]",
        "[data-slot=alert-dialog-content]",
      ].join(","),
    ),
  );
}

function sortPinnedSessions(sessions: Session[]) {
  return [...sessions].sort((left, right) => {
    const leftOrder = left.pinnedOrder || Number.MAX_SAFE_INTEGER;
    const rightOrder = right.pinnedOrder || Number.MAX_SAFE_INTEGER;
    if (leftOrder !== rightOrder) {
      return leftOrder - rightOrder;
    }
    return sessionActivityTime(right) - sessionActivityTime(left);
  });
}

function sortSessionsByActivity(sessions: Session[]) {
  return [...sessions].sort((left, right) => sessionActivityTime(right) - sessionActivityTime(left));
}

function groupProjectSessions(projects: Project[], sessions: Session[]): ProjectSessionGroup[] {
  const groups = new Map<string, ProjectSessionGroup>();
  for (const project of projects) {
    groups.set(project.id, {
      key: project.id,
      label: project.name || basename(project.rootDirs[0] || project.id),
      project,
      projectID: project.id,
      sessions: [],
      lastActivity: new Date(project.updatedAt || project.createdAt).getTime(),
    });
  }
  for (const session of sessions) {
    const key = session.projectID || "__missing_project__";
    const existing = groups.get(key);
    if (existing) {
      existing.sessions.push(session);
      existing.lastActivity = Math.max(existing.lastActivity, sessionActivityTime(session));
      continue;
    }
    groups.set(key, {
      key,
      label: tProjectFallbackLabel(session.projectID),
      projectID: session.projectID,
      sessions: [session],
      lastActivity: sessionActivityTime(session),
    });
  }
  return Array.from(groups.values())
    .map((group) => ({ ...group, sessions: sortSessionsByActivity(group.sessions) }))
    .sort((left, right) => right.lastActivity - left.lastActivity);
}

function tProjectFallbackLabel(projectID: string | undefined) {
  return projectID || "Project";
}

function sessionActivityTime(session: Session) {
  return new Date(session.lastActivityAt || session.createdAt).getTime();
}

// hover 开合 + 点击钉住:面板内一旦发生点击(如打开主题/语言下拉),
// 鼠标离开不再自动关闭,直到 popover 真正关闭。
// suppress:收起边栏的动作刚结束时鼠标恰好停在触发器原位,此时不应
// 立即 hover 弹出 — 压制到鼠标离开触发器一次后恢复。
function useHoverPopover(closeDelay = 160) {
  const [open, setOpen] = useState(false);
  const closeTimer = useRef<number | null>(null);
  const closePausedRef = useRef(false);
  const ignoreOutsideUntilRef = useRef(0);
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
      if (closePausedRef.current) {
        return;
      }
      cancelClose();
      closeTimer.current = window.setTimeout(() => setOpen(false), closeDelay);
    },
    cancelClose,
    setClosePaused(paused: boolean) {
      const wasPaused = closePausedRef.current;
      closePausedRef.current = paused;
      if (paused) {
        cancelClose();
      } else if (wasPaused) {
        ignoreOutsideUntilRef.current = Date.now() + 180;
        window.setTimeout(() => {
          if (Date.now() >= ignoreOutsideUntilRef.current) {
            ignoreOutsideUntilRef.current = 0;
          }
        }, 200);
      }
    },
    shouldIgnoreOutsideInteraction() {
      return closePausedRef.current || Date.now() < ignoreOutsideUntilRef.current;
    },
    suppressUntilLeave() {
      suppressRef.current = true;
    },
    handleOpenChange(next: boolean) {
      setOpen(next);
    },
  };
}

function readCollapsedSessionGroups() {
  if (typeof window === "undefined") {
    return new Set<string>();
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(collapsedSessionGroupsStorageKey) || "[]");
    if (!Array.isArray(parsed)) {
      return new Set<string>();
    }
    return new Set(parsed.filter((value): value is string => typeof value === "string"));
  } catch {
    return new Set<string>();
  }
}

function writeCollapsedSessionGroups(groups: Set<string>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(collapsedSessionGroupsStorageKey, JSON.stringify([...groups]));
  } catch {
    // localStorage 可能被禁用;折叠状态继续保留在内存态。
  }
}

function useHasHoverInput() {
  const [hasHover, setHasHover] = useState(() =>
    typeof window === "undefined" ? true : window.matchMedia("(hover: hover) and (pointer: fine)").matches,
  );

  useEffect(() => {
    const media = window.matchMedia("(hover: hover) and (pointer: fine)");
    const update = () => setHasHover(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  return hasHover;
}

type RailPanelProps = {
  token: string;
  sessions: Session[];
  projects: Project[];
  selectedSessionID: string | undefined;
  appsActive: boolean;
  isLoading: boolean;
  isError: boolean;
  draftActive: boolean;
  draftProjectID?: string;
  deletePending: boolean;
  createProjectPending: boolean;
  onCreate: () => void;
  onCreateProject: () => void;
  onSearch: () => void;
  onCreateProjectSession: (projectID: string) => void;
  onSelect: (id: string) => void;
  onOpenSplit: (id: string) => void;
  onDelete: (id: string) => void;
  onPinChange: (id: string, pinned: boolean, pinnedOrder?: number) => void;
  onRename: (id: string, title: string) => void;
  onOverlayOpenChange?: (open: boolean) => void;
  onRefetch: () => void;
};

type SessionDropGroup = "pinned" | "unpinned";

type SessionDropTarget = {
  group: SessionDropGroup;
  index: number;
};

type ProjectSessionGroup = {
  key: string;
  label: string;
  project?: Project;
  projectID?: string;
  sessions: Session[];
  lastActivity: number;
};

// 面板三段:新建 / 列表 / 脚部。四边间距由外层容器(aside / popover)统一给 8px,
// 内部不再叠加水平 margin,保证两种形态边缘视觉一致。
function RailPanel({
  token,
  sessions,
  projects,
  selectedSessionID,
  appsActive,
  isLoading,
  isError,
  draftActive,
  draftProjectID,
  deletePending,
  createProjectPending,
  onCreate,
  onCreateProject,
  onSearch,
  onCreateProjectSession,
  onSelect,
  onOpenSplit,
  onDelete,
  onPinChange,
  onRename,
  onOverlayOpenChange,
  onRefetch,
}: RailPanelProps) {
  const { t } = useI18n();
  const navigate = useNavigate({ from: "/" });
  const [draggingSessionID, setDraggingSessionID] = useState<string | null>(null);
  const [dragTarget, setDragTarget] = useState<SessionDropTarget | null>(null);
  const [dragPreview, setDragPreview] = useState<{
    id: string;
    title: string;
    x: number;
    y: number;
  } | null>(null);
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(() => readCollapsedSessionGroups());
  const dragPreviewRef = useRef<HTMLDivElement | null>(null);
  const dragPreviewPointRef = useRef({ x: 0, y: 0 });
  const dragPreviewFrameRef = useRef<number | null>(null);
  const scrollContainerRef = useRef<HTMLDivElement | null>(null);
  const autoScrollFrameRef = useRef<number | null>(null);
  const autoScrollPointerRef = useRef<{ x: number; y: number } | null>(null);
  const pinnedSessions = sortPinnedSessions(sessions.filter((session) => session.pinned));
  const unpinnedSessions = sortSessionsByActivity(sessions.filter((session) => !session.pinned));
  const chatSessions = unpinnedSessions.filter((session) => !session.projectID);
  const projectGroups = groupProjectSessions(projects, unpinnedSessions.filter((session) => Boolean(session.projectID)));
  const showPinnedGroup = pinnedSessions.length > 0 || Boolean(draggingSessionID);
  const draggedSession = draggingSessionID ? sessions.find((session) => session.id === draggingSessionID) : undefined;
  const isDraggingPinned = Boolean(draggedSession?.pinned);
  const pinnedCollapsed = collapsedGroups.has("pinned");
  const chatCollapsed = collapsedGroups.has("chat");
  const onOverlayOpenChangeRef = useRef(onOverlayOpenChange);
  const overlayHoldIDsRef = useRef(new Set<string>());

  function toggleGroupCollapsed(groupID: string) {
    setCollapsedGroups((previous) => {
      const next = new Set(previous);
      if (next.has(groupID)) {
        next.delete(groupID);
      } else {
        next.add(groupID);
      }
      writeCollapsedSessionGroups(next);
      return next;
    });
  }

  useEffect(() => {
    onOverlayOpenChangeRef.current = onOverlayOpenChange;
  });

  const setOverlayHold = useCallback((id: string, open: boolean) => {
    const holds = overlayHoldIDsRef.current;
    if (open) {
      holds.add(id);
    } else {
      holds.delete(id);
    }
    onOverlayOpenChangeRef.current?.(holds.size > 0);
  }, []);

  useEffect(() => {
    return () => {
      overlayHoldIDsRef.current.clear();
      onOverlayOpenChangeRef.current?.(false);
    };
  }, []);

  useEffect(() => {
    if (!draggingSessionID) {
      return;
    }
    const cursorLock = document.createElement("style");
    cursorLock.textContent = "* { cursor: grabbing !important; }";
    document.head.append(cursorLock);
    return () => {
      cursorLock.remove();
    };
  }, [draggingSessionID]);

  useEffect(() => {
    return () => {
      if (dragPreviewFrameRef.current !== null) {
        window.cancelAnimationFrame(dragPreviewFrameRef.current);
      }
      if (autoScrollFrameRef.current !== null) {
        window.cancelAnimationFrame(autoScrollFrameRef.current);
      }
    };
  }, []);

  function clearDragState() {
    if (dragPreviewFrameRef.current !== null) {
      window.cancelAnimationFrame(dragPreviewFrameRef.current);
      dragPreviewFrameRef.current = null;
    }
    if (autoScrollFrameRef.current !== null) {
      window.cancelAnimationFrame(autoScrollFrameRef.current);
      autoScrollFrameRef.current = null;
    }
    autoScrollPointerRef.current = null;
    setDraggingSessionID(null);
    setDragTarget(null);
    setDragPreview(null);
  }

  function moveDragPreview(clientX: number, clientY: number) {
    dragPreviewPointRef.current = { x: clientX, y: clientY };
    if (dragPreviewFrameRef.current !== null) {
      window.cancelAnimationFrame(dragPreviewFrameRef.current);
    }
    dragPreviewFrameRef.current = window.requestAnimationFrame(() => {
      dragPreviewFrameRef.current = null;
      if (dragPreviewRef.current) {
        dragPreviewRef.current.style.transform = dragPreviewTransform(clientX, clientY);
      }
    });
  }

  function scheduleDragAutoScroll(clientX: number, clientY: number) {
    autoScrollPointerRef.current = { x: clientX, y: clientY };
    if (autoScrollFrameRef.current !== null) {
      return;
    }
    autoScrollFrameRef.current = window.requestAnimationFrame(runDragAutoScroll);
  }

  function runDragAutoScroll() {
    autoScrollFrameRef.current = null;
    const pointer = autoScrollPointerRef.current;
    const container = scrollContainerRef.current;
    if (!pointer || !container) {
      return;
    }
    const rect = container.getBoundingClientRect();
    if (pointer.x < rect.left - dragAutoScrollEdgePx || pointer.x > rect.right + dragAutoScrollEdgePx) {
      return;
    }
    let delta = 0;
    if (pointer.y < rect.top + dragAutoScrollEdgePx) {
      const strength = Math.min(1, (rect.top + dragAutoScrollEdgePx - pointer.y) / dragAutoScrollEdgePx);
      delta = -Math.ceil(strength * dragAutoScrollMaxStepPx);
    } else if (pointer.y > rect.bottom - dragAutoScrollEdgePx) {
      const strength = Math.min(1, (pointer.y - (rect.bottom - dragAutoScrollEdgePx)) / dragAutoScrollEdgePx);
      delta = Math.ceil(strength * dragAutoScrollMaxStepPx);
    }
    if (delta === 0) {
      return;
    }
    const previousScrollTop = container.scrollTop;
    container.scrollTop += delta;
    if (container.scrollTop !== previousScrollTop) {
      setDragTarget(findDropTarget(pointer.x, pointer.y));
      autoScrollFrameRef.current = window.requestAnimationFrame(runDragAutoScroll);
    }
  }

  function findDropTarget(clientX: number, clientY: number): SessionDropTarget | null {
    const target = document.elementFromPoint(clientX, clientY);
    const group = target?.closest<HTMLElement>("[data-session-drop-group]");
    const value = group?.dataset.sessionDropGroup;
    if (!group || (value !== "pinned" && value !== "unpinned")) {
      return null;
    }
    if (value === "unpinned") {
      return { group: value, index: 0 };
    }
    const itemElements = Array.from(group.querySelectorAll<HTMLElement>("[data-session-item-id]")).filter(
      (item) => item.dataset.sessionItemId !== draggingSessionID,
    );
    const index = itemElements.findIndex((item) => {
      const rect = item.getBoundingClientRect();
      return clientY < rect.top + rect.height / 2;
    });
    return {
      group: value,
      index: index === -1 ? itemElements.length : index,
    };
  }

  function handlePointerDragStart(sessionID: string, clientX: number, clientY: number) {
    const session = sessions.find((item) => item.id === sessionID);
    dragPreviewPointRef.current = { x: clientX, y: clientY };
    setDraggingSessionID(sessionID);
    setDragPreview({
      id: sessionID,
      title: session?.title || t("session.untitled"),
      x: clientX,
      y: clientY,
    });
  }

  function handlePointerDragMove(clientX: number, clientY: number) {
    moveDragPreview(clientX, clientY);
    scheduleDragAutoScroll(clientX, clientY);
    setDragTarget(findDropTarget(clientX, clientY));
  }

  function handlePointerDrop(sessionID: string, clientX: number, clientY: number) {
    const target = findDropTarget(clientX, clientY);
    const session = sessions.find((item) => item.id === sessionID);
    if (!session) {
      clearDragState();
      return;
    }
    if (!target || target.group !== "pinned") {
      if (session.pinned) {
        onPinChange(session.id, false, 0);
      }
      clearDragState();
      return;
    }

    const pinnedWithoutDragged = pinnedSessions.filter((item) => item.id !== sessionID);
    const insertIndex = Math.max(0, Math.min(target.index, pinnedWithoutDragged.length));
    const nextPinned = [
      ...pinnedWithoutDragged.slice(0, insertIndex),
      { ...session, pinned: true },
      ...pinnedWithoutDragged.slice(insertIndex),
    ];
    nextPinned.forEach((item, index) => {
      const pinnedOrder = index + 1;
      if (!item.pinned || item.pinnedOrder !== pinnedOrder || item.id === sessionID) {
        onPinChange(item.id, true, pinnedOrder);
      }
    });
    clearDragState();
  }

  return (
    <RailOverlayHoldContext.Provider value={setOverlayHold}>
      <SidebarProvider className="pudding-session-rail !contents">
        <Sidebar className="min-h-0 w-full flex-1 bg-transparent" collapsible="none">
          <SidebarHeader className="px-2 py-1.5">
            <SidebarMenu className="gap-0.5">
              <SidebarMenuItem>
                <SidebarMenuButton
                  className="h-7 px-2 py-1"
                  isActive={draftActive && !draftProjectID}
                  onClick={onCreate}
                >
                  <MessageCirclePlus />
                  <span>{t("session.create")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton className="h-7 px-2 py-1" disabled={createProjectPending} onClick={onCreateProject}>
                  {createProjectPending ? <Spinner /> : <FolderPlus />}
                  <span>{t("project.create")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton className="h-7 px-2 py-1" onClick={onSearch}>
                  <Search />
                  <span>{t("rail.search")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton
                  className="h-7 px-2 py-1"
                  isActive={appsActive}
                  onClick={() => {
                    void navigate({
                      to: "/",
                      search: (prev) => {
                        const next = { ...(prev as AppSearch), view: "apps" as const };
                        delete next.session;
                        delete next.split;
                        delete next.draft;
                        delete next.project;
                        return next;
                      },
                    });
                  }}
                >
                  <Package />
                  <span>{t("rail.automations")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarHeader>
          <div aria-hidden="true" className="mx-4 mb-1.5 h-px shrink-0 bg-sidebar-border/70" />
          <SidebarContent ref={scrollContainerRef} className="pt-0 pb-2 overscroll-contain">
            {isLoading ? (
              <SessionListSkeleton />
            ) : isError ? (
              <SessionListError onRefetch={onRefetch} />
            ) : (
              <>
                {showPinnedGroup ? (
                  <Collapsible asChild open={!pinnedCollapsed}>
                    <SidebarGroup
                      className="px-2 py-0"
                      data-session-drop-group="pinned"
                    >
                      <CollapsibleSessionGroupLabel
                        collapsed={pinnedCollapsed}
                        icon="pinned"
                        label={t("session.pinned")}
                        onToggle={() => toggleGroupCollapsed("pinned")}
                      />
                      <CollapsibleContent className="pudding-session-group-content overflow-hidden">
                        <SidebarGroupContent className="pt-0.5">
                          <SessionItems
                            deletePending={deletePending}
                            selectedSessionID={selectedSessionID}
                            sessions={pinnedSessions}
                            showEmptyState={false}
                            draggingSessionID={draggingSessionID}
                            dropIndex={dragTarget?.group === "pinned" ? dragTarget.index : null}
                            showEmptyDropTarget={Boolean(draggingSessionID && pinnedSessions.length === 0)}
                            onDelete={onDelete}
                            onOpenSplit={onOpenSplit}
                            onPinChange={onPinChange}
                            onPointerDragCancel={clearDragState}
                            onPointerDragEnd={handlePointerDrop}
                            onPointerDragMove={handlePointerDragMove}
                            onPointerDragStart={handlePointerDragStart}
                            onRename={onRename}
                            onSelect={onSelect}
                          />
                        </SidebarGroupContent>
                      </CollapsibleContent>
                    </SidebarGroup>
                  </Collapsible>
                ) : null}
                <div
                  className={cn(
                    "rounded-md transition-colors",
                    isDraggingPinned && "min-h-10",
                    isDraggingPinned &&
                    dragTarget?.group === "unpinned" &&
                    "pudding-session-drop-indicator-active ring-1 ring-inset ring-sidebar-ring/60",
                  )}
                  data-session-drop-group="unpinned"
                >
                  {sessions.length === 0 && projects.length === 0 ? (
                    <SessionEmptyState />
                  ) : chatSessions.length > 0 || (isDraggingPinned && unpinnedSessions.length === 0) ? (
                    <Collapsible asChild open={!chatCollapsed}>
                      <SidebarGroup className="px-2 py-0">
                        <CollapsibleSessionGroupLabel
                          collapsed={chatCollapsed}
                          icon="chat"
                          label={t("session.chats")}
                          onToggle={() => toggleGroupCollapsed("chat")}
                        />
                        <CollapsibleContent className="pudding-session-group-content overflow-hidden">
                          <SidebarGroupContent className={cn("pt-0.5", isDraggingPinned && chatSessions.length === 0 && "min-h-8")}>
                            <SessionItems
                              deletePending={deletePending}
                              selectedSessionID={selectedSessionID}
                              sessions={chatSessions}
                              showEmptyState={sessions.length === 0}
                              draggingSessionID={draggingSessionID}
                              dropIndex={null}
                              showEmptyDropTarget={Boolean(isDraggingPinned && chatSessions.length === 0 && sessions.length > 0)}
                              onDelete={onDelete}
                              onOpenSplit={onOpenSplit}
                              onPinChange={onPinChange}
                              onPointerDragCancel={clearDragState}
                              onPointerDragEnd={handlePointerDrop}
                              onPointerDragMove={handlePointerDragMove}
                              onPointerDragStart={handlePointerDragStart}
                              onRename={onRename}
                              onSelect={onSelect}
                            />
                          </SidebarGroupContent>
                        </CollapsibleContent>
                      </SidebarGroup>
                    </Collapsible>
                  ) : null}
                  {projectGroups.map((group) => {
                    const projectCollapsed = collapsedGroups.has(`project:${group.key}`);
                    return (
                      <Collapsible key={group.key} asChild open={!projectCollapsed}>
                        <SidebarGroup className="px-2 py-0">
                          <CollapsibleSessionGroupLabel
                            active={draftActive && draftProjectID === group.projectID}
                            actions={group.project ? <ProjectActionsMenu project={group.project} token={token} /> : undefined}
                            collapsed={projectCollapsed}
                            icon="project"
                            label={group.label}
                            title={group.label}
                            actionLabel={t("session.create")}
                            onAction={group.projectID ? () => onCreateProjectSession(group.projectID!) : undefined}
                            onToggle={() => toggleGroupCollapsed(`project:${group.key}`)}
                          />
                          <CollapsibleContent className="pudding-session-group-content overflow-hidden">
                            <SidebarGroupContent className="pt-0.5">
                              <SessionItems
                                deletePending={deletePending}
                                selectedSessionID={selectedSessionID}
                                sessions={group.sessions}
                                showEmptyState={false}
                                draggingSessionID={draggingSessionID}
                                dropIndex={null}
                                showEmptyDropTarget={false}
                                onDelete={onDelete}
                                onOpenSplit={onOpenSplit}
                                onPinChange={onPinChange}
                                onPointerDragCancel={clearDragState}
                                onPointerDragEnd={handlePointerDrop}
                                onPointerDragMove={handlePointerDragMove}
                                onPointerDragStart={handlePointerDragStart}
                                onRename={onRename}
                                onSelect={onSelect}
                              />
                            </SidebarGroupContent>
                          </CollapsibleContent>
                        </SidebarGroup>
                      </Collapsible>
                    );
                  })}
                </div>
              </>
            )}
            {dragPreview ? (
              <SessionDragPreview
                point={dragPreviewPointRef.current}
                preview={dragPreview}
                previewRef={dragPreviewRef}
              />
            ) : null}
          </SidebarContent>
          <SidebarFooter>
            <RailUpdateButton serverTurnRunning={sessions.some((session) => session.running)} />
            <div className="flex items-center gap-1">
              <RailThemeToggle />
              <RailLanguageToggle />
              <div className="flex-1" />
              <Button
                aria-label={t("settings.title")}
                size="icon"
                tabIndex={-1}
                variant="ghost"
                onClick={() => openSettingsDialog()}
              >
                <Settings />
              </Button>
            </div>
          </SidebarFooter>
        </Sidebar>
      </SidebarProvider>
    </RailOverlayHoldContext.Provider>
  );
}

function dragPreviewTransform(clientX: number, clientY: number) {
  return `translate3d(${clientX + 12}px, ${clientY}px, 0) translateY(-50%)`;
}

function RailThemeToggle() {
  const [open, setOpen] = useState(false);
  useRailOverlayHold(open);
  return <ThemeToggle onOpenChange={setOpen} />;
}

function RailUpdateButton({ serverTurnRunning }: { serverTurnRunning: boolean }) {
  const { t } = useI18n();
  const [state, setState] = useState<DesktopUpdateState | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const runningTurns = useOverlayStore((current) => current.runningTurns);
  const turnPhases = useOverlayStore((current) => current.turnPhases);

  useEffect(() => {
    let active = true;
    let receivedEvent = false;
    const unsubscribe = onDesktopUpdateState((next) => {
      if (active) {
        receivedEvent = true;
        setState(next);
      }
    });
    void getDesktopUpdateState().then((next) => {
      if (active && !receivedEvent) {
        setState(next);
      }
    });
    return () => {
      active = false;
      unsubscribe();
    };
  }, []);

  if (state?.status !== "downloaded" && state?.status !== "installing") {
    return null;
  }
  const installing = state.status === "installing";
  const hasActiveTurn =
    serverTurnRunning ||
    Object.values(runningTurns).some(Boolean) ||
    Object.values(turnPhases).some((phase) => isTurnPhaseActive(phase));
  const restart = () => void activateDesktopUpdate();
  return (
    <>
      <Button
        className="mb-1 h-9 w-full justify-start gap-2 px-2 font-normal"
        disabled={installing}
        title={state.version || undefined}
        variant="secondary"
        onClick={() => (hasActiveTurn ? setConfirmOpen(true) : restart())}
      >
        <span className="truncate">
          {t(installing ? "update.restarting" : "update.restart")}
          {state.version ? ` ${state.version}` : ""}
        </span>
        {installing ? <Spinner className="ml-auto size-4 shrink-0" /> : <ArrowRight className="ml-auto size-4 shrink-0" />}
      </Button>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("update.activeTurnTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("update.activeTurnDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={restart}>{t("update.restartAnyway")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function RailLanguageToggle() {
  const [open, setOpen] = useState(false);
  useRailOverlayHold(open);
  return <LanguageToggle onOpenChange={setOpen} />;
}

function SessionDragPreview({
  point,
  preview,
  previewRef,
}: {
  point: {
    x: number;
    y: number;
  };
  preview: {
    title: string;
    x: number;
    y: number;
  };
  previewRef: RefObject<HTMLDivElement | null>;
}) {
  if (typeof document === "undefined") {
    return null;
  }
  return createPortal(
    <div
      ref={previewRef}
      aria-hidden="true"
      className="pointer-events-none fixed top-0 left-0 z-50 max-w-[220px] select-none rounded-md bg-popover px-3 py-2 text-sm text-popover-foreground shadow-lg ring-1 ring-border/70 will-change-transform"
      style={{
        transform: dragPreviewTransform(point.x || preview.x, point.y || preview.y),
      }}
    >
      <div className="truncate">{preview.title}</div>
    </div>,
    document.body,
  );
}

function CollapsibleSessionGroupLabel({
  active = false,
  collapsed,
  icon,
  label,
  actions,
  actionLabel,
  title,
  onAction,
  onToggle,
}: {
  active?: boolean;
  collapsed: boolean;
  icon: "chat" | "pinned" | "project";
  label: string;
  actions?: ReactNode;
  actionLabel?: string;
  title?: string;
  onAction?: () => void;
  onToggle: () => void;
}) {
  const Icon = icon === "pinned" ? Pin : icon === "chat" ? MessageSquareText : collapsed ? FolderClosed : FolderOpen;
  return (
    <SidebarGroupLabel
      className={cn(
        "group/project-label h-7 min-h-7 gap-1 px-0 text-sm transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground has-[[data-project-actions-open=true]]:bg-sidebar-accent has-[[data-project-actions-open=true]]:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground",
      )}
      title={title || label}
    >
      <button
        className="group flex h-full min-w-0 flex-1 cursor-default items-center gap-2 rounded-md px-2 pr-1 text-left hover:text-sidebar-accent-foreground"
        type="button"
        onClick={onToggle}
      >
        <Icon className={cn("size-3.5 shrink-0 text-sidebar-foreground/60", icon === "pinned" && "rotate-45")} />
        <span className="min-w-0 truncate">{label}</span>
        <ChevronRight
          className={cn(
            "size-3.5 shrink-0 opacity-0 transition-[opacity,transform] group-hover:opacity-100 group-focus-visible:opacity-100 group-has-[[data-project-actions-open=true]]/project-label:opacity-100",
            !collapsed && "rotate-90",
          )}
        />
      </button>
      {actions}
      {onAction ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              aria-label={actionLabel || label}
              className="flex size-6 shrink-0 items-center justify-center rounded-md text-sidebar-foreground/60 opacity-0 transition-opacity hover:bg-sidebar-accent hover:text-sidebar-accent-foreground group-hover/project-label:opacity-100 group-has-[[data-state=open]]/project-label:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-hidden"
              type="button"
              onClick={(event) => {
                event.stopPropagation();
                onAction();
              }}
            >
              <MessageCirclePlus className="size-3.5" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="right">{actionLabel || label}</TooltipContent>
        </Tooltip>
      ) : null}
    </SidebarGroupLabel>
  );
}

function ProjectActionsMenu({ project, token }: { project: Project; token: string }) {
  const { t } = useI18n();
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const [menuOpen, setMenuOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [name, setName] = useState(project.name);
  const rootDir = project.rootDirs[0];
  const isMac =
    (typeof document !== "undefined" && document.documentElement.dataset.shell === "electron-mac") ||
    (typeof navigator !== "undefined" && /Mac/i.test(navigator.platform));

  useRailOverlayHold(menuOpen || renameOpen || deleteOpen);

  const revealMutation = useMutation({
    mutationFn: (path: string) => revealDesktopPath(token, path),
    onError: () => toast.error(t("project.revealFailed")),
  });
  const renameMutation = useMutation({
    mutationFn: (nextName: string) => updateProject(token, project.id, { name: nextName }),
    onSuccess: (updated) => {
      queryClient.setQueryData(queryKeys.project(updated.id), updated);
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) =>
        previous
          ? { projects: previous.projects.map((entry) => (entry.id === updated.id ? updated : entry)) }
          : { projects: [updated] },
      );
      setRenameOpen(false);
    },
    onError: () => toast.error(t("project.renameFailed")),
  });
  const deleteMutation = useMutation({
    mutationFn: () => deleteProject(token, project.id),
    onSuccess: async () => {
      const previousSessions = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      const attachedSessions = previousSessions?.sessions.filter((session) => session.projectID === project.id) || [];
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) => ({
        projects: previous?.projects.filter((entry) => entry.id !== project.id) || [],
      }));
      if (previousSessions) {
        queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), {
          sessions: previousSessions.sessions.map((session) =>
            session.projectID === project.id ? { ...session, projectID: undefined } : session,
          ),
        });
      }
      for (const session of attachedSessions) {
        queryClient.setQueryData<Session>(queryKeys.session(session.id), (previous) =>
          previous ? { ...previous, projectID: undefined } : previous,
        );
      }
      queryClient.removeQueries({ queryKey: queryKeys.project(project.id), exact: true });
      setDeleteOpen(false);
      await navigate({
        to: "/",
        search: (previous) => {
          const next = { ...(previous as AppSearch) };
          if (next.project === project.id) {
            delete next.project;
          }
          return next;
        },
        replace: true,
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projects() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
      ]);
    },
    onError: () => toast.error(t("project.deleteFailed")),
  });

  const openRename = () => {
    setName(project.name);
    setRenameOpen(true);
  };
  const saveRename = () => {
    const nextName = name.trim();
    if (!nextName || nextName === project.name) {
      setRenameOpen(false);
      return;
    }
    renameMutation.mutate(nextName);
  };
  const copyPaths = (paths: string[]) => {
    void navigator.clipboard.writeText(paths.join("\n")).then(
      () => toast.success(t(paths.length > 1 ? "project.pathsCopied" : "project.pathCopied")),
      () => toast.error(t("project.pathCopyFailed")),
    );
  };

  return (
    <>
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild>
          <button
            aria-label={t("project.actions")}
            className="flex size-6 shrink-0 items-center justify-center rounded-md text-sidebar-foreground/60 opacity-0 transition-opacity hover:bg-sidebar-accent hover:text-sidebar-accent-foreground group-hover/project-label:opacity-100 data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground data-[state=open]:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-hidden"
            data-project-actions-open={menuOpen}
            type="button"
            onClick={(event) => event.stopPropagation()}
          >
            <Ellipsis className="size-3.5" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48 space-y-1">
          {project.rootDirs.length > 1 ? (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <FolderOpen />
                {t("project.directories")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-64 max-w-[calc(100vw-2rem)]">
                {project.rootDirs.map((path) => (
                  <DropdownMenuItem
                    key={path}
                    disabled={!isMac || revealMutation.isPending}
                    title={path}
                    onSelect={() => revealMutation.mutate(path)}
                  >
                    <span className="min-w-0 truncate">{projectDirectoryLabel(path, project.rootDirs)}</span>
                  </DropdownMenuItem>
                ))}
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => copyPaths(project.rootDirs)}>
                  {t("project.copyAllPaths")}
                </DropdownMenuItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          ) : (
            <>
              {isMac && rootDir ? (
                <DropdownMenuItem disabled={revealMutation.isPending} onSelect={() => revealMutation.mutate(rootDir)}>
                  {revealMutation.isPending ? <Spinner /> : null}
                  {t("project.revealFinder")}
                </DropdownMenuItem>
              ) : null}
              {rootDir ? (
                <DropdownMenuItem title={rootDir} onSelect={() => copyPaths([rootDir])}>
                  {t("project.copyPath")}
                </DropdownMenuItem>
              ) : null}
            </>
          )}
          <DropdownMenuItem onSelect={openRename}>
            {t("project.rename")}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={deleteMutation.isPending}
            onSelect={() => setDeleteOpen(true)}
          >
            <Trash />
            {t("project.delete")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <form
            className="contents"
            onSubmit={(event) => {
              event.preventDefault();
              saveRename();
            }}
          >
            <DialogHeader>
              <DialogTitle>{t("project.renameTitle")}</DialogTitle>
              <DialogDescription>{t("project.renameDescription")}</DialogDescription>
            </DialogHeader>
            <Input
              autoFocus
              aria-label={t("project.name")}
              disabled={renameMutation.isPending}
              maxLength={120}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
            <DialogFooter>
              <Button disabled={renameMutation.isPending} type="button" variant="outline" onClick={() => setRenameOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button disabled={renameMutation.isPending || !name.trim()} type="submit">
                {renameMutation.isPending ? <Spinner /> : null}
                {t("common.save")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <AlertDialog open={deleteOpen} onOpenChange={(open) => !deleteMutation.isPending && setDeleteOpen(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("project.deleteTitle").replace("{name}", project.name)}</AlertDialogTitle>
            <AlertDialogDescription>{t("project.deleteDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={deleteMutation.isPending}
              onClick={() => deleteMutation.mutate()}
            >
              {deleteMutation.isPending ? <Spinner /> : null}
              {t("project.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

type SessionItemsProps = {
  sessions: Session[];
  selectedSessionID: string | undefined;
  deletePending: boolean;
  showEmptyState?: boolean;
  draggingSessionID: string | null;
  dropIndex: number | null;
  showEmptyDropTarget: boolean;
  onSelect: (id: string) => void;
  onOpenSplit: (id: string) => void;
  onPinChange: (id: string, pinned: boolean, pinnedOrder?: number) => void;
  onDelete: (id: string) => void;
  onRename: (id: string, title: string) => void;
  onPointerDragStart: (id: string, clientX: number, clientY: number) => void;
  onPointerDragMove: (clientX: number, clientY: number) => void;
  onPointerDragEnd: (id: string, clientX: number, clientY: number) => void;
  onPointerDragCancel: () => void;
};

function SessionListSkeleton() {
  return (
    <div className="grid gap-0.5 p-2">
      <Skeleton className="h-7 rounded-md" />
      <Skeleton className="h-7 rounded-md" />
      <Skeleton className="h-7 rounded-md" />
    </div>
  );
}

function SessionListError({ onRefetch }: { onRefetch: () => void }) {
  const { t } = useI18n();
  return (
    <div className="grid justify-items-center gap-2 px-3 py-6 text-center text-xs text-muted-foreground">
      <div>{t("session.loadFailed")}</div>
      <Button className="h-7 px-2 text-xs" size="sm" type="button" variant="outline" onClick={onRefetch}>
        {t("common.refresh")}
      </Button>
    </div>
  );
}

function SessionItems({
  sessions,
  selectedSessionID,
  deletePending,
  showEmptyState = true,
  draggingSessionID,
  dropIndex,
  showEmptyDropTarget,
  onSelect,
  onOpenSplit,
  onPinChange,
  onDelete,
  onRename,
  onPointerDragStart,
  onPointerDragMove,
  onPointerDragEnd,
  onPointerDragCancel,
}: SessionItemsProps) {
  const { t } = useI18n();
  const [showAll, setShowAll] = useState(false);
  // 实时运行态:sessions 快照(15s 兜底)与 SSE overlay 双源取或
  const runningTurns = useOverlayStore((state) => state.runningTurns);
  const turnPhases = useOverlayStore((state) => state.turnPhases);
  const completedSessions = useOverlayStore((state) => state.completedSessions);

  if (sessions.length === 0 && showEmptyState) {
    return <SessionEmptyState />;
  }
  const canCollapse = sessions.length > sessionCollapseThreshold;
  const cappedSessions = showAll || draggingSessionID || !canCollapse
    ? sessions
    : sessions.slice(0, collapsedSessionDisplayLimit);
  const visibleSessions = cappedSessions.filter((session) => session.id !== draggingSessionID);
  const hiddenSessionCount = canCollapse ? sessions.length - collapsedSessionDisplayLimit : 0;
  if (visibleSessions.length === 0 && (dropIndex !== null || showEmptyDropTarget)) {
    return (
      <SidebarMenu className="gap-0.5">
        <SessionDropIndicator active={dropIndex !== null} />
      </SidebarMenu>
    );
  }
  if (visibleSessions.length === 0) {
    return null;
  }

  return (
    <SidebarMenu className="gap-0.5">
      {visibleSessions.map((session, index) => (
        <Fragment key={session.id}>
          {dropIndex === index ? <SessionDropIndicator active /> : null}
          <SessionItem
            completed={Boolean(completedSessions[session.id])}
            deletePending={deletePending}
            running={session.running || Boolean(runningTurns[session.id]) || isTurnPhaseActive(turnPhases[session.id])}
            selected={session.id === selectedSessionID}
            session={session}
            suppressInteractiveState={Boolean(draggingSessionID)}
            onDelete={() => onDelete(session.id)}
            onOpenSplit={() => onOpenSplit(session.id)}
            onPinChange={(pinned) => onPinChange(session.id, pinned)}
            onPointerDragCancel={onPointerDragCancel}
            onPointerDragEnd={(clientX, clientY) => onPointerDragEnd(session.id, clientX, clientY)}
            onPointerDragMove={onPointerDragMove}
            onPointerDragStart={(clientX, clientY) => onPointerDragStart(session.id, clientX, clientY)}
            onRename={(title) => onRename(session.id, title)}
            onSelect={() => onSelect(session.id)}
          />
        </Fragment>
      ))}
      {dropIndex === visibleSessions.length ? <SessionDropIndicator active /> : null}
      {hiddenSessionCount > 0 && !draggingSessionID ? (
        <SidebarMenuItem>
          <button
            aria-expanded={showAll}
            className="flex h-7 w-full items-center rounded-md pr-2 pl-7 text-left text-xs text-sidebar-foreground/50 transition-colors hover:text-sidebar-foreground/80"
            type="button"
            onClick={() => setShowAll((current) => !current)}
          >
            <span>
              {showAll
                ? t("session.showLess")
                : t("session.showMore").replace("{count}", String(hiddenSessionCount))}
            </span>
          </button>
        </SidebarMenuItem>
      ) : null}
    </SidebarMenu>
  );
}

function SessionEmptyState() {
  const { t } = useI18n();
  return (
    <div className="grid justify-items-center gap-2 px-2 py-10 text-center text-sm text-muted-foreground">
      <MessageSquareText className="h-5 w-5" />
      <div>{t("session.empty")}</div>
    </div>
  );
}

function SessionDropIndicator({ active }: { active: boolean }) {
  return (
    <li
      aria-hidden="true"
      className={cn(
        "h-7 rounded-md ring-1 ring-inset transition-colors",
        active ? "pudding-session-drop-indicator-active ring-sidebar-ring/70" : "ring-sidebar-border/70",
      )}
    />
  );
}

type SessionItemProps = {
  session: Session;
  selected: boolean;
  running: boolean;
  completed: boolean;
  deletePending: boolean;
  suppressInteractiveState: boolean;
  onSelect: () => void;
  onOpenSplit: () => void;
  onPinChange: (pinned: boolean) => void;
  onDelete: () => void;
  onRename: (title: string) => void;
  onPointerDragStart: (clientX: number, clientY: number) => void;
  onPointerDragMove: (clientX: number, clientY: number) => void;
  onPointerDragEnd: (clientX: number, clientY: number) => void;
  onPointerDragCancel: () => void;
};

function SessionItem({
  session,
  selected,
  running,
  completed,
  deletePending,
  suppressInteractiveState,
  onSelect,
  onOpenSplit,
  onPinChange,
  onDelete,
  onRename,
  onPointerDragStart,
  onPointerDragMove,
  onPointerDragEnd,
  onPointerDragCancel,
}: SessionItemProps) {
  const { t, locale } = useI18n();
  const actionsAlwaysVisible = !useHasHoverInput();
  const title = session.title || t("session.untitled");
  const [actionsOpen, setActionsOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const inputRef = useRef<HTMLInputElement>(null);
  const editAfterMenuCloseRef = useRef(false);
  const pointerDragRef = useRef<{
    pointerID: number;
    startX: number;
    startY: number;
    dragging: boolean;
    cleanup?: () => void;
  } | null>(null);
  const suppressClickRef = useRef(false);

  useRailOverlayHold(actionsOpen || deleteOpen);

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

  function selectSession() {
    onSelect();
  }

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

  function handlePointerDown(event: ReactPointerEvent<HTMLButtonElement>) {
    if (event.button !== 0) {
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    pointerDragRef.current = {
      pointerID: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      dragging: false,
    };
  }

  function handlePointerMove(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = pointerDragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) {
      return;
    }
    const distance = Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY);
    if (!drag.dragging && distance < 6) {
      return;
    }
    if (!drag.dragging) {
      drag.dragging = true;
      bindWindowPointerDrag(event.pointerId);
      event.currentTarget.setPointerCapture(event.pointerId);
      onPointerDragStart(event.clientX, event.clientY);
    }
    event.preventDefault();
    onPointerDragMove(event.clientX, event.clientY);
  }

  function finishPointerDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = pointerDragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) {
      return;
    }
    pointerDragRef.current = null;
    drag.cleanup?.();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    if (!drag.dragging) {
      return;
    }
    event.preventDefault();
    suppressClickRef.current = true;
    onPointerDragEnd(event.clientX, event.clientY);
    window.setTimeout(() => {
      suppressClickRef.current = false;
    }, 0);
  }

  function cancelPointerDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = pointerDragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) {
      return;
    }
    pointerDragRef.current = null;
    drag.cleanup?.();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    onPointerDragCancel();
  }

  function releasePointerFocus(event: ReactPointerEvent<HTMLButtonElement>) {
    if (document.activeElement === event.currentTarget && !event.currentTarget.matches(":focus-visible")) {
      event.currentTarget.blur();
    }
  }

  function bindWindowPointerDrag(pointerID: number) {
    const drag = pointerDragRef.current;
    if (!drag || drag.cleanup) {
      return;
    }
    const handleMove = (event: PointerEvent) => {
      if (event.pointerId !== pointerID || !pointerDragRef.current?.dragging) {
        return;
      }
      event.preventDefault();
      onPointerDragMove(event.clientX, event.clientY);
    };
    const handleEnd = (event: PointerEvent) => {
      const current = pointerDragRef.current;
      if (event.pointerId !== pointerID || !current) {
        return;
      }
      pointerDragRef.current = null;
      current.cleanup?.();
      if (current.dragging) {
        event.preventDefault();
        suppressClickRef.current = true;
        onPointerDragEnd(event.clientX, event.clientY);
        window.setTimeout(() => {
          suppressClickRef.current = false;
        }, 0);
      } else {
        onPointerDragCancel();
      }
    };
    drag.cleanup = () => {
      window.removeEventListener("pointermove", handleMove);
      window.removeEventListener("pointerup", handleEnd);
      window.removeEventListener("pointercancel", handleEnd);
    };
    window.addEventListener("pointermove", handleMove, { passive: false });
    window.addEventListener("pointerup", handleEnd);
    window.addEventListener("pointercancel", handleEnd);
  }

  return (
    <SidebarMenuItem className={suppressInteractiveState ? "pointer-events-none" : undefined}>
      {editing ? (
        <SidebarMenuButton
          asChild
          className="h-7 px-2 py-1"
          isActive
        >
          <div className="cursor-text">
            <span aria-hidden="true" className="size-3 shrink-0" />
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
          className={cn(
            "h-7 px-2 py-1",
            running
              ? "pr-24 data-active:font-normal group-has-data-[sidebar=menu-action]/menu-item:pr-24"
              : actionsAlwaysVisible
              ? "pr-20 data-active:font-normal group-has-data-[sidebar=menu-action]/menu-item:pr-20"
              : "pr-11 data-active:font-normal group-has-data-[sidebar=menu-action]/menu-item:pr-11",
            suppressInteractiveState
              ? "hover:bg-transparent hover:text-sidebar-foreground active:bg-transparent active:text-sidebar-foreground"
              : "group-hover/menu-item:bg-sidebar-accent group-hover/menu-item:text-sidebar-accent-foreground",
          )}
          isActive={selected || actionsOpen}
        >
          <button
            data-session-item-id={session.id}
            type="button"
            onClick={(event) => {
              if (suppressClickRef.current) {
                event.preventDefault();
                event.stopPropagation();
                return;
              }
              selectSession();
            }}
            onDoubleClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              startEditing();
            }}
            onPointerCancel={cancelPointerDrag}
            onPointerDown={handlePointerDown}
            onPointerLeave={releasePointerFocus}
            onPointerMove={handlePointerMove}
            onPointerUp={finishPointerDrag}
          >
            <span aria-hidden="true" className="size-3 shrink-0" />
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
              "min-w-0 px-0 font-normal text-muted-foreground",
              actionsAlwaysVisible ? "right-8" : "right-2",
              !actionsAlwaysVisible &&
              !suppressInteractiveState &&
              "group-focus-within/menu-item:opacity-0 group-hover/menu-item:opacity-0",
              actionsOpen && "opacity-0",
            )}
          >
            {running ? (
              <span className="flex items-center gap-1 whitespace-nowrap">
                <Spinner className="size-3" />
                {t("session.processing")}
              </span>
            ) : session.backgroundProcessCount > 0 ? (
              <span
                aria-label={t("session.backgroundTasks").replace("{count}", String(session.backgroundProcessCount))}
                className="flex items-center gap-1 whitespace-nowrap"
              >
                <SquareTerminal className="size-3" />
                <span className="tabular-nums">{session.backgroundProcessCount}</span>
              </span>
            ) : completed ? (
              <span aria-label={t("session.completed")} className="size-2 rounded-full bg-blue-500" />
            ) : (
              formatRelative(session.lastActivityAt || session.createdAt, locale)
            )}
          </SidebarMenuBadge>
          <DropdownMenu open={actionsOpen} onOpenChange={setActionsOpen}>
            <DropdownMenuTrigger asChild>
              <SidebarMenuAction
                aria-label={t("session.actions")}
                className={cn(
                  "right-1.5 rounded-sm bg-transparent text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-0",
                  actionsAlwaysVisible && "opacity-100",
                  !actionsAlwaysVisible &&
                  suppressInteractiveState &&
                  "group-hover/menu-item:opacity-0 hover:bg-transparent hover:text-muted-foreground md:opacity-0",
                  actionsOpen && "opacity-100 md:opacity-100",
                )}
                showOnHover={!actionsAlwaysVisible}
                tabIndex={-1}
              >
                <Ellipsis className="size-3.5!" />
              </SidebarMenuAction>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="end"
              onCloseAutoFocus={(event) => {
                event.preventDefault();
                if (!editAfterMenuCloseRef.current) {
                  return;
                }
                editAfterMenuCloseRef.current = false;
                startEditing();
              }}
            >
              <DropdownMenuItem onSelect={() => onPinChange(!session.pinned)}>
                <Pin className="rotate-45" />
                {session.pinned ? t("session.unpin") : t("session.pin")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onOpenSplit}>
                {t("session.openSplit")}
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={() => {
                  editAfterMenuCloseRef.current = true;
                }}
              >
                {t("session.rename")}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
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
                <AlertDialogDescription>
                  {session.backgroundProcessCount > 0
                    ? t("deleteSession.descriptionWithProcesses").replace("{count}", String(session.backgroundProcessCount))
                    : t("deleteSession.description")}
                </AlertDialogDescription>
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

function basename(path: string) {
  const normalized = path.replace(/\/+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}

function projectDirectoryLabel(path: string, paths: string[]) {
  const name = basename(path);
  if (paths.filter((candidate) => basename(candidate) === name).length < 2) {
    return name;
  }
  const normalized = path.replace(/[\\/]+$/, "");
  const parts = normalized.split(/[\\/]/).filter(Boolean);
  return parts.slice(-2).join("/") || path;
}
