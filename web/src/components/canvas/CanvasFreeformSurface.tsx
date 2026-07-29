import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Blocks } from "@/components/icons";
import { useEffect, useMemo, useRef, useState } from "react";

import { patchCanvasItemWindow } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type { GalleryLayout } from "@/components/canvas/CanvasItemContent";
import { CanvasWindow } from "@/components/canvas/CanvasWindow";
import { asRecord, numberValue } from "@/components/canvas/canvasPayload";
import {
  MIN_CANVAS_WINDOW_HEIGHT,
  MIN_CANVAS_WINDOW_WIDTH,
  type CanvasWindowGeometry as WindowGeometry,
  type CanvasWindowPosition as WindowPosition,
  type CanvasWindowRestoreState as WindowRestoreState,
  type CanvasWindowState as WindowState,
} from "@/components/canvas/windowModel";
import type { CanvasItem } from "@/contracts/api";

const DEFAULT_W = 680;
const DEFAULT_H = 480;
const CASCADE = 28;
const FULLSCREEN_SNAP = 12;

export function CanvasFreeformSurface({
  activeItemID,
  items,
  loading,
  sessionID,
  token,
  onActiveItemChange,
  onClose,
  onGalleryLayoutChange,
}: {
  activeItemID?: string;
  items: CanvasItem[];
  loading: boolean;
  sessionID: string;
  token: string;
  onActiveItemChange: (itemID: string) => void;
  onClose: (item: CanvasItem) => void;
  onGalleryLayoutChange: (item: CanvasItem, layout: GalleryLayout) => void;
}) {
  const queryClient = useQueryClient();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const draftWindowsRef = useRef<Record<string, WindowState>>({});
  const restoreWindowsRef = useRef<Record<string, WindowState>>({});
  const hydratedItemIDsRef = useRef<Set<string>>(new Set());
  const resizeStartWindowsRef = useRef<Record<string, WindowState>>({});
  const resizeStartRestoresRef = useRef<Record<string, WindowState | undefined>>({});
  const [containerSize, setContainerSize] = useState({ w: 0, h: 0 });
  const [draftWindows, setDraftWindows] = useState<Record<string, WindowState>>({});
  const [restoreWindows, setRestoreWindows] = useState<Record<string, WindowState>>({});
  const [galleryActiveIndices, setGalleryActiveIndices] = useState<Record<string, number>>({});

  const windows = useMemo(() => {
    const out: Record<string, WindowState> = {};
    items.forEach((item, index) => {
      out[item.id] = draftWindows[item.id] || windowFromItem(item, index);
    });
    return out;
  }, [draftWindows, items]);
  const maxZ = useMemo(() => {
    const values = Object.values(windows).map((window) => window.z);
    return values.length > 0 ? Math.max(...values) : 0;
  }, [windows]);

  const patchWindowMutation = useMutation({
    mutationFn: ({ itemID, window }: { itemID: string; window: WindowState }) =>
      patchCanvasItemWindow(token, sessionID, itemID, { window }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(sessionID) });
    },
  });

  useEffect(() => {
    const element = containerRef.current;
    if (!element) return;
    let resizeFrame = 0;
    const update = () => {
      resizeFrame = 0;
      const rect = element.getBoundingClientRect();
      const w = Math.round(rect.width);
      const h = Math.round(rect.height);
      setContainerSize((current) => (current.w === w && current.h === h ? current : { w, h }));
    };
    const scheduleUpdate = () => {
      if (resizeFrame) return;
      resizeFrame = window.requestAnimationFrame(update);
    };
    scheduleUpdate();
    const observer = new ResizeObserver(scheduleUpdate);
    observer.observe(element);
    return () => {
      observer.disconnect();
      if (resizeFrame) window.cancelAnimationFrame(resizeFrame);
      document.body.style.userSelect = "";
      document.body.style.webkitUserSelect = "";
    };
  }, []);

  useEffect(() => {
    if (loading || containerSize.w <= 0 || containerSize.h <= 0) return;
    const liveIDs = new Set(items.map((item) => item.id));
    const hydratedIDs = new Set([...hydratedItemIDsRef.current].filter((itemID) => liveIDs.has(itemID)));
    const nextDrafts = keepRecordKeys(draftWindowsRef.current, liveIDs);
    const nextRestores = { ...keepRecordKeys(restoreWindowsRef.current, liveIDs) };

    items.forEach((item) => {
      if (hydratedIDs.has(item.id)) return;
      const restore = restoreWindowFromItem(item, containerSize);
      if (restore) nextRestores[item.id] = restore;
      hydratedIDs.add(item.id);
    });

    hydratedItemIDsRef.current = hydratedIDs;
    if (Object.keys(nextDrafts).length !== Object.keys(draftWindowsRef.current).length) {
      draftWindowsRef.current = nextDrafts;
      setDraftWindows(nextDrafts);
    }
    if (!sameWindowRecords(nextRestores, restoreWindowsRef.current)) {
      restoreWindowsRef.current = nextRestores;
      setRestoreWindows(nextRestores);
    }
    setGalleryActiveIndices((current) => keepRecordKeys(current, liveIDs));
  }, [containerSize, items, loading]);

  useEffect(() => {
    if (containerSize.w <= 0 || containerSize.h <= 0) return;
    const currentDrafts = draftWindowsRef.current;
    let nextDrafts = currentDrafts;
    let draftsChanged = false;
    let nextRestores = restoreWindowsRef.current;
    let restoresChanged = false;
    const changeDraft = (itemID: string, window: WindowState) => {
      if (!draftsChanged) nextDrafts = { ...nextDrafts };
      draftsChanged = true;
      nextDrafts[itemID] = window;
    };
    const changeRestore = (itemID: string, window: WindowState) => {
      if (!restoresChanged) nextRestores = { ...nextRestores };
      restoresChanged = true;
      nextRestores[itemID] = window;
    };
    items.forEach((item, index) => {
      const current = currentDrafts[item.id] || windowFromItem(item, index);
      const fitted = nextRestores[item.id]
        ? fullscreenWindow(containerSize, current.z)
        : fitOpeningWindow(current, containerSize);
      if (!("window" in fitted)) {
        if (!sameWindow(current, fitted)) changeDraft(item.id, fitted);
        return;
      }
      if (fitted.restore) changeRestore(item.id, fitted.restore);
      if (!sameWindow(current, fitted.window)) changeDraft(item.id, fitted.window);
    });
    if (draftsChanged) {
      draftWindowsRef.current = nextDrafts;
      setDraftWindows(nextDrafts);
    }
    if (restoresChanged) {
      restoreWindowsRef.current = nextRestores;
      setRestoreWindows(nextRestores);
    }
  }, [containerSize, items, restoreWindows]);

  const setWindowDraft = (itemID: string, window: WindowState) => {
    const next = { ...draftWindowsRef.current, [itemID]: window };
    draftWindowsRef.current = next;
    setDraftWindows(next);
  };

  const liftWindow = (itemID: string) => {
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (!current) return;
    const currentMaxZ = Math.max(maxZ, ...Object.values(draftWindowsRef.current).map((window) => window.z));
    if (current.z < currentMaxZ) {
      setWindowDraft(itemID, { ...current, z: currentMaxZ + 1 });
    }
  };

  useEffect(() => {
    if (activeItemID) liftWindow(activeItemID);
  }, [activeItemID]);

  const commitWindow = (itemID: string, window: WindowState) => {
    setWindowDraft(itemID, window);
    patchWindowMutation.mutate({
      itemID,
      window: windowPayloadForPersist(window, restoreWindowsRef.current[itemID]),
    });
  };

  const updateWindowDraftGeometry = (itemID: string, geometry: Partial<WindowGeometry>) => {
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (current) setWindowDraft(itemID, clampWindow({ ...current, ...geometry }, containerSize));
  };

  const startWindowDrag = (itemID: string) => {
    liftWindow(itemID);
    setBodySelectionDisabled(true);
  };

  const stopWindowDrag = (itemID: string, position: WindowPosition) => {
    setBodySelectionDisabled(false);
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (current) commitWindow(itemID, clampWindow({ ...current, ...position }, containerSize));
  };

  const startWindowResize = (itemID: string) => {
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (!current) return;
    resizeStartWindowsRef.current = { ...resizeStartWindowsRef.current, [itemID]: current };
    resizeStartRestoresRef.current = { ...resizeStartRestoresRef.current, [itemID]: restoreWindowsRef.current[itemID] };
    liftWindow(itemID);
    restoreWindowsRef.current = withoutKey(restoreWindowsRef.current, itemID);
    setRestoreWindows(restoreWindowsRef.current);
    setBodySelectionDisabled(true);
  };

  const stopWindowResize = (itemID: string, geometry: WindowGeometry) => {
    setBodySelectionDisabled(false);
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (!current) return;
    const candidate = clampWindow({ ...current, ...geometry }, containerSize);
    const start = resizeStartWindowsRef.current[itemID] || current;
    const existingRestore = resizeStartRestoresRef.current[itemID];
    resizeStartWindowsRef.current = withoutKey(resizeStartWindowsRef.current, itemID);
    resizeStartRestoresRef.current = withoutKey(resizeStartRestoresRef.current, itemID);
    if (isNearFullscreenWindow(candidate, containerSize)) {
      const restore = existingRestore || (isNearFullscreenWindow(start, containerSize) ? undefined : clampWindow(start, containerSize));
      restoreWindowsRef.current = restore
        ? { ...restoreWindowsRef.current, [itemID]: restore }
        : withoutKey(restoreWindowsRef.current, itemID);
      setRestoreWindows(restoreWindowsRef.current);
      commitWindow(itemID, fullscreenWindow(containerSize, candidate.z));
      return;
    }
    restoreWindowsRef.current = withoutKey(restoreWindowsRef.current, itemID);
    setRestoreWindows(restoreWindowsRef.current);
    commitWindow(itemID, candidate);
  };

  const toggleMaximize = (itemID: string) => {
    const current = windows[itemID];
    if (!current) return;
    const restore = restoreWindowsRef.current[itemID];
    if (restore) {
      restoreWindowsRef.current = withoutKey(restoreWindowsRef.current, itemID);
      setRestoreWindows(restoreWindowsRef.current);
      const restored = clampWindow({ ...restore, z: maxZ + 1 }, containerSize);
      setWindowDraft(itemID, restored);
      patchWindowMutation.mutate({ itemID, window: restored });
      return;
    }
    const restoreWindow = clampWindow(current, containerSize);
    restoreWindowsRef.current = { ...restoreWindowsRef.current, [itemID]: restoreWindow };
    setRestoreWindows(restoreWindowsRef.current);
    const maximized = fullscreenWindow(containerSize, maxZ + 1);
    setWindowDraft(itemID, maximized);
    patchWindowMutation.mutate({ itemID, window: windowPayloadForPersist(maximized, restoreWindow) });
  };

  return (
    <div ref={containerRef} className="relative isolate min-h-0 flex-1 overflow-hidden">
      {!loading && items.length === 0 ? (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <Blocks className="size-12 -translate-y-6 stroke-[1.25] text-muted-foreground/40" />
        </div>
      ) : null}
      {items.map((item, index) => (
        <CanvasWindow
          key={item.id}
          bounds={containerSize}
          galleryActiveIndex={galleryActiveIndices[item.id] || 0}
          isMaximized={Boolean(restoreWindows[item.id])}
          item={item}
          token={token}
          window={windows[item.id] || windowFromItem(item, index)}
          onDelete={() => onClose(item)}
          onDrag={(position) => updateWindowDraftGeometry(item.id, position)}
          onDragStart={() => startWindowDrag(item.id)}
          onDragStop={(position) => stopWindowDrag(item.id, position)}
          onFocus={() => { onActiveItemChange(item.id); liftWindow(item.id); }}
          onGalleryActiveIndexChange={(activeIndex) => {
            setGalleryActiveIndices((current) => ({ ...current, [item.id]: activeIndex }));
          }}
          onGalleryLayoutChange={(layout) => onGalleryLayoutChange(item, layout)}
          onMaximize={() => toggleMaximize(item.id)}
          onResize={(geometry) => updateWindowDraftGeometry(item.id, geometry)}
          onResizeStart={() => startWindowResize(item.id)}
          onResizeStop={(geometry) => stopWindowResize(item.id, geometry)}
        />
      ))}
    </div>
  );
}

function setBodySelectionDisabled(disabled: boolean) {
  document.body.style.userSelect = disabled ? "none" : "";
  document.body.style.webkitUserSelect = disabled ? "none" : "";
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

function restoreWindowFromItem(item: CanvasItem, bounds: { w: number; h: number }) {
  const raw = asRecord(item.window);
  const restore = raw?.maximized === true ? asRecord(raw.restore) : undefined;
  if (!restore) return undefined;
  return clampWindow({
    x: numberValue(restore.x, 16),
    y: numberValue(restore.y, 16),
    w: numberValue(restore.w, DEFAULT_W),
    h: numberValue(restore.h, DEFAULT_H),
    z: numberValue(restore.z, numberValue(raw?.z, 1)),
  }, bounds);
}

function windowPayloadForPersist(window: WindowState, restore?: WindowState): WindowState {
  const clean = serializeWindow(window);
  return restore ? { ...clean, maximized: true, restore: serializeWindow(restore) } : clean;
}

function serializeWindow(window: WindowState): WindowRestoreState {
  return {
    x: Math.round(window.x), y: Math.round(window.y),
    w: Math.round(window.w), h: Math.round(window.h),
    z: Math.max(1, Math.round(window.z)),
  };
}

function fitOpeningWindow(window: WindowState, bounds: { w: number; h: number }) {
  const clamped = clampWindow(window, bounds);
  if (!windowOverflowsBounds(window, bounds)) return clamped;
  return { window: fullscreenWindow(bounds, window.z), restore: clamped };
}

function isNearFullscreenWindow(window: WindowState, bounds: { w: number; h: number }) {
  return bounds.w > 0 && bounds.h > 0 && window.x <= FULLSCREEN_SNAP && window.y <= FULLSCREEN_SNAP
    && Math.abs(window.x + window.w - bounds.w) <= FULLSCREEN_SNAP
    && Math.abs(window.y + window.h - bounds.h) <= FULLSCREEN_SNAP;
}

function fullscreenWindow(bounds: { w: number; h: number }, z: number): WindowState {
  return clampWindow({ x: 0, y: 0, w: bounds.w, h: bounds.h, z }, bounds);
}

function windowOverflowsBounds(window: WindowState, bounds: { w: number; h: number }) {
  return bounds.w > 0 && bounds.h > 0 && (
    window.x < 0 || window.y < 0 || window.w > bounds.w || window.h > bounds.h
    || window.x + window.w > bounds.w || window.y + window.h > bounds.h
  );
}

function clampWindow(window: WindowState, bounds = { w: 0, h: 0 }): WindowState {
  const minW = bounds.w > 0 ? Math.min(MIN_CANVAS_WINDOW_WIDTH, bounds.w) : MIN_CANVAS_WINDOW_WIDTH;
  const minH = bounds.h > 0 ? Math.min(MIN_CANVAS_WINDOW_HEIGHT, bounds.h) : MIN_CANVAS_WINDOW_HEIGHT;
  const w = Math.min(Math.max(minW, Math.round(window.w)), bounds.w > 0 ? Math.max(minW, bounds.w) : Number.POSITIVE_INFINITY);
  const h = Math.min(Math.max(minH, Math.round(window.h)), bounds.h > 0 ? Math.max(minH, bounds.h) : Number.POSITIVE_INFINITY);
  return {
    x: Math.min(Math.max(0, Math.round(window.x)), bounds.w > 0 ? Math.max(0, bounds.w - w) : Number.POSITIVE_INFINITY),
    y: Math.min(Math.max(0, Math.round(window.y)), bounds.h > 0 ? Math.max(0, bounds.h - h) : Number.POSITIVE_INFINITY),
    w, h, z: Math.max(1, Math.round(window.z)),
  };
}

function sameWindow(left: WindowState, right: WindowState) {
  return left.x === right.x && left.y === right.y && left.w === right.w && left.h === right.h && left.z === right.z;
}

function withoutKey<T>(record: Record<string, T>, key: string) {
  const next = { ...record };
  delete next[key];
  return next;
}

function keepRecordKeys<T>(record: Record<string, T>, keys: Set<string>) {
  const entries = Object.entries(record).filter(([key]) => keys.has(key));
  return entries.length === Object.keys(record).length ? record : Object.fromEntries(entries);
}

function sameWindowRecords(left: Record<string, WindowState>, right: Record<string, WindowState>) {
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return leftKeys.length === rightKeys.length
    && leftKeys.every((key) => Boolean(right[key]) && sameWindow(left[key], right[key]));
}
