import { Maximize2, Minimize2, X } from "@/components/icons";
import { useMemo } from "react";
import { Rnd } from "react-rnd";

import {
  GalleryLayoutControls,
  MemoCanvasContent,
  TableExportMenu,
  galleryLayoutForItem,
  tableExportData,
  type GalleryLayout,
} from "@/components/canvas/CanvasItemContent";
import { asRecord, stringValue } from "@/components/canvas/canvasPayload";
import { Button } from "@/components/ui/button";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { CanvasKindIcon, titleForCanvasItem } from "./CanvasKindIcon";
import {
  MIN_CANVAS_WINDOW_HEIGHT,
  MIN_CANVAS_WINDOW_WIDTH,
  type CanvasWindowGeometry,
  type CanvasWindowPosition,
  type CanvasWindowState,
} from "./windowModel";

export function CanvasWindow({
  item,
  bounds,
  token,
  window,
  galleryActiveIndex,
  isMaximized,
  onDelete,
  onDrag,
  onDragStart,
  onDragStop,
  onFocus,
  onGalleryActiveIndexChange,
  onGalleryLayoutChange,
  onMaximize,
  onResize,
  onResizeStart,
  onResizeStop,
}: {
  item: CanvasItem;
  bounds: { w: number; h: number };
  token: string;
  window: CanvasWindowState;
  galleryActiveIndex: number;
  isMaximized: boolean;
  onDelete: () => void;
  onDrag: (position: CanvasWindowPosition) => void;
  onDragStart: () => void;
  onDragStop: (position: CanvasWindowPosition) => void;
  onFocus: () => void;
  onGalleryActiveIndexChange: (activeIndex: number) => void;
  onGalleryLayoutChange: (layout: GalleryLayout) => void;
  onMaximize: () => void;
  onResize: (geometry: CanvasWindowGeometry) => void;
  onResizeStart: () => void;
  onResizeStop: (geometry: CanvasWindowGeometry) => void;
}) {
  const { t } = useI18n();
  const title = titleForCanvasItem(item, t);
  const table = useMemo(() => tableExportData(item, t), [item, t]);
  const galleryLayout = galleryLayoutForItem(item);
  const contentKind = stringValue(asRecord(item.item)?.kind) || item.kind;
  const isMaximizedGrid = isMaximized && contentKind === "grid";
  const usesCanvasBackground = contentKind === "grid" || contentKind === "gallery";
  return (
    <Rnd
      bounds="parent"
      cancel=".canvas-window-no-drag"
      className="absolute"
      disableDragging={isMaximized}
      disableResizing={isMaximized}
      dragHandleClassName="canvas-window-drag-handle"
      maxHeight={bounds.h > 0 ? bounds.h : undefined}
      maxWidth={bounds.w > 0 ? bounds.w : undefined}
      minHeight={Math.min(MIN_CANVAS_WINDOW_HEIGHT, bounds.h || MIN_CANVAS_WINDOW_HEIGHT)}
      minWidth={Math.min(MIN_CANVAS_WINDOW_WIDTH, bounds.w || MIN_CANVAS_WINDOW_WIDTH)}
      position={isMaximized ? { x: 0, y: 0 } : { x: window.x, y: window.y }}
      size={isMaximized ? { width: "100%", height: "100%" } : { width: window.w, height: window.h }}
      style={{ zIndex: window.z }}
      onDrag={(_event, data) => onDrag({ x: data.x, y: data.y })}
      onDragStart={onDragStart}
      onDragStop={(_event, data) => onDragStop({ x: data.x, y: data.y })}
      onMouseDown={() => {
        globalThis.requestAnimationFrame(onFocus);
      }}
      onResize={(_event, _direction, ref, _delta, position) => onResize({
        x: position.x,
        y: position.y,
        w: ref.offsetWidth,
        h: ref.offsetHeight,
      })}
      onResizeStart={onResizeStart}
      onResizeStop={(_event, _direction, ref, _delta, position) => onResizeStop({
        x: position.x,
        y: position.y,
        w: ref.offsetWidth,
        h: ref.offsetHeight,
      })}
    >
      <div className={cn(
        "relative flex h-full w-full min-h-0 flex-col overflow-hidden rounded-lg text-card-foreground",
        usesCanvasBackground ? "bg-[var(--workspace-background)]" : "bg-card",
        isMaximized ? "shadow-none" : "shadow-lg",
      )}>
        <div
          className={cn(
            "canvas-window-drag-handle flex h-10 shrink-0 cursor-default items-center gap-2 border bg-card px-3",
            isMaximizedGrid ? "rounded-lg" : "rounded-t-lg",
          )}
          onDoubleClick={onMaximize}
        >
          <CanvasKindIcon kind={item.kind} size="xs" />
          <div className="min-w-0 flex-1 truncate text-sm font-medium">{title}</div>
          {galleryLayout ? <GalleryLayoutControls layout={galleryLayout} onLayoutChange={onGalleryLayoutChange} /> : null}
          {table ? <TableExportMenu table={table} token={token} /> : null}
          <Button
            aria-label={isMaximized ? t("canvas.restore") : t("canvas.maximize")}
            className="canvas-window-no-drag"
            size="icon-sm"
            variant="ghost"
            onPointerDown={(event) => event.stopPropagation()}
            onDoubleClick={(event) => event.stopPropagation()}
            onClick={(event) => { event.stopPropagation(); onMaximize(); }}
          >
            {isMaximized ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </Button>
          <Button
            aria-label={t("canvas.delete")}
            className="canvas-window-no-drag"
            size="icon-sm"
            variant="ghost"
            onPointerDown={(event) => event.stopPropagation()}
            onDoubleClick={(event) => event.stopPropagation()}
            onClick={(event) => { event.stopPropagation(); onDelete(); }}
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
        <div className={cn(
          "min-h-0 flex-1 overflow-auto rounded-b-lg",
          isMaximizedGrid ? "" : "border-x border-b",
          usesCanvasBackground ? "bg-[var(--workspace-background)]" : "bg-card",
        )}>
          <MemoCanvasContent
            item={item}
            token={token}
            galleryActiveIndex={galleryActiveIndex}
            onGalleryActiveIndexChange={onGalleryActiveIndexChange}
            onGalleryLayoutChange={onGalleryLayoutChange}
          />
        </div>
        {isMaximizedGrid ? (
          <>
            <div aria-hidden="true" className="pointer-events-none absolute bottom-0 left-0 h-4 w-4 rounded-bl-lg border-b border-l" />
            <div aria-hidden="true" className="pointer-events-none absolute right-0 bottom-0 h-4 w-4 rounded-br-lg border-r border-b" />
          </>
        ) : null}
      </div>
    </Rnd>
  );
}
