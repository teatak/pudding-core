import { useI18n } from "@/i18n";
import { revealComputerPreview, type DesktopComputerPreviewFrame } from "@/lib/desktopBridge";
import { toast } from "sonner";

export function ComputerUsePip({ previews, completed }: { previews: DesktopComputerPreviewFrame[]; completed: boolean }) {
  const offset = (previews.length - 1) * 12;
  return (
    <div data-computer-preview-stack className="grid w-full shrink-0 self-end" style={{ paddingLeft: offset, paddingBottom: offset }}>
      {previews.map((preview, index) => <ComputerPreviewCard key={preview.appID} preview={preview} completed={completed}
        depth={index} order={previews.length - index} />)}
    </div>
  );
}

function ComputerPreviewCard({ preview, completed, depth, order }: {
  preview: DesktopComputerPreviewFrame; completed: boolean; depth: number; order: number;
}) {
  const { t } = useI18n();
  const name = preview.name || t("computer.preview.application");
  const status = completed ? t("computer.preview.completed") : t("computer.preview.running");
  return (
    <button
      type="button"
      data-computer-preview
      data-app-id={preview.appID}
      data-status={completed ? "complete" : preview.status}
      aria-label={`${t("computer.preview.reveal")}: ${name}`}
      title={`${preview.title || name} · ${status}`}
      disabled={completed || !preview.pid}
      style={{ gridArea: "1 / 1", alignSelf: "start", justifySelf: "end", zIndex: order,
        transform: `translate(${-depth * 12}px, ${depth * 12}px)`, maxWidth: preview.width && preview.height
        ? `min(240px, 100%, calc(min(240px, 30vh) * ${preview.width / preview.height}))`
        : "240px" }}
      className="pointer-events-auto flex w-full shrink-0 flex-col self-end overflow-hidden rounded-xl border border-border/70 bg-popover text-left text-popover-foreground shadow-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      onClick={() => { void revealComputerPreview(preview).then(ok => { if (!ok) toast.error(t("computer.preview.revealFailed")); }).catch(() => toast.error(t("computer.preview.revealFailed"))); }}
    >
      <span className="sr-only">{status}</span>
      <span className="block w-full overflow-hidden bg-muted/30">
        <img alt="" draggable={false} src={preview.imageURL} width={preview.width} height={preview.height} className="block h-auto w-full" />
      </span>
    </button>
  );
}
