import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import { useMemo } from "react";

import { listTurns, type ConversationTurn } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";

import { formatDurationBetween } from "./time";

const TURNS_PAGE_SIZE = 5;
type TurnsPage = { turns: ConversationTurn[]; hasMore: boolean };
export type TurnsInfiniteData = InfiniteData<TurnsPage, string | undefined>;

export function useTranscriptTurns(token: string, sessionID: string) {
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
  });

  const turns = useMemo(() => flattenTurnPages(query.data?.pages), [query.data]);
  const messages = useMemo(() => turns.flatMap((turn) => turn.messages), [turns]);
  const turnDurationByID = useMemo(() => {
    const out = new Map<string, string>();
    for (const page of query.data?.pages || []) {
      for (const turn of page.turns) {
        const duration = formatDurationBetween(turn.createdAt, turn.updatedAt);
        if (duration) {
          out.set(turn.id, duration);
        }
      }
    }
    return out;
  }, [query.data]);

  return { messages, query, turnDurationByID, turns };
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

export function upsertTurnIntoPages(previous: TurnsInfiniteData | undefined, turn: ConversationTurn) {
  if (!previous?.pages?.length) {
    return previous;
  }
  const existingPageIndex = previous.pages.findIndex((page) => page.turns.some((item) => item.id === turn.id));
  const pages = previous.pages.map((page, pageIndex) => {
    const existingIndex = page.turns.findIndex((item) => item.id === turn.id);
    if (existingIndex >= 0) {
      const turns = page.turns.slice();
      turns[existingIndex] = turn;
      return { ...page, turns };
    }
    if (existingPageIndex < 0 && pageIndex === 0) {
      return {
        ...page,
        turns: sortTurnsByCreatedAt([...page.turns, turn]),
      };
    }
    return page;
  });
  return { ...previous, pages };
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
