import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, Ellipsis, Search, Trash } from "@/components/icons";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import {
  deleteSession,
  listArchivedSessions,
  listProjects,
  restoreSession,
  type Project,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ConfirmationDialog";
import { Spinner } from "@/components/Spinner";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
} from "@/components/AppMenu";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { useSessionMessageSearch } from "@/hooks/useSessionMessageSearch";
import { useI18n } from "@/i18n";
import { buildSessionSearchResults, sessionSearchExcerpt, sessionSearchTerms } from "@/lib/sessionSearch";
import { cn } from "@/lib/utils";

import { SETTINGS_CARD_CLASS, SETTINGS_NARROW_CONTENT_CLASS } from "./shared";

type ArchiveGroup = {
  id: string;
  label: string;
  sessions: Session[];
  allSessionIDs: string[];
};

type ClearTarget = {
  id: string;
  kind: "all" | "group";
  label: string;
  sessionIDs: string[];
};

export function ArchivedSessionsSettings({ token }: { token: string }) {
  const { t, locale } = useI18n();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<Session | null>(null);
  const [clearTarget, setClearTarget] = useState<ClearTarget | null>(null);
  const sessionsQuery = useQuery({
    queryKey: queryKeys.archivedSessions(),
    queryFn: () => listArchivedSessions(token),
    enabled: Boolean(token),
  });
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects(),
    queryFn: () => listProjects(token),
    enabled: Boolean(token),
  });
  const projects = projectsQuery.data?.projects || [];
  const sessions = sessionsQuery.data?.sessions || [];
  const messageSearch = useSessionMessageSearch({ active: true, query, sessions, token });
  const matchingSessionIDs = useMemo(() => {
    if (!messageSearch.normalizedQuery) {
      return null;
    }
    return new Set(
      buildSessionSearchResults(sessions, projects, messageSearch.messages, messageSearch.normalizedQuery)
        .map((result) => result.session.id),
    );
  }, [messageSearch.messages, messageSearch.normalizedQuery, projects, sessions]);
  const messagesBySessionID = useMemo(
    () => new Map(messageSearch.messages.map((message) => [message.sessionID, message])),
    [messageSearch.messages],
  );
  const excerptTerms = useMemo(
    () => [
      messageSearch.normalizedQuery,
      ...sessionSearchTerms(messageSearch.normalizedQuery),
      ...(messageSearch.matchTerms || []),
    ].filter(Boolean),
    [messageSearch.matchTerms, messageSearch.normalizedQuery],
  );
  const groups = useMemo(
    () => groupArchivedSessions(sessions, projects, matchingSessionIDs, t("archivedSessions.noProject")),
    [matchingSessionIDs, projects, sessions, t],
  );

  const refreshLists = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.archivedSessions() }),
      queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
      queryClient.invalidateQueries({ queryKey: queryKeys.projects() }),
    ]);
  };

  const restoreMutation = useMutation({
    mutationFn: (sessionID: string) => restoreSession(token, sessionID),
    onSuccess: refreshLists,
    onError: () => toast.error(t("archivedSessions.restoreFailed")),
  });
  const deleteMutation = useMutation({
    mutationFn: (sessionID: string) => deleteSession(token, sessionID),
    onSuccess: async () => {
      setDeleteTarget(null);
      await refreshLists();
    },
    onError: () => toast.error(t("archivedSessions.deleteFailed")),
  });
  const clearMutation = useMutation({
    mutationFn: async (target: ClearTarget) => {
      let failed = false;
      for (const sessionID of target.sessionIDs) {
        try {
          await deleteSession(token, sessionID);
        } catch {
          failed = true;
        }
      }
      if (failed) {
        throw new Error("some archived sessions could not be deleted");
      }
    },
    onSuccess: async () => {
      setClearTarget(null);
      await refreshLists();
    },
    onError: async () => {
      toast.error(t("archivedSessions.clearFailed"));
      await refreshLists();
    },
  });
  const busy = restoreMutation.isPending || deleteMutation.isPending || clearMutation.isPending;

  return (
    <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-4")}>
      <div className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pr-9 pl-9"
            placeholder={t("archivedSessions.searchPlaceholder")}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {messageSearch.waiting ? (
            <Spinner className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-muted-foreground" />
          ) : null}
        </div>
        <Button
          disabled={sessions.length === 0 || busy}
          type="button"
          variant="destructive"
          onClick={() => setClearTarget({
            id: "__all__",
            kind: "all",
            label: "",
            sessionIDs: sessions.map((session) => session.id),
          })}
        >
          {clearMutation.isPending && clearMutation.variables.kind === "all" ? <Spinner /> : <Trash />}
          {t("archivedSessions.clearAll")}
        </Button>
      </div>

      {sessionsQuery.isError || projectsQuery.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t("archivedSessions.loadFailed")}</AlertDescription>
        </Alert>
      ) : null}
      {sessionsQuery.isLoading || projectsQuery.isLoading ? (
        <div className="grid place-items-center py-12 text-muted-foreground"><Spinner /></div>
      ) : null}
      {!sessionsQuery.isLoading && !projectsQuery.isLoading && groups.length === 0 ? (
        messageSearch.waiting ? (
          <div className={cn(SETTINGS_CARD_CLASS, "grid place-items-center px-4 py-12 text-muted-foreground")}>
            <Spinner className="size-5" />
          </div>
        ) : (
          <div className={cn(SETTINGS_CARD_CLASS, "grid justify-items-center gap-2 px-4 py-12 text-sm text-muted-foreground")}>
            <Archive className="size-5" />
            <span>
              {messageSearch.isError && messageSearch.normalizedQuery
                ? t("rail.searchFailed")
                : t(messageSearch.normalizedQuery ? "rail.searchEmpty" : "archivedSessions.empty")}
            </span>
          </div>
        )
      ) : null}
      {groups.map((group) => (
        <section key={group.id} className={SETTINGS_CARD_CLASS}>
          <div className="flex items-center justify-between gap-3 border-b border-border/70 bg-muted/20 px-3 py-1.5">
            <h3 className="min-w-0 truncate text-sm font-normal">{group.label}</h3>
            <span className="flex shrink-0 items-center gap-1">
              <span className="text-xs text-muted-foreground">
                {t("archivedSessions.sessionCount").replace("{count}", String(group.allSessionIDs.length))}
              </span>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    aria-label={t("archivedSessions.projectActions")}
                    disabled={busy}
                    size="icon-sm"
                    type="button"
                    variant="ghost"
                  >
                    {clearMutation.isPending && clearMutation.variables.id === group.id
                      ? <Spinner />
                      : <Ellipsis />}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    variant="destructive"
                    onSelect={() => setClearTarget({
                      id: group.id,
                      kind: "group",
                      label: group.label,
                      sessionIDs: group.allSessionIDs,
                    })}
                  >
                    <Trash />
                    {t("archivedSessions.clearProject")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </span>
          </div>
          <div className="divide-y divide-border/70">
            {group.sessions.map((session) => (
              <div key={session.id} className="flex items-center justify-between gap-3 px-3 py-3">
                <span className="grid min-w-0 gap-1">
                  <span className="truncate text-sm">{session.title || t("session.untitled")}</span>
                  {messagesBySessionID.has(session.id) ? (
                    <span className="truncate text-xs text-muted-foreground">
                      {sessionSearchExcerpt(messagesBySessionID.get(session.id)!.text, excerptTerms)}
                    </span>
                  ) : null}
                  <span className="text-xs text-muted-foreground">
                    {t("archivedSessions.archivedOn").replace(
                      "{date}",
                      formatArchivedDate(session.archivedAt, locale),
                    )}
                  </span>
                </span>
                <span className="flex shrink-0 items-center gap-1">
                  <Button
                    aria-label={t("archivedSessions.delete")}
                    disabled={busy}
                    size="icon-sm"
                    type="button"
                    variant="ghost"
                    onClick={() => setDeleteTarget(session)}
                  >
                    <Trash />
                  </Button>
                  <Button
                    disabled={busy}
                    size="sm"
                    type="button"
                    variant="ghost"
                    onClick={() => restoreMutation.mutate(session.id)}
                  >
                    {restoreMutation.isPending && restoreMutation.variables === session.id ? <Spinner /> : null}
                    {t("archivedSessions.restore")}
                  </Button>
                </span>
              </div>
            ))}
          </div>
        </section>
      ))}

      <AlertDialog open={Boolean(deleteTarget)} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("archivedSessions.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("archivedSessions.deleteConfirm")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={!deleteTarget || deleteMutation.isPending}
              variant="destructive"
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
            >
              {deleteMutation.isPending ? <Spinner /> : null}
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={Boolean(clearTarget)} onOpenChange={(open) => !open && !clearMutation.isPending && setClearTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {clearTarget?.kind === "group"
                ? t("archivedSessions.clearGroupTitle").replace("{group}", clearTarget.label)
                : t("archivedSessions.clearAllTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {clearTarget?.kind === "group"
                ? t("archivedSessions.clearGroupConfirm")
                : t("archivedSessions.clearAllConfirm")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={clearMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={clearMutation.isPending}
              variant="destructive"
              onClick={() => clearTarget && clearMutation.mutate(clearTarget)}
            >
              {clearMutation.isPending ? <Spinner /> : null}
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function groupArchivedSessions(
  sessions: Session[],
  projects: Project[],
  matchingSessionIDs: ReadonlySet<string> | null,
  noProjectLabel: string,
): ArchiveGroup[] {
  const projectsByID = new Map(projects.map((project) => [project.id, project]));
  const groups = new Map<string, ArchiveGroup>();
  for (const session of sessions) {
    const project = projectsByID.get(session.projectID || "");
    const label = project?.name || noProjectLabel;
    const id = project?.id || "__no_project__";
    const group = groups.get(id) || { id, label, sessions: [], allSessionIDs: [] };
    group.allSessionIDs.push(session.id);
    if (!matchingSessionIDs || matchingSessionIDs.has(session.id)) {
      group.sessions.push(session);
    }
    groups.set(id, group);
  }
  return Array.from(groups.values())
    .filter((group) => group.sessions.length > 0)
    .sort((left, right) => left.label.localeCompare(right.label));
}

function formatArchivedDate(value: string | undefined, locale: string) {
  if (!value) {
    return "—";
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}
