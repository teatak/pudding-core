import { Search } from "@/components/icons";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";

import type { Project, Session } from "@/api/client";
import { Spinner } from "@/components/Spinner";
import { SessionModeIcon } from "@/components/SessionModeIcon";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useSessionMessageSearch } from "@/hooks/useSessionMessageSearch";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import {
  buildSessionSearchResults,
  normalizeSessionSearchText,
  sessionSearchExcerpt,
  sessionSearchTerms,
  type SessionSearchResult,
} from "@/lib/sessionSearch";

const maxVisibleResults = 50;
const maxRecentResults = 8;

export type SessionSearchSelection = {
  messageRole?: "assistant" | "user";
  sessionID: string;
  turnID?: string;
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
  onSelect: (selection: SessionSearchSelection) => void;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const resultListRef = useRef<HTMLDivElement | null>(null);
  const messageSearch = useSessionMessageSearch({ active: open, query, sessions, token });
  const normalizedQuery = messageSearch.normalizedQuery;

  useEffect(() => {
    if (open) {
      return;
    }
    setQuery("");
    setActiveIndex(0);
  }, [open]);
  const highlightTerms = useMemo(
    () => normalizeHighlightTerms([normalizedQuery, ...sessionSearchTerms(normalizedQuery), ...(messageSearch.matchTerms || [])]),
    [messageSearch.matchTerms, normalizedQuery],
  );
  const results = useMemo(
    () => buildSessionSearchResults(sessions, projects, messageSearch.messages, normalizedQuery)
      .slice(0, normalizedQuery ? maxVisibleResults : maxRecentResults),
    [messageSearch.messages, normalizedQuery, projects, sessions],
  );

  useEffect(() => {
    setActiveIndex((current) => Math.max(0, Math.min(current, results.length - 1)));
  }, [results.length]);

  useEffect(() => {
    const active = resultListRef.current?.querySelector<HTMLElement>("[aria-selected='true']");
    active?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  function chooseResult(result: SessionSearchResult | undefined) {
    if (!result) {
      return;
    }
    onSelect({
      messageRole:
        result.message?.role === "assistant" || result.message?.role === "user"
          ? result.message.role
          : undefined,
      sessionID: result.session.id,
      turnID: result.message?.turnID || undefined,
    });
    onOpenChange(false);
  }

  function clearSearch() {
    setQuery("");
    setActiveIndex(0);
    window.requestAnimationFrame(() => searchInputRef.current?.focus());
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

  const waitingForMessageResults = messageSearch.waiting;

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
            ref={searchInputRef}
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
            {t(normalizedQuery ? "rail.searchResultsSection" : "rail.searchRecentSection")}
          </div>
          {results.length > 0 ? (
            <div className="space-y-0.5">
              {results.map((result, index) => {
                const active = index === activeIndex;
                const secondary = result.message ? sessionSearchExcerpt(result.message.text, highlightTerms) : "";
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
            <div className="flex h-32 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
              <span>
                {messageSearch.isError && normalizedQuery
                  ? t("rail.searchFailed")
                  : t(normalizedQuery ? "rail.searchEmpty" : "rail.searchNoSessions")}
              </span>
              {normalizedQuery ? (
                <Button size="sm" type="button" variant="outline" onClick={clearSearch}>
                  {t("rail.searchClear")}
                </Button>
              ) : null}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function normalizeHighlightTerms(terms: string[]) {
  return Array.from(new Set(terms.map(normalizeSessionSearchText).filter(Boolean))).sort(
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
