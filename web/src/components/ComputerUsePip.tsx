import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import { revealComputerPreview, type DesktopComputerPreview } from "@/lib/desktopBridge";
import { toast } from "sonner";

export function ComputerUsePip({ preview, completed }: { preview: DesktopComputerPreview; completed: boolean }) {
  const { t } = useI18n();
  const name = preview.name || t("computer.preview.application");
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
      <span className="sr-only">{status}</span>
      <span className="block w-full overflow-hidden bg-muted/30">
        {preview.imageURL ? <img alt="" draggable={false} src={preview.imageURL} width={preview.width} height={preview.height} className="block h-auto w-full" /> : (
          <span className="flex h-24 items-center justify-center text-xs text-muted-foreground">
            {preview.status === "unavailable" ? t("computer.preview.unavailable") : <Spinner className="size-5" />}
          </span>
        )}
      </span>
    </button>
  );
}
