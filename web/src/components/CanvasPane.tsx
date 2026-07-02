import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { GripHorizontal, Shapes, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import {
  deleteCanvasItem,
  listCanvasItems,
  patchCanvasItemWindow,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/button";
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";

type CanvasPaneProps = {
  token: string;
  sessionID?: string;
};

type WindowState = {
  x: number;
  y: number;
  w: number;
  h: number;
  z: number;
};

type Gesture =
  | {
      type: "drag";
      itemID: string;
      startX: number;
      startY: number;
      window: WindowState;
    }
  | {
      type: "resize";
      itemID: string;
      startX: number;
      startY: number;
      window: WindowState;
    };

const MIN_W = 260;
const MIN_H = 160;
const DEFAULT_W = 420;
const DEFAULT_H = 300;
const CASCADE = 28;

export function CanvasPane({ token, sessionID }: CanvasPaneProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const actorSessionIDRef = useRef("");
  const draftWindowsRef = useRef<Record<string, WindowState>>({});
  const [containerSize, setContainerSize] = useState({ w: 0, h: 0 });
  const [draftWindows, setDraftWindows] = useState<Record<string, WindowState>>({});
  const [gesture, setGesture] = useState<Gesture | null>(null);
  useEffect(() => {
    if (sessionID) {
      actorSessionIDRef.current = sessionID;
    }
  }, [sessionID]);
  const actorSessionID = sessionID || actorSessionIDRef.current;
  const enabled = Boolean(token && actorSessionID);

  const itemsQuery = useQuery({
    enabled,
    queryKey: queryKeys.canvasItems(),
    queryFn: () => listCanvasItems(token, actorSessionID),
    placeholderData: (previous) => previous,
    staleTime: Infinity,
  });

  const items = itemsQuery.data?.items ?? [];
  const windows = useMemo(() => {
    const out: Record<string, WindowState> = {};
    items.forEach((item, index) => {
      out[item.id] = draftWindows[item.id] || windowFromItem(item, index);
    });
    return out;
  }, [draftWindows, items]);

  const maxZ = useMemo(() => {
    const zs = Object.values(windows).map((win) => win.z);
    return zs.length > 0 ? Math.max(...zs) : 0;
  }, [windows]);

  const patchWindowMutation = useMutation({
    mutationFn: ({ itemID, window }: { itemID: string; window: WindowState }) =>
      patchCanvasItemWindow(token, actorSessionID, itemID, { window }),
    onSuccess: () => {
      if (actorSessionID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems() });
      }
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (itemID: string) => deleteCanvasItem(token, actorSessionID, itemID),
    onSuccess: () => {
      if (actorSessionID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems() });
      }
    },
  });

  useEffect(() => {
    draftWindowsRef.current = draftWindows;
  }, [draftWindows]);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) {
      return;
    }
    const update = () => {
      const rect = el.getBoundingClientRect();
      setContainerSize({ w: Math.round(rect.width), h: Math.round(rect.height) });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    window.addEventListener("resize", update);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", update);
    };
  }, []);

  useEffect(() => {
    if (!gesture) {
      return;
    }
    const move = (event: PointerEvent) => {
      const dx = event.clientX - gesture.startX;
      const dy = event.clientY - gesture.startY;
      const next =
        gesture.type === "drag"
          ? { ...gesture.window, x: gesture.window.x + dx, y: gesture.window.y + dy }
          : { ...gesture.window, w: gesture.window.w + dx, h: gesture.window.h + dy };
      setDraftWindows((prev) => ({
        ...prev,
        [gesture.itemID]: clampWindow(next, containerSize),
      }));
    };
    const stop = () => {
      const current = draftWindowsRef.current[gesture.itemID] || gesture.window;
      patchWindowMutation.mutate({ itemID: gesture.itemID, window: clampWindow(current, containerSize) });
      setGesture(null);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop, { once: true });
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
    };
  }, [containerSize, gesture, patchWindowMutation]);

  const startGesture = (event: ReactPointerEvent, type: Gesture["type"], itemID: string) => {
    event.preventDefault();
    const win = windows[itemID];
    if (!win) {
      return;
    }
    const lifted = { ...win, z: maxZ + 1 };
    setDraftWindows((prev) => ({ ...prev, [itemID]: lifted }));
    setGesture({
      type,
      itemID,
      startX: event.clientX,
      startY: event.clientY,
      window: lifted,
    });
  };

  return (
    <aside className="flex h-full shrink-0 flex-col bg-sidebar text-sidebar-foreground">
      <div className="flex h-(--toolbar-h) shrink-0 items-center gap-2 pr-12 pl-4">
        <div className="text-sm font-normal">{t("canvas.title")}</div>
        {items.length > 0 ? <div className="text-xs text-muted-foreground">{items.length}</div> : null}
      </div>
      <div ref={containerRef} className="relative min-h-0 flex-1 overflow-hidden">
        {(!enabled && items.length === 0) || (itemsQuery.isLoading && items.length === 0) ? (
          <CanvasEmpty text={t("canvas.empty")} />
        ) : items.length === 0 ? (
          <CanvasEmpty text={t("canvas.empty")} />
        ) : (
          items.map((item, index) => (
            <CanvasWindow
              key={item.id}
              item={item}
              window={windows[item.id] || windowFromItem(item, index)}
              onDelete={() => deleteMutation.mutate(item.id)}
              onDragStart={(event) => startGesture(event, "drag", item.id)}
              onResizeStart={(event) => startGesture(event, "resize", item.id)}
            />
          ))
        )}
      </div>
    </aside>
  );
}

function CanvasEmpty({ text }: { text: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
      <Shapes className="h-8 w-8 text-muted-foreground/60" />
      <div className="text-sm text-muted-foreground">{text}</div>
    </div>
  );
}

function CanvasWindow({
  item,
  window,
  onDelete,
  onDragStart,
  onResizeStart,
}: {
  item: CanvasItem;
  window: WindowState;
  onDelete: () => void;
  onDragStart: (event: ReactPointerEvent) => void;
  onResizeStart: (event: ReactPointerEvent) => void;
}) {
  const { t } = useI18n();
  const title = item.title?.trim() || titleFromPayload(item.item) || item.kind || t("canvas.untitled");
  return (
    <section
      className="absolute flex min-h-0 flex-col overflow-hidden rounded-md border bg-card text-card-foreground shadow-sm"
      style={{
        left: window.x,
        top: window.y,
        width: window.w,
        height: window.h,
        zIndex: window.z,
      }}
    >
      <div
        className="flex h-9 shrink-0 cursor-grab items-center gap-2 border-b px-2 active:cursor-grabbing"
        onPointerDown={onDragStart}
      >
        <GripHorizontal className="h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1 truncate text-sm font-medium">{title}</div>
        <Button
          aria-label={t("canvas.delete")}
          size="icon-sm"
          variant="ghost"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.stopPropagation();
            onDelete();
          }}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-3">
        <CanvasContent item={item} />
      </div>
      <div
        aria-hidden="true"
        className="absolute right-0 bottom-0 h-4 w-4 cursor-nwse-resize border-r-2 border-b-2 border-muted-foreground/50"
        onPointerDown={onResizeStart}
      />
    </section>
  );
}

function CanvasContent({ item }: { item: CanvasItem }) {
  const payload = asRecord(item.item);
  const kind = typeof payload?.kind === "string" ? payload.kind : item.kind;
  if (kind === "markdown") {
    const content = stringValue(payload?.content) || stringValue(payload?.markdown) || "";
    return (
      <div className="space-y-2 text-sm leading-6 break-words">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
      </div>
    );
  }
  if (kind === "table") {
    return <CanvasTable payload={payload} />;
  }
  if (kind === "gallery") {
    return <CanvasGallery payload={payload} />;
  }
  if (kind === "form") {
    return <CanvasForm payload={payload} />;
  }
  return (
    <pre className="overflow-auto rounded bg-muted/50 p-3 text-xs whitespace-pre-wrap">
      {JSON.stringify(item.item, null, 2)}
    </pre>
  );
}

function CanvasTable({ payload }: { payload: Record<string, unknown> | undefined }) {
  const rows = Array.isArray(payload?.rows) ? payload.rows : [];
  const rawColumns = Array.isArray(payload?.columns) ? payload.columns : columnsFromRows(rows);
  const columns = rawColumns
    .map((column) => {
      const object = asRecord(column);
      if (object) {
        const key = stringValue(object.key);
        return key ? { key, label: stringValue(object.label) || key } : undefined;
      }
      const key = stringValue(column);
      return key ? { key, label: key } : undefined;
    })
    .filter((column): column is { key: string; label: string } => Boolean(column));
  return (
    <div className="overflow-auto">
      <table className="w-full min-w-max border-collapse text-left text-sm">
        <thead>
          <tr className="border-b text-muted-foreground">
            {columns.map((column) => (
              <th key={column.key} className="px-2 py-1 font-medium">
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => {
            const record = asRecord(row);
            return (
              <tr key={index} className="border-b last:border-0">
                {columns.map((column) => (
                  <td key={column.key} className="px-2 py-1 align-top">
                    {formatCell(record?.[column.key])}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function CanvasGallery({ payload }: { payload: Record<string, unknown> | undefined }) {
  const items = Array.isArray(payload?.items) ? payload.items : [];
  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(120px,1fr))] gap-2">
      {items.map((item, index) => {
        const record = asRecord(item);
        const src = stringValue(record?.src) || stringValue(record?.url);
        if (!src) {
          return null;
        }
        return (
          <figure key={`${src}-${index}`} className="overflow-hidden rounded border bg-background">
            <img alt={stringValue(record?.alt) || ""} className="aspect-video w-full object-cover" src={src} />
            {record?.caption ? (
              <figcaption className="px-2 py-1 text-xs text-muted-foreground">{stringValue(record.caption)}</figcaption>
            ) : null}
          </figure>
        );
      })}
    </div>
  );
}

function CanvasForm({ payload }: { payload: Record<string, unknown> | undefined }) {
  const fields = Array.isArray(payload?.fields) ? payload.fields : [];
  return (
    <div className="space-y-3">
      {fields.map((field, index) => {
        const record = asRecord(field);
        const label = stringValue(record?.label) || stringValue(record?.name) || `Field ${index + 1}`;
        const value = stringValue(record?.default_value) || "";
        return (
          <label key={`${label}-${index}`} className="block space-y-1 text-sm">
            <span className="text-muted-foreground">{label}</span>
            <input className="h-8 w-full rounded border bg-muted/40 px-2" readOnly value={value} />
          </label>
        );
      })}
    </div>
  );
}

function windowFromItem(item: CanvasItem, index: number): WindowState {
  const raw = asRecord(item.window);
  return clampWindow({
    x: numberValue(raw?.x, 16 + index * CASCADE),
    y: numberValue(raw?.y, 16 + index * CASCADE),
    w: numberValue(raw?.w, DEFAULT_W),
    h: numberValue(raw?.h, DEFAULT_H),
    z: numberValue(raw?.z, index + 1),
  });
}

function clampWindow(win: WindowState, bounds = { w: 0, h: 0 }): WindowState {
  const maxW = bounds.w > 0 ? Math.max(MIN_W, bounds.w) : Number.POSITIVE_INFINITY;
  const maxH = bounds.h > 0 ? Math.max(MIN_H, bounds.h) : Number.POSITIVE_INFINITY;
  const w = Math.min(Math.max(MIN_W, Math.round(win.w)), maxW);
  const h = Math.min(Math.max(MIN_H, Math.round(win.h)), maxH);
  const maxX = bounds.w > 0 ? Math.max(0, bounds.w - w) : Number.POSITIVE_INFINITY;
  const maxY = bounds.h > 0 ? Math.max(0, bounds.h - h) : Number.POSITIVE_INFINITY;
  return {
    x: Math.min(Math.max(0, Math.round(win.x)), maxX),
    y: Math.min(Math.max(0, Math.round(win.y)), maxY),
    w,
    h,
    z: Math.max(1, Math.round(win.z)),
  };
}

function titleFromPayload(value: unknown): string {
  const payload = asRecord(value);
  return stringValue(payload?.title);
}

function columnsFromRows(rows: unknown[]): string[] {
  const first = asRecord(rows[0]);
  return first ? Object.keys(first).slice(0, 8) : [];
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return undefined;
  }
  return value as Record<string, unknown>;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function formatCell(value: unknown): string {
  if (value == null) {
    return "";
  }
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value);
}
