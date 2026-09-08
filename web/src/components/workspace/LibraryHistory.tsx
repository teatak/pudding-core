import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  clearBrowserHistory,
  deleteBrowserHistoryEntry,
  listBrowserHistory,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type { BrowserHistoryEntry } from "@/contracts/api";
import { AppDropdownMenuItem } from "@/components/AppMenu";
import { Trash2 } from "@/components/icons";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { LibraryResourceIcon, LibraryResourceRow } from "./LibraryResourceRow";

export function useLibraryBrowserHistory(
  active: boolean,
  token: string,
  sessionID: string,
  query: string,
  expanded: boolean,
) {
  const { t } = useI18n();
  const client = useQueryClient();
  const limit = expanded || query ? 1000 : 5;
  const history = useQuery({
    queryKey: [...queryKeys.browserHistory(query), limit],
    queryFn: () => listBrowserHistory(token, sessionID, query, limit),
    enabled: active && Boolean(token && sessionID),
    staleTime: 0,
  });
  const refresh = () =>
    client.invalidateQueries({ queryKey: queryKeys.browserHistory() });
  const remove = useMutation({
    mutationFn: (entry: BrowserHistoryEntry) =>
      deleteBrowserHistoryEntry(token, sessionID, entry.id),
    onSuccess: refresh,
    onError: () => toast.error(t("browser.historyDeleteFailed")),
  });
  const clear = useMutation({
    mutationFn: () => clearBrowserHistory(token, sessionID),
    onSuccess: refresh,
    onError: () => toast.error(t("browser.historyClearFailed")),
  });
  return { history, remove, clear };
}

export function LibraryHistoryRows({
  entries,
  actions,
  onOpenBrowserURL,
  groupByDate,
}: {
  entries: BrowserHistoryEntry[];
  actions: ReturnType<typeof useLibraryBrowserHistory>;
  onOpenBrowserURL: (url: string) => void;
  groupByDate: boolean;
}) {
  const { t, locale } = useI18n();
  let previousDate = "";
  return entries.map((entry, index) => {
    const date = new Date(entry.visitedAt).toLocaleDateString(locale, {
      year: "numeric",
      month: "long",
      day: "numeric",
    });
    const showDate = groupByDate && date !== previousDate;
    previousDate = date;
    const host = new URL(entry.url).host;
    return (
      <div key={entry.id}>
        {showDate ? (
          <h3
            className={cn(
              "px-2 pb-1 text-xs text-muted-foreground",
              index === 0 ? "pt-1" : "pt-4",
            )}
          >
            {date}
          </h3>
        ) : null}
        <LibraryResourceRow
          title={entry.title || host}
          description={host}
          icon={
            <LibraryResourceIcon
              entry={{
                kind: "web",
                url: entry.url,
                faviconURL: entry.faviconURL,
              }}
            />
          }
          date={entry.visitedAt}
          dateLabel={t("workspace.resourceOpenedAt")}
          onOpen={() => onOpenBrowserURL(entry.url)}
          details={[
            { label: t("workspace.resourceLocation"), value: entry.url },
          ]}
          actions={
            <AppDropdownMenuItem
              disabled={actions.remove.isPending}
              onSelect={() => actions.remove.mutate(entry)}
            >
              <Trash2 />
              {t("browser.historyDelete")}
            </AppDropdownMenuItem>
          }
        />
      </div>
    );
  });
}
