import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { X } from "lucide-react";
import { useEffect } from "react";

import { listSessions, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Composer } from "@/components/Composer";
import { Transcript } from "@/components/Transcript";
import { Button } from "@/components/ui/button";
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
  const { t } = useI18n();
  const railCollapsed = useRailCollapsed();
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const selectedSession = sessions.find((session) => session.id === sessionID);
  const isPrimary = role === "primary";

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
    <section className="relative flex min-h-0 flex-1 basis-0 flex-col overflow-hidden">
      <header
        className={cn(
          "flex h-(--toolbar-h) shrink-0 items-center justify-between gap-3 px-5",
          isPrimary && "drag-region",
        )}
        // 折叠态给悬浮触发器让位;壳模式下触发器随红绿灯右移,让位同步加宽
        style={
          isPrimary && railCollapsed
            ? { paddingLeft: "calc(var(--traffic-inset) + 3.25rem)" }
            : undefined
        }
      >
        <div className="truncate text-sm font-medium">
          {selectedSession?.title || t("session.noSelected")}
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
      {/* 标题栏与会话区的渐变衔接,与 composer 上沿同款 */}
      <div className="pointer-events-none absolute inset-x-0 top-(--toolbar-h) z-10 h-6 bg-gradient-to-b from-background to-transparent" />
      {selectedSession ? (
        <>
          <Transcript token={token} sessionID={selectedSession.id} />
          <Composer token={token} session={selectedSession} />
        </>
      ) : (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {t("session.selectOrCreate")}
        </div>
      )}
    </section>
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
