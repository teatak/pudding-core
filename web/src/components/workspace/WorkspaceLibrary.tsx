import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type RefObject,
} from "react";
import type { CanvasItem, LibraryEntry } from "@/contracts/api";
import { AppTooltip } from "@/components/AppTooltip";
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
import {
  ArrowLeft,
  FolderClosed,
  Globe,
  Search,
  Trash2,
  X,
} from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { CanvasLibraryRows } from "./CanvasLibraryRows";
import { LibraryHistoryRows, useLibraryBrowserHistory } from "./LibraryHistory";
import { LibraryRows } from "./LibraryRows";
import { libraryMatches } from "./libraryPresentation";

type LibraryQuery = {
  isLoading: boolean;
  isError: boolean;
  refetch: () => unknown;
};
export function WorkspaceLibrary({
  active,
  sessionID,
  token,
  entries,
  savedQuery,
  canvasItems,
  canvasQuery,
  onOpenSaved,
  onRemoveSaved,
  onRemoveCanvas,
  onOpenBrowserURL,
  onOpenProject,
  onNewBrowserTab,
  creatingBrowserTab,
  searchRef,
}: {
  searchRef: RefObject<HTMLInputElement | null>;
  onOpenProject: () => void;
  onNewBrowserTab: () => void;
  creatingBrowserTab: boolean;
  active: boolean;
  sessionID: string;
  token: string;
  entries: LibraryEntry[];
  savedQuery: LibraryQuery;
  canvasItems: CanvasItem[];
  canvasQuery: LibraryQuery;
  onOpenSaved: (item: { id: string; title?: string }) => void;
  onRemoveSaved: (item: { id: string; title?: string }) => void;
  onRemoveCanvas: (item: CanvasItem) => void;
  onOpenBrowserURL: (url: string) => void;
}) {
  const { t } = useI18n();
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [composing, setComposing] = useState(false);
  const [canvasLimit, setCanvasLimit] = useState(6);
  const [favoritesExpanded, setFavoritesExpanded] = useState(false);
  const [historyExpanded, setHistoryExpanded] = useState(false);
  const [historyLimit, setHistoryLimit] = useState(20);
  const [confirmClear, setConfirmClear] = useState(false);
  const resultsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (composing) return;
    const timeout = window.setTimeout(() => {
      setQuery(search.trim());
      setCanvasLimit(6);
      setHistoryLimit(20);
    }, 180);
    return () => window.clearTimeout(timeout);
  }, [search, composing]);
  useEffect(() => {
    if (active) searchRef.current?.focus({ preventScroll: true });
  }, [active, searchRef]);
  const refetchSaved = savedQuery.refetch;
  useEffect(() => {
    if (active) void refetchSaved();
  }, [active, refetchSaved]);
  const browser = useLibraryBrowserHistory(
    active,
    token,
    sessionID,
    query,
    historyExpanded,
  );
  const favorites = entries
    .filter((entry) => entry.favoriteID && libraryMatches(entry, query, t))
    .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  const favoriteIDs = new Map(
    entries
      .filter((entry) => entry.savedItemID && entry.favoriteID)
      .map((entry) => [entry.savedItemID!, entry.favoriteID!]),
  );
  const canvasEntries = canvasItems
    .filter(
      (entry) =>
        [entry.title, t(`workspace.kind.${entry.kind}`)]
          .join(" ")
          .toLocaleLowerCase()
          .includes(query.toLocaleLowerCase()) &&
        (!query || !favorites.some((favorite) =>
          favorite.savedItemID && favorite.savedItemID === entry.sourceSavedItemID,
        )),
    )
    .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  const workingSavedIDs = new Set(
    canvasEntries.map((entry) => entry.sourceSavedItemID),
  );
  // Only matching results suppress another entry for the same opening target;
  // the saved title and a renamed working copy can match different searches.
  // Unfavoriting only removes the shortcut. Retain the existing ability to find
  // saved content by search, including when its original conversation is gone.
  const savedResults = query
    ? entries.filter(
        (entry) =>
          entry.kind === "canvas" &&
          !entry.favoriteID &&
          !workingSavedIDs.has(entry.savedItemID) &&
          libraryMatches(entry, query, t),
      )
    : [];
  const canvasResultCount = canvasEntries.length + savedResults.length;
  const openEntry = (entry: LibraryEntry) => {
    if (entry.kind === "canvas" && entry.savedItemID)
      onOpenSaved({ id: entry.savedItemID });
    else if (entry.url) onOpenBrowserURL(entry.url);
  };
  const history = browser.history.data?.history || [];
  const pendingSearch = composing || search.trim() !== query;
  const loading =
    pendingSearch ||
    canvasQuery.isLoading ||
    browser.history.isFetching ||
    savedQuery.isLoading;
  const failed =
    canvasQuery.isError || browser.history.isError || savedQuery.isError;
  const emptyStartPage =
    !loading &&
    !failed &&
    !search &&
    !historyExpanded &&
    !favorites.length &&
    !canvasItems.length &&
    !history.length;
  const navigateResults = (event: KeyboardEvent<HTMLElement>) => {
    if (
      composing ||
      event.nativeEvent.isComposing ||
      event.nativeEvent.keyCode === 229 ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey
    )
      return;
    const fromSearch = event.target === searchRef.current;
    const buttons = Array.from(
      resultsRef.current?.querySelectorAll<HTMLButtonElement>(
        "[data-library-open]:not(:disabled)",
      ) || [],
    );
    const index = buttons.indexOf(event.target as HTMLButtonElement);
    if (!fromSearch && index < 0) return;
    if (event.key === "Escape" && !fromSearch) {
      event.preventDefault();
      searchRef.current?.focus({ preventScroll: true });
    } else if (["ArrowDown", "ArrowUp", "Enter"].includes(event.key)) {
      if (loading || failed) {
        event.preventDefault();
        return;
      }
      if (event.key === "Enter") {
        if (fromSearch && buttons[0]) {
          event.preventDefault();
          buttons[0].click();
        }
        return;
      }
      event.preventDefault();
      if (event.key === "ArrowUp" && index <= 0)
        searchRef.current?.focus({ preventScroll: true });
      else {
        const next =
          buttons[
            Math.min(
              buttons.length - 1,
              index + (event.key === "ArrowDown" ? 1 : -1),
            )
          ];
        next?.focus({ preventScroll: true });
        next?.scrollIntoView({ block: "nearest" });
      }
    }
  };
  const noResults = (message: string) =>
    !loading && !failed ? (
      <p className="py-2 text-xs text-muted-foreground">{message}</p>
    ) : null;
  return (
    <section
      data-workspace-library
      aria-label={t("workspace.app.library")}
      aria-hidden={!active}
      aria-busy={loading}
      onKeyDown={navigateResults}
      className={cn(
        "@container absolute inset-0 overflow-y-auto bg-[var(--workspace-chrome-background)]",
        !active && "hidden",
      )}
    >
      <div className="mx-auto flex w-full max-w-[600px] flex-col gap-8 px-5 pt-16 pb-18 @min-[32rem]:px-6">
        <header>
          <div className="mb-4 grid grid-cols-1 gap-3 @min-[22rem]:grid-cols-2">
            <Button
              aria-label={t("workspace.openProject")}
              className="h-[42px] gap-2 rounded-xl border-[var(--workspace-border)] bg-muted/20 text-[13px] font-normal shadow-none hover:bg-accent/60"
              variant="outline"
              onClick={onOpenProject}
            >
              <FolderClosed className="size-5" />
              {t("workspace.openProject")}
            </Button>
            <Button
              aria-label={t("workspace.newBrowser")}
              className="h-[42px] gap-2 rounded-xl border-[var(--workspace-border)] bg-muted/20 text-[13px] font-normal shadow-none hover:bg-accent/60"
              variant="outline"
              disabled={creatingBrowserTab}
              onClick={onNewBrowserTab}
            >
              {creatingBrowserTab ? (
                <Spinner className="size-5" />
              ) : (
                <Globe className="size-5" />
              )}
              {t("workspace.newBrowserShort")}
            </Button>
          </div>
          <div className="relative min-w-0">
            <span className="pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2 text-muted-foreground">
              {loading ? (
                <Spinner className="size-4" />
              ) : (
                <Search className="size-4" />
              )}
            </span>
            <Input
              ref={searchRef}
              aria-label={t("workspace.searchLibrary")}
              placeholder={t("workspace.searchResources")}
              autoComplete="off"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              onCompositionStart={() => setComposing(true)}
              onCompositionEnd={(event) => {
                setComposing(false);
                setSearch(event.currentTarget.value);
              }}
              className="h-11 rounded-xl border-[var(--workspace-border)] bg-muted/30 pr-10 pl-10 text-[13px] shadow-none focus-visible:ring-0 md:text-[13px]"
            />
            {search ? (
              <AppTooltip content={t("workspace.clearSearch")}>
                <Button
                  aria-label={t("workspace.clearSearch")}
                  size="icon-xs"
                  variant="ghost"
                  className="absolute inset-y-0 right-2 my-auto text-muted-foreground"
                  onClick={() => {
                    setSearch("");
                    searchRef.current?.focus();
                  }}
                >
                  <X className="size-3.5" />
                </Button>
              </AppTooltip>
            ) : null}
          </div>
        </header>
        {failed ? (
          <div
            role="alert"
            className="flex items-center gap-2 text-sm text-muted-foreground"
          >
            {t("workspace.libraryLoadFailed")}
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                void canvasQuery.refetch();
                void browser.history.refetch();
                void savedQuery.refetch();
              }}
            >
              {t("common.refresh")}
            </Button>
          </div>
        ) : null}
        <div
          ref={resultsRef}
          data-library-results
          className={cn("flex flex-col gap-7", emptyStartPage && "hidden")}
          inert={pendingSearch || undefined}
        >
          {favorites.length > 0 ? (
            <section data-library-favorites>
              <div className="mb-2.5 flex items-center justify-between gap-2">
                <h2 className="text-sm font-medium">
                  {t("workspace.library.saved")}
                </h2>
                {favorites.length > 6 ? (
                  <Button
                    size="xs"
                    variant="ghost"
                    className="text-xs font-normal text-muted-foreground"
                    aria-expanded={favoritesExpanded}
                    onClick={() => setFavoritesExpanded((value) => !value)}
                  >
                    {t(
                      favoritesExpanded
                        ? "workspace.collapseFavorites"
                        : "workspace.viewAll",
                    )}{" "}
                    · {favorites.length}
                  </Button>
                ) : null}
              </div>
              <div
                data-library-shortcuts
                className="grid grid-cols-1 gap-2.5 @min-[24rem]:grid-cols-2"
              >
                <LibraryRows
                  entries={
                    favoritesExpanded || query
                      ? favorites
                      : favorites.slice(0, 6)
                  }
                  token={token}
                  sessionID={sessionID}
                  onOpen={openEntry}
                  onDeleteSaved={onRemoveSaved}
                  layout="shortcut"
                />
              </div>
            </section>
          ) : null}
          {canvasResultCount > 0 ? (
            <section data-library-canvases>
              <div className="mb-2.5 flex flex-wrap items-center justify-between gap-2">
                <h2 className="text-sm font-medium">
                  {t("workspace.kind.canvas")}
                </h2>
              </div>
              <div className="grid grid-cols-1 gap-2.5 @min-[32rem]:grid-cols-2">
                <CanvasLibraryRows
                  entries={canvasEntries.slice(0, canvasLimit)}
                  token={token}
                  sessionID={sessionID}
                  favoriteIDs={favoriteIDs}
                  onRemoveCanvas={onRemoveCanvas}
                />
                <LibraryRows
                  entries={savedResults.slice(
                    0,
                    Math.max(0, canvasLimit - canvasEntries.length),
                  )}
                  token={token}
                  sessionID={sessionID}
                  onOpen={openEntry}
                  onDeleteSaved={onRemoveSaved}
                  layout="card"
                />
              </div>
              {canvasResultCount > canvasLimit ? (
                <Button
                  className="mt-2 h-7 px-0 text-xs font-normal text-muted-foreground"
                  variant="ghost"
                  onClick={() => setCanvasLimit((value) => value + 6)}
                >
                  {t("workspace.library.moreCanvas")} · {canvasResultCount}
                </Button>
              ) : null}
            </section>
          ) : null}
          {!emptyStartPage ? (
            <section data-library-browser>
              <div className="mb-2.5 flex items-center justify-between gap-2">
                <h2 className="text-sm font-medium">
                  {t(
                    historyExpanded
                      ? "browser.historyResults"
                      : "browser.historyRecent",
                  )}
                </h2>
                <div className="flex shrink-0 items-center gap-1">
                  <Button
                    size="xs"
                    variant="ghost"
                    className={cn(
                      "gap-1 font-normal text-muted-foreground",
                      !historyExpanded && "-mr-2",
                    )}
                    aria-expanded={historyExpanded}
                    onClick={() => {
                      setHistoryExpanded((value) => !value);
                      setHistoryLimit(20);
                    }}
                  >
                    {historyExpanded ? <ArrowLeft className="size-3" /> : null}
                    {t(
                      historyExpanded
                        ? "workspace.library.backRecent"
                        : "browser.historyResults",
                    )}
                  </Button>
                  {historyExpanded && history.length > 0 ? (
                    <AppTooltip content={t("browser.historyClear")}>
                      <Button
                        size="icon-xs"
                        variant="ghost"
                        aria-label={t("browser.historyClear")}
                        className="text-muted-foreground"
                        onClick={() => setConfirmClear(true)}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </AppTooltip>
                  ) : null}
                </div>
              </div>
              <div className="-mx-2">
                <LibraryHistoryRows
                  entries={history.slice(
                    0,
                    historyExpanded || query ? historyLimit : 5,
                  )}
                  actions={browser}
                  onOpenBrowserURL={onOpenBrowserURL}
                  groupByDate={historyExpanded}
                />
              </div>
              {!history.length ? noResults(t("browser.historyEmpty")) : null}
              {(historyExpanded || query) && history.length > historyLimit ? (
                <Button
                  size="xs"
                  variant="ghost"
                  className="mt-2 font-normal text-muted-foreground"
                  onClick={() => setHistoryLimit((value) => value + 20)}
                >
                  {t("workspace.library.moreHistory")}
                </Button>
              ) : null}
            </section>
          ) : null}
        </div>
      </div>
      <AlertDialog open={confirmClear} onOpenChange={setConfirmClear}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("browser.historyClearTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("browser.historyClearDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={browser.clear.isPending}
              onClick={(event) => {
                event.preventDefault();
                browser.clear.mutate(undefined, {
                  onSuccess: () => setConfirmClear(false),
                });
              }}
            >
              {t("browser.historyClear")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
