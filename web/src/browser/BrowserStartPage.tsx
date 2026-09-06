import { AppTooltip } from "@/components/AppTooltip";
import { useQuery } from "@tanstack/react-query";
import { listBrowserHistory } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Globe } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { WorkspaceStartPage } from "@/components/workspace/WorkspaceStartPage";
import { useI18n } from "@/i18n";
import { BrowserFavicon } from "./BrowserFavicon";
import { BrowserStartIllustration } from "./BrowserStartIllustration";
import { browserCompactURL, uniqueBrowserHistoryBySite } from "./helpers";

export function BrowserStartPage({ sessionID, token, pending = false, openingURL, onOpen }: {
  sessionID: string;
  token: string;
  pending?: boolean;
  openingURL?: string;
  onOpen: (url: string) => void;
}) {
  const { t } = useI18n();
  const historyQuery = useQuery({
    enabled: Boolean(token && sessionID),
    queryKey: queryKeys.browserHistoryRecent(16),
    queryFn: () => listBrowserHistory(token, sessionID, "", 64),
    staleTime: 0,
  });
  const history = uniqueBrowserHistoryBySite(historyQuery.data?.history || [], 16);

  return (
    <WorkspaceStartPage
      icon={<BrowserStartIllustration />}
      title={t("workspace.browserNewTabTitle")}
    >
      {history.length > 0 ? (
        <div role="group" aria-label={t("browser.historyRecent")} className="flex flex-wrap justify-center gap-x-3 gap-y-2">
          {history.map((entry) => (
            <AppTooltip key={entry.id} content={entry.url}><button className="flex w-20 min-w-0 flex-col items-center gap-2 rounded-xl px-1 py-3 hover:bg-accent focus-visible:bg-accent disabled:opacity-60" type="button" disabled={pending} onClick={() => onOpen(entry.url)}>
              <span className="grid size-11 place-items-center rounded-2xl border border-border/60 bg-background">
                {openingURL === entry.url ? <Spinner className="size-5" /> : <BrowserFavicon className="size-5 object-contain" fallback={<Globe className="size-5 text-muted-foreground" />} faviconURL={entry.faviconURL} pageURL={entry.url} />}
              </span>
              <span className="block w-full truncate text-xs text-foreground/80">{entry.title || browserCompactURL(entry.url)}</span>
            </button></AppTooltip>
          ))}
        </div>
      ) : null}
    </WorkspaceStartPage>
  );
}
