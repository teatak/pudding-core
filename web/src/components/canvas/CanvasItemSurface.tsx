import { useMemo } from "react";
import { Star } from "@/components/icons";

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

export function CanvasItemSurface({
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
  onActiveIndexChange: (activeIndex: number) => void;
  onGalleryLayoutChange: (layout: GalleryLayout) => void;
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
        active ? "z-10" : "pointer-events-none invisible z-0",
        backgroundClass,
      )}
    >
      <MemoCanvasContent
        galleryActiveIndex={activeIndex}
        item={item}
        token={token}
        onGalleryActiveIndexChange={onActiveIndexChange}
        onGalleryLayoutChange={onGalleryLayoutChange}
      />
    </div>
  );
}

export function CanvasItemActions({
  item,
  saving,
  token,
  onSave,
  onGalleryLayoutChange,
}: {
  item?: CanvasItem;
  saving: boolean;
  token: string;
  onSave: () => void;
  onGalleryLayoutChange: (layout: GalleryLayout) => void;
}) {
  const { t } = useI18n();
  const table = useMemo(() => item ? tableExportData(item, t) : null, [item, t]);
  const galleryLayout = item ? galleryLayoutForItem(item) : null;
  const showSave = Boolean(item && (!item.sourceSavedItemID || item.savedDirty));
  if (!galleryLayout && !table && !showSave) return null;

  return (
    <div className="no-drag-region flex items-center gap-1 rounded-lg border border-border/70 bg-background/90 p-1 text-foreground shadow-sm backdrop-blur-[2px]">
      {galleryLayout ? <GalleryLayoutControls layout={galleryLayout} onLayoutChange={onGalleryLayoutChange} /> : null}
      {table ? <TableExportMenu table={table} token={token} /> : null}
      {showSave ? (
        <Button
          aria-label={item?.sourceSavedItemID ? t("canvas.saveChanges") : t("canvas.saveWidget")}
          disabled={saving}
          size="icon-sm"

          type="button"
          variant="ghost"
          onClick={onSave}
        >
          {saving ? <Spinner className="size-3.5" /> : <Star className="size-3.5" />}
        </Button>
      ) : null}
    </div>
  );
}
