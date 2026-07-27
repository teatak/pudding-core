import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Ellipsis, Trash, X } from "@/components/icons";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";

import {
  deleteSession,
  listSessions,
  updateSession,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
} from "@/components/AppMenu";
import { Conversation } from "@/components/Conversation";
import { DraftConversation } from "@/components/DraftConversation";
import { PhaseDot } from "@/components/PhaseDot";
import { SessionAppsControl } from "@/components/SessionAppsControl";
import { SessionModeIcon } from "@/components/SessionModeIcon";
import { Spinner } from "@/components/Spinner";
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
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import type { AppSearch } from "@/lib/route";
import { cn } from "@/lib/utils";
import { isTurnPhaseActive, useOverlayStore } from "@/state/overlayStore";
import { useRailCollapsed } from "@/state/railStore";
import {
  clearWorkspaceActivity,
  useWorkspaceActivities,
} from "@/state/workspaceActivityStore";
import { useWorkspaceOpen } from "@/state/workspaceStore";

type ChatPaneProps = {
  token: string;
  sessionID: string | undefined;
  draftActive?: boolean;
  draftProjectID?: string;
  headerActions?: ReactNode;
  headerDragHandle?: boolean;
  reserveTopLeftInset?: boolean;
  reserveTopRightAction?: boolean;
  // primary = 主 pane(承担会话自动跳转、rail 触发器让位);
  // split = 分屏 pane(会话失效时自动收屏,header 带关闭钮)
  role: "primary" | "split";
};

export function ChatPane({
  token,
  sessionID,
  draftActive = false,
  draftProjectID,
  headerActions,
  headerDragHandle = false,
  reserveTopLeftInset = true,
  reserveTopRightAction = false,
  role,
}: ChatPaneProps) {
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const railCollapsed = useRailCollapsed();
  const workspaceOpen = useWorkspaceOpen();
  const clearSession = useOverlayStore((state) => state.clearSession);
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
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteSession(token, id),
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
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const selectedSession = sessions.find((session) => session.id === sessionID);
  const workspaceActivities = useWorkspaceActivities(selectedSession?.id);
  const isPrimary = role === "primary";
  const showDraft = isPrimary && !sessionID;
  const sessionsPending = sessionsQuery.isPending;
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
    ...(reserveTopRightAction
      ? {
          paddingRight:
            "calc(var(--toolbar-edge-inset) + var(--toolbar-icon-button-size) + 0.5rem)",
        }
      : {}),
  };

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
    <section className="relative flex h-full min-h-0 min-w-0 flex-1 basis-0 flex-col overflow-hidden">
      <header
        className={cn(
          "pudding-chat-pane-header flex h-(--toolbar-h) min-w-0 shrink-0 items-center justify-between gap-3 overflow-hidden px-(--toolbar-edge-inset)",
          headerDragHandle && "pudding-agent-console-drag-handle cursor-grab active:cursor-grabbing",
        )}
        // 折叠态给悬浮触发器让位;壳模式下触发器随红绿灯右移,让位同步加宽
        style={Object.keys(headerStyle).length ? headerStyle : undefined}
      >
        <div className="flex h-8 min-w-0 max-w-(--chat-title-max-w) flex-1 items-center overflow-visible text-sm font-normal">
          {selectedSession ? (
            <>
              <HeaderSessionTitle
                key={selectedSession.id}
                deletePending={deleteMutation.isPending}
                renamePending={renameMutation.isPending}
                session={selectedSession}
                onDelete={() => deleteMutation.mutate(selectedSession.id)}
                onRename={(title) => renameMutation.mutate({ id: selectedSession.id, title })}
              />
              <HeaderStatus session={selectedSession} />
            </>
          ) : (
            <span className="inline-flex h-7 min-w-0 max-w-full items-center truncate rounded-md border border-transparent px-2 text-sm font-normal leading-6">
              {headerTitle}
            </span>
          )}
        </div>
        <div className="no-drag-region relative z-30 flex shrink-0 items-center gap-2">
          {selectedSession ? <SessionAppsControl session={selectedSession} token={token} /> : null}
          {isPrimary ? headerActions : null}
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
      </header>
      <div className="flex min-h-0 flex-1 flex-col">
        {showDraft ? (
          <DraftConversation token={token} projectID={draftProjectID} />
        ) : selectedSession ? (
          <Conversation token={token} session={selectedSession} />
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
  renamePending,
  deletePending,
  onRename,
  onDelete,
}: {
  session: Session;
  renamePending: boolean;
  deletePending: boolean;
  onRename: (title: string) => void;
  onDelete: () => void;
}) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const [localTitle, setLocalTitle] = useState<string | null>(null);
  const [editWidth, setEditWidth] = useState<number | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
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
      <span
        aria-label={t(`mode.${session.activeMode}`)}
        className="pudding-chrome-icon no-drag-region pointer-events-auto flex h-(--toolbar-icon-button-size) w-(--toolbar-icon-button-size) shrink-0 items-center justify-center text-foreground/80!"
        role="img"
      >
        <SessionModeIcon mode={session.activeMode} />
      </span>
      <div
        className={cn(
          "relative grid h-7 min-w-0 max-w-full items-center",
          editing ? "shrink" : "overflow-hidden",
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
            className="col-start-1 row-start-1 inline-flex h-7 w-full min-w-0 cursor-default items-center rounded-md border border-transparent px-0 text-left font-medium leading-6 select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"

            onDoubleClick={startEditing}
          >
            <span className="min-w-0 truncate">{displayTitle}</span>
          </button>
        )}
      </div>
      {editing ? null : (
        <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
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
              className="w-36"
              onCloseAutoFocus={(event) => {
                if (!editAfterMenuCloseRef.current) {
                  return;
                }
                event.preventDefault();
                editAfterMenuCloseRef.current = false;
                startEditing();
              }}
            >
              <DropdownMenuItem onSelect={() => {
                editAfterMenuCloseRef.current = true;
              }}>
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
      )}
    </div>
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

// header 单行状态点(docs/design.md 第 5 节):running 双源取或
// (sessions 快照兜底 + overlay 实时);estimatedSteps/currentStep 协议
// 落地后在此渲染细进度条,字段缺省时只显示状态点。
function HeaderStatus({ session }: { session: Session | undefined }) {
  const liveRunning = useOverlayStore((state) =>
    session ? Boolean(state.runningTurns[session.id]) : false,
  );
  const livePhase = useOverlayStore((state) => (session ? state.turnPhases[session.id] : undefined));
  if (!session) {
    return null;
  }
  const running = session.running || liveRunning || isTurnPhaseActive(livePhase);
  if (!running) {
    return null;
  }
  return (
    <div className="flex size-4 shrink-0 items-center justify-center" aria-hidden="true">
      <PhaseDot phase={livePhase?.phase} size="sm" />
    </div>
  );
}
