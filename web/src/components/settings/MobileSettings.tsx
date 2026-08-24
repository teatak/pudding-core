import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

import { Copy } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { createDesktopMobilePairing, type MobilePairing } from "@/lib/desktopBridge";

import { SETTINGS_NARROW_CONTENT_CLASS, SettingsSection } from "./shared";

export function MobileSettings() {
  const { locale, t } = useI18n();
  const [pairing, setPairing] = useState<MobilePairing | null>(null);
  const mutation = useMutation({
    mutationFn: createDesktopMobilePairing,
    onSuccess: setPairing,
    onError: () => toast.error(t("settings.mobile.createFailed")),
  });
  const expiresAt = pairing?.expiresAt
    ? new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(
        new Date(pairing.expiresAt),
      )
    : "";

  function copyURL() {
    if (!pairing?.url) return;
    void navigator.clipboard.writeText(pairing.url).then(() => toast.success(t("common.copied")));
  }

  return (
    <div className={SETTINGS_NARROW_CONTENT_CLASS}>
      <SettingsSection
        action={(
          <Button disabled={mutation.isPending} size="sm" type="button" onClick={() => mutation.mutate()}>
            {mutation.isPending ? <Spinner /> : null}
            {t("settings.mobile.generate")}
          </Button>
        )}
        title={t("settings.mobile.title")}
      >
        <p className="text-sm leading-6 text-muted-foreground">{t("settings.mobile.desc")}</p>
        {pairing ? (
          <div className="grid gap-4 sm:grid-cols-[16rem_1fr]">
            <div className="grid justify-items-center gap-2 rounded-lg border bg-background p-4">
              {pairing.qrDataURL ? (
                <img className="size-52 rounded-md bg-white p-2" src={pairing.qrDataURL} alt={t("settings.mobile.qrAlt")} />
              ) : null}
              <div className="text-xs text-muted-foreground">
                {expiresAt ? `${t("settings.mobile.expiresAt")} ${expiresAt}` : null}
              </div>
            </div>
            <div className="grid content-start gap-2">
              <label className="text-sm" htmlFor="pudding-mobile-pairing-url">{t("settings.mobile.url")}</label>
              <div className="flex min-w-0 gap-2">
                <Input readOnly className="font-mono text-xs" id="pudding-mobile-pairing-url" value={pairing.url} />
                <Button aria-label={t("common.copy")} size="icon" type="button" variant="outline" onClick={copyURL}>
                  <Copy />
                </Button>
              </div>
            </div>
          </div>
        ) : (
          <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
            {t("settings.mobile.empty")}
          </div>
        )}
      </SettingsSection>
    </div>
  );
}
