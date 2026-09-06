import { Monitor } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { useDesktopApplicationIdentity } from "@/hooks/useDesktopApplicationIdentity";
import { useI18n } from "@/i18n";
import { revealComputerPreview, type DesktopComputerPreview } from "@/lib/desktopBridge";
import { toast } from "sonner";

export function ComputerUsePip({ preview, completed }: { preview: DesktopComputerPreview; completed: boolean }) {
  const { t } = useI18n();
  const { data: identity } = useDesktopApplicationIdentity(preview.appID);
  const name = preview.name || identity?.name || t("computer.preview.application");
  const status = completed ? t("computer.preview.completed")
    : preview.status === "unavailable" ? t("computer.preview.unavailable") : t("computer.preview.running");
  return (
    <button
      type="button"
      data-computer-preview
      data-status={completed ? "complete" : preview.status}
      aria-label={`${t("computer.preview.reveal")}: ${name}`}
      title={`${preview.title || name} · ${status}`}
      disabled={completed || !preview.pid || preview.status === "unavailable"}
      style={{ maxWidth: preview.width && preview.height
        ? `min(240px, 100%, calc(min(240px, 30vh) * ${preview.width / preview.height}))`
        : "240px" }}
      className="pointer-events-auto flex w-full shrink-0 flex-col self-end overflow-hidden rounded-xl border border-border/70 bg-popover text-left text-popover-foreground shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      onClick={() => { void revealComputerPreview(preview).then(ok => { if (!ok) toast.error(t("computer.preview.revealFailed")); }).catch(() => toast.error(t("computer.preview.revealFailed"))); }}
    >
      <span className="flex h-8 min-w-0 items-center gap-2 px-2.5">
        {identity?.iconURL ? <img src={identity.iconURL} className="size-4 shrink-0 rounded-sm" alt="" /> : <Monitor className="size-4 shrink-0 text-muted-foreground" />}
        <span className="min-w-0 flex-1 truncate text-xs font-medium">{name}</span>
        <span className="sr-only">{status}</span>
        {!completed && preview.status !== "unavailable" ? <Spinner className="size-3 shrink-0 text-muted-foreground" /> : null}
      </span>
      <span className="block w-full overflow-hidden border-t border-border/60 bg-muted/30">
        {preview.imageURL ? <img alt="" draggable={false} src={preview.imageURL} width={preview.width} height={preview.height} className="block h-auto w-full" /> : (
          <span className="flex h-24 items-center justify-center text-xs text-muted-foreground">
            {preview.status === "unavailable" ? t("computer.preview.unavailable") : <Spinner className="size-5" />}
          </span>
        )}
      </span>
    </button>
  );
}
