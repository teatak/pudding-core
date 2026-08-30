import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { listBrowserTabs, type BrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useBrowserRuntimeContext } from "@/browser/browserRuntimeContext";
import { browserQueryStaleTimeMS, mergeBrowserTabs } from "@/browser/helpers";

const emptyBrowserTabs: BrowserTab[] = [];

export function useSessionBrowserTabs(
  sessionID: string,
  token: string,
  enabled = Boolean(sessionID && token),
) {
  const { requiredTabsBySession } = useBrowserRuntimeContext();
  const query = useQuery({
    enabled,
    queryKey: queryKeys.browserTabs(sessionID),
    queryFn: () => listBrowserTabs(token, sessionID),
    staleTime: browserQueryStaleTimeMS,
  });
  const requiredTabs = requiredTabsBySession[sessionID] || emptyBrowserTabs;
  const tabs = useMemo(() => mergeBrowserTabs(
    (query.data?.tabs || []).filter((tab) => tab.sessionID === sessionID),
    requiredTabs,
  ).sort((left, right) => (
    Date.parse(left.createdAt) - Date.parse(right.createdAt)
    || left.id.localeCompare(right.id)
  )), [query.data?.tabs, requiredTabs, sessionID]);

  return { query, tabs };
}
