import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Blocks,
  CalendarDays,
  ChartPie,
  Compass,
  FileText,
  Image,
  Loader2,
  Maximize2,
  Minimize2,
  Sheet,
  Trash2,
  Undo2,
  X,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Rnd } from "react-rnd";
import { toast } from "sonner";

import {
  clearClosedCanvasItems,
  closeBrowserSession,
  createBrowserTab,
  createClosedCanvasItem,
  deleteCanvasItem,
  deleteClosedCanvasItem,
  getBrowserState,
  listBrowserTabs,
  listClosedCanvasItems,
  listCanvasItems,
  patchCanvasItemWindow,
  putCanvasItem,
  syncBrowserTab,
  type BrowserTab,
  type CanvasItemPayload,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrowserCanvasTabButton } from "@/browser/BrowserCanvasTabButton";
import { BrowserToolbar } from "@/browser/BrowserToolbar";
import { ElectronWebviewBrowser } from "@/browser/ElectronWebviewBrowser";
import {
  allowElectronBrowserTab,
  cacheElectronBrowserSnapshot,
  clearElectronBrowserSessionGate,
  electronBrowserBridge,
  markElectronBrowserSessionClosed,
} from "@/browser/electronBridge";
import {
  browserPayloadFromState,
  browserPayloadHasBlankTabIntent,
  browserPayloadForItem,
  browserPayloadHasRealState,
  browserQueryStaleTimeMS,
  browserTabFaviconURL,
  browserTabTitle,
  faviconURLForPage,
  preferredBrowserTab,
  upsertBrowserTab,
} from "@/browser/helpers";
import type {
  BrowserTabsData,
  CanvasSurface,
} from "@/browser/types";
import {
  GalleryLayoutControls,
  MemoCanvasContent,
  TableExportMenu,
  galleryLayoutForItem,
  tableExportData,
  type GalleryLayout,
} from "@/components/canvas/CanvasItemContent";
import { asRecord, numberValue, stringValue, titleFromPayload } from "@/components/canvas/canvasPayload";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { CanvasItem, ClosedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { setCanvasOpen } from "@/state/canvasStore";
import { consumeBrowserReveal, useBrowserRevealEpoch } from "@/state/browserRevealStore";

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
  maximized?: boolean;
  restore?: WindowRestoreState;
};

type WindowPosition = Pick<WindowState, "x" | "y">;
type WindowGeometry = Pick<WindowState, "x" | "y" | "w" | "h">;
type WindowRestoreState = Pick<WindowState, "x" | "y" | "w" | "h" | "z">;

const SESSION_SURFACE_STORAGE_KEY = "pudding.canvas.sessionSurface.v1";
const MIN_W = 260;
const MIN_H = 160;
const DEFAULT_W = 420;
const DEFAULT_H = 300;
const CASCADE = 28;
const FULLSCREEN_SNAP = 12;
const KIND_ICON: Record<string, LucideIcon> = {
  browser: Compass,
  chart: ChartPie,
  form: FileText,
  gallery: Image,
  grid: Blocks,
  iframe: Blocks,
  image: Image,
  markdown: FileText,
  metric: ChartPie,
  table: Sheet,
  timeline: CalendarDays,
  widget: Blocks,
};
const KIND_TILE_CLASS: Record<string, string> = {
  browser: "bg-blue-600",
  chart: "bg-amber-600",
  form: "bg-violet-600",
  gallery: "bg-pink-600",
  grid: "bg-indigo-600",
  iframe: "bg-sky-600",
  image: "bg-pink-600",
  markdown: "bg-blue-600",
  metric: "bg-sky-600",
  table: "bg-emerald-600",
  timeline: "bg-cyan-600",
  widget: "bg-orange-500",
};

function CanvasKindIcon({
  kind,
  size = "sm",
}: {
  kind?: string;
  size?: "xs" | "sm";
}) {
  const Icon = KIND_ICON[kind || ""] || Blocks;
  const sizeClass = size === "xs" ? "h-[18px] w-[18px] rounded-[5px]" : "h-5 w-5 rounded-md";
  const iconClass = size === "xs" ? "h-3.5 w-3.5" : "h-4 w-4";
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex shrink-0 items-center justify-center text-white shadow-sm",
        sizeClass,
        KIND_TILE_CLASS[kind || ""] || "bg-muted-foreground",
      )}
    >
      <Icon className={iconClass} />
    </span>
  );
}

function CanvasItemIcon({ item, size = "sm" }: { item: CanvasItem; size?: "xs" | "sm" }) {
  const browserPayload = browserPayloadForItem(item);
  const faviconURL = browserPayload?.faviconURL || (browserPayload?.url ? faviconURLForPage(browserPayload.url) : "");
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [faviconURL]);

  if (faviconURL && !failed) {
    const sizeClass = size === "xs" ? "h-[18px] w-[18px] rounded-[5px]" : "h-5 w-5 rounded-md";
    return (
      <span
        aria-hidden="true"
        className={cn("inline-flex shrink-0 items-center justify-center overflow-hidden", sizeClass)}
      >
        <img
          alt=""
          className="h-full w-full object-cover"
          draggable={false}
          src={faviconURL}
          onError={() => setFailed(true)}
        />
      </span>
    );
  }

  return <CanvasKindIcon kind={item.kind} size={size} />;
}

export function CanvasPane({ token, sessionID }: CanvasPaneProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const actorSessionIDRef = useRef("");
  const currentActorSessionIDRef = useRef("");
  const canvasSessionStateRef = useRef("");
  const draftWindowsRef = useRef<Record<string, WindowState>>({});
  const restoreWindowsRef = useRef<Record<string, WindowState>>({});
  const restoredWindowHydrationRef = useRef("");
  const seenCanvasItemIDsRef = useRef<Set<string>>(new Set());
  const hasSeenCanvasItemsRef = useRef(false);
  const sessionSurfaceRef = useRef<Record<string, CanvasSurface>>(readSessionSurfaces());
  const resizeStartWindowsRef = useRef<Record<string, WindowState>>({});
  const resizeStartRestoresRef = useRef<Record<string, WindowState | undefined>>({});
  const browserCloseEpochRef = useRef<Record<string, number>>({});
  const closingBrowserSessionsRef = useRef<Record<string, boolean>>({});
  const browserSyncTimersRef = useRef<Record<string, number>>({});
  const [containerSize, setContainerSize] = useState({ w: 0, h: 0 });
  const [draftWindows, setDraftWindows] = useState<Record<string, WindowState>>({});
  const [browserActive, setBrowserActive] = useState(false);
  const [galleryActiveIndices, setGalleryActiveIndices] = useState<Record<string, number>>({});
  const [restoreWindows, setRestoreWindows] = useState<Record<string, WindowState>>({});
  const [closingBrowserSessions, setClosingBrowserSessions] = useState<Record<string, boolean>>({});
  const [canvasLibraryOpen, setCanvasLibraryOpen] = useState(false);
  closingBrowserSessionsRef.current = closingBrowserSessions;
  useEffect(() => {
    if (sessionID) {
      actorSessionIDRef.current = sessionID;
    }
  }, [sessionID]);
  const actorSessionID = sessionID || actorSessionIDRef.current;
  currentActorSessionIDRef.current = actorSessionID;
  const enabled = Boolean(token && actorSessionID);
  const rememberSessionSurface = (targetSessionID: string, surface: CanvasSurface) => {
    sessionSurfaceRef.current = { ...sessionSurfaceRef.current, [targetSessionID]: surface };
    writeSessionSurfaces(sessionSurfaceRef.current);
  };
  const clearBrowserSyncTimers = (targetSessionID: string) => {
    const prefix = `${targetSessionID}:`;
    Object.entries(browserSyncTimersRef.current).forEach(([key, timer]) => {
      if (key.startsWith(prefix)) {
        window.clearTimeout(timer);
        delete browserSyncTimersRef.current[key];
      }
    });
  };
  const setActiveSurface = (surface: CanvasSurface) => {
    if (actorSessionID) {
      rememberSessionSurface(actorSessionID, surface);
    }
    setBrowserActive(surface === "browser");
  };

  useEffect(() => {
    if (!actorSessionID || canvasSessionStateRef.current === actorSessionID) {
      return;
    }
    canvasSessionStateRef.current = actorSessionID;
    resizeStartWindowsRef.current = {};
    resizeStartRestoresRef.current = {};
    setBrowserActive(sessionSurfaceRef.current[actorSessionID] === "browser");
  }, [actorSessionID]);

  const itemsQuery = useQuery({
    enabled,
    queryKey: queryKeys.canvasItems(actorSessionID),
    queryFn: () => listCanvasItems(token, actorSessionID),
    staleTime: Infinity,
  });
  const closedItemsQuery = useQuery({
    enabled,
    queryKey: queryKeys.closedCanvasItems(actorSessionID),
    queryFn: () => listClosedCanvasItems(token, actorSessionID),
    staleTime: 30_000,
  });

  const allItems = itemsQuery.data?.items ?? [];
  const items = useMemo(() => allItems.filter((item) => !canvasItemIsBrowser(item)), [allItems]);
  const closedItems = useMemo(
    () => (closedItemsQuery.data?.items ?? []).filter((item) => !closedCanvasItemIsBrowser(item)),
    [closedItemsQuery.data?.items],
  );
  const browserClosing = Boolean(actorSessionID && closingBrowserSessions[actorSessionID]);
  const browserRevealEpoch = useBrowserRevealEpoch(actorSessionID);
  const browserStateQuery = useQuery({
    enabled,
    queryKey: actorSessionID ? queryKeys.browserState(actorSessionID) : ["browser", "missing-session", "state"],
    queryFn: () => {
      if (!actorSessionID) {
        throw new Error("browser session id missing");
      }
      return getBrowserState(token, actorSessionID);
    },
    staleTime: browserQueryStaleTimeMS,
  });
  const browserState = browserStateQuery.data?.sessionID === actorSessionID ? browserStateQuery.data : undefined;
  const browserPayload = browserClosing ? null : browserPayloadFromState(browserState);
  const browserTabsQuery = useQuery({
    enabled,
    queryKey: actorSessionID ? queryKeys.browserTabs(actorSessionID) : ["browser", "missing-session"],
    queryFn: () => {
      if (!actorSessionID) {
        throw new Error("browser session id missing");
      }
      return listBrowserTabs(token, actorSessionID);
    },
    staleTime: browserQueryStaleTimeMS,
  });
  const browserTabs = browserClosing ? [] : (browserTabsQuery.data?.tabs ?? []).filter((tab) => tab.sessionID === actorSessionID);
  const activeBrowserTab = preferredBrowserTab(browserTabs, browserPayload);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge || !enabled || !actorSessionID) {
      return;
    }
    return bridge.onUpdated((snapshot) => {
      if (snapshot.sessionID !== actorSessionID) {
        return;
      }
      if (closingBrowserSessionsRef.current[snapshot.sessionID]) {
        return;
      }
      const tab = cacheElectronBrowserSnapshot(queryClient, snapshot, actorSessionID);
      if (!tab) {
        return;
      }
      const key = `${tab.sessionID}:${tab.id}`;
      window.clearTimeout(browserSyncTimersRef.current[key]);
      browserSyncTimersRef.current[key] = window.setTimeout(() => {
        delete browserSyncTimersRef.current[key];
        if (closingBrowserSessionsRef.current[tab.sessionID]) {
          return;
        }
        void syncBrowserTab(token, tab.sessionID, tab.id, {
          targetID: tab.targetID,
          url: tab.url,
          title: tab.title,
          faviconURL: tab.faviconURL,
          canGoBack: tab.canGoBack,
          canGoForward: tab.canGoForward,
        }).catch(() => undefined);
      }, 250);
    });
  }, [actorSessionID, enabled, queryClient, token]);

  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge || !enabled || !actorSessionID || browserClosing) {
      return;
    }
    let disposed = false;
    void bridge
      .listTabs({ sessionID: actorSessionID })
      .then((result) => {
        if (disposed || currentActorSessionIDRef.current !== actorSessionID || closingBrowserSessionsRef.current[actorSessionID]) {
          return;
        }
        result.tabs
          .filter((snapshot) => snapshot.sessionID === actorSessionID)
          .forEach((snapshot) => cacheElectronBrowserSnapshot(queryClient, snapshot, actorSessionID));
      })
      .catch(() => undefined);
    return () => {
      disposed = true;
    };
  }, [actorSessionID, browserClosing, enabled, queryClient]);

  useEffect(() => {
    return () => {
      Object.values(browserSyncTimersRef.current).forEach((timer) => window.clearTimeout(timer));
      browserSyncTimersRef.current = {};
    };
  }, []);

  const selectCanvasSurface = () => {
    setActiveSurface("canvas");
  };
  const hasBrowserState = Boolean(activeBrowserTab || browserPayloadHasRealState(browserPayload) || browserPayloadHasBlankTabIntent(browserPayload));
  const browserTabTitleText = activeBrowserTab
    ? browserTabTitle(activeBrowserTab, browserPayload?.title || t("browser.newTab"), t("browser.newTab"))
    : hasBrowserState
      ? browserPayload?.title || t("browser.newTab")
      : "";
  const browserTabFaviconURLText = activeBrowserTab
    ? browserTabFaviconURL(activeBrowserTab)
    : hasBrowserState
      ? browserPayload?.faviconURL || (browserPayload?.url ? faviconURLForPage(browserPayload.url) : "")
      : "";

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

  useEffect(() => {
    if (
      !enabled ||
      itemsQuery.isLoading ||
      containerSize.w <= 0 ||
      containerSize.h <= 0 ||
      restoredWindowHydrationRef.current === "global"
    ) {
      return;
    }
    restoredWindowHydrationRef.current = "global";
    const restored: Record<string, WindowState> = {};
    items.forEach((item) => {
      const restore = restoreWindowFromItem(item, containerSize);
      if (restore) {
        restored[item.id] = restore;
      }
    });
    restoreWindowsRef.current = restored;
    setRestoreWindows(restored);
  }, [containerSize, enabled, items, itemsQuery.isLoading]);

  const patchWindowMutation = useMutation({
    mutationFn: ({ itemID, window }: { itemID: string; window: WindowState }) =>
      patchCanvasItemWindow(token, actorSessionID, itemID, { window }),
    onSuccess: () => {
      if (actorSessionID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
      }
    },
  });
  const galleryLayoutMutation = useMutation({
    mutationFn: ({ item, layout }: { item: CanvasItem; layout: GalleryLayout }) => {
      const payload = asRecord(item.item) || {};
      const title = titleForItem(item, t);
      const kind = stringValue(payload.kind) || item.kind;
      return putCanvasItem(token, actorSessionID, item.id, {
        id: item.id,
        kind,
        title,
        item: { ...payload, kind, title, layout },
        window: draftWindowsRef.current[item.id] || item.window,
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
    },
    onError: () => {
      toast.error(t("canvas.galleryLayoutFailed"));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async (item: CanvasItem) => {
      await createClosedCanvasItem(token, actorSessionID, {
        sourceItemID: item.id,
        kind: item.kind,
        title: titleForItem(item, t),
        item: item.item,
        window: draftWindowsRef.current[item.id] || item.window,
        closedAt: new Date().toISOString(),
      });
      await deleteCanvasItem(token, actorSessionID, item.id);
    },
    onMutate: (item) => {
      restoreWindowsRef.current = withoutKey(restoreWindowsRef.current, item.id);
      setRestoreWindows(restoreWindowsRef.current);
      setDraftWindows((prev) => withoutKey(prev, item.id));
      setGalleryActiveIndices((prev) => withoutKey(prev, item.id));
    },
    onError: (_error, item) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) });
      toast.error(t("canvas.closeFailed"));
    },
    onSuccess: (_result, item) => {
      if (actorSessionID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) });
      }
      const remainingWindowCount = items.filter((entry) => entry.id !== item.id).length;
      if (remainingWindowCount === 0 && !browserActive) {
        setCanvasOpen(false);
      }
    },
  });

  const restoreMutation = useMutation({
    mutationFn: async (entry: ClosedCanvasItem) => {
      const item = await putCanvasItem(token, actorSessionID, entry.sourceItemID, canvasPayloadFromClosedItem(entry));
      await deleteClosedCanvasItem(token, actorSessionID, entry.id);
      return item;
    },
    onSuccess: (_item, entry) => {
      const restoredWindow = clampWindow({ ...windowFromClosedItem(entry), z: maxZ + 1 }, containerSize);
      selectCanvasSurface();
      setDraftWindows((prev) => ({ ...prev, [entry.sourceItemID]: restoredWindow }));
      if (actorSessionID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) });
      }
    },
    onError: () => {
      toast.error(t("canvas.restoreFailed"));
    },
  });

  const restoreClosedItem = (entry: ClosedCanvasItem) => {
    if (windows[entry.sourceItemID]) {
      void deleteClosedCanvasItem(token, actorSessionID, entry.id).then(() =>
        queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) }),
      );
      selectCanvasSurface();
      focusWindow(entry.sourceItemID);
      return;
    }
    restoreMutation.mutate(entry);
  };

  const removeClosedMutation = useMutation({
    mutationFn: (entry: ClosedCanvasItem) => deleteClosedCanvasItem(token, actorSessionID, entry.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) });
    },
  });

  const clearClosedMutation = useMutation({
    mutationFn: () => clearClosedCanvasItems(token, actorSessionID),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) });
    },
  });
  const createBrowserTabMutation = useMutation({
    mutationFn: async ({ targetSessionID, fresh }: { targetSessionID: string; fresh?: boolean }) => {
      if (!targetSessionID) {
        throw new Error("browser session id missing");
      }
      let startCloseEpoch = browserCloseEpochRef.current[targetSessionID] || 0;
      if (fresh) {
        startCloseEpoch += 1;
        browserCloseEpochRef.current = {
          ...browserCloseEpochRef.current,
          [targetSessionID]: startCloseEpoch,
        };
      }
      const tab = await createBrowserTab(token, targetSessionID);
      return { fresh: Boolean(fresh), sessionID: targetSessionID, startCloseEpoch, tab };
    },
    onSuccess: ({ fresh, sessionID: targetSessionID, startCloseEpoch, tab }) => {
      const closedAfterRequest = (browserCloseEpochRef.current[targetSessionID] || 0) > startCloseEpoch;
      if (closedAfterRequest || (closingBrowserSessionsRef.current[targetSessionID] && !fresh)) {
        queryClient.setQueryData(queryKeys.browserState(targetSessionID), { hasState: false, sessionID: targetSessionID });
        queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), { tabs: [], processMode: "headless" });
        return;
      }
      const title = browserTabTitle(tab, t("browser.newTab"), t("browser.newTab"));
      const faviconURL = browserTabFaviconURL(tab);
      allowElectronBrowserTab(targetSessionID, tab.id);
      clearElectronBrowserSessionGate(targetSessionID);
      setClosingBrowserSessions((prev) => withoutKey(prev, targetSessionID));
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), (current: BrowserTabsData | undefined) => ({
        tabs: upsertBrowserTab(current?.tabs || [], tab),
        processMode: tab.mode || current?.processMode || "headless",
      }));
      queryClient.setQueryData(queryKeys.browserState(targetSessionID), {
        hasState: true,
        sessionID: targetSessionID,
        tabID: tab.id,
        url: tab.url,
        title,
        faviconURL,
        mode: tab.mode,
        processMode: tab.mode || "headless",
        createdAt: tab.createdAt,
        updatedAt: tab.updatedAt,
      });
      if (currentActorSessionIDRef.current === targetSessionID && sessionSurfaceRef.current[targetSessionID] === "browser") {
        setBrowserActive(true);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
    },
    onError: () => {
      toast.error(t("browser.createFailed"));
    },
  });

  useEffect(() => {
    if (
      !enabled ||
      !actorSessionID ||
      !browserActive ||
      browserClosing ||
      createBrowserTabMutation.isPending ||
      browserStateQuery.isFetching ||
      browserTabsQuery.isFetching ||
      hasBrowserState
    ) {
      return;
    }
    rememberSessionSurface(actorSessionID, "canvas");
    setBrowserActive(false);
  }, [
    actorSessionID,
    browserActive,
    browserClosing,
    browserStateQuery.isFetching,
    browserTabsQuery.isFetching,
    createBrowserTabMutation.isPending,
    enabled,
    hasBrowserState,
  ]);

  useEffect(() => {
    if (!enabled || !actorSessionID || browserActive || browserClosing || !hasBrowserState || items.length > 0) {
      return;
    }
    rememberSessionSurface(actorSessionID, "browser");
    setBrowserActive(true);
  }, [actorSessionID, browserActive, browserClosing, enabled, hasBrowserState, items.length]);

  const activateBrowserSurface = () => {
    if (!actorSessionID) {
      return;
    }
    const fresh = Boolean(closingBrowserSessionsRef.current[actorSessionID]);
    setActiveSurface("browser");
    if (!activeBrowserTab && !hasBrowserState && !createBrowserTabMutation.isPending) {
      createBrowserTabMutation.mutate({ targetSessionID: actorSessionID, fresh });
    }
  };

  useEffect(() => {
    if (!enabled || !actorSessionID || browserClosing || browserRevealEpoch <= 0) {
      return;
    }
    setActiveSurface("browser");
    consumeBrowserReveal(actorSessionID, browserRevealEpoch);
  }, [actorSessionID, browserClosing, browserRevealEpoch, enabled]);

  const browserCloseMutation = useMutation({
    mutationFn: async (targetSessionID: string) => {
      if (!targetSessionID) {
        throw new Error("browser session id missing");
      }
      await closeBrowserSession(token, targetSessionID);
      return { sessionID: targetSessionID };
    },
    onMutate: async (targetSessionID: string) => {
      if (!targetSessionID) {
        return;
      }
      const closeEpoch = (browserCloseEpochRef.current[targetSessionID] || 0) + 1;
      browserCloseEpochRef.current = {
        ...browserCloseEpochRef.current,
        [targetSessionID]: closeEpoch,
      };
      clearBrowserSyncTimers(targetSessionID);
      await Promise.all([
        queryClient.cancelQueries({ queryKey: queryKeys.browserState(targetSessionID) }),
        queryClient.cancelQueries({ queryKey: queryKeys.browserTabs(targetSessionID) }),
      ]);
      markElectronBrowserSessionClosed(targetSessionID);
      setClosingBrowserSessions((prev) => ({ ...prev, [targetSessionID]: true }));
      rememberSessionSurface(targetSessionID, "canvas");
      if (currentActorSessionIDRef.current === targetSessionID) {
        setBrowserActive(false);
      }
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), { tabs: [], processMode: "headless" });
      queryClient.setQueryData(queryKeys.browserState(targetSessionID), { hasState: false, sessionID: targetSessionID });
      return { closeEpoch, sessionID: targetSessionID };
    },
    onSuccess: ({ sessionID: targetSessionID }, _targetSessionID, context) => {
      const closeEpoch = context?.closeEpoch || 0;
      if ((browserCloseEpochRef.current[targetSessionID] || 0) > closeEpoch) {
        return;
      }
      rememberSessionSurface(targetSessionID, "canvas");
      if (currentActorSessionIDRef.current === targetSessionID) {
        setBrowserActive(false);
      }
      queryClient.setQueryData(queryKeys.browserTabs(targetSessionID), { tabs: [], processMode: "headless" });
      queryClient.setQueryData(queryKeys.browserState(targetSessionID), { hasState: false, sessionID: targetSessionID });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
      setClosingBrowserSessions((prev) => withoutKey(prev, targetSessionID));
      if (items.length === 0) {
        setCanvasOpen(false);
      }
    },
    onError: (_error, targetSessionID) => {
      if (targetSessionID) {
        clearElectronBrowserSessionGate(targetSessionID);
        setClosingBrowserSessions((prev) => withoutKey(prev, targetSessionID));
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(targetSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(targetSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(targetSessionID) });
      }
      toast.error(t("browser.releaseFailed"));
    },
  });

  useEffect(() => {
    if (!enabled || !actorSessionID || browserClosing || !activeBrowserTab) {
      return;
    }
    clearElectronBrowserSessionGate(actorSessionID);
  }, [activeBrowserTab, actorSessionID, browserClosing, enabled]);

  useEffect(() => {
    draftWindowsRef.current = draftWindows;
  }, [draftWindows]);

  useEffect(() => {
    if (items.length === 0) {
      if (!itemsQuery.isLoading) {
        seenCanvasItemIDsRef.current = new Set();
        hasSeenCanvasItemsRef.current = true;
      }
      return;
    }
    if (containerSize.w <= 0 || containerSize.h <= 0) {
      return;
    }
    const seenIDs = seenCanvasItemIDsRef.current;
    const shouldPromoteNewItems = hasSeenCanvasItemsRef.current;
    const newItemIDs = new Set<string>();
    if (shouldPromoteNewItems) {
      items.forEach((item) => {
        if (!seenIDs.has(item.id)) {
          newItemIDs.add(item.id);
        }
      });
    }
    seenCanvasItemIDsRef.current = new Set(items.map((item) => item.id));
    hasSeenCanvasItemsRef.current = true;
    if (newItemIDs.size > 0) {
      selectCanvasSurface();
    }

    setDraftWindows((prev) => {
      let changed = false;
      let nextZ = Math.max(
        maxZ,
        ...Object.values(prev).map((window) => window.z),
        ...items.map((item, index) => windowFromItem(item, index).z),
      );
      const next = { ...prev };
      items.forEach((item, index) => {
        const current = prev[item.id] || windowFromItem(item, index);
        let fitted = restoreWindows[item.id]
          ? clampWindow({ ...current, x: 0, y: 0, w: containerSize.w, h: containerSize.h }, containerSize)
          : clampWindow(current, containerSize);
        if (newItemIDs.has(item.id)) {
          nextZ += 1;
          fitted = { ...fitted, z: nextZ };
        }
        if (!sameWindow(current, fitted)) {
          next[item.id] = fitted;
          changed = true;
        }
      });
      return changed ? next : prev;
    });
  }, [containerSize, items, itemsQuery.isLoading, maxZ, restoreWindows]);

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

  const setBodySelectionDisabled = (disabled: boolean) => {
    document.body.style.userSelect = disabled ? "none" : "";
    document.body.style.webkitUserSelect = disabled ? "none" : "";
  };

  const setWindowDraft = (itemID: string, win: WindowState) => {
    draftWindowsRef.current = { ...draftWindowsRef.current, [itemID]: win };
    setDraftWindows(draftWindowsRef.current);
  };

  const liftWindow = (itemID: string): WindowState | undefined => {
    const win = draftWindowsRef.current[itemID] || windows[itemID];
    if (!win) {
      return undefined;
    }
    const currentMaxZ = Math.max(maxZ, ...Object.values(draftWindowsRef.current).map((window) => window.z));
    if (win.z >= currentMaxZ) {
      return win;
    }
    const lifted = { ...win, z: currentMaxZ + 1 };
    setWindowDraft(itemID, lifted);
    return lifted;
  };

  const focusWindow = (itemID: string) => {
    liftWindow(itemID);
  };

  const commitWindow = (itemID: string, win: WindowState) => {
    setWindowDraft(itemID, win);
    patchWindowMutation.mutate({ itemID, window: windowPayloadForPersist(win, restoreWindowsRef.current[itemID]) });
  };

  const updateWindowDraftGeometry = (itemID: string, geometry: Partial<WindowGeometry>) => {
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (!current) {
      return;
    }
    setWindowDraft(itemID, clampWindow({ ...current, ...geometry }, containerSize));
  };

  const startWindowDrag = (itemID: string) => {
    liftWindow(itemID);
    setBodySelectionDisabled(true);
  };

  const stopWindowDrag = (itemID: string, position: WindowPosition) => {
    setBodySelectionDisabled(false);
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (!current) {
      return;
    }
    commitWindow(itemID, clampWindow({ ...current, x: position.x, y: position.y }, containerSize));
  };

  const startWindowResize = (itemID: string) => {
    const current = draftWindowsRef.current[itemID] || windows[itemID];
    if (!current) {
      return;
    }
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
    if (!current) {
      return;
    }
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
    const win = windows[itemID];
    if (!win) {
      return;
    }
    const restore = restoreWindowsRef.current[itemID];
    if (restore) {
      restoreWindowsRef.current = withoutKey(restoreWindowsRef.current, itemID);
      setRestoreWindows(restoreWindowsRef.current);
      const restored = clampWindow({ ...restore, z: maxZ + 1 }, containerSize);
      setWindowDraft(itemID, restored);
      patchWindowMutation.mutate({ itemID, window: restored });
      return;
    }
    const restoreWindow = clampWindow(win, containerSize);
    restoreWindowsRef.current = { ...restoreWindowsRef.current, [itemID]: restoreWindow };
    setRestoreWindows(restoreWindowsRef.current);
    const maximized = fullscreenWindow(containerSize, maxZ + 1);
    setWindowDraft(itemID, maximized);
    patchWindowMutation.mutate({ itemID, window: windowPayloadForPersist(maximized, restoreWindow) });
  };

  const browserSurfaceVisible = browserActive || hasBrowserState || createBrowserTabMutation.isPending;
  const browserButtonPending =
    !activeBrowserTab && (createBrowserTabMutation.isPending || browserStateQuery.isFetching || browserTabsQuery.isFetching);

  return (
    <aside className="relative flex h-full shrink-0 flex-col bg-[var(--canvas-background)] text-sidebar-foreground">
      <div className="relative z-30 flex h-(--toolbar-h) shrink-0 items-center gap-2 overflow-hidden pr-(--canvas-toolbar-pr) pl-(--canvas-toolbar-pl)">
        {actorSessionID ? (
          <BrowserCanvasTabButton
            active={browserActive}
            closePending={browserCloseMutation.isPending}
            closable={hasBrowserState}
            faviconURL={browserTabFaviconURLText}
            hasTitle={hasBrowserState}
            pending={browserButtonPending}
            title={browserTabTitleText}
            onClick={activateBrowserSurface}
            onClose={() => {
              if (actorSessionID) {
                browserCloseMutation.mutate(actorSessionID);
              }
            }}
          />
        ) : null}
        {items.length > 0 ? (
          <div className="no-drag-region w-fit max-w-full min-w-0 overflow-x-auto overflow-y-hidden rounded-lg bg-muted p-(--canvas-toolbar-tab-padding) text-muted-foreground [scrollbar-width:none] [-webkit-overflow-scrolling:touch] [&::-webkit-scrollbar]:hidden">
            <div className="inline-flex min-w-max items-center gap-1">
              {items.map((item) => {
                const win = windows[item.id];
                const active = !browserActive && win ? win.z === maxZ : false;
                const title = titleForItem(item, t);
                return (
                  <button
                    key={item.id}
                    aria-selected={active}
                    className="group inline-flex h-(--canvas-toolbar-tab-h) min-w-24 max-w-[44vw] shrink-0 items-center gap-1.5 rounded-md border border-transparent px-2 text-xs font-medium whitespace-nowrap transition-colors data-[active=true]:bg-background data-[active=true]:text-foreground data-[active=true]:shadow-sm hover:bg-background hover:text-foreground sm:max-w-40"
                    data-active={active}
                    title={title}
                    type="button"
                    onClick={() => {
                      selectCanvasSurface();
                      focusWindow(item.id);
                    }}
                  >
                    <CanvasItemIcon item={item} size="xs" />
                    <span className="min-w-0 flex-1 truncate text-left">{title}</span>
                    <span
                      aria-label={t("canvas.delete")}
                      className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded opacity-70 hover:bg-muted-foreground/20 hover:opacity-100"
                      role="button"
                      tabIndex={-1}
                      onClick={(event) => {
                        event.stopPropagation();
                        deleteMutation.mutate(item);
                      }}
                    >
                      <X className="h-3 w-3" />
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}
        <div aria-hidden="true" className="pointer-events-none min-w-0 flex-1 self-stretch" />
        <div className="no-drag-region flex shrink-0 items-center gap-1.5">
          <CanvasLibraryMenu
            closedItems={closedItems}
            open={canvasLibraryOpen}
            onClearClosed={() => clearClosedMutation.mutate()}
            onOpenChange={setCanvasLibraryOpen}
            onRemoveClosed={(entry) => removeClosedMutation.mutate(entry)}
            onRestoreClosed={restoreClosedItem}
          />
        </div>
      </div>
      <div className="relative z-0 min-h-0 flex-1 overflow-hidden px-3 pb-3">
        <div ref={containerRef} className="relative isolate z-0 h-full overflow-hidden">
          {browserActive ? null : (!enabled && items.length === 0) || (itemsQuery.isLoading && items.length === 0) ? (
            <CanvasEmpty />
          ) : items.length === 0 ? (
            <CanvasEmpty />
          ) : (
            items.map((item, index) => (
              <CanvasWindow
                key={item.id}
                item={item}
                bounds={containerSize}
                token={token}
                window={windows[item.id] || windowFromItem(item, index)}
                galleryActiveIndex={galleryActiveIndices[item.id] || 0}
                isMaximized={Boolean(restoreWindows[item.id])}
                onDelete={() => deleteMutation.mutate(item)}
                onDrag={(position) => updateWindowDraftGeometry(item.id, position)}
                onDragStart={() => startWindowDrag(item.id)}
                onDragStop={(position) => stopWindowDrag(item.id, position)}
                onFocus={() => focusWindow(item.id)}
                onGalleryActiveIndexChange={(activeIndex) => {
                  setGalleryActiveIndices((prev) => ({ ...prev, [item.id]: activeIndex }));
                }}
                onGalleryLayoutChange={(layout) => galleryLayoutMutation.mutate({ item, layout })}
                onMaximize={() => toggleMaximize(item.id)}
                onResize={(geometry) => updateWindowDraftGeometry(item.id, geometry)}
                onResizeStart={() => startWindowResize(item.id)}
                onResizeStop={(geometry) => stopWindowResize(item.id, geometry)}
              />
            ))
          )}
        </div>
        {actorSessionID && browserSurfaceVisible ? (
          <BrowserSurface
            key={`browser:${actorSessionID}`}
            active={browserActive}
            pending={createBrowserTabMutation.isPending || browserStateQuery.isFetching || browserTabsQuery.isFetching}
            sessionID={actorSessionID}
            tab={activeBrowserTab}
            token={token}
          />
        ) : null}
      </div>
    </aside>
  );
}

function CanvasEmpty() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center">
      <Blocks className="h-8 w-8 text-muted-foreground/60" />
    </div>
  );
}

function CanvasBrowserLoading() {
  const { t } = useI18n();
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-[var(--canvas-background)] text-sm text-muted-foreground">
      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
      {t("browser.loading")}
    </div>
  );
}

function BrowserSurface({
  active,
  pending,
  sessionID,
  tab,
  token,
}: {
  active: boolean;
  pending: boolean;
  sessionID: string;
  tab?: BrowserTab;
  token: string;
}) {
  const browserKey = `${sessionID}:${tab?.id || "empty"}`;
  return (
    <div
      aria-hidden={!active}
      className={cn(
        "absolute inset-0 z-20 flex min-h-0 flex-col overflow-hidden bg-[var(--canvas-background)] text-card-foreground shadow-none",
        !active && "pointer-events-none invisible opacity-0",
      )}
    >
      <div className="canvas-window-drag-handle flex h-10 shrink-0 cursor-default items-center gap-2 border-y bg-card px-3">
        <BrowserToolbar key={`toolbar:${browserKey}`} activeTab={tab} sessionID={sessionID} token={token} />
      </div>
      <div className="min-h-0 flex-1 overflow-hidden bg-[var(--canvas-background)]">
        {tab ? (
          <ElectronWebviewBrowser key={`widget:${sessionID}:${tab.id}`} activeTab={tab} sessionID={sessionID} token={token} />
        ) : pending ? (
          <CanvasBrowserLoading />
        ) : (
          <CanvasEmpty />
        )}
      </div>
    </div>
  );
}

function CanvasWindow({
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
  window: WindowState;
  galleryActiveIndex: number;
  isMaximized: boolean;
  onDelete: () => void;
  onDrag: (position: WindowPosition) => void;
  onDragStart: () => void;
  onDragStop: (position: WindowPosition) => void;
  onFocus: () => void;
  onGalleryActiveIndexChange: (activeIndex: number) => void;
  onGalleryLayoutChange: (layout: GalleryLayout) => void;
  onMaximize: () => void;
  onResize: (geometry: WindowGeometry) => void;
  onResizeStart: () => void;
  onResizeStop: (geometry: WindowGeometry) => void;
}) {
  const { t } = useI18n();
  const title = titleForItem(item, t);
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
      minHeight={Math.min(MIN_H, bounds.h || MIN_H)}
      minWidth={Math.min(MIN_W, bounds.w || MIN_W)}
      position={{ x: window.x, y: window.y }}
      size={{ width: window.w, height: window.h }}
      style={{
        zIndex: window.z,
      }}
      onDrag={(_event, data) => onDrag({ x: data.x, y: data.y })}
      onDragStart={() => onDragStart()}
      onDragStop={(_event, data) => onDragStop({ x: data.x, y: data.y })}
      onMouseDown={(event) => {
        if (event.target instanceof Element && event.target.closest(".canvas-window-no-drag")) {
          return;
        }
        globalThis.requestAnimationFrame(onFocus);
      }}
      onResize={(_event, _direction, ref, _delta, position) =>
        onResize({
          x: position.x,
          y: position.y,
          w: ref.offsetWidth,
          h: ref.offsetHeight,
        })
      }
      onResizeStart={() => onResizeStart()}
      onResizeStop={(_event, _direction, ref, _delta, position) =>
        onResizeStop({
          x: position.x,
          y: position.y,
          w: ref.offsetWidth,
          h: ref.offsetHeight,
        })
      }
    >
      <div
        className={cn(
          "relative flex h-full w-full min-h-0 flex-col overflow-hidden rounded-lg text-card-foreground",
          usesCanvasBackground ? "bg-[var(--canvas-background)]" : "bg-card",
          isMaximized ? "shadow-none" : "shadow-sm",
        )}
      >
        <div
          className={cn(
            "canvas-window-drag-handle flex h-10 shrink-0 cursor-default items-center gap-2 border bg-card px-3",
            isMaximizedGrid ? "rounded-lg" : "rounded-t-lg",
          )}
          onDoubleClick={onMaximize}
        >
          <CanvasKindIcon kind={item.kind} size="xs" />
          <div className="min-w-0 flex-1 truncate text-sm font-medium">{title}</div>
          {galleryLayout ? (
            <GalleryLayoutControls layout={galleryLayout} onLayoutChange={onGalleryLayoutChange} />
          ) : null}
          {table ? <TableExportMenu table={table} token={token} /> : null}
          <Button
            aria-label={isMaximized ? t("canvas.restore") : t("canvas.maximize")}
            className="canvas-window-no-drag"
            size="icon-sm"
            variant="ghost"
            onPointerDown={(event) => event.stopPropagation()}
            onDoubleClick={(event) => event.stopPropagation()}
            onClick={(event) => {
              event.stopPropagation();
              onMaximize();
            }}
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
            onClick={(event) => {
              event.stopPropagation();
              onDelete();
            }}
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
        <div
          className={cn(
            "min-h-0 flex-1 overflow-auto rounded-b-lg",
            isMaximizedGrid ? "" : "border-x border-b",
            usesCanvasBackground ? "bg-[var(--canvas-background)]" : "bg-card",
          )}
        >
          <MemoCanvasContent
            item={item}
            token={token}
            galleryActiveIndex={galleryActiveIndex}
            isMaximized={isMaximized}
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


function CanvasLibraryMenu({
  closedItems,
  open,
  onClearClosed,
  onOpenChange,
  onRemoveClosed,
  onRestoreClosed,
}: {
  closedItems: ClosedCanvasItem[];
  open: boolean;
  onClearClosed: () => void;
  onOpenChange: (open: boolean) => void;
  onRemoveClosed: (entry: ClosedCanvasItem) => void;
  onRestoreClosed: (entry: ClosedCanvasItem) => void;
}) {
  const { t } = useI18n();
  const hasEntries = closedItems.length > 0;
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button aria-label={t("canvas.widgetLibrary")} size="icon-sm" variant="ghost">
          <Blocks className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64" collisionPadding={8}>
        <DropdownMenuLabel>{t("canvas.widgetLibrary")}</DropdownMenuLabel>
        {!hasEntries ? <div className="px-2 py-3 text-xs text-muted-foreground">{t("canvas.widgetLibraryEmpty")}</div> : null}
        {hasEntries ? (
          <>
            <DropdownMenuSeparator />
            <div className="flex items-center justify-between px-2 py-1.5">
              <span className="text-xs font-medium text-muted-foreground">{t("canvas.recentClosed")}</span>
              <button
                className="rounded px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                type="button"
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  onClearClosed();
                }}
              >
                {t("canvas.clearRecentClosed")}
              </button>
            </div>
            {closedItems.map((entry) => (
              <ClosedCanvasItemRow
                key={entry.id}
                entry={entry}
                onRemove={() => onRemoveClosed(entry)}
                onRestore={() => {
                  onRestoreClosed(entry);
                  onOpenChange(false);
                }}
              />
            ))}
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ClosedCanvasItemRow({
  entry,
  onRemove,
  onRestore,
}: {
  entry: ClosedCanvasItem;
  onRemove: () => void;
  onRestore: () => void;
}) {
  const { t } = useI18n();
  const title = entry.title || entry.kind;
  return (
    <div
      className="group/closed mx-1 flex h-9 min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-sm hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
      role="button"
      tabIndex={0}
      title={title}
      onClick={onRestore}
      onKeyDown={(event) => {
        if (event.target !== event.currentTarget) {
          return;
        }
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onRestore();
        }
      }}
    >
      <CanvasKindIcon kind={entry.kind} size="xs" />
      <span className="min-w-0 flex-1 truncate text-left">{title}</span>
      <div className="relative flex h-6 w-12 shrink-0 items-center justify-end">
        <span className="absolute right-0 text-xs text-muted-foreground transition-opacity group-hover/closed:opacity-0 group-focus-within/closed:opacity-0">
          {formatClosedTime(entry.closedAt)}
        </span>
        <span className="absolute right-0 flex items-center gap-1 opacity-0 transition-opacity group-hover/closed:opacity-100 group-focus-within/closed:opacity-100">
          <button
            aria-label={t("canvas.restore")}
            className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-background/80 hover:text-foreground"
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              onRestore();
            }}
          >
            <Undo2 className="h-3.5 w-3.5" />
          </button>
          <button
            aria-label={t("canvas.delete")}
            className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-destructive hover:bg-destructive/10"
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              onRemove();
            }}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </span>
      </div>
    </div>
  );
}


function formatClosedTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}

function canvasPayloadFromClosedItem(item: ClosedCanvasItem): CanvasItemPayload {
  return {
    id: item.sourceItemID,
    kind: item.kind,
    title: item.title,
    item: item.item,
    window: item.window,
  };
}

function windowFromClosedItem(item: ClosedCanvasItem): WindowState {
  return clampWindow({
    x: numberValue(asRecord(item.window)?.x, 16),
    y: numberValue(asRecord(item.window)?.y, 16),
    w: numberValue(asRecord(item.window)?.w, DEFAULT_W),
    h: numberValue(asRecord(item.window)?.h, DEFAULT_H),
    z: numberValue(asRecord(item.window)?.z, 1),
  });
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

function restoreWindowFromItem(item: CanvasItem, bounds = { w: 0, h: 0 }): WindowState | undefined {
  const raw = asRecord(item.window);
  if (raw?.maximized !== true) {
    return undefined;
  }
  const restore = asRecord(raw.restore);
  if (!restore) {
    return undefined;
  }
  return clampWindow(
    {
      x: numberValue(restore.x, 16),
      y: numberValue(restore.y, 16),
      w: numberValue(restore.w, DEFAULT_W),
      h: numberValue(restore.h, DEFAULT_H),
      z: numberValue(restore.z, numberValue(raw.z, 1)),
    },
    bounds,
  );
}

function windowPayloadForPersist(win: WindowState, restore: WindowState | undefined): WindowState {
  const clean = serializeWindow(win);
  if (!restore) {
    return clean;
  }
  return {
    ...clean,
    maximized: true,
    restore: serializeWindow(restore),
  };
}

function serializeWindow(win: WindowState): WindowRestoreState {
  return {
    x: Math.round(win.x),
    y: Math.round(win.y),
    w: Math.round(win.w),
    h: Math.round(win.h),
    z: Math.max(1, Math.round(win.z)),
  };
}

function titleForItem(item: CanvasItem, t: (key: string) => string): string {
  return item.title?.trim() || titleFromPayload(item.item) || item.kind || t("canvas.untitled");
}

function readSessionSurfaces(): Record<string, CanvasSurface> {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(SESSION_SURFACE_STORAGE_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const out: Record<string, CanvasSurface> = {};
    Object.entries(parsed).forEach(([sessionID, surface]) => {
      if (surface === "canvas" || surface === "browser") {
        out[sessionID] = surface;
      }
    });
    return out;
  } catch {
    return {};
  }
}

function writeSessionSurfaces(surfaces: Record<string, CanvasSurface>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(SESSION_SURFACE_STORAGE_KEY, JSON.stringify(surfaces));
  } catch {
    // Best-effort UI preference.
  }
}

function closedCanvasItemIsBrowser(item: ClosedCanvasItem): boolean {
  const payload = asRecord(item.item);
  return (stringValue(payload?.kind) || item.kind) === "browser";
}

function canvasItemIsBrowser(item: CanvasItem): boolean {
  const payload = asRecord(item.item);
  return (stringValue(payload?.kind) || item.kind) === "browser";
}

function withoutKey<T>(record: Record<string, T>, key: string): Record<string, T> {
  const next = { ...record };
  delete next[key];
  return next;
}

function sameWindow(a: WindowState, b: WindowState): boolean {
  return a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h && a.z === b.z;
}

function isNearFullscreenWindow(win: WindowState, bounds = { w: 0, h: 0 }): boolean {
  if (bounds.w <= 0 || bounds.h <= 0) {
    return false;
  }
  return (
    win.x <= FULLSCREEN_SNAP &&
    win.y <= FULLSCREEN_SNAP &&
    Math.abs(win.x + win.w - bounds.w) <= FULLSCREEN_SNAP &&
    Math.abs(win.y + win.h - bounds.h) <= FULLSCREEN_SNAP
  );
}

function fullscreenWindow(bounds: { w: number; h: number }, z: number): WindowState {
  return clampWindow({ x: 0, y: 0, w: bounds.w, h: bounds.h, z }, bounds);
}

function clampWindow(win: WindowState, bounds = { w: 0, h: 0 }): WindowState {
  const minW = bounds.w > 0 ? Math.min(MIN_W, bounds.w) : MIN_W;
  const minH = bounds.h > 0 ? Math.min(MIN_H, bounds.h) : MIN_H;
  const maxW = bounds.w > 0 ? Math.max(minW, bounds.w) : Number.POSITIVE_INFINITY;
  const maxH = bounds.h > 0 ? Math.max(minH, bounds.h) : Number.POSITIVE_INFINITY;
  const w = Math.min(Math.max(minW, Math.round(win.w)), maxW);
  const h = Math.min(Math.max(minH, Math.round(win.h)), maxH);
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
