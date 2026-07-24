import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";

import { searchSessionMessages, type Message, type Project, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { SessionModeIcon } from "@/components/SessionModeIcon";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

const searchDelayMs = 180;
const maxVisibleResults = 50;

type SearchResult = {
  session: Session;
  project?: Project;
  message?: Message;
  score: number;
};

export function SessionSearchDialog({
  open,
  projects,
  sessions,
  token,
  onOpenChange,
  onSelect,
}: {
  open: boolean;
  projects: Project[];
  sessions: Session[];
  token: string;
  onOpenChange: (open: boolean) => void;
  onSelect: (sessionID: string) => void;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const resultListRef = useRef<HTMLDivElement | null>(null);
  const sessionIDs = useMemo(() => sessions.map((session) => session.id).sort(), [sessions]);
  const normalizedQuery = normalizeSearchText(query);
  const normalizedDebouncedQuery = normalizeSearchText(debouncedQuery);

  useEffect(() => {
    if (open) {
      return;
    }
    setQuery("");
    setDebouncedQuery("");
    setActiveIndex(0);
  }, [open]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const timeoutID = window.setTimeout(() => setDebouncedQuery(query.trim()), searchDelayMs);
    return () => window.clearTimeout(timeoutID);
  }, [open, query]);

  const messageSearch = useQuery({
    queryKey: queryKeys.sessionSearch(sessionIDs, debouncedQuery),
    queryFn: () => searchSessionMessages(token, { sessionIDs, query: debouncedQuery, limit: 100 }),
    enabled: Boolean(open && token && sessionIDs.length > 0 && debouncedQuery),
    retry: false,
  });
  const matchingMessages =
    normalizedQuery && normalizedQuery === normalizedDebouncedQuery ? messageSearch.data?.messages || [] : [];
  const responseMatchTerms =
    normalizedQuery && normalizedQuery === normalizedDebouncedQuery ? messageSearch.data?.matchTerms : undefined;
  const highlightTerms = useMemo(
    () => normalizeHighlightTerms([normalizedQuery, ...searchTerms(normalizedQuery), ...(responseMatchTerms || [])]),
    [normalizedQuery, responseMatchTerms],
  );
  const results = useMemo(
    () => buildSearchResults(sessions, projects, matchingMessages, normalizedQuery),
    [matchingMessages, normalizedQuery, projects, sessions],
  );

  useEffect(() => {
    setActiveIndex((current) => Math.max(0, Math.min(current, results.length - 1)));
  }, [results.length]);

  useEffect(() => {
    const active = resultListRef.current?.querySelector<HTMLElement>("[aria-selected='true']");
    active?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  function chooseResult(result: SearchResult | undefined) {
    if (!result) {
      return;
    }
    onSelect(result.session.id);
    onOpenChange(false);
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing) {
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex((current) => Math.min(results.length - 1, current + 1));
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex((current) => Math.max(0, current - 1));
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      chooseResult(results[activeIndex]);
    }
  }

  const waitingForMessageResults = Boolean(
    normalizedQuery && (normalizedQuery !== normalizedDebouncedQuery || messageSearch.isFetching),
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex max-h-[min(720px,calc(95svh-1rem))] w-[min(780px,calc(100vw-2rem))] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none"
        showCloseButton={false}
        style={{ top: "max(1rem, 5svh)", translate: "-50% 0" }}
      >
        <DialogHeader className="sr-only">
          <DialogTitle>{t("rail.searchTitle")}</DialogTitle>
          <DialogDescription>{t("rail.searchPlaceholder")}</DialogDescription>
        </DialogHeader>
        <div className="flex h-14 shrink-0 items-center gap-3 border-b px-5">
          <Search className="size-5 shrink-0 text-muted-foreground" aria-hidden="true" />
          <Input
            autoFocus
            aria-label={t("rail.searchPlaceholder")}
            className="h-full rounded-none border-0 bg-transparent px-0 text-base shadow-none focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent md:text-base"
            placeholder={t("rail.searchPlaceholder")}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActiveIndex(0);
            }}
            onKeyDown={handleKeyDown}
          />
          {waitingForMessageResults ? <Spinner className="size-4 shrink-0 text-muted-foreground" /> : null}
        </div>
        <div ref={resultListRef} className="min-h-0 overflow-y-auto p-2" role="listbox">
          <div className="px-3 pt-2.5 pb-1.5 text-sm font-medium text-muted-foreground">
            {t("rail.searchSection")}
          </div>
          {results.length > 0 ? (
            <div className="space-y-0.5">
              {results.map((result, index) => {
                const active = index === activeIndex;
                const secondary = result.message ? searchExcerpt(result.message.text, highlightTerms) : "";
                return (
                  <button
                    key={result.session.id}
                    aria-selected={active}
                    className={cn(
                      "grid w-full grid-cols-[minmax(0,1fr)_auto] gap-x-3 rounded-lg px-3 text-left outline-none",
                      secondary ? "min-h-16 items-start py-2.5" : "h-10 items-center py-0",
                      active ? "bg-accent text-accent-foreground" : "hover:bg-accent/60",
                    )}
                    role="option"
                    type="button"
                    onClick={() => chooseResult(result)}
                    onMouseMove={() => setActiveIndex(index)}
                  >
                    <span className="flex min-w-0 items-start gap-2">
                      <span
                        aria-label={t(`mode.${result.session.activeMode}`)}
                        className="mt-0.5 grid size-4 shrink-0 place-items-center text-muted-foreground"
                        role="img"
                      >
                        <SessionModeIcon mode={result.session.activeMode} />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">
                          <HighlightedText
                            text={result.session.title || t("session.untitled")}
                            terms={highlightTerms}
                          />
                        </span>
                        {secondary ? (
                          <span className="mt-1 block truncate text-sm text-muted-foreground">
                            <HighlightedText text={secondary} terms={highlightTerms} />
                          </span>
                        ) : null}
                      </span>
                    </span>
                    {result.project ? (
                      <span
                        className={cn(
                          "hidden max-w-40 truncate text-sm text-muted-foreground sm:block",
                          secondary && "pt-0.5",
                        )}
                      >
                        <HighlightedText text={result.project.name} terms={highlightTerms} />
                      </span>
                    ) : null}
                  </button>
                );
              })}
            </div>
          ) : waitingForMessageResults ? (
            <div className="flex h-28 items-center justify-center text-muted-foreground">
              <Spinner className="size-5" />
            </div>
          ) : (
            <div className="flex h-28 items-center justify-center text-sm text-muted-foreground">
              {messageSearch.isError && normalizedQuery ? t("rail.searchFailed") : t("rail.searchEmpty")}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function buildSearchResults(
  sessions: Session[],
  projects: Project[],
  messages: Message[],
  normalizedQuery: string,
): SearchResult[] {
  const queryTerms = searchTerms(normalizedQuery);
  const projectByID = new Map(projects.map((project) => [project.id, project]));
  const messageBySessionID = new Map<string, Message>();
  for (const message of messages) {
    if (!messageBySessionID.has(message.sessionID)) {
      messageBySessionID.set(message.sessionID, message);
    }
  }

  return sessions
    .map((session): SearchResult | null => {
      const project = session.projectID ? projectByID.get(session.projectID) : undefined;
      const message = messageBySessionID.get(session.id);
      const title = normalizeSearchText(session.title);
      const projectText = normalizeSearchText([project?.name, ...(project?.rootDirs || [])].filter(Boolean).join(" "));
      const modelText = normalizeSearchText(`${session.provider} ${session.model}`);
      let score = 0;

      if (normalizedQuery) {
        if (title === normalizedQuery) {
          score = 0;
        } else if (title.startsWith(normalizedQuery)) {
          score = 1;
        } else if (containsSearchTerms(title, queryTerms)) {
          score = 2;
        } else if (containsSearchTerms(projectText, queryTerms)) {
          score = 3;
        } else if (containsSearchTerms(modelText, queryTerms)) {
          score = 4;
        } else if (message) {
          score = 5;
        } else {
          return null;
        }
      }
      return { session, project, message, score };
    })
    .filter((result): result is SearchResult => Boolean(result))
    .sort((left, right) => {
      if (left.score !== right.score) {
        return left.score - right.score;
      }
      return sessionActivityTime(right.session) - sessionActivityTime(left.session);
    })
    .slice(0, maxVisibleResults);
}

function normalizeSearchText(value: string) {
  return value.trim().toLocaleLowerCase();
}

function searchTerms(normalizedQuery: string) {
  return normalizedQuery.split(/\s+/).filter(Boolean);
}

function containsSearchTerms(text: string, terms: string[]) {
  return terms.length > 0 && terms.every((term) => text.includes(term));
}

function sessionActivityTime(session: Session) {
  return new Date(session.lastActivityAt || session.updatedAt || session.createdAt).getTime();
}

function searchExcerpt(text: string, terms: string[], maxLength = 120) {
  const compact = text.replace(/\s+/g, " ").trim();
  if (compact.length <= maxLength) {
    return compact;
  }
  const normalizedCompact = compact.toLocaleLowerCase();
  const matchIndexes = terms
    .map((term) => normalizedCompact.indexOf(term))
    .filter((index) => index >= 0);
  const matchIndex = matchIndexes.length > 0 ? Math.min(...matchIndexes) : -1;
  const start = Math.max(0, matchIndex < 0 ? 0 : matchIndex - Math.floor(maxLength / 3));
  const excerpt = compact.slice(start, start + maxLength).trim();
  return `${start > 0 ? "…" : ""}${excerpt}${start + maxLength < compact.length ? "…" : ""}`;
}

function normalizeHighlightTerms(terms: string[]) {
  return Array.from(new Set(terms.map(normalizeSearchText).filter(Boolean))).sort(
    (left, right) => right.length - left.length,
  );
}

function HighlightedText({ text, terms }: { text: string; terms: string[] }) {
  const ranges = matchRanges(text, terms);
  if (ranges.length === 0) {
    return text;
  }
  const content = [];
  let cursor = 0;
  for (const range of ranges) {
    if (range.start > cursor) {
      content.push(text.slice(cursor, range.start));
    }
    content.push(
      <mark
        key={`${range.start}-${range.end}`}
        className="rounded-[2px] bg-amber-200/80 text-inherit dark:bg-amber-400/30"
      >
        {text.slice(range.start, range.end)}
      </mark>,
    );
    cursor = range.end;
  }
  if (cursor < text.length) {
    content.push(text.slice(cursor));
  }
  return <>{content}</>;
}

function matchRanges(text: string, terms: string[]) {
  const normalizedText = text.toLocaleLowerCase();
  const ranges: Array<{ start: number; end: number }> = [];
  for (const term of terms) {
    let start = normalizedText.indexOf(term);
    while (start >= 0) {
      const candidate = { start, end: start + term.length };
      const overlaps = ranges.some((range) => candidate.start < range.end && candidate.end > range.start);
      if (!overlaps) {
        ranges.push(candidate);
      }
      start = normalizedText.indexOf(term, start + Math.max(1, term.length));
    }
  }
  return ranges.sort((left, right) => left.start - right.start);
}
