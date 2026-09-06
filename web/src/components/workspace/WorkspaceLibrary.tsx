import { useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react";
import type { LibraryEntry } from "@/contracts/api";
import { AppTooltip } from "@/components/AppTooltip";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ConfirmationDialog";
import { ChevronDown, ChevronUp, FolderClosed, Globe, Search, Trash2, X } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { CanvasLibraryRows, type ClosedCanvasEntry } from "./CanvasLibraryRows";
import { LibraryHistoryRows, useLibraryHistory } from "./LibraryHistory";
import { LibraryRows } from "./LibraryRows";
import { libraryMatches, libraryReferenceKey } from "./libraryPresentation";

type LibraryQuery = { isLoading: boolean; isError: boolean; refetch: () => unknown };

export function WorkspaceLibrary({ active, sessionID, token, entries, closedItems, resourceQueries, onOpenSaved, onRestoreClosed, onRemoveSaved, onRemoveClosed, onOpenBrowserURL, onOpenProject, onNewBrowserTab, creatingBrowserTab, searchRef }: {
  searchRef: RefObject<HTMLInputElement | null>;
  onOpenProject: () => void;
  onNewBrowserTab: () => void;
  creatingBrowserTab: boolean;
  active: boolean;
  sessionID: string;
  token: string;
  entries: LibraryEntry[];
  closedItems: ClosedCanvasEntry[];
  resourceQueries: Record<"saved" | "closed", LibraryQuery>;
  onOpenSaved: (item: { id: string; title?: string }) => void;
  onRestoreClosed: (item: ClosedCanvasEntry) => void;
  onRemoveSaved: (item: { id: string; title?: string }) => void;
  onRemoveClosed: (item: ClosedCanvasEntry) => void;
  onOpenBrowserURL: (url: string) => void;
}) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const [search, setSearch] = useState("");
  const resultsRef = useRef<HTMLElement>(null);
  const [composing, setComposing] = useState(false);
  const [kind, setKind] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);
  const [query, setQuery] = useState("");
  useEffect(() => {
    if (composing) return;
    const timeout = window.setTimeout(() => setQuery(search.trim()), 180);
    return () => window.clearTimeout(timeout);
  }, [search, composing]);
  useEffect(() => { if (active) searchRef.current?.focus({ preventScroll: true }); }, [active, searchRef]);
  const historyActions = useLibraryHistory(active, token, sessionID, query, kind);
  const { history, clear } = historyActions;
  const recent = history.data?.entries || [];
  // All sections use the same completed search while its replacement loads.
  const displayedQuery = history.data?.query ?? query;
  const displayedKind = history.data?.kind ?? kind;
  const searching = Boolean(displayedQuery);
  const refetchLibrary = resourceQueries.saved.refetch;
  useEffect(() => { if (active) void refetchLibrary(); }, [active, sessionID, refetchLibrary]);

  const favorites = entries.filter(entry => entry.favoriteID).sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  const recentKeys = new Set(recent.map(libraryReferenceKey));
  const saved = entries.filter(entry => (!displayedKind || entry.kind === displayedKind) && libraryMatches(entry, displayedQuery, t))
    .filter(entry => !recentKeys.has(libraryReferenceKey(entry)))
    .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  const recentCanvasIDs = new Set(recent.filter(entry => entry.kind === "canvas" && entry.sourceSessionID === sessionID).map(entry => entry.itemID));
  const closed = closedItems.filter(entry => (!displayedKind || displayedKind === "canvas") && `${entry.title || ""} ${t(`workspace.kind.${entry.kind}`)}`.toLocaleLowerCase().includes(displayedQuery.toLocaleLowerCase()))
    .filter(entry => !searching || !recentCanvasIDs.has(entry.id)).sort((a, b) => Date.parse(b.closedAt) - Date.parse(a.closedAt));

  const openEntry = (entry: LibraryEntry) => {
    if (entry.kind === "canvas" && entry.savedItemID) onOpenSaved({ id: entry.savedItemID });
    else if (entry.kind === "web" && entry.url) onOpenBrowserURL(entry.url);
  };
  const relevantQueries = searching ? [history, resourceQueries.saved, resourceQueries.closed] : [history, resourceQueries.saved];
  const loading = composing || search.trim() !== query || history.isFetching || relevantQueries.some(item => item.isLoading);
  const initialLoading = !history.data && history.isLoading;
  const failed = relevantQueries.some(item => item.isError);
  const empty = !recent.length && (!searching || (!saved.length && !closed.length));
  const savedRows = <LibraryRows entries={saved} token={token} sessionID={sessionID} onOpen={openEntry} onDeleteSaved={onRemoveSaved} />;
  const closedRows = <CanvasLibraryRows closedItems={closed} onRestoreClosed={onRestoreClosed} onRemoveClosed={onRemoveClosed} />;

  const navigateResults = (event: KeyboardEvent<HTMLElement>) => {
    if (composing || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229 || event.altKey || event.ctrlKey || event.metaKey) return;
    const target = event.target;
    const fromSearch = target === searchRef.current;
    const buttons = Array.from(resultsRef.current?.querySelectorAll<HTMLButtonElement>("[data-library-open]:not(:disabled)") ?? []);
    const index = buttons.indexOf(target as HTMLButtonElement);
    if (!fromSearch && index < 0) return;
    if (event.key === "Escape" && !fromSearch) {
      event.preventDefault();
      searchRef.current?.focus({ preventScroll: true });
    } else if (["ArrowDown", "ArrowUp", "Enter"].includes(event.key)) {
      // A pending search must not open a result belonging to the previous query.
      if (loading || failed) { event.preventDefault(); return; }
      if (event.key === "Enter") {
        if (fromSearch && buttons[0]) { event.preventDefault(); buttons[0].click(); }
        return;
      }
      event.preventDefault();
      if (event.key === "ArrowUp" && index <= 0) searchRef.current?.focus({ preventScroll: true });
      else {
        const next = buttons[Math.min(buttons.length - 1, index + (event.key === "ArrowDown" ? 1 : -1))];
        next?.focus({ preventScroll: true });
        next?.scrollIntoView({ block: "nearest" });
      }
    }
  };

  return <section data-workspace-library onKeyDown={navigateResults} aria-label={t("workspace.app.library")} aria-hidden={!active} aria-busy={loading} className={cn("@container absolute inset-0 overflow-y-auto bg-[var(--workspace-chrome-background)]", !active && "hidden")}>
    <div className="mx-auto flex w-full max-w-[600px] flex-col gap-7 px-5 pt-7 pb-10 @min-[26rem]:px-[26px]">
      <header>
        <h1 className="mb-6 text-center text-lg font-medium text-muted-foreground">{t("workspace.app.library")}</h1>
        <div className="relative min-w-0">
          <span aria-hidden="true" className="pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2 text-muted-foreground">{loading ? <Spinner className="size-4" /> : <Search className="size-4" />}</span>
          <Input ref={searchRef} aria-label={t("workspace.searchLibrary")} placeholder={t("workspace.searchResources")} autoComplete="off" value={search} onChange={event => setSearch(event.target.value)} onCompositionStart={() => setComposing(true)} onCompositionEnd={event => { setComposing(false); setSearch(event.currentTarget.value); }} className="h-[46px] rounded-xl border-[var(--workspace-border)] bg-muted/30 pr-10 pl-10 text-[13px] shadow-none focus-visible:ring-0 md:text-[13px]" />
          {search ? <AppTooltip content={t("workspace.clearSearch")}><Button aria-label={t("workspace.clearSearch")} size="icon-xs" variant="ghost" className="absolute inset-y-0 right-2 my-auto text-muted-foreground" onClick={() => { setSearch(""); searchRef.current?.focus(); }}><X className="size-3.5" /></Button></AppTooltip> : null}
        </div>
        <div className="mt-3 flex flex-wrap items-center justify-center gap-2">
          <Button aria-label={t("workspace.openProject")} className="h-8 gap-2 text-xs font-normal text-muted-foreground" variant="ghost" onClick={onOpenProject}><FolderClosed className="size-4" />{t("workspace.openProject")}</Button>
          <Button aria-label={t("workspace.newBrowser")} className="h-8 gap-2 text-xs font-normal text-muted-foreground" variant="ghost" disabled={creatingBrowserTab} onClick={onNewBrowserTab}>{creatingBrowserTab ? <Spinner className="size-4" /> : <Globe className="size-4" />}{t("workspace.newBrowserShort")}</Button>
        </div>
      </header>
      {!searching ? <section aria-label={t("workspace.favoriteContents")}>
        <div className="mb-3 flex min-h-5 items-center justify-between gap-3">
          <h2 className="text-xs font-medium text-muted-foreground">{t("workspace.library.saved")}</h2>
          {favorites.length > 3 ? <Button size="xs" variant="ghost" className="gap-1 px-1 text-[11px] font-normal text-muted-foreground" aria-expanded={expanded} onClick={() => setExpanded(value => !value)}>
            {t(expanded ? "workspace.collapseFavorites" : "workspace.viewAll")}{expanded ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
          </Button> : null}
        </div>
        {favorites.length ? <div className="flex flex-wrap gap-2" data-library-shortcuts>
          <LibraryRows entries={expanded ? favorites : favorites.slice(0, 3)} token={token} sessionID={sessionID} onOpen={openEntry} onDeleteSaved={onRemoveSaved} layout="shortcut" />
        </div> : <p className="py-2 text-xs text-muted-foreground">{t("workspace.noFavorites")}</p>}
      </section> : null}
      <section ref={resultsRef} data-library-results>
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-xs font-medium text-muted-foreground">{t(searching ? "workspace.searchResults" : "workspace.library.history")}</h2>
          <div className="ml-auto flex flex-wrap items-center gap-1">
            <div className="flex flex-wrap gap-0.5" aria-label={t("workspace.recentType")}>
              {["", "file", "web", "canvas"].map(value => <Button key={value} type="button" variant={kind === value ? "secondary" : "ghost"} size="sm" className="h-7 px-2 text-[11px] font-normal text-muted-foreground aria-pressed:text-foreground" aria-pressed={kind === value} onClick={() => setKind(value)}>{t(value ? `workspace.kind.${value}` : "workspace.allResources")}</Button>)}
            </div>
            {recent.length > 0 ? <AppTooltip content={t("workspace.recentClear")}><Button size="icon-xs" variant="ghost" aria-label={t("workspace.recentClear")} className="text-muted-foreground" onClick={() => setConfirmClear(true)}><Trash2 className="size-3.5" /></Button></AppTooltip> : null}
          </div>
        </div>
        {initialLoading ? <div className="flex justify-center p-4"><Spinner className="size-4" /></div> : null}
        {failed ? <div role="alert" className="flex items-center gap-2 p-3 text-sm text-muted-foreground">{t("workspace.libraryLoadFailed")}<Button size="sm" variant="ghost" onClick={() => relevantQueries.filter(item => item.isError).forEach(item => void item.refetch())}>{t("common.refresh")}</Button></div> : null}
        {empty && !loading && !failed ? <p className="py-14 text-center text-sm text-muted-foreground">{t("workspace.noResults")}</p> : null}
        <div className="-mx-2 flex flex-col gap-0.5">
        <LibraryHistoryRows entries={recent} actions={historyActions} onOpenBrowserURL={onOpenBrowserURL} />
        {searching ? <>{savedRows}{closedRows}</> : null}
        </div>
      </section>
    </div>
    <AlertDialog open={confirmClear} onOpenChange={setConfirmClear}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>{t("workspace.recentClearTitle").replace("{kind}", t(kind ? `workspace.kind.${kind}` : "workspace.recentAll"))}</AlertDialogTitle><AlertDialogDescription>{t("workspace.recentClearDescription")}</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel><AlertDialogAction disabled={clear.isPending} onClick={event => { event.preventDefault(); clear.mutate(undefined, { onSuccess: () => setConfirmClear(false) }); }}>{t("workspace.recentClear")}</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
  </section>;
}
