import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { createBrowserTab, listBrowserTabs, openBrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { browserWorkspaceTabKey, openWorkspaceTab } from "@/state/workspaceStore";
import { allowElectronBrowserTab, clearElectronBrowserSessionGate, electronBrowserBridge, electronBrowserSnapshotToTab } from "./electronBridge";
import { upsertBrowserTab } from "./helpers";
import type { BrowserTabsData } from "./types";

// User navigation shares the same session-scoped browser tabs in every surface.
export function useOpenBrowserTab(token: string) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  return useMutation({
    mutationFn: async ({ targetSessionID, url }: { targetSessionID: string; url?: string }) => {
      if (!targetSessionID) throw new Error("browser session id missing");
      const localFile = url?.startsWith("file:") ? url : undefined;
      const localResult = localFile ? await electronBrowserBridge()?.openLocalFile({ sessionID: targetSessionID, url: localFile }) : undefined;
      if (localFile && !localResult?.ok) {
        if (localResult?.cancelled) return;
        toast.error(t(localResult?.reason === "not_found" ? "project.browserFileUnavailable" : localResult?.reason === "denied" ? "project.browserFileAccessDenied" : "project.browserLocalFileFailed"));
        return;
      }
      const existing = localResult?.ok ? electronBrowserSnapshotToTab(localResult.tab)
        : url ? (await listBrowserTabs(token, targetSessionID)).tabs.find(tab => tab.url === url) : undefined;
      let tab = existing || await createBrowserTab(token, targetSessionID);
      const reveal = () => {
        allowElectronBrowserTab(targetSessionID, tab.id);
        clearElectronBrowserSessionGate(targetSessionID);
        queryClient.setQueryData<BrowserTabsData>(queryKeys.browserTabs(targetSessionID), current => ({
          tabs: upsertBrowserTab(current?.tabs || [], tab),
          processMode: tab.mode || current?.processMode || (electronBrowserBridge() ? "webview" : "headless"),
        }));
        openWorkspaceTab(targetSessionID, browserWorkspaceTabKey(tab.id));
      };
      reveal();
      if (url && !existing) {
        tab = await openBrowserTab(token, targetSessionID, tab.id, { url });
        reveal();
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: () => toast.error(t("browser.createFailed")),
  });
}
