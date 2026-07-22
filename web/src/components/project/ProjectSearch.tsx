import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { searchProjectFiles, type ProjectBrowserRoot, type ProjectSearchMatch } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { ProjectFileTypeIcon, ProjectFolderTypeIcon } from "./ProjectFileTypeIcon";
import { projectBrowserError } from "./projectErrors";
import { projectFileName, projectParentPath } from "./projectPaths";

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
  const matchGroups = useMemo(() => groupMatchesByRoot(matches), [matches]);
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
              <span>
                {t("project.browserSearchTreeSummary")
                  .replace("{files}", String(matchGroups.reduce((count, group) => count + group.files.length, 0)))
                  .replace("{count}", String(matches.length))}
              </span>
              {searchQuery.data?.resultsCapped ? <span>· {t("project.browserSearchCapped")}</span> : null}
            </div>
            {matchGroups.map((rootGroup) => (
              <SearchRootGroup
                key={`${debouncedQuery}:${rootGroup.rootID}`}
                caseSensitive={searchQuery.data?.caseSensitive || false}
                group={rootGroup}
                query={debouncedQuery}
                rootName={rootNames.get(rootGroup.rootID) || rootGroup.rootID}
                onOpen={onOpen}
              />
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

type SearchFileGroup = { path: string; matches: ProjectSearchMatch[] };
type SearchRootGroup = { rootID: string; files: SearchFileGroup[] };

function SearchRootGroup({
  caseSensitive,
  group,
  query,
  rootName,
  onOpen,
}: {
  caseSensitive: boolean;
  group: SearchRootGroup;
  query: string;
  rootName: string;
  onOpen: (match: ProjectSearchMatch) => void;
}) {
  const [open, setOpen] = useState(true);
  const files = group.files.map((file) => (
    <SearchFileGroup
      key={file.path}
      caseSensitive={caseSensitive}
      file={file}
      query={query}
      onOpen={onOpen}
    />
  ));
  const matchCount = group.files.reduce((count, file) => count + file.matches.length, 0);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <button
          className="flex h-6 w-full min-w-0 items-center gap-1 pr-2 text-left text-xs hover:bg-[var(--workspace-tree-hover-background)] hover:text-accent-foreground"
          style={{ paddingLeft: 7 }}
          type="button"
        >
          <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
          <ProjectFolderTypeIcon name={rootName} open={open} />
          <span className="min-w-0 flex-1 truncate">{rootName}</span>
          <ResultCount count={matchCount} />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>{files}</CollapsibleContent>
    </Collapsible>
  );
}

function SearchFileGroup({
  caseSensitive,
  file,
  query,
  onOpen,
}: {
  caseSensitive: boolean;
  file: SearchFileGroup;
  query: string;
  onOpen: (match: ProjectSearchMatch) => void;
}) {
  const [open, setOpen] = useState(true);
  const name = projectFileName(file.path);
  const parent = projectParentPath(file.path);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <button
          className="flex h-6 w-full min-w-0 items-center gap-1 pr-2 text-left text-xs hover:bg-[var(--workspace-tree-hover-background)] hover:text-accent-foreground"
          style={{ paddingLeft: 20 }}
          type="button"
        >
          <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
          <ProjectFileTypeIcon path={file.path} />
          <span className="min-w-0 truncate font-medium">{name}</span>
          {parent !== "." ? (
            <span className="min-w-0 flex-1 truncate text-[10px] text-muted-foreground">{parent}</span>
          ) : (
            <span className="flex-1" />
          )}
          <ResultCount count={file.matches.length} />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        {file.matches.map((match, index) => (
          <button
            key={`${match.line}:${match.lineStart}:${index}`}
            className="flex h-6 w-full min-w-0 items-center gap-2 pr-2 text-left hover:bg-[var(--workspace-tree-hover-background)] hover:text-accent-foreground"
            style={{ paddingLeft: 51 }}
            type="button"
            onClick={() => onOpen(match)}
          >
            <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground">
              <HighlightedText caseSensitive={caseSensitive} query={query} text={match.text} />
            </span>
            <span className="shrink-0 font-mono text-[10px] text-muted-foreground/70">:{match.line}</span>
          </button>
        ))}
      </CollapsibleContent>
    </Collapsible>
  );
}

function ResultCount({ count }: { count: number }) {
  return (
    <span className="min-w-5 shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-center text-[10px] leading-none tabular-nums text-muted-foreground">
      {count}
    </span>
  );
}

function groupMatchesByRoot(matches: ProjectSearchMatch[]) {
  const groups: Array<{
    rootID: string;
    files: Array<{ path: string; matches: ProjectSearchMatch[] }>;
    fileIndexByPath: Map<string, number>;
  }> = [];
  const indexByRoot = new Map<string, number>();
  for (const match of matches) {
    let rootIndex = indexByRoot.get(match.rootID);
    if (rootIndex == null) {
      rootIndex = groups.length;
      indexByRoot.set(match.rootID, rootIndex);
      groups.push({ rootID: match.rootID, files: [], fileIndexByPath: new Map() });
    }
    const rootGroup = groups[rootIndex];
    let fileIndex = rootGroup.fileIndexByPath.get(match.path);
    if (fileIndex == null) {
      fileIndex = rootGroup.files.length;
      rootGroup.fileIndexByPath.set(match.path, fileIndex);
      rootGroup.files.push({ path: match.path, matches: [] });
    }
    rootGroup.files[fileIndex].matches.push(match);
  }
  return groups.map((group) => ({ rootID: group.rootID, files: group.files }));
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
