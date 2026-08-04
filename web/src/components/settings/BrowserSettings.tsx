import { PasswordSettings } from "@/components/settings/PasswordSettings";
import { useI18n } from "@/i18n";

export function BrowserSettings() {
  const { t } = useI18n();
  return (
    <div className="grid min-w-0 gap-3">
      <h3 className="text-[15px] font-semibold tracking-tight">{t("browser.passwords")}</h3>
      <PasswordSettings />
    </div>
  );
}
