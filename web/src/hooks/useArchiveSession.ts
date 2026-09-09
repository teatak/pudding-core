import { useIsMutating, useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";

import { archiveSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { routeAfterSessionArchive, type AppSearch } from "@/lib/route";
import { useOverlayStore } from "@/state/overlayStore";

const archiveMutationKey = ["sessions", "archive"] as const;

export function useIsSessionArchiving(sessionID: string | undefined) {
  return useIsMutating({
    mutationKey: archiveMutationKey,
    predicate: (mutation) => mutation.state.variables === sessionID,
  }) > 0;
}

export function useArchiveSession(token: string) {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const clearSession = useOverlayStore((state) => state.clearSession);

  return useMutation({
    mutationKey: archiveMutationKey,
    mutationFn: (sessionID: string) => archiveSession(token, sessionID),
    onSuccess: async (archived, sessionID) => {
      await navigate({
        to: "/",
        search: (prev) => routeAfterSessionArchive(prev as AppSearch, archived),
        replace: true,
      });
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
        previous ? { sessions: previous.sessions.filter((session) => session.id !== sessionID) } : previous,
      );
      clearSession(sessionID);
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: () => toast.error(t("session.archiveFailed")),
  });
}
