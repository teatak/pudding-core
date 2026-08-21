import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  archiveSession,
  getAudioBindings,
  listProjects,
  listSessions,
  updateSession,
} from "@/api/client";
import type { Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { RailIconAction } from "@/components/session-rail/RailIconAction";
import { PanelLeft } from "@/components/icons";
import { PuddingWordmark } from "@/components/PuddingWordmark";
import { RailPanel } from "@/components/session-rail/RailPanel";
import { SessionSearchDialog, type SessionSearchSelection } from "@/components/SessionSearchDialog";
import { Popover, PopoverAnchor } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useBackgroundSessionEvents } from "@/hooks/useSessionEvents";
import { useHasHoverInput } from "@/hooks/use-hover-input";
import { useI18n } from "@/i18n";
import { onDesktopMenuCommand } from "@/lib/desktopBridge";
import type { AppSearch } from "@/lib/route";
import { syncSessionProjectState } from "@/lib/sessionProjectState";
import { openSettingsDialog } from "@/lib/settingsDialog";
import { isTurnPhaseActive, useOverlayStore } from "@/state/overlayStore";
import {
  setRailCollapsed,
  useRailCollapsed,
  useRailResponsiveCollapsed,
} from "@/state/railStore";
import { requestTranscriptTurnReveal } from "@/state/transcriptRevealStore";


const popoverAlignNudgePx = 3;

// 会话栏(rail):展开 = 左侧整栏;折叠 = 悬浮触发器 + hover 浮出 popover 面板。
// 面板内容(RailPanel)两种形态完全复用。
export function SessionRail({
  token,
  selectedSessionID,
  activeSessionIDs = [],
  draftActive,
  onCreateProject,
}: {
  token: string;
  selectedSessionID: string | undefined;
  activeSessionIDs?: string[];
  draftActive: boolean;
  onCreateProject: () => void;
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
  const responsiveCollapsed = useRailResponsiveCollapsed();
  const hasHoverInput = useHasHoverInput();
  const hover = useHoverPopover();
  const [searchOpen, setSearchOpen] = useState(false);

  useEffect(() => {
    if (!collapsed) {
      hover.close();
    }
  }, [collapsed]);

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
    [hover, navigate],
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
  const projectsActive = view === "projects";
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
    onMutate: async ({ id, title }) => {
      await Promise.all([
        queryClient.cancelQueries({ queryKey: queryKeys.sessions() }),
        queryClient.cancelQueries({ queryKey: queryKeys.session(id) }),
      ]);
      const previousSessions = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      const previousSession = queryClient.getQueryData<Session>(queryKeys.session(id));
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => ({
        sessions: (previous?.sessions || []).map((session) =>
          session.id === id ? { ...session, title } : session,
        ),
      }));
      queryClient.setQueryData<Session>(queryKeys.session(id), (previous) =>
        previous ? { ...previous, title } : previous,
      );
      return { previousSession, previousSessions };
    },
    onError: (_error, { id }, context) => {
      if (context?.previousSessions) {
        queryClient.setQueryData(queryKeys.sessions(), context.previousSessions);
      }
      if (context?.previousSession) {
        queryClient.setQueryData(queryKeys.session(id), context.previousSession);
      }
    },
    onSuccess: (updated) => {
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => ({
        sessions: (previous?.sessions || []).map((session) =>
          session.id === updated.id ? updated : session,
        ),
      }));
      queryClient.setQueryData(queryKeys.session(updated.id), updated);
    },
    onSettled: (_data, _error, { id }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.session(id) });
    },
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

  function changePinned(id: string, pinned: boolean, pinnedOrder?: number): Promise<void> {
    return pinMutation.mutateAsync({
      id,
      pinned,
      pinnedOrder: pinned ? pinnedOrder ?? nextPinnedOrder(id) : 0,
    }).then(
      () => undefined,
      () => undefined,
    );
  }

  const archiveMutation = useMutation({
    mutationFn: (sessionID: string) => archiveSession(token, sessionID),
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
    onError: () => toast.error(t("session.archiveFailed")),
  });

  const sessionPlacementMutation = useMutation({
    mutationFn: ({
      id,
      projectID,
      pinned,
      pinnedOrder,
    }: {
      id: string;
      projectID: string;
      pinned?: boolean;
      pinnedOrder?: number;
    }) => updateSession(token, id, { projectID, pinned, pinnedOrder }),
    onMutate: async ({ id, projectID, pinned, pinnedOrder }) => {
      await Promise.all([
        queryClient.cancelQueries({ queryKey: queryKeys.sessions() }),
        queryClient.cancelQueries({ queryKey: queryKeys.session(id) }),
      ]);
      const previousSessions = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      const previousSession = queryClient.getQueryData<Session>(queryKeys.session(id));
      const applyPlacement = (session: Session): Session => {
        if (session.id !== id) {
          return session;
        }
        const next: Session = { ...session, projectID };
        if (projectID) {
          next.activeMode = "code";
          next.modeLease = "session";
        }
        if (pinned !== undefined) {
          next.pinned = pinned;
        }
        if (pinnedOrder !== undefined) {
          next.pinnedOrder = pinnedOrder;
        }
        return next;
      };
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
        previous ? { sessions: previous.sessions.map(applyPlacement) } : previous,
      );
      queryClient.setQueryData<Session>(queryKeys.session(id), (previous) =>
        previous ? applyPlacement(previous) : previous,
      );
      return { previousSession, previousSessions };
    },
    onSuccess: async (updated) => {
      await syncSessionProjectState(queryClient, token, updated.id, updated);
    },
    onError: (_error, { id }, context) => {
      if (context?.previousSessions) {
        queryClient.setQueryData(queryKeys.sessions(), context.previousSessions);
      }
      if (context?.previousSession) {
        queryClient.setQueryData(queryKeys.session(id), context.previousSession);
      }
      toast.error(t("session.projectChangeFailed"));
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
  }

  function openProjectCreate() {
    hover.close();
    onCreateProject();
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
  }

  function selectSearchResult(selection: SessionSearchSelection) {
    if (selection.turnID) {
      requestTranscriptTurnReveal(selection.sessionID, selection.turnID, selection.messageRole);
    }
    selectSession(selection.sessionID);
  }

  function renderPanel() {
    return (
      <RailPanel
        appsActive={appsActive}
        projectsActive={projectsActive}
        draftActive={draftActive}
        archivePending={archiveMutation.isPending}
        draftProjectID={draftProjectID}
        isError={sessionsQuery.isError}
        isLoading={sessionsQuery.isLoading}
        projects={projects}
        projectChangePendingID={
          sessionPlacementMutation.isPending ? sessionPlacementMutation.variables?.id : undefined
        }
        selectedSessionID={selectedSessionID}
        sessions={sessions}
        token={token}
        onSearch={openSessionSearch}
        onCreate={openNewSession}
        onCreateProject={openProjectCreate}
        onArchive={(id) => archiveMutation.mutate(id)}
        onCreateProjectSession={openProjectDraft}
        onRename={async (id, title) => {
          await renameMutation.mutateAsync({ id, title });
        }}
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
        }}
        onPinChange={changePinned}
        onPlacementChange={(id, projectID) =>
          sessionPlacementMutation
            .mutateAsync({ id, projectID, pinned: false, pinnedOrder: 0 })
            .then(() => undefined)
        }
        onProjectChange={(id, projectID) =>
          sessionPlacementMutation.mutateAsync({ id, projectID }).then(() => undefined)
        }
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
      className="pudding-rail-toggle no-drag-region absolute top-0 left-(--rail-toggle-left) z-40 flex translate-x-1 items-center"
      style={{
        height: "var(--toolbar-h)",
        marginLeft: "var(--traffic-inset)",
      }}
    >
      <Popover open={collapsed && hover.open} onOpenChange={hover.handleOpenChange}>
        <Tooltip>
          <PopoverAnchor asChild>
            <TooltipTrigger asChild>
              <RailIconAction
                aria-label={collapsed ? t("rail.expand") : t("rail.collapse")}
                className="size-7 opacity-100"
                tabIndex={-1}
                onClick={() => {
                  if (responsiveCollapsed) {
                    hover.toggle();
                    return;
                  }
                  if (!collapsed) {
                    collapse(true);
                    return;
                  }
                  collapse(false);
                }}
                onMouseEnter={() => {
                  if (collapsed && hasHoverInput) {
                    hover.openNow();
                  }
                }}
                onMouseLeave={() => {
                  if (collapsed && hasHoverInput) {
                    hover.scheduleClose();
                  }
                }}
              >
                <PanelLeft />
              </RailIconAction>
            </TooltipTrigger>
          </PopoverAnchor>
          {!collapsed ? <TooltipContent side="bottom">{t("rail.collapse")}</TooltipContent> : null}
        </Tooltip>
        {collapsed ? (
          <PopoverContent
            align="start"
            alignOffset={popoverAlignOffset}
            className="flex h-[min(48rem,var(--radix-popover-content-available-height))] max-h-[var(--radix-popover-content-available-height)] w-[260px] flex-col gap-0 border-0 !bg-sidebar p-0 text-sidebar-foreground shadow-[0_8px_20px_-16px_rgb(0_0_0/0.14)] ring-0 outline-none"
            collisionPadding={12}
            side="bottom"
            sideOffset={11}
            onMouseEnter={hover.cancelClose}
            onMouseLeave={hover.scheduleClose}
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
            <div className="flex h-10 shrink-0 items-center px-3">
              <PuddingWordmark />
            </div>
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
      onSelect={selectSearchResult}
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
      <aside className="pudding-session-rail-surface relative h-full w-[268px] shrink-0 border-r bg-sidebar text-sidebar-foreground">
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 z-0 bg-sidebar"
        />
        <div className="absolute inset-0 z-10 flex min-h-0 flex-col gap-(--rail-content-align-gap)">
          {/* 顶行是 --toolbar-h 工具条占位;窗口拖拽由 App 根部统一透明拖拽带承载 */}
          <div
            className="h-(--toolbar-h) shrink-0 transition-[padding] duration-200"
            style={{ paddingLeft: "var(--traffic-inset)" }}
          />
          <div className="flex h-10 shrink-0 items-center px-3">
            <PuddingWordmark />
          </div>
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

// hover 开合；关闭态点击会钉住打开，hover 已经打开时点击则立即关闭。
// 面板内一旦打开主题/语言等浮层，鼠标离开不再自动关闭。
// suppress:收起边栏的动作刚结束时鼠标恰好停在触发器原位,此时不应
// 立即 hover 弹出 — 压制到鼠标离开触发器一次后恢复。
function useHoverPopover(closeDelay = 160) {
  const [open, setOpen] = useState(false);
  const openRef = useRef(false);
  const closeTimer = useRef<number | null>(null);
  const closePausedRef = useRef(false);
  const pinnedRef = useRef(false);
  const ignoreOutsideUntilRef = useRef(0);
  const suppressRef = useRef(false);

  function cancelClose() {
    if (closeTimer.current) {
      window.clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }

  function updateOpen(next: boolean) {
    openRef.current = next;
    setOpen(next);
  }

  return {
    open,
    openNow() {
      if (suppressRef.current) {
        return;
      }
      cancelClose();
      updateOpen(true);
    },
    close() {
      cancelClose();
      pinnedRef.current = false;
      updateOpen(false);
    },
    toggle() {
      cancelClose();
      if (openRef.current) {
        pinnedRef.current = false;
        updateOpen(false);
        return;
      }
      pinnedRef.current = true;
      updateOpen(true);
    },
    scheduleClose() {
      suppressRef.current = false; // 鼠标离开过一次,恢复 hover 弹出
      if (closePausedRef.current || pinnedRef.current) {
        return;
      }
      cancelClose();
      closeTimer.current = window.setTimeout(() => updateOpen(false), closeDelay);
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
      if (!next) {
        pinnedRef.current = false;
      }
      updateOpen(next);
    },
  };
}
