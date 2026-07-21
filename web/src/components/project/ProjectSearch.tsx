import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { searchProjectFiles, type ProjectBrowserRoot, type ProjectSearchMatch } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";

import { ProjectFileTypeIcon } from "./ProjectFileTypeIcon";
import { projectBrowserError } from "./projectErrors";

const searchDelayMs = 180;

export function ProjectSearch({
  roots,
  sessionID,
  token,
  onClose,
  onOpen,
}: {
  roots: ProjectBrowserRoot[];
  sessionID: string;
  token: string;
  onClose: () => void;
  onOpen: (match: ProjectSearchMatch) => void;
}) {
  const { t } = useI18n();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const normalizedQuery = query.trim();
  const rootNames = useMemo(() => new Map(roots.map((root) => [root.id, root.name])), [roots]);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    const timeoutID = window.setTimeout(() => setDebouncedQuery(normalizedQuery), searchDelayMs);
    return () => window.clearTimeout(timeoutID);
  }, [normalizedQuery]);

  const searchQuery = useQuery({
    enabled: Boolean(token && sessionID && debouncedQuery),
    queryKey: queryKeys.projectSearch(sessionID, debouncedQuery),
    queryFn: ({ signal }) => searchProjectFiles(token, sessionID, debouncedQuery, signal),
    retry: false,
    staleTime: 5_000,
  });
  const settled = Boolean(normalizedQuery && normalizedQuery === debouncedQuery);
  const matches = settled ? searchQuery.data?.matches || [] : [];
  const searching = Boolean(normalizedQuery && (!settled || searchQuery.isFetching));

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key === "Enter" && matches[0]) {
      event.preventDefault();
      onOpen(matches[0]);
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 border-b border-[var(--workspace-border-subtle)] p-1.5">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={inputRef}
            aria-label={t("project.browserSearch")}
            className="h-7 rounded-md bg-transparent pl-7 text-xs focus-visible:ring-0"
            placeholder={t("project.browserSearchPlaceholder")}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={handleKeyDown}
          />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {!normalizedQuery ? (
          <ProjectSearchMessage>{t("project.browserSearchHint")}</ProjectSearchMessage>
        ) : searching && matches.length === 0 ? (
          <ProjectSearchMessage><Spinner />{t("common.loading")}</ProjectSearchMessage>
        ) : settled && searchQuery.isError ? (
          <ProjectSearchMessage>{projectBrowserError(searchQuery.error, t)}</ProjectSearchMessage>
        ) : settled && matches.length === 0 ? (
          <ProjectSearchMessage>{t("project.browserSearchNoResults")}</ProjectSearchMessage>
        ) : (
          <>
            <div className="flex h-6 items-center gap-1.5 px-2 text-[11px] text-muted-foreground">
              {searching ? <Spinner className="size-3" /> : null}
              <span>{t("project.browserSearchSummary").replace("{count}", String(matches.length))}</span>
              {searchQuery.data?.resultsCapped ? <span>· {t("project.browserSearchCapped")}</span> : null}
            </div>
            {matches.map((match, index) => (
              <button
                key={`${match.rootID}:${match.path}:${match.line}:${index}`}
                className="block w-full min-w-0 border-t border-[var(--workspace-border-subtle)] px-2 py-1.5 text-left hover:bg-[var(--workspace-tree-hover-background)]"
                title={`${match.path}:${match.line}`}
                type="button"
                onClick={() => onOpen(match)}
              >
                <span className="flex min-w-0 items-center gap-1.5 text-xs">
                  <ProjectFileTypeIcon path={match.path} />
                  <span className="min-w-0 flex-1 truncate font-medium">{match.path}</span>
                  <span className="shrink-0 font-mono text-[10px] text-muted-foreground">:{match.line}</span>
                </span>
                {roots.length > 1 ? (
                  <span className="mt-0.5 block truncate pl-5 text-[10px] text-muted-foreground">{rootNames.get(match.rootID)}</span>
                ) : null}
                <span className="mt-1 block overflow-hidden pl-5 font-mono text-[11px] leading-4 text-muted-foreground">
                  <HighlightedText caseSensitive={searchQuery.data?.caseSensitive || false} query={debouncedQuery} text={match.text} />
                </span>
              </button>
            ))}
          </>
        )}
      </div>
    </div>
  );
}

function ProjectSearchMessage({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">{children}</div>;
}

function HighlightedText({ caseSensitive, query, text }: { caseSensitive: boolean; query: string; text: string }) {
  if (!query) return text;
  const source = caseSensitive ? text : text.toLocaleLowerCase();
  const needle = caseSensitive ? query : query.toLocaleLowerCase();
  const parts: ReactNode[] = [];
  let start = 0;
  while (start < text.length) {
    const index = source.indexOf(needle, start);
    if (index < 0) break;
    if (index > start) parts.push(text.slice(start, index));
    parts.push(
      <mark
        className="rounded-[2px] bg-amber-200/80 text-inherit dark:bg-amber-400/30"
        key={`${index}:${parts.length}`}
      >
        {text.slice(index, index + query.length)}
      </mark>,
    );
    start = index + query.length;
  }
  if (start < text.length) parts.push(text.slice(start));
  return parts.length > 0 ? parts : text;
}
