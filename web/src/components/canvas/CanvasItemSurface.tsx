import { LibraryFavoriteButton } from "@/components/workspace/LibraryFavoriteButton";
import { memo, useMemo } from "react";
import { Save, Star, Trash2 } from "@/components/icons";
import { AppTooltip } from "@/components/AppTooltip";

import {
  GalleryLayoutControls,
  MemoCanvasContent,
  TableExportMenu,
  galleryLayoutForItem,
  tableExportData,
  type GalleryLayout,
} from "@/components/canvas/CanvasItemContent";
import { asRecord, stringValue } from "@/components/canvas/canvasPayload";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/Spinner";

export const CanvasItemSurface = memo(function CanvasItemSurface({
  active,
  activeIndex,
  item,
  token,
  onActiveIndexChange,
  onGalleryLayoutChange,
}: {
  active: boolean;
  activeIndex: number;
  item: CanvasItem;
  token: string;
  onActiveIndexChange: (itemID: string, activeIndex: number) => void;
  onGalleryLayoutChange: (item: CanvasItem, layout: GalleryLayout) => void;
}) {
  const contentKind = stringValue(asRecord(item.item)?.kind) || item.kind;
  const backgroundClass = contentKind === "grid"
    ? "bg-[var(--workspace-chrome-background)]"
    : contentKind === "gallery"
      ? "bg-[var(--workspace-background)]"
      : "bg-card";
  return (
    <div
      aria-hidden={!active}
      className={cn(
        "absolute inset-0 min-h-0 overflow-auto",
        active ? "z-10" : "hidden",
        backgroundClass,
      )}
    >
      <MemoCanvasContent
        galleryActiveIndex={activeIndex}
        item={item}
        token={token}
        onGalleryActiveIndexChange={(nextIndex) => onActiveIndexChange(item.id, nextIndex)}
        onGalleryLayoutChange={(layout) => onGalleryLayoutChange(item, layout)}
      />
    </div>
  );
});

export function CanvasItemActions({
  item,
  saving,
  token,
  onSave,
  onDelete,
  onGalleryLayoutChange,
}: {
  item?: CanvasItem;
  saving: boolean;
  token: string;
  onSave: () => void;
  onDelete: () => void;
  onGalleryLayoutChange: (layout: GalleryLayout) => void;
}) {
  const { t } = useI18n();
  const table = useMemo(() => item ? tableExportData(item, t) : null, [item, t]);
  const galleryLayout = item ? galleryLayoutForItem(item) : null;
  const showSave = Boolean(item && (!item.sourceSavedItemID || item.savedDirty));
  if (!item) return null;

  return (
    <div className="no-drag-region flex items-center gap-1 rounded-lg border border-border/70 bg-background/90 dark:bg-[var(--workspace-content-toolbar-background)] p-1 text-foreground shadow-sm backdrop-blur-[2px]">
      {galleryLayout ? <GalleryLayoutControls layout={galleryLayout} onLayoutChange={onGalleryLayoutChange} /> : null}
      {table ? <TableExportMenu table={table} token={token} /> : null}
      {showSave ? (
        <AppTooltip content={t(item.sourceSavedItemID ? "canvas.saveChanges" : "canvas.saveWidget")}><Button
          aria-label={item?.sourceSavedItemID ? t("canvas.saveChanges") : t("canvas.saveWidget")}
          disabled={saving}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={onSave}
        >
          {saving ? <Spinner className="size-3.5" /> : item.sourceSavedItemID ? <Save className="size-3.5" /> : <Star className="size-3.5" />}
        </Button></AppTooltip>
      ) : null}
      {item.sourceSavedItemID ? <LibraryFavoriteButton token={token} sessionID={item.sessionID} target={{kind:"canvas", savedItemID:item.sourceSavedItemID}} /> : null}
      <AppTooltip content={t("canvas.delete")}>
        <Button aria-label={t("canvas.delete")} size="icon-sm" variant="ghost" type="button" disabled={saving} onClick={onDelete}>
          <Trash2 className="size-3.5" />
        </Button>
      </AppTooltip>
    </div>
  );
}
