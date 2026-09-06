import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { listBrowserTabs, listCanvasItems } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { browserQueryStaleTimeMS } from "@/browser/helpers";
import type { WorkspaceArtifact } from "@/components/workspace/types";
import { canvasWorkspaceTabKey, resolveCanvasTabs, useWorkspaceSessionUI } from "@/state/workspaceStore";

export function useSessionWorkspaceArtifacts(
  sessionID: string,
  token: string,
  subscribed = true,
) {
  const enabled = Boolean(sessionID && token);
  const workspaceUI = useWorkspaceSessionUI(sessionID);
  const browserTabsQuery = useQuery({
    enabled,
    queryKey: queryKeys.browserTabs(sessionID),
    queryFn: () => listBrowserTabs(token, sessionID),
    staleTime: browserQueryStaleTimeMS,
    subscribed,
  });
  const canvasItemsQuery = useQuery({
    enabled,
    queryKey: queryKeys.canvasItems(sessionID),
    queryFn: () => listCanvasItems(token, sessionID),
    staleTime: Infinity,
    subscribed,
  });

  return useMemo(() => {
    const canvasItems = canvasItemsQuery.data?.items || [];
    const { closedCanvasTabs } = resolveCanvasTabs(workspaceUI, canvasItems);
    const artifacts: WorkspaceArtifact[] = [
      ...(browserTabsQuery.data?.tabs || []).filter((tab) => tab.sessionID === sessionID).map((tab) => ({
        createdAt: tab.createdAt,
        faviconURL: tab.faviconURL,
        kind: "browser" as const,
        resourceID: tab.id,
        sessionID,
        title: tab.title,
        url: tab.url,
      })),
      ...canvasItems.filter((item) => !closedCanvasTabs[canvasWorkspaceTabKey(item.id)]).map((item) => ({
        createdAt: item.createdAt,
        kind: "canvas" as const,
        resourceID: item.id,
        resourceKind: item.kind,
        sessionID,
        title: item.title,
      })),
    ];
    return artifacts.sort((left, right) => (
      Date.parse(left.createdAt) - Date.parse(right.createdAt)
      || left.resourceID.localeCompare(right.resourceID)
    ));
  }, [browserTabsQuery.data?.tabs, canvasItemsQuery.data?.items, sessionID, workspaceUI]);
}
