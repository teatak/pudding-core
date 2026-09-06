import { useNavigate } from "@tanstack/react-router";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { clearLibraryRecent, deleteLibraryRecent, listLibraryRecent, openLibraryRecent } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type { LibraryRecentEntry } from "@/contracts/api";
import { AppDropdownMenuItem } from "@/components/AppMenu";
import { Trash2 } from "@/components/icons";
import { useI18n } from "@/i18n";
import type { AppSearch } from "@/lib/route";
import { openCanvasReveal } from "@/state/canvasRevealStore";
import { requestProjectFileReveal } from "@/state/projectRevealStore";
import { LibraryResourceIcon, LibraryResourceRow } from "./LibraryResourceRow";
import { libraryDescription } from "./libraryPresentation";

export function useLibraryHistory(active: boolean, token: string, sessionID: string, query: string, kind: string) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const history = useQuery({
    queryKey: queryKeys.libraryRecent(sessionID, query, kind),
    queryFn: async ({ signal }) => ({ ...await listLibraryRecent(token, sessionID, query, kind, signal), query, kind }),
    enabled: active && Boolean(token && sessionID),
    placeholderData: keepPreviousData,
    staleTime: 0,
  });
  const refresh = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ["library-recent"] }),
    queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() }),
  ]);
  const remove = useMutation({ mutationFn: (entry: LibraryRecentEntry) => deleteLibraryRecent(token, sessionID, entry.kind, entry.id), onSuccess: refresh, onError: () => toast.error(t("workspace.recentDeleteFailed")) });
  const clear = useMutation({ mutationFn: () => clearLibraryRecent(token, sessionID, kind), onSuccess: refresh, onError: () => toast.error(t("workspace.recentDeleteFailed")) });
  const open = useMutation({ mutationFn: (entry: LibraryRecentEntry) => openLibraryRecent(token, sessionID, entry.kind, entry.id), onSuccess: (target) => {
    if (target.kind === "file") requestProjectFileReveal(target);
    else openCanvasReveal(target.sessionID, target.itemID);
    if (target.sessionID !== sessionID) void navigate({ to: "/", search: (previous) => {
      const next = { ...(previous as AppSearch), session: target.sessionID };
      delete next.split; delete next.draft; delete next.view; delete next.project;
      return next;
    } });
  }, onError: () => { toast.error(t("workspace.sourceUnavailable")); void refresh(); } });
  return { history, remove, clear, open };
}

export function LibraryHistoryRows({ entries, actions, onOpenBrowserURL }: {
  entries: LibraryRecentEntry[];
  actions: ReturnType<typeof useLibraryHistory>;
  onOpenBrowserURL: (url: string) => void;
}) {
  const { t } = useI18n();
  return entries.map(entry => {
    const display = libraryDescription(entry, t);
    return <LibraryResourceRow key={`${entry.kind}:${entry.id}`} {...display} icon={<LibraryResourceIcon entry={entry} />} date={entry.openedAt} dateLabel={t("workspace.resourceOpenedAt")} available={entry.available} disabled={actions.open.isPending}
      onOpen={() => { if (entry.kind === "web" && entry.url) onOpenBrowserURL(entry.url); else actions.open.mutate(entry); }} details={[
        { label: t("workspace.recentType"), value: display.kind },
        { label: t("workspace.resourceSource"), value: display.source },
        { label: t("workspace.resourceLocation"), value: display.location },
      ]} actions={<AppDropdownMenuItem disabled={actions.remove.isPending} onSelect={() => actions.remove.mutate(entry)}><Trash2 />{t("workspace.recentRemove")}</AppDropdownMenuItem>} />;
  });
}
