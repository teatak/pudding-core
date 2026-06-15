import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Loader2, Plus, X } from "lucide-react";
import { useEffect } from "react";

import { createSession, listSessions, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Conversation } from "@/components/Conversation";
import { Mascot } from "@/components/Mascot";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import type { AppSearch } from "@/lib/route";
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
        <div
          className="min-w-0 max-w-(--chat-title-max-w) flex-1 overflow-hidden truncate text-sm font-medium"
          title={selectedSession?.title || undefined}
        >
          {headerTitle}
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
