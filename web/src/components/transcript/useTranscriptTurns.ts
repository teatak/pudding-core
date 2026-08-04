import { useInfiniteQuery, useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { listTurns, type ConversationTurn } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";

import { formatDurationBetween } from "./time";

const TURNS_PAGE_SIZE = 20;
const VISIBLE_TURN_STEP = TURNS_PAGE_SIZE;
type TurnsPage = { turns: ConversationTurn[]; hasMore: boolean };
export type TurnsInfiniteData = InfiniteData<TurnsPage, string | undefined>;
const latestTurnsRefreshes = new Map<string, Promise<void>>();

export function useTranscriptTurns(token: string, sessionID: string) {
  const { locale } = useI18n();
  const queryClient = useQueryClient();
  const [visibleTurnCount, setVisibleTurnCount] = useState(VISIBLE_TURN_STEP);
  const latestRefreshRef = useRef({
    cachePresent: false,
    key: "",
    started: false,
  });
  const latestRefreshKey = `${token}\u0000${sessionID}`;
  if (latestRefreshRef.current.key !== latestRefreshKey) {
    latestRefreshRef.current = {
      cachePresent: Boolean(queryClient.getQueryData<TurnsInfiniteData>(queryKeys.turns(sessionID))?.pages.length),
      key: latestRefreshKey,
      started: false,
    };
  }
  const query = useInfiniteQuery({
    queryKey: queryKeys.turns(sessionID),
    queryFn: ({ pageParam }) => listTurns(token, sessionID, { before: pageParam, limit: TURNS_PAGE_SIZE }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => {
      if (!lastPage?.hasMore || !lastPage.turns?.length) {
        return undefined;
      }
      return lastPage.turns[0].id;
    },
    getPreviousPageParam: () => undefined,
    enabled: Boolean(token && sessionID),
    refetchOnMount: false,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    staleTime: "static",
  });
  const queryDataRef = useRef(query.data);
  const fetchNextPageRef = useRef(query.fetchNextPage);
  const revealRequestsRef = useRef(new Map<string, Promise<boolean>>());
  const sessionIDRef = useRef(sessionID);
  queryDataRef.current = query.data;
  fetchNextPageRef.current = query.fetchNextPage;
  sessionIDRef.current = sessionID;

  useEffect(() => {
    setVisibleTurnCount(VISIBLE_TURN_STEP);
  }, [sessionID]);

  useEffect(() => {
    const refresh = latestRefreshRef.current;
    if (!token || !sessionID || refresh.key !== latestRefreshKey || !refresh.cachePresent || refresh.started) {
      return;
    }
    refresh.started = true;
    void refreshLatestTurns(queryClient, token, sessionID).catch((error) => {
      console.warn("failed to refresh latest turns", error);
    });
  }, [latestRefreshKey, queryClient, sessionID, token]);

  const cachedTurnCount = useMemo(() => countTurnPages(query.data?.pages), [query.data]);
  const hasCachedHistory = cachedTurnCount > visibleTurnCount;
  const hasMoreHistory = hasCachedHistory || Boolean(query.hasNextPage);
  const loadHistory = useCallback(async () => {
    const targetVisibleCount = visibleTurnCount + VISIBLE_TURN_STEP;
    let cachedCount = countTurnPages(query.data?.pages);
    if (cachedCount < targetVisibleCount && query.hasNextPage) {
      let result = await query.fetchNextPage();
      cachedCount = countTurnPages(result.data?.pages);
      while (cachedCount < targetVisibleCount && result.hasNextPage) {
        result = await query.fetchNextPage();
        cachedCount = countTurnPages(result.data?.pages);
      }
    }
    setVisibleTurnCount((current) => Math.min(current + VISIBLE_TURN_STEP, Math.max(cachedCount, current)));
  }, [query, visibleTurnCount]);
  const revealTurn = useCallback((turnID: string) => {
    const targetSessionID = sessionIDRef.current;
    const requestKey = `${targetSessionID}:${turnID}`;
    const existing = revealRequestsRef.current.get(requestKey);
    if (existing) {
      return existing;
    }
    const request = (async () => {
      try {
        let pages = queryDataRef.current?.pages;
        while (true) {
          if (sessionIDRef.current !== targetSessionID) {
            return false;
          }
          const targetVisibleCount = visibleTurnCountForTarget(pages, turnID);
          if (targetVisibleCount !== undefined) {
            setVisibleTurnCount((current) => Math.max(current, targetVisibleCount));
            return true;
          }
          const lastPage = pages?.at(-1);
          if (!lastPage?.hasMore) {
            return false;
          }
          const previousOldestTurnID = lastPage.turns[0]?.id;
          const result = await fetchNextPageRef.current();
          pages = result.data?.pages;
          if (result.isError) {
            return false;
          }
          if (pages?.at(-1)?.turns[0]?.id === previousOldestTurnID) {
            return false;
          }
        }
      } catch {
        return false;
      }
    })().finally(() => {
      revealRequestsRef.current.delete(requestKey);
    });
    revealRequestsRef.current.set(requestKey, request);
    return request;
  }, []);

  const turns = useMemo(() => flattenVisibleTurnPages(query.data?.pages, visibleTurnCount), [query.data, visibleTurnCount]);
  const messages = useMemo(() => turns.flatMap((turn) => turn.messages), [turns]);
  const turnDurationByID = useMemo(() => {
    const out = new Map<string, string>();
    for (const turn of turns) {
      const duration = formatDurationBetween(turn.createdAt, turn.updatedAt, locale);
      if (duration) {
        out.set(turn.id, duration);
      }
    }
    return out;
  }, [locale, turns]);

  return {
    hasMoreHistory,
    isLoadingHistory: query.isFetchingNextPage,
    loadHistory,
    messages,
    query,
    revealTurn,
    turnDurationByID,
    turns,
  };
}

function refreshLatestTurns(queryClient: QueryClient, token: string, sessionID: string) {
  const refreshKey = `${token}\u0000${sessionID}`;
  const existing = latestTurnsRefreshes.get(refreshKey);
  if (existing) {
    return existing;
  }
  const request = listTurns(token, sessionID, { limit: TURNS_PAGE_SIZE })
    .then((latestPage) => {
      queryClient.setQueryData<TurnsInfiniteData>(queryKeys.turns(sessionID), (previous) =>
        mergeLatestTurnPage(previous, latestPage),
      );
    })
    .finally(() => {
      if (latestTurnsRefreshes.get(refreshKey) === request) {
        latestTurnsRefreshes.delete(refreshKey);
      }
    });
  latestTurnsRefreshes.set(refreshKey, request);
  return request;
}

function countTurnPages(pages: { turns: ConversationTurn[] }[] | undefined) {
  return pages?.reduce((sum, page) => sum + page.turns.length, 0) || 0;
}

function flattenVisibleTurnPages(pages: { turns: ConversationTurn[] }[] | undefined, visibleTurnCount: number) {
  if (!pages?.length) {
    return [];
  }
  let includedTurnCount = 0;
  let includedPageCount = 0;
  while (includedPageCount < pages.length && includedTurnCount < visibleTurnCount) {
    includedTurnCount += pages[includedPageCount].turns.length;
    includedPageCount += 1;
  }
  return flattenTurnPages(pages.slice(0, includedPageCount)).slice(-visibleTurnCount);
}

function flattenTurnPages(pages: { turns: ConversationTurn[] }[] | undefined) {
  const out: ConversationTurn[] = [];
  const indexByID = new Map<string, number>();
  for (const page of pages?.slice().reverse() || []) {
    for (const turn of page.turns) {
      const existing = indexByID.get(turn.id);
      if (existing == null) {
        indexByID.set(turn.id, out.length);
        out.push(turn);
      } else {
        out[existing] = turn;
      }
    }
  }
  return out;
}

function mergeLatestTurnPage(previous: TurnsInfiniteData | undefined, latestPage: TurnsPage) {
  if (!previous?.pages.length) {
    return previous;
  }
  const cachedTurns = flattenTurnPages(previous.pages);
  const cachedTurnIDs = new Set(cachedTurns.map((turn) => turn.id));
  if (latestPage.turns.length > 0 && !latestPage.turns.some((turn) => cachedTurnIDs.has(turn.id))) {
    return { pages: [latestPage], pageParams: [undefined] };
  }
  const turnsByID = new Map<string, ConversationTurn>();
  for (const turn of cachedTurns) {
    turnsByID.set(turn.id, turn);
  }
  for (const turn of latestPage.turns) {
    const existing = turnsByID.get(turn.id);
    if (!existing || !turnUpdateIsStale(existing, turn)) {
      turnsByID.set(turn.id, turn);
    }
  }
  const turns = sortTurnsByCreatedAt(Array.from(turnsByID.values()));
  const oldestPageHasMore = Boolean(previous.pages.at(-1)?.hasMore);
  const pages: TurnsPage[] = [];
  for (let end = turns.length; end > 0; end -= TURNS_PAGE_SIZE) {
    const start = Math.max(0, end - TURNS_PAGE_SIZE);
    pages.push({
      hasMore: start > 0 || oldestPageHasMore,
      turns: turns.slice(start, end),
    });
  }
  const pageParams = pages.map((_, index) =>
    index === 0 ? undefined : pages[index - 1]?.turns[0]?.id,
  );
  return { pages, pageParams };
}

function visibleTurnCountForTarget(
  pages: { turns: ConversationTurn[] }[] | undefined,
  turnID: string,
) {
  const turns = flattenTurnPages(pages);
  const targetIndex = turns.findIndex((turn) => turn.id === turnID);
  return targetIndex >= 0 ? turns.length - targetIndex : undefined;
}

export function upsertTurnIntoPages(previous: TurnsInfiniteData | undefined, turn: ConversationTurn) {
  if (!previous?.pages?.length) {
    return previous;
  }
  const existingPageIndex = previous.pages.findIndex((page) => page.turns.some((item) => item.id === turn.id));
  const pages = previous.pages.map((page, pageIndex) => {
    const existingIndex = page.turns.findIndex((item) => item.id === turn.id);
    if (existingIndex >= 0) {
      const existing = page.turns[existingIndex];
      if (turnUpdateIsStale(existing, turn)) {
        return page;
      }
      const turns = page.turns.slice();
      turns[existingIndex] = turn;
      return { ...page, turns };
    }
    if (existingPageIndex < 0 && pageIndex === 0) {
      const turns = sortTurnsByCreatedAt([...page.turns, turn]);
      const cappedTurns = previous.pages.length === 1 ? turns.slice(-TURNS_PAGE_SIZE) : turns;
      return {
        ...page,
        hasMore: page.hasMore || cappedTurns.length < turns.length,
        turns: cappedTurns,
      };
    }
    return page;
  });
  return { ...previous, pages };
}

function turnUpdateIsStale(existing: ConversationTurn, incoming: ConversationTurn) {
  if (existing.status !== "running" && incoming.status === "running") {
    return true;
  }
  const existingUpdatedAt = Date.parse(existing.updatedAt);
  const incomingUpdatedAt = Date.parse(incoming.updatedAt);
  return Number.isFinite(existingUpdatedAt) && Number.isFinite(incomingUpdatedAt) && incomingUpdatedAt < existingUpdatedAt;
}

function sortTurnsByCreatedAt(turns: ConversationTurn[]) {
  const indexByID = new Map<string, ConversationTurn>();
  for (const turn of turns) {
    indexByID.set(turn.id, turn);
  }
  return Array.from(indexByID.values()).sort((a, b) => {
    const createdDiff = Date.parse(a.createdAt) - Date.parse(b.createdAt);
    if (createdDiff !== 0) {
      return createdDiff;
    }
    return a.id.localeCompare(b.id);
  });
}
