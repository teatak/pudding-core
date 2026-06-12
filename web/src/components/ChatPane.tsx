import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

import { listSessions, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Composer } from "@/components/Composer";
import { Transcript } from "@/components/Transcript";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { useOverlayStore } from "@/state/overlayStore";
import { useRailCollapsed } from "@/state/railStore";

type ChatPaneProps = {
  token: string;
  selectedSessionID: string | undefined;
};

export function ChatPane({ token, selectedSessionID }: ChatPaneProps) {
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const railCollapsed = useRailCollapsed();
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const selectedSession = sessions.find((session) => session.id === selectedSessionID);
  const activeSessionID = selectedSession?.id;

  useEffect(() => {
    if (!sessionsQuery.isSuccess) {
      return;
    }
    if (!selectedSessionID && sessions[0]) {
      void navigate({ to: "/", search: { session: sessions[0].id } });
      return;
    }
    if (selectedSessionID && !selectedSession) {
      const nextSessionID = sessions[0]?.id;
      if (nextSessionID) {
        void navigate({ to: "/", search: { session: nextSessionID } });
      } else {
        void navigate({ to: "/", search: {}, replace: true });
      }
    }
  }, [navigate, selectedSession, selectedSessionID, sessions, sessionsQuery.isSuccess]);

  useSessionEvents(activeSessionID, token);

  return (
    <section className="flex h-full min-w-0 flex-1 flex-col overflow-hidden">
      <header
        className={cn(
          "flex h-12 shrink-0 items-center justify-between gap-3 px-5 transition-[padding] duration-200",
          railCollapsed && "pl-13",
        )}
      >
        <div className="truncate text-sm font-medium">
          {selectedSession?.title || t("session.noSelected")}
        </div>
        <HeaderStatus session={selectedSession} />
      </header>
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
