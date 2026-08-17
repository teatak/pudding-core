import { ExternalLink, ShieldCheck } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useI18n } from "@/i18n";
import {
  denyComputerUsePermissionGuide,
  getComputerUsePermissionGuide,
  onComputerUsePermissionGuide,
  openDesktopPermissionSettings,
  requestDesktopPermission,
  restartDesktopApp,
  type ComputerUsePermission,
  type ComputerUsePermissionGuide as Guide,
} from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";
import { useEffect, useState } from "react";

export function ComputerUsePermissionGuide() {
  const { t } = useI18n();
  const [guide, setGuide] = useState<Guide | null>(null);
  const [opening, setOpening] = useState<ComputerUsePermission | null>(null);

  useEffect(() => {
    let alive = true;
    const unsubscribe = onComputerUsePermissionGuide((next) => {
      if (alive) setGuide(next);
    });
    void getComputerUsePermissionGuide().then((next) => {
      if (alive) setGuide(next);
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  if (!guide) return null;

  const deny = () => void denyComputerUsePermissionGuide(guide.requestID);
  const openSettings = async (permission: ComputerUsePermission) => {
    setOpening(permission);
    try {
      await requestDesktopPermission(permission);
      await openDesktopPermissionSettings(permission);
    } catch {
      // Keep the guide open so the user can retry.
    } finally {
      setOpening(null);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && deny()}>
      <DialogContent className="gap-5 sm:max-w-lg" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{guide.restartRequired ? t("computerPermission.restartTitle") : t("computerPermission.title")}</DialogTitle>
          <DialogDescription>
            {guide.restartRequired ? t("computerPermission.restartDesc") : t("computerPermission.desc")}
          </DialogDescription>
        </DialogHeader>
        <div className="divide-y divide-border/70 rounded-lg border border-border/70">
          {guide.permissions.map((item) => (
            <PermissionItem
              key={item.permission}
              allowed={item.allowed}
              opening={opening === item.permission}
              permission={item.permission}
              required={guide.required.includes(item.permission)}
              onOpen={openSettings}
            />
          ))}
        </div>
        <DialogFooter className="mx-0 mb-0 rounded-none border-0 bg-transparent p-0">
          <Button type="button" variant="ghost" onClick={deny}>{t("computerPermission.notNow")}</Button>
          {guide.restartRequired ? (
            <Button type="button" onClick={() => void restartDesktopApp()}>{t("computerPermission.restart")}</Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PermissionItem({
  allowed,
  opening,
  permission,
  required,
  onOpen,
}: {
  allowed: boolean;
  opening: boolean;
  permission: ComputerUsePermission;
  required: boolean;
  onOpen: (permission: ComputerUsePermission) => Promise<void>;
}) {
  const { t } = useI18n();
  const prefix = `computerPermission.${permission}` as const;
  return (
    <div className="grid gap-3 p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="grid min-w-0 gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium">{t(`${prefix}.title`)}</span>
          <span className={cn("rounded-md px-2 py-0.5 text-xs", allowed ? "bg-success/15 text-success" : "bg-warning/15 text-warning")}>
            {allowed ? t("settings.permissions.allowed") : t("settings.permissions.required")}
          </span>
          {!required ? <span className="text-xs text-muted-foreground">{t("computerPermission.notRequired")}</span> : null}
        </div>
        <span className="text-xs leading-5 text-muted-foreground">{t(`${prefix}.desc`)}</span>
      </div>
      {allowed ? (
        <ShieldCheck aria-label={t("settings.permissions.allowed")} className="size-5 text-success" />
      ) : (
        <Button disabled={opening} size="sm" type="button" variant="outline" onClick={() => void onOpen(permission)}>
          {opening ? <Spinner className="size-3.5" /> : <ExternalLink className="size-3.5" />}
          {t("settings.permissions.openSettings")}
        </Button>
      )}
    </div>
  );
}
