import { toast } from "sonner";
import type { useI18n } from "@/i18n";
import { electronBrowserBridge, electronBrowserSnapshotToTab } from "./electronBridge";

// Both Markdown links and the address bar use the native scope/confirmation
// authority. An absent result means cancellation or a reported local-file error.
export async function openLocalBrowserFile(
  request: { sessionID: string; tabID?: string; url: string },
  t: ReturnType<typeof useI18n>["t"],
) {
  const result = await electronBrowserBridge()?.openLocalFile(request);
  if (!result?.ok) {
    if (!result?.cancelled) {
      toast.error(t(result?.reason === "not_found" ? "project.browserFileUnavailable"
        : result?.reason === "denied" ? "project.browserFileAccessDenied" : "project.browserLocalFileFailed"));
    }
    return;
  }
  return electronBrowserSnapshotToTab(result.tab);
}
