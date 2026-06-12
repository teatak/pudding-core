import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

import { listSessions } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Composer } from "@/components/Composer";
import { Transcript } from "@/components/Transcript";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";

type ChatPaneProps = {
  token: string;
  selectedSessionID: string | undefined;
};

export function ChatPane({ token, selectedSessionID }: ChatPaneProps) {
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
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
      <header className="flex h-12 shrink-0 items-center px-5">
        <div className="truncate text-sm font-medium">
          {selectedSession?.title || t("session.noSelected")}
        </div>
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
