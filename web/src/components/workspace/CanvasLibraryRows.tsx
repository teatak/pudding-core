import type { CanvasItem } from "@/contracts/api";
import { AppDropdownMenuItem } from "@/components/AppMenu";
import { Trash2 } from "@/components/icons";
import { useI18n } from "@/i18n";
import { LibraryResourceIcon, LibraryResourceRow } from "./LibraryResourceRow";

export type ClosedCanvasEntry = CanvasItem & { closedAt: string };

export function CanvasLibraryRows({ closedItems, onRestoreClosed, onRemoveClosed }: {
  closedItems: ClosedCanvasEntry[];
  onRestoreClosed: (entry: ClosedCanvasEntry) => void;
  onRemoveClosed: (entry: ClosedCanvasEntry) => void;
}) {
  const { t } = useI18n();
  return closedItems.map(entry => <LibraryResourceRow key={entry.id} title={entry.title || t("canvas.untitled")} description={`${t(`workspace.kind.${entry.kind}`)} · ${t("workspace.currentSession")}`}
    icon={<LibraryResourceIcon entry={{ kind: "canvas", canvasKind: entry.kind }} />} date={entry.closedAt} dateLabel={t("workspace.resourceClosedAt")} onOpen={() => onRestoreClosed(entry)}
    details={[{ label: t("workspace.recentType"), value: t(`workspace.kind.${entry.kind}`) }, { label: t("workspace.resourceSource"), value: t("workspace.currentSession") }]}
    actions={<AppDropdownMenuItem variant="destructive" onSelect={() => onRemoveClosed(entry)}><Trash2 />{t("canvas.delete")}</AppDropdownMenuItem>} />);
}
