import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { createBrowserTab, listBrowserTabs, openBrowserTab } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { browserWorkspaceTabKey, openWorkspaceTab } from "@/state/workspaceStore";
import { allowElectronBrowserTab, clearElectronBrowserSessionGate, electronBrowserBridge } from "./electronBridge";
import { upsertBrowserTab } from "./helpers";
import { openLocalBrowserFile } from "./openLocalFile";
import type { BrowserTabsData } from "./types";

// User navigation shares the same session-scoped browser tabs in every surface.
export function useOpenBrowserTab(token: string) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  return useMutation({
    mutationFn: async ({ targetSessionID, url }: { targetSessionID: string; url?: string }) => {
      if (!targetSessionID) throw new Error("browser session id missing");
      const localFile = url && /^file:/i.test(url) ? url : undefined;
      const localTab = localFile ? await openLocalBrowserFile({ sessionID: targetSessionID, url: localFile }, t) : undefined;
      if (localFile && !localTab) return;
      const existing = localTab || (url ? (await listBrowserTabs(token, targetSessionID)).tabs.find(tab => tab.url === url) : undefined);
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
