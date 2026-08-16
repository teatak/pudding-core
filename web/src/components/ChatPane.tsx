import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Archive, Ellipsis, FolderClosed, X } from "@/components/icons";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  archiveSession,
  listProjects,
  listSessions,
  updateSession,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  activateConversationFindRegion,
  closeFind,
  openActiveBrowserPageFind,
  openFind,
} from "@/browser/pageFindTarget";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
} from "@/components/AppMenu";
import { Conversation } from "@/components/Conversation";
import { DraftConversation } from "@/components/DraftConversation";
import { SessionAppsControl } from "@/components/SessionAppsControl";
import { SessionModeIcon } from "@/components/SessionModeIcon";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { onDesktopMenuCommand } from "@/lib/desktopBridge";
import type { AppSearch } from "@/lib/route";
import { cn } from "@/lib/utils";
import { useOverlayStore } from "@/state/overlayStore";
import { useRailCollapsed } from "@/state/railStore";
import {
  clearWorkspaceActivity,
  useWorkspaceActivities,
} from "@/state/workspaceActivityStore";
import { useWorkspaceOpen } from "@/state/workspaceStore";

type ChatPaneRole = "primary" | "split";

type ChatPaneProps = {
  token: string;
  sessionID: string | undefined;
  draftActive?: boolean;
  draftProjectID?: string;
  presentation?: "default" | "floating";
  reserveTopLeftInset?: boolean;
  reserveTopRightActions?: 0 | 1 | 2;
  // primary = 主 pane(承担会话自动跳转、rail 触发器让位);
  // split = 分屏 pane(会话失效时自动收屏,header 带关闭钮)
  role: ChatPaneRole;
};

let activeChatPaneRole: ChatPaneRole = "primary";

export function ChatPane({
  token,
  sessionID,
  draftActive = false,
  draftProjectID,
  presentation = "default",
  reserveTopLeftInset = true,
  reserveTopRightActions = 0,
  role,
}: ChatPaneProps) {
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const railCollapsed = useRailCollapsed();
  const workspaceOpen = useWorkspaceOpen();
  const floating = presentation === "floating";
  const clearSession = useOverlayStore((state) => state.clearSession);
  const [conversationSearchOpen, setConversationSearchOpen] = useState(false);
  const [conversationSearchFocusSignal, setConversationSearchFocusSignal] = useState(0);
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  const renameMutation = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) => updateSession(token, id, { title }),
    onMutate: async ({ id, title }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.sessions() });
      const previous = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      if (previous) {
        queryClient.setQueryData(queryKeys.sessions(), {
          sessions: previous.sessions.map((session) => (session.id === id ? { ...session, title } : session)),
        });
      }
      return { previous };
    },
    onError: (_err, _vars, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.sessions(), context.previous);
      }
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
  });
  const archiveMutation = useMutation({
    mutationFn: (id: string) => archiveSession(token, id),
    onSuccess: async (_, deletedSessionID) => {
      const previous = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      const remaining = previous?.sessions.filter((session) => session.id !== deletedSessionID) || [];
      if (previous) {
        queryClient.setQueryData(queryKeys.sessions(), { sessions: remaining });
      }
      clearSession(deletedSessionID);
      await navigate({
        to: "/",
        search: (prev) => {
          const next = { ...(prev as AppSearch) };
          if (next.split === deletedSessionID) {
            delete next.split;
          }
          if (next.session === deletedSessionID) {
            const fallback = remaining.find((session) => session.id !== next.split)?.id || remaining[0]?.id;
            if (fallback) {
              next.session = fallback;
              delete next.project;
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
    onError: () => toast.error(t("session.archiveFailed")),
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const selectedSession = sessions.find((session) => session.id === sessionID);
  const workspaceActivities = useWorkspaceActivities(selectedSession?.id);
  const isPrimary = role === "primary";
  const showDraft = isPrimary && !sessionID;
  const sessionsPending = sessionsQuery.isPending;
  const headerProjectID = showDraft ? draftProjectID : selectedSession?.projectID;
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects(),
    queryFn: () => listProjects(token),
    enabled: Boolean(token && headerProjectID),
  });
  const headerProjectName = projectsQuery.data?.projects.find(
    (project) => project.id === headerProjectID,
  )?.name;
  const headerTitle = showDraft
    ? t("session.create")
    : selectedSession
    ? selectedSession.title || t("session.untitled")
    : sessionsPending
      ? ""
      : t("session.noSelected");
  const headerStyle = {
    ...(isPrimary && railCollapsed && reserveTopLeftInset
      ? { paddingLeft: "calc(var(--traffic-inset) + var(--rail-toggle-left) + var(--toolbar-icon-button-size) + var(--rail-title-gap))" }
      : {}),
    ...(reserveTopRightActions > 0
      ? {
          paddingRight:
            reserveTopRightActions === 2
              ? "calc(var(--toolbar-edge-inset) + var(--toolbar-icon-button-size) + var(--toolbar-icon-button-size) + 1rem)"
              : "calc(var(--toolbar-edge-inset) + var(--toolbar-icon-button-size) + 0.5rem)",
        }
      : {}),
  };
  const conversationFindTarget = useMemo(() => ({
    id: `conversation:${role}:${selectedSession?.id || "empty"}`,
    open: () => {
      setConversationSearchOpen(true);
      setConversationSearchFocusSignal((signal) => signal + 1);
    },
    close: () => setConversationSearchOpen(false),
  }), [role, selectedSession?.id]);
  const openConversationSearch = useCallback(() => {
    if (!selectedSession) {
      return;
    }
    activeChatPaneRole = role;
    openFind(conversationFindTarget);
  }, [conversationFindTarget, role, selectedSession]);
  const changeConversationSearchOpen = useCallback((open: boolean) => {
    if (open) {
      openConversationSearch();
      return;
    }
    closeFind(conversationFindTarget.id);
  }, [conversationFindTarget.id, openConversationSearch]);

  const openProjects = useCallback(() => {
    void navigate({
      to: "/",
      search: (previous) => {
        const next = { ...(previous as AppSearch), view: "projects" as const };
        delete next.session;
        delete next.split;
        delete next.draft;
        delete next.project;
        return next;
      },
    });
  }, [navigate]);

  useEffect(
    () => () => closeFind(conversationFindTarget.id),
    [conversationFindTarget.id],
  );

  useEffect(
    () => () => {
      if (activeChatPaneRole === role) {
        activeChatPaneRole = "primary";
      }
    },
    [role],
  );

  useEffect(() => {
    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      if (
        activeChatPaneRole !== role ||
        floating ||
        !selectedSession ||
        event.altKey ||
        (!event.metaKey && !event.ctrlKey) ||
        event.key.toLowerCase() !== "f"
      ) {
        return;
      }
      event.preventDefault();
      if (openActiveBrowserPageFind()) {
        return;
      }
      openConversationSearch();
    };
    window.addEventListener("keydown", handleKeyDown);
    const unsubscribeMenu = onDesktopMenuCommand((command) => {
      if (command === "search-conversation" && activeChatPaneRole === role) {
        if (openActiveBrowserPageFind()) {
          return;
        }
        openConversationSearch();
      }
    });
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      unsubscribeMenu();
    };
  }, [floating, openConversationSearch, role, selectedSession]);

  useEffect(() => {
    if (workspaceOpen && selectedSession && workspaceActivities.length > 0) {
      clearWorkspaceActivity(selectedSession.id);
    }
  }, [selectedSession, workspaceActivities, workspaceOpen]);

  useEffect(() => {
    if (!sessionsQuery.isSuccess) {
      return;
    }
    if (!isPrimary) {
      // 分屏会话不存在(被删/失效):收掉分屏,主 pane 不受影响
      if (sessionID && !selectedSession) {
        void navigate({
          to: "/",
          search: (prev) => {
            const { split: _split, ...rest } = prev as AppSearch;
            return rest;
          },
          replace: true,
        });
      }
      return;
    }
    if (!sessionID) {
      if (!draftActive) {
        void navigate({
          to: "/",
          search: (prev) => {
            const next = { ...(prev as AppSearch), draft: "1" };
            delete next.session;
            return next;
          },
          replace: true,
        });
      }
      return;
    }
    if (sessionID && !selectedSession) {
      const nextSessionID = sessions[0]?.id;
      void navigate({
        to: "/",
        search: (prev) => {
          const next = { ...(prev as AppSearch) };
          if (nextSessionID) {
            next.session = nextSessionID;
            delete next.draft;
            delete next.project;
          } else {
            delete next.session;
            next.draft = "1";
          }
          return next;
        },
        replace: true,
      });
    }
  }, [draftActive, isPrimary, navigate, selectedSession, sessionID, sessions, sessionsQuery.isSuccess]);

  return (
    <section
      className="relative flex h-full min-h-0 min-w-0 flex-1 basis-0 flex-col overflow-hidden"
      data-chat-pane-role={role}
      onFocusCapture={() => {
        activeChatPaneRole = role;
        activateConversationFindRegion();
      }}
      onPointerDownCapture={() => {
        activeChatPaneRole = role;
        activateConversationFindRegion();
      }}
    >
      {floating ? null : <header
        className="pudding-chat-pane-header flex h-(--toolbar-h) min-w-0 shrink-0 items-center justify-between gap-3 overflow-hidden px-(--toolbar-edge-inset)"
        // 折叠态给悬浮触发器让位;壳模式下触发器随红绿灯右移,让位同步加宽
        style={Object.keys(headerStyle).length ? headerStyle : undefined}
      >
        <div className="flex h-8 min-w-0 max-w-(--chat-title-max-w) flex-1 items-center overflow-visible text-sm font-normal">
          {selectedSession ? (
            <HeaderSessionTitle
              key={selectedSession.id}
              archivePending={archiveMutation.isPending}
              projectName={headerProjectName}
              renamePending={renameMutation.isPending}
              session={selectedSession}
              onArchive={() => archiveMutation.mutate(selectedSession.id)}
              onOpenProject={openProjects}
              onRename={(title) => renameMutation.mutate({ id: selectedSession.id, title })}
              onSearch={openConversationSearch}
            />
          ) : (
            <div className="no-drag-region relative z-30 inline-flex h-8 min-w-0 max-w-full items-center gap-1 overflow-visible text-sm font-normal">
              {headerProjectName ? (
                <>
                  <HeaderLeadingIcon>
                    <FolderClosed aria-hidden="true" />
                  </HeaderLeadingIcon>
                  <HeaderProjectLink name={headerProjectName} onClick={openProjects} />
                </>
              ) : null}
              <span className="inline-flex h-7 min-w-0 items-center truncate rounded-md border border-transparent px-2 leading-6">
                {headerTitle}
              </span>
            </div>
          )}
        </div>
        <div className="no-drag-region relative z-30 flex shrink-0 items-center gap-2">
          {selectedSession ? <SessionAppsControl session={selectedSession} token={token} /> : null}
          {!isPrimary ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  aria-label={t("pane.closeSplit")}
                  className="pudding-toolbar-icon-button"
                  size="icon-sm"
                  variant="ghost"
                  onClick={() =>
                    void navigate({
                      to: "/",
                      search: (prev) => {
                        const { split: _split, ...rest } = prev as AppSearch;
                        return rest;
                      },
                    })
                  }
                >
                  <X />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom">{t("pane.closeSplit")}</TooltipContent>
            </Tooltip>
          ) : null}
        </div>
      </header>}
      <div className="flex min-h-0 flex-1 flex-col">
        {showDraft ? (
          <DraftConversation token={token} projectID={draftProjectID} />
        ) : selectedSession ? (
          <Conversation
            searchFocusSignal={conversationSearchFocusSignal}
            searchOpen={conversationSearchOpen}
            searchSlot={role}
            session={selectedSession}
            token={token}
            presentation={presentation}
            onSearchOpenChange={changeConversationSearchOpen}
          />
        ) : sessionsPending ? (
          <LoadingState />
        ) : (
          <DraftConversation token={token} projectID={draftProjectID} />
        )}
      </div>
    </section>
  );
}

function HeaderSessionTitle({
  session,
  projectName,
  renamePending,
  archivePending,
  onRename,
  onArchive,
  onOpenProject,
  onSearch,
}: {
  session: Session;
  projectName?: string;
  renamePending: boolean;
  archivePending: boolean;
  onRename: (title: string) => void;
  onArchive: () => void;
  onOpenProject: () => void;
  onSearch: () => void;
}) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const [localTitle, setLocalTitle] = useState<string | null>(null);
  const [editWidth, setEditWidth] = useState<number | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const measureRef = useRef<HTMLSpanElement>(null);
  const editAfterMenuCloseRef = useRef(false);
  const rawTitle = localTitle ?? session.title;
  const displayTitle = rawTitle || t("session.untitled");

  useEffect(() => {
    if (!editing) {
      setDraft(rawTitle);
    }
  }, [editing, rawTitle]);

  useEffect(() => {
    if (localTitle !== null && session.title === localTitle) {
      setLocalTitle(null);
    }
  }, [localTitle, session.title]);

  useEffect(() => {
    if (!editing) {
      return;
    }
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [editing]);

  useLayoutEffect(() => {
    if (!editing) {
      setEditWidth(null);
      return;
    }
    const measured = measureRef.current?.getBoundingClientRect().width || 0;
    setEditWidth(Math.ceil(Math.max(measured + 2, 72)));
  }, [draft, displayTitle, editing]);

  function startEditing() {
    setDraft(rawTitle);
    setEditing(true);
  }

  function save() {
    const nextTitle = draft.trim();
    if (!nextTitle) {
      cancel();
      return;
    }
    if (nextTitle !== rawTitle) {
      setLocalTitle(nextTitle);
      onRename(nextTitle);
    }
    setEditing(false);
  }

  function cancel() {
    setDraft(rawTitle);
    setEditing(false);
  }

  return (
    <div
      className={cn(
        "no-drag-region relative z-30 h-8 min-w-0 max-w-full items-center gap-1 overflow-visible",
        editing ? "flex w-full" : "inline-flex",
      )}
    >
      <HeaderLeadingIcon label={projectName ? undefined : t(`mode.${session.activeMode}`)}>
        {projectName ? (
          <FolderClosed aria-hidden="true" />
        ) : (
          <SessionModeIcon mode={session.activeMode} />
        )}
      </HeaderLeadingIcon>
      {projectName ? (
        <HeaderProjectLink name={projectName} onClick={onOpenProject} />
      ) : null}
      <div
        className={cn(
          "relative grid h-7 min-w-0 max-w-full items-center",
          editing ? cn("shrink", !projectName && "-ml-2") : "overflow-visible",
        )}
        style={editing && editWidth ? { width: `min(${editWidth}px, 100%)` } : undefined}
      >
        {editing ? (
          <>
            <span
              ref={measureRef}
              aria-hidden="true"
              className="pointer-events-none absolute top-0 left-0 h-7 whitespace-pre rounded-md border border-transparent px-2 font-normal leading-6 opacity-0"
            >
              {draft || t("session.untitled")}
            </span>
            <Input
              ref={inputRef}
              aria-label={t("session.rename")}
              className="pudding-title-edit-input mt-[-1px] relative z-10 col-start-1 row-start-1 h-7 min-w-0 appearance-none rounded-md border-input px-2 py-0 font-normal leading-6 shadow-xs ring-0 caret-foreground focus-visible:ring-0"
              disabled={renamePending}
              placeholder={t("session.untitled")}
              value={draft}
              onBlur={save}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  event.currentTarget.blur();
                }
                if (event.key === "Escape") {
                  event.preventDefault();
                  cancel();
                }
              }}
            />
          </>
        ) : (
          <button
            type="button"
            aria-label={t("session.rename")}
            className={cn(
              "col-start-1 row-start-1 inline-flex h-7 min-w-0 cursor-default items-center rounded-md border border-transparent px-2 text-left font-medium leading-6 select-none hover:bg-accent focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none",
              projectName ? "w-full" : "-ml-2 w-[calc(100%+1rem)]",
            )}

            onDoubleClick={startEditing}
          >
            <span className="min-w-0 truncate">{displayTitle}</span>
          </button>
        )}
      </div>
      {editing ? null : (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              aria-label={t("session.actions")}
              className="pudding-toolbar-icon-button pudding-chat-title-action"
              size="icon-sm"
              tabIndex={-1}
              variant="ghost"
            >
              <Ellipsis />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="start"
            className="w-max min-w-48 max-w-[var(--radix-dropdown-menu-content-available-width)]"
            onCloseAutoFocus={(event) => {
              if (!editAfterMenuCloseRef.current) {
                return;
              }
              event.preventDefault();
              editAfterMenuCloseRef.current = false;
              startEditing();
            }}
          >
            <DropdownMenuItem onSelect={onSearch}>
              {t("conversationSearch.open")}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => {
              editAfterMenuCloseRef.current = true;
            }}>
              {t("session.rename")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem disabled={archivePending} onSelect={onArchive}>
              <Archive />
              {t("session.archive")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}

function HeaderProjectLink({ name, onClick }: { name: string; onClick: () => void }) {
  return (
    <span className="flex min-w-0 max-w-56 shrink items-center gap-1 font-medium">
      <button
        className="no-drag-region pointer-events-auto -ml-2 h-(--toolbar-icon-button-size) min-w-0 max-w-full truncate rounded-md pr-2 pl-2 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        type="button"
        onClick={onClick}
      >
        {name}
      </button>
      <span className="shrink-0 text-muted-foreground">/</span>
    </span>
  );
}

function HeaderLeadingIcon({ children, label }: { children: React.ReactNode; label?: string }) {
  return (
    <span
      aria-label={label}
      className="pudding-chrome-icon no-drag-region pointer-events-auto flex h-(--toolbar-icon-button-size) w-(--toolbar-icon-button-size) shrink-0 items-center justify-center text-foreground/80!"
      role={label ? "img" : undefined}
    >
      {children}
    </span>
  );
}

function LoadingState() {
  const { t } = useI18n();
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground">
      <Spinner className="size-5" aria-label={t("common.loading")} />
    </div>
  );
}
