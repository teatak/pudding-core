import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ChevronDown, Loader2, Pencil, Plus, Trash2, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { createSession, deleteSession, listSessions, updateSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Conversation } from "@/components/Conversation";
import { Mascot } from "@/components/Mascot";
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
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import type { AppSearch } from "@/lib/route";
import { cn } from "@/lib/utils";
import { useOverlayStore } from "@/state/overlayStore";
import { useRailCollapsed } from "@/state/railStore";

type ChatPaneProps = {
  token: string;
  sessionID: string | undefined;
  // primary = 主 pane(承担会话自动跳转、rail 触发器让位);
  // split = 分屏 pane(会话失效时自动收屏,header 带关闭钮)
  role: "primary" | "split";
};

export function ChatPane({ token, sessionID, role }: ChatPaneProps) {
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const railCollapsed = useRailCollapsed();
  const clearSession = useOverlayStore((state) => state.clearSession);
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  // 全空库的欢迎空态用:与 rail 的新建语义一致
  const createMutation = useMutation({
    mutationFn: () => createSession(token, { title: "" }),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      await navigate({ to: "/", search: (prev) => ({ ...prev, session: created.id }) });
    },
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
  const isPrimary = role === "primary";
  const sessionsPending = sessionsQuery.isPending;
  const headerTitle = selectedSession
    ? selectedSession.title || t("session.untitled")
    : sessionsPending
      ? ""
      : t("session.noSelected");

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
    if (!sessionID && sessions[0]) {
      void navigate({ to: "/", search: (prev) => ({ ...(prev as AppSearch), session: sessions[0].id }) });
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
          } else {
            delete next.session;
          }
          return next;
        },
        replace: true,
      });
    }
  }, [isPrimary, navigate, selectedSession, sessionID, sessions, sessionsQuery.isSuccess]);

  useSessionEvents(selectedSession?.id, token);

  return (
    <section className="relative flex h-full min-h-0 min-w-0 flex-1 basis-0 flex-col overflow-hidden">
      <header
        className="flex h-(--toolbar-h) min-w-0 shrink-0 items-center justify-between gap-3 overflow-hidden px-5"
        // 折叠态给悬浮触发器让位;壳模式下触发器随红绿灯右移,让位同步加宽
        style={
          isPrimary && railCollapsed
            ? { paddingLeft: "calc(var(--traffic-inset) + 3.25rem)" }
            : undefined
        }
      >
        <div className="flex h-8 min-w-0 max-w-(--chat-title-max-w) flex-1 items-center overflow-visible text-sm font-normal">
          {selectedSession ? (
            <HeaderSessionTitle
              key={selectedSession.id}
              deletePending={deleteMutation.isPending}
              renamePending={renameMutation.isPending}
              session={selectedSession}
              onDelete={() => deleteMutation.mutate(selectedSession.id)}
              onRename={(title) => renameMutation.mutate({ id: selectedSession.id, title })}
            />
          ) : (
            <span className="flex h-8 min-w-0 max-w-full items-center truncate text-sm font-normal leading-6">
              {headerTitle}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <HeaderStatus session={selectedSession} />
          {!isPrimary ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  aria-label={t("pane.closeSplit")}
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
      {selectedSession ? (
        <Conversation token={token} session={selectedSession} />
      ) : sessionsPending ? (
        <LoadingState />
      ) : (
        // 欢迎空态(全空库 / 无选中):mascot + 一句话 + 主操作
        <div className="flex flex-1 flex-col items-center justify-center gap-4 text-center">
          <Mascot className="w-20" />
          <div className="text-sm text-muted-foreground">{t("session.selectOrCreate")}</div>
          <Button disabled={createMutation.isPending} type="button" onClick={() => createMutation.mutate()}>
            {createMutation.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
            {t("session.startFirst")}
          </Button>
        </div>
      )}
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
      <div
        className={cn(
          "relative grid h-8 min-w-0 max-w-full items-center",
          editing ? "shrink" : "overflow-hidden",
        )}
        style={editing && editWidth ? { width: `min(${editWidth}px, 100%)` } : undefined}
      >
        {editing ? (
          <>
            <span
              ref={measureRef}
              aria-hidden="true"
              className="pointer-events-none absolute top-0 left-0 h-8 whitespace-pre text-sm font-normal leading-8 opacity-0"
            >
              {draft || t("session.untitled")}
            </span>
            <span
              aria-hidden="true"
              className="pointer-events-none absolute -inset-x-1.5 top-1/2 h-7 -translate-y-1/2 rounded-md border border-input bg-background/90 shadow-xs"
            />
            <Input
              ref={inputRef}
              aria-label={t("session.rename")}
              className="relative z-10 col-start-1 row-start-1 h-8 min-w-0 cursor-text rounded-none border-0 bg-transparent px-0 py-0 text-sm font-normal leading-6 shadow-none ring-0 caret-foreground focus-visible:border-transparent focus-visible:ring-0 md:text-sm dark:bg-transparent"
              disabled={renamePending}
              placeholder={t("session.untitled")}
              value={draft}
              onBlur={save}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
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
          <span
            className="col-start-1 row-start-1 block h-8 w-full min-w-0 cursor-default truncate text-sm font-normal leading-8 select-none"
            title={rawTitle || undefined}
            onDoubleClick={startEditing}
          >
            {displayTitle}
          </span>
        )}
      </div>
      {editing ? null : (
        <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                aria-label={t("session.actions")}
                className="size-7 text-muted-foreground hover:text-foreground"
                size="icon-sm"
                variant="ghost"
              >
                <ChevronDown className="size-4" />
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
                <Pencil />
                {t("session.rename")}
              </DropdownMenuItem>
              <DropdownMenuItem
                disabled={deletePending}
                variant="destructive"
                onSelect={() => setDeleteOpen(true)}
              >
                <Trash2 />
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
      <Loader2 className="size-5 animate-spin" aria-label={t("common.loading")} />
    </div>
  );
}

// header 单行状态区(docs/design.md 第 5 节):running 双源取或
// (sessions 快照兜底 + overlay 实时);estimatedSteps/currentStep 协议
// 落地后在此渲染细进度条,字段缺省时只显示状态点 + 文案。
function HeaderStatus({ session }: { session: Session | undefined }) {
  const { t } = useI18n();
  const liveRunning = useOverlayStore((state) =>
    session ? Boolean(state.runningTurns[session.id]) : false,
  );
  if (!session) {
    return null;
  }
  const running = session.running || liveRunning;
  if (!running) {
    return null;
  }
  return (
    <div className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
      <span className="size-2 animate-pulse rounded-full bg-primary" />
      {t("session.generating")}
    </div>
  );
}
