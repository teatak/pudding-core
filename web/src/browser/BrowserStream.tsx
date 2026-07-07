import { hasElectronWebviewBrowser } from "@/browser/electronBridge";
import { ElectronWebviewBrowser } from "@/browser/ElectronWebviewBrowser";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

export function forgetBrowserCursor(_tabID?: string) {}

export function BrowserStream({
  token,
  item,
}: {
  token: string;
  item: CanvasItem;
}) {
  const { t } = useI18n();

  if (!hasElectronWebviewBrowser()) {
    return <div className="p-3 text-sm text-muted-foreground">{t("browser.loadFailed")}</div>;
  }

  return <ElectronWebviewBrowser token={token} item={item} />;
}
