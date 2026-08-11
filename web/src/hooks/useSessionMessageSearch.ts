import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import { searchSessionMessages, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { normalizeSessionSearchText } from "@/lib/sessionSearch";

const searchDelayMs = 180;

export function useSessionMessageSearch({
  active,
  query,
  sessions,
  token,
}: {
  active: boolean;
  query: string;
  sessions: Session[];
  token: string;
}) {
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const sessionIDs = useMemo(() => sessions.map((session) => session.id).sort(), [sessions]);
  const normalizedQuery = normalizeSessionSearchText(query);
  const normalizedDebouncedQuery = normalizeSessionSearchText(debouncedQuery);

  useEffect(() => {
    if (!active) {
      setDebouncedQuery("");
      return;
    }
    const timeoutID = window.setTimeout(() => setDebouncedQuery(query.trim()), searchDelayMs);
    return () => window.clearTimeout(timeoutID);
  }, [active, query]);

  const search = useQuery({
    queryKey: queryKeys.sessionSearch(sessionIDs, debouncedQuery),
    queryFn: () => searchSessionMessages(token, { sessionIDs, query: debouncedQuery, limit: 100 }),
    enabled: Boolean(active && token && sessionIDs.length > 0 && debouncedQuery),
    retry: false,
  });
  const settled = Boolean(normalizedQuery && normalizedQuery === normalizedDebouncedQuery);

  return {
    isError: settled && search.isError,
    matchTerms: settled ? search.data?.matchTerms : undefined,
    messages: settled ? search.data?.messages || [] : [],
    normalizedQuery,
    waiting: Boolean(normalizedQuery && (!settled || search.isFetching)),
  };
}
