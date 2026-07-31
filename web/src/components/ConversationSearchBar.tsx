import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Search, X } from "@/components/icons";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";

import { searchMessagesInSession, type Message } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type { TranscriptSearchState, TranscriptSearchTarget } from "@/components/transcript/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";

const SEARCH_DELAY_MS = 180;
const SEARCH_LIMIT = 100;
const EMPTY_SEARCH_STATE: TranscriptSearchState = { terms: [] };

type ConversationSearchMatch = {
  message: Message;
  occurrenceIndex: number;
};

export function ConversationSearchBar({
  focusSignal,
  open,
  sessionID,
  token,
  onOpenChange,
  onSearchChange,
}: {
  focusSignal: number;
  open: boolean;
  sessionID: string;
  token: string;
  onOpenChange: (open: boolean) => void;
  onSearchChange: (state: TranscriptSearchState) => void;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(-1);
  const [isComposing, setIsComposing] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const appliedResultsRef = useRef("");
  const normalizedQuery = query.trim();

  useEffect(() => {
    if (!open) {
      setQuery("");
      setDebouncedQuery("");
      setActiveIndex(-1);
      setIsComposing(false);
      appliedResultsRef.current = "";
      onSearchChange(EMPTY_SEARCH_STATE);
      return;
    }
    window.requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
  }, [focusSignal, onSearchChange, open, sessionID]);

  useEffect(() => {
    if (!open || isComposing) {
      return;
    }
    const timeoutID = window.setTimeout(() => setDebouncedQuery(normalizedQuery), SEARCH_DELAY_MS);
    return () => window.clearTimeout(timeoutID);
  }, [isComposing, normalizedQuery, open]);

  const searchQuery = useQuery({
    queryKey: queryKeys.conversationSearch(sessionID, debouncedQuery),
    queryFn: () => searchMessagesInSession(token, sessionID, { query: debouncedQuery, limit: SEARCH_LIMIT }),
    enabled: Boolean(open && token && sessionID && debouncedQuery),
    retry: false,
  });
  const resultsReady = Boolean(normalizedQuery && normalizedQuery === debouncedQuery && searchQuery.isSuccess);
  const messages = resultsReady ? searchQuery.data?.messages || [] : [];
  const matches = useMemo(
    () =>
      messages.flatMap((message) =>
        Array.from(
          { length: textOccurrenceCount(message.text, normalizedQuery) },
          (_, occurrenceIndex): ConversationSearchMatch => ({ message, occurrenceIndex }),
        ),
      ),
    [messages, normalizedQuery],
  );
  const terms = useMemo(
    () =>
      Array.from(
        new Set(
          [normalizedQuery, ...(resultsReady ? searchQuery.data?.matchTerms || [] : [])]
            .map((term) => term.trim())
            .filter(Boolean),
        ),
      ).sort((left, right) => right.length - left.length),
    [normalizedQuery, resultsReady, searchQuery.data?.matchTerms],
  );

  useEffect(() => {
    if (!resultsReady) {
      return;
    }
    const signature = `${sessionID}:${normalizedQuery}:${matches
      .map((match) => `${match.message.id}:${match.occurrenceIndex}`)
      .join(",")}`;
    if (appliedResultsRef.current === signature) {
      return;
    }
    appliedResultsRef.current = signature;
    const nextIndex = matches.length - 1;
    setActiveIndex(nextIndex);
    onSearchChange({
      target: matchTarget(matches[nextIndex]),
      terms: matches.length > 0 ? terms : [],
    });
  }, [matches, normalizedQuery, onSearchChange, resultsReady, sessionID, terms]);

  function close() {
    onSearchChange(EMPTY_SEARCH_STATE);
    onOpenChange(false);
  }

  function activate(nextIndex: number) {
    if (matches.length === 0) {
      return;
    }
    const wrappedIndex = (nextIndex + matches.length) % matches.length;
    setActiveIndex(wrappedIndex);
    onSearchChange({
      target: matchTarget(matches[wrappedIndex]),
      terms,
    });
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing) {
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      activate(activeIndex + (event.shiftKey ? -1 : 1));
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      close();
    }
  }

  const failed = Boolean(
    normalizedQuery && normalizedQuery === debouncedQuery && searchQuery.isError,
  );
  const waiting = Boolean(
    normalizedQuery && !isComposing && !failed && (!resultsReady || searchQuery.isFetching),
  );
  const resultCount = matches.length;
  const resultPosition = resultCount > 0 && activeIndex >= 0 ? activeIndex + 1 : 0;

  if (!open) {
    return null;
  }

  return (
    <div
      aria-label={t("conversationSearch.open")}
      className="absolute top-3 right-5 z-40 w-[min(22rem,calc(100%-2.5rem))] overflow-hidden rounded-2xl border border-border/80 bg-popover/95 shadow-lg backdrop-blur-md"
      role="search"
    >
      <div className="flex h-12 items-center gap-2 px-3">
        <Search className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <Input
          ref={inputRef}
          aria-label={t("conversationSearch.placeholder")}
          className="h-full min-w-0 rounded-none border-0 bg-transparent px-0 shadow-none focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent"
          placeholder={t("conversationSearch.placeholder")}
          value={query}
          onCompositionEnd={(event) => {
            setIsComposing(false);
            setQuery(event.currentTarget.value);
          }}
          onCompositionStart={() => setIsComposing(true)}
          onChange={(event) => {
            setQuery(event.target.value);
            setActiveIndex(-1);
            appliedResultsRef.current = "";
            onSearchChange(EMPTY_SEARCH_STATE);
          }}
          onKeyDown={handleKeyDown}
        />
        {waiting ? <Spinner className="size-4 shrink-0 text-muted-foreground" aria-label={t("common.loading")} /> : null}
        <Button
          aria-label={t("conversationSearch.close")}
          className="size-7 shrink-0"
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={close}
        >
          <X />
        </Button>
      </div>
      <div className="flex h-10 items-center border-t border-border/70 px-2.5">
        <Button
          aria-label={t("conversationSearch.previous")}
          className="size-7"
          disabled={resultCount === 0}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => activate(activeIndex - 1)}
        >
          <ArrowUp />
        </Button>
        <Button
          aria-label={t("conversationSearch.next")}
          className="size-7"
          disabled={resultCount === 0}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={() => activate(activeIndex + 1)}
        >
          <ArrowDown />
        </Button>
        <span className="ml-auto px-1 text-xs tabular-nums text-muted-foreground" aria-live="polite">
          {failed
            ? t("conversationSearch.failed")
            : t("conversationSearch.count")
                .replace("{current}", String(resultPosition))
                .replace("{total}", String(resultCount))}
        </span>
      </div>
    </div>
  );
}

function matchTarget(match: ConversationSearchMatch | undefined): TranscriptSearchTarget | undefined {
  const message = match?.message;
  if (!match || !message?.turnID || (message.role !== "assistant" && message.role !== "user")) {
    return undefined;
  }
  return {
    messageID: message.id,
    occurrenceIndex: match.occurrenceIndex,
    role: message.role,
    turnID: message.turnID,
  };
}

function textOccurrenceCount(text: string, query: string) {
  const haystack = text.toLocaleLowerCase();
  const needle = query.toLocaleLowerCase();
  if (!needle) {
    return 0;
  }
  let count = 0;
  let offset = 0;
  while (offset <= haystack.length - needle.length) {
    const index = haystack.indexOf(needle, offset);
    if (index < 0) {
      break;
    }
    count += 1;
    offset = index + Math.max(1, needle.length);
  }
  return count;
}
