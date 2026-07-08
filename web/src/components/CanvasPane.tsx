import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Blocks,
  CalendarDays,
  ChartPie,
  Check,
  Clock,
  Compass,
  Copy,
  DollarSign,
  Download,
  FileText,
  GalleryHorizontal,
  GalleryVertical,
  Grid2X2,
  Gauge,
  Hash,
  Image,
  ImageOff,
  Loader2,
  Maximize2,
  Minimize2,
  Percent,
  Sheet,
  Trash2,
  Undo2,
  Users,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  memo,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MutableRefObject,
} from "react";
import { Rnd } from "react-rnd";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  XAxis,
} from "recharts";
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
import { MarkdownBody } from "@/components/transcript/TurnParts";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import {
  ChartContainer,
  ChartLegend as RechartsChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { CanvasItem, ClosedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { cn } from "@/lib/utils";
import { setCanvasOpen } from "@/state/canvasStore";
import { apiURL } from "@/state/apiBase";
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

type ColumnType = "text" | "enum" | "number" | "currency" | "date" | "datetime" | "truncate";
type SemanticColor = "red" | "amber" | "green" | "sky" | "violet" | "gray";
type ColumnColor = SemanticColor | `#${string}`;

type Column = {
  key: string;
  label: string;
  type?: ColumnType;
  map?: Record<string, string>;
  colors?: Record<string, ColumnColor>;
  divide?: number;
  decimals?: number;
  thousands?: boolean;
  currency?: string;
  format?: string;
  max?: number;
};

type ChartType = "bar" | "line" | "area" | "pie" | "donut";
type ChartSeries = {
  key: string;
  label?: string;
  color?: string;
};
type GalleryLayout = "grid" | "row" | "column";
type TimelineStatus = "done" | "in_progress" | "planned" | "blocked" | "";
type GalleryImageItem = {
  src: string;
  alt: string;
  caption: string;
  key: string;
};

type TableExportData = {
  id: string;
  title: string;
  filename: string;
  columns: Column[];
  rows: unknown[];
  caption: string;
};

type SaveResult = {
  filename: string;
  path?: string;
  via: "desktop" | "browser";
};

const SESSION_SURFACE_STORAGE_KEY = "pudding.canvas.sessionSurface.v1";
const MIN_W = 260;
const MIN_H = 160;
const DEFAULT_W = 420;
const DEFAULT_H = 300;
const CASCADE = 28;
const FULLSCREEN_SNAP = 12;
const CHART_COLORS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
];
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
const BADGE_COLOR_CLASS: Record<SemanticColor, string> = {
  red: "bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-200",
  amber: "bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-200",
  green: "bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-200",
  sky: "bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-200",
  violet: "bg-violet-100 text-violet-700 dark:bg-violet-900/40 dark:text-violet-200",
  gray: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-200",
};
const GRID_COLOR_CLASS: Record<string, string> = {
  default: "bg-muted/40 text-foreground",
  green: "bg-green-50 text-green-800 dark:bg-green-950/40 dark:text-green-100",
  amber: "bg-amber-50 text-amber-800 dark:bg-amber-950/40 dark:text-amber-100",
  red: "bg-red-50 text-red-800 dark:bg-red-950/40 dark:text-red-100",
  sky: "bg-sky-50 text-sky-800 dark:bg-sky-950/40 dark:text-sky-100",
  violet: "bg-violet-50 text-violet-800 dark:bg-violet-950/40 dark:text-violet-100",
};
const METRIC_VALUE_COLOR_CLASS: Record<string, string> = {
  default: "text-foreground",
  green: "text-green-600 dark:text-green-300",
  amber: "text-amber-600 dark:text-amber-300",
  red: "text-red-600 dark:text-red-300",
  sky: "text-sky-600 dark:text-sky-300",
  violet: "text-violet-600 dark:text-violet-300",
};
const METRIC_ICON: Record<string, LucideIcon> = {
  activity: Activity,
  calendar: CalendarDays,
  clock: Clock,
  gauge: Gauge,
  hash: Hash,
  money: DollarSign,
  percent: Percent,
  users: Users,
};
const TIMELINE_STATUS_COLOR: Record<Exclude<TimelineStatus, "">, SemanticColor> = {
  done: "green",
  in_progress: "sky",
  planned: "gray",
  blocked: "red",
};
const TIMELINE_DOT_CLASS: Record<SemanticColor, string> = {
  red: "bg-red-500",
  amber: "bg-amber-500",
  green: "bg-green-500",
  sky: "bg-sky-500",
  violet: "bg-violet-500",
  gray: "bg-muted-foreground",
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
      if (items.length === 0) {
        setCanvasOpen(false);
      }
    },
    onError: () => {
      if (actorSessionID) {
        clearElectronBrowserSessionGate(actorSessionID);
        setClosingBrowserSessions((prev) => withoutKey(prev, actorSessionID));
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(actorSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(actorSessionID) });
      }
      toast.error(t("browser.releaseFailed"));
    },
  });

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

function GalleryLayoutControls({
  layout,
  onLayoutChange,
}: {
  layout: GalleryLayout;
  onLayoutChange: (layout: GalleryLayout) => void;
}) {
  const { t } = useI18n();
  const options: Array<{ layout: GalleryLayout; label: string; Icon: LucideIcon }> = [
    { layout: "grid", label: t("canvas.galleryLayoutGrid"), Icon: Grid2X2 },
    { layout: "row", label: t("canvas.galleryLayoutRow"), Icon: GalleryHorizontal },
    { layout: "column", label: t("canvas.galleryLayoutColumn"), Icon: GalleryVertical },
  ];
  return (
    <ButtonGroup
      aria-label={t("canvas.galleryLayout")}
      className="canvas-window-no-drag mr-1 shrink-0"
      onDoubleClick={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
      onPointerDown={(event) => event.stopPropagation()}
    >
      {options.map(({ layout: option, label, Icon }) => (
        <Button
          key={option}
          aria-label={label}
          aria-pressed={layout === option}
          className="text-muted-foreground hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground aria-pressed:shadow-none dark:aria-pressed:bg-input/50"
          size="icon-sm"
          title={label}
          type="button"
          variant="outline"
          onClick={(event) => {
            event.stopPropagation();
            if (layout !== option) {
              onLayoutChange(option);
            }
          }}
        >
          <Icon className="h-4 w-4" />
        </Button>
      ))}
    </ButtonGroup>
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

function TableExportMenu({ table, token }: { table: TableExportData; token: string }) {
  const { t } = useI18n();
  const runExport = async (format: "csv" | "json") => {
    const toastID = toast.loading(t("canvas.exporting"), { position: "top-center" });
    try {
      const result = await exportTable(table, format, token);
      const exportedPath = result.path;
      toast.success(t("canvas.exportDone"), {
        id: toastID,
        description: exportedPath ? <ExportSavedDescription path={exportedPath} /> : result.filename,
        action: exportedPath
          ? {
              label: t("canvas.exportReveal"),
              onClick: () => void revealFile(exportedPath, token),
            }
          : undefined,
        duration: 6000,
        position: "top-center",
      });
    } catch (error) {
      toast.error(t("canvas.exportFailed"), {
        id: toastID,
        description: error instanceof Error ? error.message : String(error),
        duration: 6000,
        position: "top-center",
      });
    }
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t("canvas.exportTable")}
          className="canvas-window-no-drag"
          size="icon-sm"
          variant="ghost"
          onMouseDown={(event) => event.stopPropagation()}
          onPointerDown={(event) => event.stopPropagation()}
          onDoubleClick={(event) => event.stopPropagation()}
        >
          <Download className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-32">
        <DropdownMenuItem onSelect={() => void runExport("csv")}>{t("canvas.exportCsv")}</DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void runExport("json")}>{t("canvas.exportJson")}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ExportSavedDescription({ path }: { path: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const copyPath = async () => {
    try {
      await navigator.clipboard.writeText(path);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch (error) {
      toast.error(t("canvas.exportCopyFailed"), { description: error instanceof Error ? error.message : String(error) });
    }
  };
  return (
    <div className="mt-0.5 flex w-full min-w-0 max-w-full items-center gap-1.5 text-xs">
      <span className="shrink-0 text-muted-foreground">{t("canvas.exportSavedTo")}</span>
      <button
        className="inline-flex min-w-0 flex-1 items-center gap-1 rounded-md border bg-background px-2 py-0.5 text-left"
        type="button"
        onClick={() => void copyPath()}
      >
        <span className="min-w-0 truncate font-mono">{prettyPath(path)}</span>
        {copied ? <Check className="h-3.5 w-3.5 shrink-0 text-emerald-500" /> : <Copy className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
      </button>
    </div>
  );
}

function CanvasContent({
  item,
  token,
  galleryActiveIndex,
  isMaximized,
  onGalleryActiveIndexChange,
  onGalleryLayoutChange,
}: {
  item: CanvasItem;
  token: string;
  galleryActiveIndex: number;
  isMaximized: boolean;
  onGalleryActiveIndexChange: (activeIndex: number) => void;
  onGalleryLayoutChange: (layout: GalleryLayout) => void;
}) {
  const payload = asRecord(item.item);
  const kind = typeof payload?.kind === "string" ? payload.kind : item.kind;
  if (kind === "markdown") {
    const content = stringValue(payload?.content) || stringValue(payload?.markdown) || "";
    return (
      <div className="p-3">
        <MarkdownBody text={content} />
      </div>
    );
  }
  if (kind === "table") {
    return <CanvasTable payload={payload} />;
  }
  if (kind === "chart") {
    return (
      <div className="h-full min-h-0 p-3">
        <CanvasChart payload={payload} />
      </div>
    );
  }
  if (kind === "timeline") {
    return (
      <div className="p-3">
        <CanvasTimeline payload={payload} />
      </div>
    );
  }
  if (kind === "grid") {
    return (
      <div className={cn(isMaximized ? "py-3" : "p-3")}>
        <CanvasGrid payload={payload} token={token} />
      </div>
    );
  }
  if (kind === "gallery") {
    const galleryLayout = galleryLayoutValue(payload?.layout);
    const singleImage = galleryItemCount(payload) === 1;
    return (
      <div className={cn("h-full min-h-0", galleryLayout === "grid" && !singleImage ? "p-3" : "")}>
        <CanvasGallery
          payload={payload}
          token={token}
          activeIndex={galleryActiveIndex}
          onActiveIndexChange={onGalleryActiveIndexChange}
          onLayoutChange={onGalleryLayoutChange}
        />
      </div>
    );
  }
  if (kind === "form") {
    return (
      <div className="p-3">
        <CanvasForm payload={payload} />
      </div>
    );
  }
  return (
    <div className="p-3">
      <pre className="overflow-auto rounded bg-muted/50 p-3 text-xs whitespace-pre-wrap">
        {JSON.stringify(item.item, null, 2)}
      </pre>
    </div>
  );
}

const MemoCanvasContent = memo(CanvasContent);

function CanvasGrid({ payload, token, nested = false }: { payload: Record<string, unknown> | undefined; token: string; nested?: boolean }) {
  const items = Array.isArray(payload?.items) ? payload.items : [];
  const gap = Math.max(4, Math.min(32, numberValue(asRecord(payload?.layout)?.gap, nested ? 8 : 12)));
  const caption = stringValue(payload?.caption);
  return (
    <div className="min-w-0">
      <div
        className="grid min-w-0"
        style={{
          gridTemplateColumns: "repeat(12, minmax(0, 1fr))",
          gap,
        }}
      >
        {items.map((item, index) => {
          const record = asRecord(item);
          if (!record) {
            return null;
          }
          const kind = stringValue(record.kind) || "markdown";
          const span = gridItemSpan(record, stringValue(payload?.columns), nested);
          const isMetric = kind === "metric";
          return (
            <section
              key={stringValue(record.id) || `${kind}-${index}`}
              className={cn(
                "min-w-0 overflow-hidden rounded-lg",
                isMetric ? "border-0 bg-transparent shadow-none" : "border",
                !isMetric ? gridItemSurfaceClass(record) : null,
                !isMetric && stringValue(record.variant) === "subtle" ? "shadow-none" : null,
                !isMetric && stringValue(record.variant) !== "subtle" ? "shadow-sm" : null,
              )}
              style={{ gridColumn: `span ${span} / span ${span}` }}
            >
              {!isMetric && stringValue(record.title) ? (
                <div className="px-3 pt-2.5 text-[13px] font-semibold">
                  <span className="block min-w-0 truncate">{stringValue(record.title)}</span>
                </div>
              ) : null}
              <div
                className={cn(
                  "min-w-0 overflow-auto",
                  kind === "metric"
                    ? "flex min-h-24"
                    : kind === "chart"
                      ? "h-64 p-3"
                      : stringValue(record.variant) === "compact"
                        ? "p-2"
                        : "p-3",
                )}
              >
                <GridItemContent item={record} kind={kind} token={token} />
              </div>
            </section>
          );
        })}
      </div>
      {caption ? <div className="mt-2 text-xs text-muted-foreground">{caption}</div> : null}
    </div>
  );
}

function GridItemContent({ item, kind, token }: { item: Record<string, unknown>; kind: string; token: string }) {
  if (kind === "metric") {
    return <CanvasMetric item={item} />;
  }
  if (kind === "table") {
    return <CanvasTable payload={item} />;
  }
  if (kind === "gallery") {
    return <CanvasGallery payload={item} token={token} compact />;
  }
  if (kind === "chart") {
    return <CanvasChart payload={item} />;
  }
  if (kind === "timeline") {
    return <CanvasTimeline payload={item} />;
  }
  if (kind === "grid") {
    return <CanvasGrid payload={item} token={token} nested />;
  }
  return <MarkdownBody text={stringValue(item.content) || stringValue(item.text)} />;
}

function gridItemSpan(item: Record<string, unknown>, columns: string, nested: boolean): number {
  const span = asRecord(item.span);
  const explicit = numberValue(span?.lg, numberValue(span?.md, numberValue(span?.sm, numberValue(span?.xs, 0))));
  if (explicit > 0) {
    return Math.min(12, Math.max(1, Math.round(explicit)));
  }
  if (columns === "1") {
    return 12;
  }
  if (columns === "2") {
    return 6;
  }
  if (columns === "3") {
    return 4;
  }
  if (nested) {
    return 12;
  }
  const kind = stringValue(item.kind);
  return kind === "metric" || kind === "chart" ? 6 : 12;
}

function gridItemSurfaceClass(item: Record<string, unknown>): string {
  if (stringValue(item.surface) === "tinted") {
    return GRID_COLOR_CLASS[stringValue(item.color)] || GRID_COLOR_CLASS.default;
  }
  return stringValue(item.variant) === "subtle" ? "border-border/40 bg-card" : "border-border/60 bg-card";
}

function CanvasMetric({ item }: { item: Record<string, unknown> }) {
  const title = stringValue(item.title);
  const description = stringValue(item.description);
  const color = stringValue(item.color);
  const Icon = METRIC_ICON[stringValue(item.icon)];
  return (
    <div className="@container/metric flex h-full min-h-24 min-w-0 flex-1 flex-col justify-between rounded-lg border border-border/60 bg-card px-4 py-3">
      <div className="min-w-0">
        {title ? (
          <div className="mb-2 flex min-w-0 items-center gap-1.5 text-sm font-medium text-muted-foreground">
            {Icon ? <Icon className="h-4 w-4 shrink-0" /> : null}
            <span className="min-w-0 truncate">{title}</span>
          </div>
        ) : null}
        <div
          className={cn(
            "min-w-0 break-words text-[28px] leading-[0.95] font-semibold tracking-normal tabular-nums @[260px]/metric:text-[34px]",
            METRIC_VALUE_COLOR_CLASS[color] || METRIC_VALUE_COLOR_CLASS.default,
          )}
        >
          {formatMetricValue(item.value)}
        </div>
      </div>
      {description ? (
        <div className="mt-3 line-clamp-2 text-sm text-muted-foreground">{description}</div>
      ) : null}
    </div>
  );
}

function formatMetricValue(value: unknown): string {
  if (value === null || value === undefined) {
    return "";
  }
  return typeof value === "string" || typeof value === "number" || typeof value === "boolean" ? String(value) : JSON.stringify(value);
}

function CanvasChart({ payload }: { payload: Record<string, unknown> | undefined }) {
  const { t } = useI18n();
  const chart = asRecord(payload?.chart) ?? payload;
  const data = sanitizeChartData(Array.isArray(chart?.data) ? chart.data : []);
  const caption = stringValue(payload?.caption);
  const type = chartTypeValue(chart?.type);
  if (data.length === 0) {
    return <div className="text-xs text-muted-foreground">{t("canvas.chartNoData")}</div>;
  }
  if (type === "pie" || type === "donut") {
    return <CanvasPieChart chart={chart} data={data} caption={caption} donut={type === "donut"} />;
  }
  const xKey = stringValue(chart?.x_key) || stringValue(chart?.xKey) || inferChartXKey(data);
  const series = normalizeChartSeries(chart?.series, data, xKey, stringValue(chart?.value_key) || stringValue(chart?.valueKey));
  if (series.length === 0) {
    return <div className="text-xs text-muted-foreground">{t("canvas.chartNoSeries")}</div>;
  }
  const chartConfig = buildChartConfig(series);
  return (
    <div className="flex h-full min-h-[180px] flex-col">
      <ChartContainer config={chartConfig} className="aspect-auto min-h-[160px] flex-1">
          {type === "line" ? (
            <LineChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
              <ChartAxes xKey={xKey} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              {series.length > 1 ? <RechartsChartLegend content={<ChartLegendContent />} /> : null}
              {series.map((entry, index) => (
                <Line
                  key={entry.key}
                  dataKey={entry.key}
                  dot={false}
                  name={entry.label ?? entry.key}
                  stroke={chartSeriesCSSColor(entry, index)}
                  strokeWidth={2}
                  type="monotone"
                />
              ))}
            </LineChart>
          ) : type === "area" ? (
            <AreaChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
              <ChartAxes xKey={xKey} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              {series.length > 1 ? <RechartsChartLegend content={<ChartLegendContent />} /> : null}
              {series.map((entry, index) => {
                const color = chartSeriesCSSColor(entry, index);
                return (
                  <Area
                    key={entry.key}
                    dataKey={entry.key}
                    fill={color}
                    fillOpacity={0.18}
                    name={entry.label ?? entry.key}
                    stroke={color}
                    strokeWidth={2}
                    type="monotone"
                  />
                );
              })}
            </AreaChart>
          ) : (
            <BarChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
              <ChartAxes xKey={xKey} />
              <ChartTooltip content={<ChartTooltipContent />} />
              {series.length > 1 ? <RechartsChartLegend content={<ChartLegendContent />} /> : null}
              {series.map((entry, index) => (
                <Bar
                  key={entry.key}
                  dataKey={entry.key}
                  fill={chartSeriesCSSColor(entry, index)}
                  name={entry.label ?? entry.key}
                  radius={8}
                />
              ))}
            </BarChart>
          )}
      </ChartContainer>
      {caption ? <div className="mt-2 shrink-0 text-xs text-muted-foreground">{caption}</div> : null}
    </div>
  );
}

function ChartAxes({ xKey }: { xKey: string }) {
  return (
    <>
      <CartesianGrid vertical={false} />
      <XAxis axisLine={false} dataKey={xKey} fontSize={11} tickLine={false} tickMargin={10} />
    </>
  );
}

function CanvasPieChart({
  chart,
  data,
  caption,
  donut,
}: {
  chart: Record<string, unknown> | undefined;
  data: Record<string, unknown>[];
  caption: string;
  donut: boolean;
}) {
  const nameKey = stringValue(chart?.name_key) || stringValue(chart?.x_key) || "name";
  const valueKey = stringValue(chart?.value_key) || normalizeChartSeries(chart?.series, data, nameKey)[0]?.key || "value";
  const chartConfig: ChartConfig = {
    [valueKey]: {
      label: valueKey,
      color: CHART_COLORS[0],
    },
  };
  return (
    <div className="flex h-full min-h-[180px] flex-col">
      <ChartContainer config={chartConfig} className="aspect-auto min-h-[160px] flex-1">
          <PieChart>
            <ChartTooltip content={<ChartTooltipContent hideLabel />} />
            <Pie
              data={data}
              dataKey={valueKey}
              innerRadius={donut ? "46%" : 0}
              nameKey={nameKey}
              outerRadius="78%"
              paddingAngle={donut ? 2 : 0}
            >
              {data.map((_, index) => (
                <Cell key={index} fill={CHART_COLORS[index % CHART_COLORS.length]} />
              ))}
            </Pie>
          </PieChart>
      </ChartContainer>
      {caption ? <div className="mt-2 shrink-0 text-xs text-muted-foreground">{caption}</div> : null}
    </div>
  );
}

type TimelineEntry = {
  key: string;
  group: string;
  time: string;
  title: string;
  status: TimelineStatus;
  description: string;
  meta: string;
  link: string;
  color?: SemanticColor;
};

function CanvasTimeline({ payload }: { payload: Record<string, unknown> | undefined }) {
  const { t } = useI18n();
  const entries = timelineEntries(payload);
  const groups = timelineGroups(entries);
  const caption = stringValue(payload?.caption);
  if (entries.length === 0) {
    return <div className="text-xs text-muted-foreground">{t("canvas.timelineEmpty")}</div>;
  }
  return (
    <div className="min-w-0">
      <div className="space-y-4">
        {groups.map((group) => (
          <section key={group.key} className="min-w-0">
            {group.title ? <div className="mb-2 text-xs font-semibold text-muted-foreground">{group.title}</div> : null}
            <ol className="relative ml-1 border-l border-border/80">
              {group.items.map((entry) => {
                const color = entry.color || (entry.status ? TIMELINE_STATUS_COLOR[entry.status] : "gray");
                return (
                  <li key={entry.key} className="relative pb-4 pl-5 last:pb-0">
                    <span
                      aria-hidden="true"
                      className={cn(
                        "absolute top-1.5 -left-[5px] h-2.5 w-2.5 rounded-full border-2 border-background",
                        TIMELINE_DOT_CLASS[color],
                      )}
                    />
                    <div className="min-w-0">
                      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                        {entry.time ? (
                          <span className="shrink-0 font-mono text-[11px] text-muted-foreground tabular-nums">{entry.time}</span>
                        ) : null}
                        {entry.link ? (
                          <a
                            className="min-w-0 truncate text-sm font-medium text-foreground underline-offset-2 hover:underline"
                            href={entry.link}
                            rel="noreferrer"
                            target="_blank"
                          >
                            {entry.title}
                          </a>
                        ) : (
                          <span className="min-w-0 truncate text-sm font-medium text-foreground">{entry.title}</span>
                        )}
                        {entry.status ? (
                          <span className={cn("rounded px-1.5 py-0.5 text-[11px] font-medium", BADGE_COLOR_CLASS[color])}>
                            {timelineStatusLabel(entry.status, t)}
                          </span>
                        ) : null}
                        {entry.meta ? <span className="text-xs text-muted-foreground">{entry.meta}</span> : null}
                      </div>
                      {entry.description ? (
                        <p className="mt-1 whitespace-pre-wrap text-sm leading-5 text-muted-foreground">{entry.description}</p>
                      ) : null}
                    </div>
                  </li>
                );
              })}
            </ol>
          </section>
        ))}
      </div>
      {caption ? <div className="mt-3 text-xs text-muted-foreground">{caption}</div> : null}
    </div>
  );
}

function timelineEntries(payload: Record<string, unknown> | undefined): TimelineEntry[] {
  const items = Array.isArray(payload?.items) ? payload.items : [];
  return items.flatMap((item, index) => {
    const record = asRecord(item);
    const title = stringValue(record?.title);
    if (!record || !title) {
      return [];
    }
    const status = timelineStatusValue(record.status);
    const color = timelineColorValue(record.color);
    return [
      {
        key: `${title}-${index}`,
        group: stringValue(record.group) || stringValue(record.date),
        time: stringValue(record.time),
        title,
        status,
        description: stringValue(record.description),
        meta: stringValue(record.meta),
        link: stringValue(record.link),
        ...(color ? { color } : {}),
      },
    ];
  });
}

function timelineGroups(entries: TimelineEntry[]): Array<{ key: string; title: string; items: TimelineEntry[] }> {
  const groups: Array<{ key: string; title: string; items: TimelineEntry[] }> = [];
  const byTitle = new Map<string, { key: string; title: string; items: TimelineEntry[] }>();
  entries.forEach((entry) => {
    const title = entry.group;
    const key = title || "__default";
    let group = byTitle.get(key);
    if (!group) {
      group = { key, title, items: [] };
      byTitle.set(key, group);
      groups.push(group);
    }
    group.items.push(entry);
  });
  return groups;
}

function timelineStatusValue(value: unknown): TimelineStatus {
  return value === "done" || value === "in_progress" || value === "planned" || value === "blocked" ? value : "";
}

function timelineColorValue(value: unknown): SemanticColor | undefined {
  return value === "gray" || value === "green" || value === "amber" || value === "red" || value === "sky" || value === "violet"
    ? value
    : undefined;
}

function timelineStatusLabel(status: TimelineStatus, t: (key: string) => string): string {
  if (status === "done") {
    return t("canvas.timelineStatusDone");
  }
  if (status === "in_progress") {
    return t("canvas.timelineStatusInProgress");
  }
  if (status === "blocked") {
    return t("canvas.timelineStatusBlocked");
  }
  return t("canvas.timelineStatusPlanned");
}

function CanvasTable({ payload }: { payload: Record<string, unknown> | undefined }) {
  const rows = Array.isArray(payload?.rows) ? payload.rows : [];
  const columns = normalizeColumns(Array.isArray(payload?.columns) ? payload.columns : columnsFromRows(rows));
  const caption = stringValue(payload?.caption);
  return (
    <div className="min-w-0 overflow-x-auto">
      <table className="w-max min-w-full table-auto border-separate border-spacing-0 text-[12.5px]">
        <thead>
          <tr>
            {columns.map((column) => (
              <th
                key={column.key}
                className="border-b border-border bg-card px-2.5 py-1.5 text-left font-semibold text-muted-foreground whitespace-nowrap"
                title={column.label}
              >
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="[&>tr:last-child>td]:border-b-0">
          {rows.map((row, index) => {
            return (
              <tr key={index}>
                {columns.map((column, columnIndex) => (
                  <td
                    key={column.key}
                    className={`border-b border-border px-2.5 py-1.5 align-top whitespace-nowrap ${columnAlignClass(column)}`}
                  >
                    <TableCell column={column} fallbackIndex={columnIndex} row={row} />
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
      {caption ? <div className="border-t border-border px-3 py-2 text-xs text-muted-foreground">{caption}</div> : null}
    </div>
  );
}

function tableExportData(item: CanvasItem, t: (key: string) => string): TableExportData | null {
  const payload = asRecord(item.item);
  const kind = typeof payload?.kind === "string" ? payload.kind : item.kind;
  if (kind !== "table") {
    return null;
  }
  const rows = Array.isArray(payload?.rows) ? payload.rows : [];
  const columns = normalizeColumns(Array.isArray(payload?.columns) ? payload.columns : columnsFromRows(rows));
  const title = titleForItem(item, t);
  return {
    id: item.id,
    title,
    filename: safeFilename(title || item.id),
    columns,
    rows,
    caption: stringValue(payload?.caption),
  };
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

async function exportTable(table: TableExportData, format: "csv" | "json", token: string): Promise<SaveResult> {
  if (format === "json") {
    return saveText({
      token,
      filename: `${table.filename}.json`,
      mime: "application/json;charset=utf-8",
      content: JSON.stringify(
        {
          id: table.id,
          title: table.title,
          columns: table.columns,
          rows: table.rows,
          caption: table.caption,
        },
        null,
        2,
      ),
    });
  }
  const header = table.columns.map((column) => escapeCsvCell(column.label));
  const rows = table.rows.map((row) =>
    table.columns.map((column, index) => escapeCsvCell(formatTableCell(rawCellValue(row, column, index), column).text)),
  );
  const content = [header, ...rows].map((line) => line.join(",")).join("\r\n");
  return saveText({
    token,
    filename: `${table.filename}.csv`,
    mime: "text/csv;charset=utf-8",
    content: `\uFEFF${content}`,
  });
}

async function saveText({
  token,
  filename,
  mime,
  content,
}: {
  token: string;
  filename: string;
  mime: string;
  content: string;
}): Promise<SaveResult> {
  if (isDesktopRuntime()) {
    const response = await fetch(apiURL("/desktop/save-file"), {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        filename,
        mime,
        data: bytesToBase64(new TextEncoder().encode(content)),
      }),
    });
    const body = (await response.json().catch(() => null)) as
      | { ok?: boolean; error?: unknown; filename?: unknown; path?: unknown }
      | null;
    if (!response.ok || !body?.ok) {
      throw new Error(typeof body?.error === "string" ? body.error : `HTTP ${response.status}`);
    }
    return {
      filename: typeof body.filename === "string" ? body.filename : filename,
      path: typeof body.path === "string" ? body.path : undefined,
      via: "desktop",
    };
  }
  downloadText(filename, mime, content);
  return { filename, via: "browser" };
}

function downloadText(filename: string, mime: string, content: string): void {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.style.display = "none";
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}

async function revealFile(path: string, token: string): Promise<void> {
  try {
    const response = await fetch(apiURL("/desktop/reveal-file"), {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ path }),
    });
    if (!response.ok) {
      const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
      throw new Error(typeof body?.error === "string" ? body.error : `HTTP ${response.status}`);
    }
  } catch (error) {
    toast.error("Finder", { description: error instanceof Error ? error.message : String(error) });
  }
}

function isDesktopRuntime(): boolean {
  return typeof document !== "undefined" && Boolean(document.documentElement.dataset.shell);
}

function prettyPath(path: string): string {
  return path.replace(/^\/Users\/[^/]+/, "~");
}

function formatClosedTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}

function escapeCsvCell(value: string): string {
  const normalized = value.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const escaped = normalized.replace(/"/g, '""');
  return /[",\n]/.test(escaped) ? `"${escaped}"` : escaped;
}

function safeFilename(raw: string): string {
  const name = raw
    .trim()
    .replace(/[\\/:*?"<>|]+/g, "-")
    .replace(/\s+/g, " ")
    .slice(0, 80);
  return name || "table";
}

function CanvasGallery({
  payload,
  token,
  compact = false,
  activeIndex = 0,
  onActiveIndexChange,
  onLayoutChange,
}: {
  payload: Record<string, unknown> | undefined;
  token: string;
  compact?: boolean;
  activeIndex?: number;
  onActiveIndexChange?: (activeIndex: number) => void;
  onLayoutChange?: (layout: GalleryLayout) => void;
}) {
  const { t } = useI18n();
  const images = useMemo(() => galleryImages(payload, token), [payload, token]);
  const layout = galleryLayoutValue(payload?.layout);
  const caption = stringValue(payload?.caption);
  const currentIndex = clampIndex(activeIndex, images.length);
  const isRow = layout === "row";
  const isColumn = layout === "column";
  const mainRef = useRef<HTMLDivElement | null>(null);
  const gridRef = useRef<HTMLDivElement | null>(null);
  const thumbScrollerRef = useRef<HTMLDivElement | null>(null);
  const itemRefs = useRef<Array<HTMLElement | null>>([]);
  const thumbRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const scrollFrameRef = useRef<number | null>(null);
  const previousLayoutRef = useRef(layout);
  useEffect(() => {
    if (activeIndex !== currentIndex) {
      onActiveIndexChange?.(currentIndex);
    }
  }, [activeIndex, currentIndex, onActiveIndexChange]);
  useEffect(() => {
    return () => {
      if (scrollFrameRef.current !== null) {
        window.cancelAnimationFrame(scrollFrameRef.current);
      }
    };
  }, []);
  useEffect(() => {
    const previousLayout = previousLayoutRef.current;
    if (previousLayout === layout) {
      return;
    }
    previousLayoutRef.current = layout;
    window.requestAnimationFrame(() => {
      if (layout === "row" || layout === "column") {
        scrollGalleryIndexToCenter(layout, currentIndex, mainRef.current, itemRefs.current, thumbScrollerRef.current, thumbRefs.current, "auto");
        return;
      }
      const item = itemRefs.current[currentIndex] ?? null;
      const scroller = gridRef.current?.parentElement ?? null;
      scrollChildToCenter(scroller, item, "y", "auto");
      scrollChildToCenter(scroller, item, "x", "auto");
    });
  }, [currentIndex, layout]);
  if (images.length === 0) {
    return <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">{t("canvas.galleryEmpty")}</div>;
  }
  if (compact) {
    return <CompactGallery images={images} layout={layout} caption={caption} />;
  }
  if (images.length === 1) {
    return <SingleGalleryImage image={images[0]} caption={caption} />;
  }
  const activate = (index: number, focusThumb = false, behavior: ScrollBehavior = "smooth") => {
    const next = clampIndex(index, images.length);
    onActiveIndexChange?.(next);
    if (isRow || isColumn) {
      window.requestAnimationFrame(() => {
        scrollGalleryIndexToCenter(layout, next, mainRef.current, itemRefs.current, thumbScrollerRef.current, thumbRefs.current, behavior);
        if (focusThumb) {
          thumbRefs.current[next]?.focus({ preventScroll: true });
        }
      });
    }
  };
  const syncActiveFromScroll = () => {
    if (!mainRef.current || (!isRow && !isColumn)) {
      return;
    }
    const next = nearestGalleryIndex(mainRef.current, itemRefs.current, isColumn ? "y" : "x", currentIndex);
    if (next === currentIndex) {
      return;
    }
    onActiveIndexChange?.(next);
    scrollChildToCenter(thumbScrollerRef.current, thumbRefs.current[next] ?? null, isColumn ? "y" : "x", "auto");
  };
  const handleMainScroll = () => {
    if (scrollFrameRef.current !== null) {
      return;
    }
    scrollFrameRef.current = window.requestAnimationFrame(() => {
      scrollFrameRef.current = null;
      syncActiveFromScroll();
    });
  };
  const handleNavigationKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!isRow && !isColumn) {
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      onLayoutChange?.("grid");
      return;
    }
    if (event.key === " ") {
      event.preventDefault();
      activate(currentIndex + 1, true);
      return;
    }
    if ((isRow && event.key === "ArrowLeft") || (isColumn && event.key === "ArrowUp")) {
      event.preventDefault();
      activate(currentIndex - 1, true);
      return;
    }
    if ((isRow && event.key === "ArrowRight") || (isColumn && event.key === "ArrowDown")) {
      event.preventDefault();
      activate(currentIndex + 1, true);
    }
  };
  if (isRow || isColumn) {
    return (
      <div className="flex h-full min-h-[360px] min-w-0 flex-col">
        <div
          className={cn(
            "min-w-0 overflow-hidden outline-none",
            isRow ? "flex min-h-0 flex-1 flex-col" : "flex min-h-0 flex-1",
          )}
          tabIndex={0}
          onKeyDown={handleNavigationKeyDown}
        >
          <div
            ref={mainRef}
            className={cn(
              "min-h-0 min-w-0 flex-1 gap-4",
              isRow
                ? "flex snap-x snap-mandatory overflow-x-auto overflow-y-hidden px-3 py-3"
                : "flex snap-y snap-mandatory flex-col overflow-x-hidden overflow-y-auto px-3 py-3",
            )}
            onScroll={handleMainScroll}
          >
            {images.map((image, index) => (
              <figure
                key={image.key}
                ref={(el) => {
                  itemRefs.current[index] = el;
                }}
                className="relative m-0 flex h-full min-h-full w-full shrink-0 snap-center items-center justify-center overflow-hidden rounded-md border bg-card"
              >
                <CanvasImage alt={image.alt} className="block h-full w-full object-contain" src={image.src} />
                {image.caption ? (
                  <figcaption className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-background/95 via-background/70 to-transparent px-3 pt-8 pb-2.5 text-xs font-medium text-muted-foreground">
                    {image.caption}
                  </figcaption>
                ) : null}
              </figure>
            ))}
          </div>
          <GalleryDetailThumbs
            axis={isColumn ? "y" : "x"}
            images={images}
            activeIndex={currentIndex}
            scrollerRef={thumbScrollerRef}
            thumbRefs={thumbRefs}
            onActivate={(index) => activate(index)}
          />
        </div>
        {caption ? (
          <div className="shrink-0 border-t border-border/60 bg-card/50 px-3 pt-2 pb-2.5 text-xs text-muted-foreground">
            {caption}
          </div>
        ) : null}
      </div>
    );
  }
  return (
    <div className="min-w-0">
      <div ref={gridRef} className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-2">
        {images.map((image, index) => (
          <GalleryGridTile
            key={image.key}
            image={image}
            innerRef={(el) => {
              itemRefs.current[index] = el;
            }}
            onClick={() => {
              onActiveIndexChange?.(index);
              onLayoutChange?.("row");
            }}
          />
        ))}
      </div>
      {caption ? <div className="mt-3 text-xs text-muted-foreground">{caption}</div> : null}
    </div>
  );
}

function CompactGallery({ images, layout, caption }: { images: GalleryImageItem[]; layout: GalleryLayout; caption: string }) {
  const containerClass =
    layout === "row"
      ? "flex min-w-0 gap-2 overflow-x-auto"
      : layout === "column"
        ? "grid grid-cols-1 gap-2"
        : "grid grid-cols-[repeat(auto-fit,minmax(100px,1fr))] gap-2";
  return (
    <div className="min-w-0">
      <div className={containerClass}>
        {images.map((image) => (
          <figure key={image.key} className={cn("overflow-hidden rounded border bg-background", layout === "row" ? "w-40 shrink-0" : "")}>
            <CanvasImage alt={image.alt} className="h-28 w-full object-cover" src={image.src} />
            {image.caption ? <figcaption className="px-2 py-1 text-xs text-muted-foreground">{image.caption}</figcaption> : null}
          </figure>
        ))}
      </div>
      {caption ? <div className="mt-2 text-xs text-muted-foreground">{caption}</div> : null}
    </div>
  );
}

function SingleGalleryImage({ image, caption }: { image: GalleryImageItem; caption: string }) {
  const footer = uniqueGalleryFooter(image.caption, caption);
  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-card">
      <figure className="m-0 flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-card">
        <CanvasImage alt={image.alt} className="block max-h-full max-w-full object-contain" src={image.src} />
      </figure>
      {footer.length > 0 ? (
        <div className="shrink-0 space-y-1 border-t bg-card/85 px-3 py-2 text-xs text-muted-foreground">
          {footer.map((text) => (
            <div key={text} className="truncate">
              {text}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function CanvasImage({ alt, className, src }: { alt: string; className?: string; src: string }) {
  const { t } = useI18n();
  const [failed, setFailed] = useState(false);
  if (failed) {
    return (
      <div
        aria-label={alt || t("canvas.imageUnavailable")}
        className={cn(className, "flex min-h-20 flex-col items-center justify-center gap-1.5 bg-muted/40 px-3 py-6 text-[11px] text-muted-foreground")}
        role="img"
      >
        <ImageOff className="h-5 w-5 opacity-60" />
        <span className="text-center leading-tight">{t("canvas.imageUnavailable")}</span>
      </div>
    );
  }
  return <img alt={alt} className={className} decoding="async" loading="lazy" referrerPolicy="no-referrer" src={src} onError={() => setFailed(true)} />;
}

function uniqueGalleryFooter(...values: string[]): string[] {
  const out: string[] = [];
  for (const value of values) {
    const text = value.trim();
    if (text && !out.includes(text)) {
      out.push(text);
    }
  }
  return out;
}

const GalleryGridTile = memo(function GalleryGridTile({
  image,
  innerRef,
  onClick,
}: {
  image: GalleryImageItem;
  innerRef: (element: HTMLButtonElement | null) => void;
  onClick: () => void;
}) {
  return (
    <button
      ref={innerRef}
      aria-label={image.caption || image.alt}
      className="group relative flex aspect-[4/3] items-center justify-center overflow-hidden rounded-md border bg-card text-left shadow-sm transition-colors hover:border-muted-foreground/50"
      type="button"
      onClick={onClick}
    >
      <CanvasImage alt={image.alt} className="block max-h-full max-w-full object-contain transition duration-150 group-hover:scale-[1.02]" src={image.src} />
      {image.caption ? (
        <span className="pointer-events-none absolute inset-x-0 bottom-0 truncate bg-background/75 px-1.5 py-1 text-[10px] text-foreground opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100">
          {image.caption}
        </span>
      ) : null}
    </button>
  );
});

function GalleryDetailThumbs({
  axis,
  images,
  activeIndex,
  scrollerRef,
  thumbRefs,
  onActivate,
}: {
  axis: "x" | "y";
  images: GalleryImageItem[];
  activeIndex: number;
  scrollerRef: MutableRefObject<HTMLDivElement | null>;
  thumbRefs: MutableRefObject<Array<HTMLButtonElement | null>>;
  onActivate: (index: number) => void;
}) {
  const vertical = axis === "y";
  return (
    <div
      ref={scrollerRef}
      className={cn(
        "shrink-0 bg-card/50",
        vertical ? "w-16 overflow-x-hidden overflow-y-auto border-l" : "h-16 overflow-x-auto overflow-y-hidden border-t",
      )}
    >
      <div
        className={cn(
          "gap-1.5",
          vertical
            ? "flex h-max min-h-full flex-col items-center justify-center py-2"
            : "flex h-full min-w-full w-max items-center justify-center px-2",
        )}
      >
        {images.map((image, index) => {
          const active = index === activeIndex;
          return (
            <button
              key={image.key}
              ref={(el) => {
                thumbRefs.current[index] = el;
              }}
              aria-current={active ? "true" : undefined}
              aria-label={image.caption || image.alt}
              className={cn(
                "shrink-0 overflow-hidden rounded-md border bg-card p-0.5 shadow-sm transition focus-visible:ring-2 focus-visible:ring-primary/55 focus-visible:outline-none",
                vertical ? "h-11 w-11" : "h-11 w-16",
                active
                  ? "border-primary shadow-[0_0_0_2px_hsl(var(--primary)/0.38)]"
                  : "border-border opacity-70 hover:border-muted-foreground/50 hover:opacity-100",
              )}
              tabIndex={active ? 0 : -1}
              type="button"
              onClick={() => onActivate(index)}
            >
              <CanvasImage alt="" className="block h-full w-full object-contain" src={image.src} />
            </button>
          );
        })}
      </div>
    </div>
  );
}

function clampIndex(index: number, length: number): number {
  if (length <= 0) {
    return 0;
  }
  return Math.min(length - 1, Math.max(0, Math.round(index)));
}

function scrollGalleryIndexToCenter(
  layout: GalleryLayout,
  index: number,
  main: HTMLElement | null,
  items: Array<HTMLElement | null>,
  thumbs: HTMLElement | null,
  thumbItems: Array<HTMLElement | null>,
  behavior: ScrollBehavior,
): void {
  const axis = layout === "column" ? "y" : "x";
  scrollChildToCenter(main, items[index] ?? null, axis, behavior);
  scrollChildToCenter(thumbs, thumbItems[index] ?? null, axis, behavior);
}

function nearestGalleryIndex(
  container: HTMLElement,
  items: Array<HTMLElement | null>,
  axis: "x" | "y",
  fallback: number,
): number {
  const containerRect = container.getBoundingClientRect();
  const center = axis === "y"
    ? containerRect.top + containerRect.height / 2
    : containerRect.left + containerRect.width / 2;
  let bestIndex = fallback;
  let bestDistance = Number.POSITIVE_INFINITY;
  items.forEach((item, index) => {
    if (!item) {
      return;
    }
    const itemRect = item.getBoundingClientRect();
    const itemCenter = axis === "y" ? itemRect.top + itemRect.height / 2 : itemRect.left + itemRect.width / 2;
    const distance = Math.abs(itemCenter - center);
    if (distance < bestDistance) {
      bestDistance = distance;
      bestIndex = index;
    }
  });
  return bestIndex;
}

function scrollChildToCenter(
  container: HTMLElement | null,
  child: HTMLElement | null,
  axis: "x" | "y",
  behavior: ScrollBehavior,
): void {
  if (!container || !child) {
    return;
  }
  const containerRect = container.getBoundingClientRect();
  const childRect = child.getBoundingClientRect();
  if (axis === "x") {
    const delta = childRect.left + childRect.width / 2 - (containerRect.left + containerRect.width / 2);
    container.scrollTo({ left: container.scrollLeft + delta, behavior });
    return;
  }
  const delta = childRect.top + childRect.height / 2 - (containerRect.top + containerRect.height / 2);
  container.scrollTo({ top: container.scrollTop + delta, behavior });
}

function galleryImages(payload: Record<string, unknown> | undefined, token: string): GalleryImageItem[] {
  const items = Array.isArray(payload?.items) ? payload.items : [];
  return items.flatMap((item, index) => {
    const record = asRecord(item);
    const src = imageSource(record, token);
    if (!src) {
      return [];
    }
    const caption = stringValue(record?.caption);
    const alt = stringValue(record?.alt) || caption;
    return [{ src, alt, caption, key: `${src}-${index}` }];
  });
}

function galleryLayoutForItem(item: CanvasItem): GalleryLayout | null {
  const payload = asRecord(item.item);
  const kind = stringValue(payload?.kind) || item.kind;
  return kind === "gallery" && galleryItemCount(payload) > 1 ? galleryLayoutValue(payload?.layout) : null;
}

function galleryItemCount(payload: Record<string, unknown> | undefined): number {
  const items = Array.isArray(payload?.items) ? payload.items : [];
  return items.filter((item) => Boolean(imageSource(asRecord(item), ""))).length;
}

function galleryLayoutValue(value: unknown): GalleryLayout {
  return value === "row" || value === "column" ? value : "grid";
}

function imageSource(record: Record<string, unknown> | undefined, token: string): string {
  const src = stringValue(record?.src) || stringValue(record?.url);
  if (src) {
    return authenticatedImageSource(src, token);
  }
  const data = stringValue(record?.data);
  if (!data) {
    return "";
  }
  return `data:${stringValue(record?.mime) || "image/jpeg"};base64,${data}`;
}

function authenticatedImageSource(raw: string, token: string): string {
  const src = raw.trim();
  if (!src || !token) {
    return src;
  }
  if (isSessionAttachmentPath(src)) {
    return attachmentResourceURL({ url: src }, token);
  }
  try {
    const url = new URL(src, window.location.href);
    if (isSessionAttachmentPath(url.pathname) && isAppResourceOrigin(url)) {
      url.searchParams.set("token", token);
      return url.toString();
    }
  } catch {
    return src;
  }
  return src;
}

function isSessionAttachmentPath(path: string): boolean {
  return /^\/sessions\/[^/]+\/attachments\//.test(path);
}

function isAppResourceOrigin(url: URL): boolean {
  if (url.origin === window.location.origin) {
    return true;
  }
  try {
    return url.origin === new URL(apiURL("/"), window.location.href).origin;
  } catch {
    return false;
  }
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

function chartTypeValue(value: unknown): ChartType {
  return value === "line" || value === "area" || value === "pie" || value === "donut" ? value : "bar";
}

function sanitizeChartData(raw: unknown[]): Record<string, unknown>[] {
  return raw
    .map((row) => asRecord(row))
    .filter((row): row is Record<string, unknown> => Boolean(row))
    .map((row) => {
      const out: Record<string, unknown> = {};
      for (const [key, value] of Object.entries(row)) {
        const number = toNumber(value);
        out[key] = number == null ? value : number;
      }
      return out;
    });
}

function inferChartXKey(data: Record<string, unknown>[]): string {
  const first = data[0] ?? {};
  return Object.keys(first).find((key) => toNumber(first[key]) == null) ?? Object.keys(first)[0] ?? "name";
}

function normalizeChartSeries(
  raw: unknown,
  data: Record<string, unknown>[],
  xKey: string,
  valueKey?: string,
): ChartSeries[] {
  if (Array.isArray(raw)) {
    const normalized = raw
      .map((entry) => {
        const record = asRecord(entry);
        const key = stringValue(record?.key);
        if (!key) {
          return null;
        }
        return {
          key,
          ...(stringValue(record?.label) ? { label: stringValue(record?.label) } : {}),
          ...(stringValue(record?.color) ? { color: stringValue(record?.color) } : {}),
        };
      })
      .filter((entry): entry is ChartSeries => Boolean(entry));
    if (normalized.length > 0) {
      return normalized;
    }
  }
  if (valueKey) {
    return [{ key: valueKey }];
  }
  const first = data[0] ?? {};
  return Object.keys(first)
    .filter((key) => key !== xKey && toNumber(first[key]) != null)
    .map((key) => ({ key }));
}

function buildChartConfig(series: ChartSeries[]): ChartConfig {
  return Object.fromEntries(
    series.map((entry, index) => [
      entry.key,
      {
        label: entry.label ?? entry.key,
        color: chartSeriesColor(entry, index),
      },
    ]),
  );
}

function chartSeriesColor(series: ChartSeries, index: number): string {
  return series.color || CHART_COLORS[index % CHART_COLORS.length] || "var(--chart-1)";
}

function chartSeriesCSSColor(series: ChartSeries, index: number): string {
  return series.color || `var(--color-${series.key}, ${CHART_COLORS[index % CHART_COLORS.length] || "var(--chart-1)"})`;
}

function normalizeColumns(raw: unknown): Column[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw
    .map((column, index) => {
      const object = asRecord(column);
      if (!object) {
        const key = stringValue(column) || String(index);
        return { key, label: key };
      }
      const key = String(object.key ?? object.label ?? index);
      const label = String(object.label ?? object.key ?? index);
      const next: Column = { key, label };
      if (isColumnType(object.type)) next.type = object.type;
      const map = normalizeStringRecord(object.map);
      if (map) next.map = map;
      const colors = normalizeColumnColors(object.colors);
      if (colors) next.colors = colors;
      if (typeof object.divide === "number") next.divide = object.divide;
      if (typeof object.decimals === "number") next.decimals = object.decimals;
      if (typeof object.thousands === "boolean") next.thousands = object.thousands;
      if (typeof object.currency === "string") next.currency = object.currency;
      if (typeof object.format === "string") next.format = object.format;
      if (typeof object.max === "number") next.max = object.max;
      return next;
    })
    .filter((column) => column.key);
}

function isColumnType(value: unknown): value is ColumnType {
  return (
    value === "text" ||
    value === "enum" ||
    value === "number" ||
    value === "currency" ||
    value === "date" ||
    value === "datetime" ||
    value === "truncate"
  );
}

function normalizeStringRecord(value: unknown): Record<string, string> | undefined {
  const record = asRecord(value);
  if (!record) {
    return undefined;
  }
  const out: Record<string, string> = {};
  for (const [key, raw] of Object.entries(record)) {
    if (typeof raw === "string" || typeof raw === "number" || typeof raw === "boolean") {
      out[key] = String(raw);
    }
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function normalizeColumnColors(value: unknown): Record<string, ColumnColor> | undefined {
  const record = asRecord(value);
  if (!record) {
    return undefined;
  }
  const out: Record<string, ColumnColor> = {};
  for (const [key, raw] of Object.entries(record)) {
    const color = normalizeColumnColor(raw);
    if (color) {
      out[key] = color;
    }
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function normalizeColumnColor(value: unknown): ColumnColor | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const color = value.trim().toLowerCase();
  if (isSemanticColor(color)) {
    return color;
  }
  return /^#(?:[0-9a-f]{3}|[0-9a-f]{6})$/i.test(color) ? (color as `#${string}`) : undefined;
}

function TableCell({ row, column, fallbackIndex }: { row: unknown; column: Column; fallbackIndex: number }) {
  const formatted = formatTableCell(rawCellValue(row, column, fallbackIndex), column);
  if (formatted.isBadge) {
    const semanticColor = isSemanticColor(formatted.color) ? formatted.color : undefined;
    const style = formatted.color?.startsWith("#")
      ? { backgroundColor: hexWithAlpha(formatted.color, "1f"), color: formatted.color }
      : undefined;
    return (
      <span
        className={`inline-block rounded px-1.5 py-0.5 text-[11px] font-medium ${semanticColor ? BADGE_COLOR_CLASS[semanticColor] : "bg-muted text-muted-foreground"}`}
        style={style}
        title={formatted.title ?? formatted.text}
      >
        {formatted.text}
      </span>
    );
  }
  return <span title={formatted.title ?? formatted.text}>{formatted.text}</span>;
}

function rawCellValue(row: unknown, column: Column, fallbackIndex: number): unknown {
  if (Array.isArray(row)) {
    return row[fallbackIndex];
  }
  const record = asRecord(row);
  if (!record) {
    return undefined;
  }
  const cleaned = column.key.replace(/\[([^\]]+)\]\([^)]*\)/g, "$1");
  if (!cleaned.includes(".")) {
    return record[cleaned];
  }
  let current: unknown = record;
  for (const part of cleaned.split(".").filter(Boolean)) {
    const currentRecord = asRecord(current);
    if (!currentRecord) {
      return undefined;
    }
    current = currentRecord[part];
  }
  return current;
}

function columnAlignClass(column: Column): string {
  return inferColumnKind(column) === "number" ? "text-right tabular-nums" : "text-left";
}

type FormattedCell = { text: string; color?: ColumnColor; isBadge?: boolean; title?: string };
type ColumnKind = "number" | "enum" | "date" | "datetime" | "identifier" | "text";

function inferColumnKind(column: Column): ColumnKind {
  const key = `${column.key} ${column.label}`.toLowerCase();
  if (column.type === "number" || column.type === "currency" || column.currency || column.divide != null || column.decimals != null) {
    return "number";
  }
  if (column.type === "enum" || column.map || column.colors || /\b(status|state|type|kind)\b/.test(key)) {
    return "enum";
  }
  if (column.type === "datetime" || /time|datetime|created|updated|create|update/.test(key)) {
    return "datetime";
  }
  if (column.type === "date" || /date|入住|离店/.test(key)) {
    return "date";
  }
  if (/(^|[_\s-])(id|code|no|serial|number)([_\s-]|$)|订单号|编号|单号/.test(key)) {
    return "identifier";
  }
  return "text";
}

function formatTableCell(value: unknown, column: Column): FormattedCell {
  const type = effectiveColumnType(column);
  if (value == null || value === "") {
    return { text: "" };
  }
  if (type === "enum") {
    const code = String(value);
    return {
      text: column.map?.[code] ?? code,
      color: column.colors?.[code],
      isBadge: true,
    };
  }
  if (type === "number") {
    return { text: formatNumber(value, column) };
  }
  if (type === "currency") {
    return { text: formatCurrency(value, column) };
  }
  if (type === "date") {
    return { text: formatDateTime(value, column.format ?? "YYYY-MM-DD") };
  }
  if (type === "datetime") {
    return { text: formatDateTime(value, column.format ?? "YYYY-MM-DD HH:mm") };
  }
  if (type === "truncate") {
    const text = formatPlainCell(value);
    const max = column.max ?? 30;
    return text.length <= max ? { text } : { text: `${text.slice(0, max)}...`, title: text };
  }
  return { text: formatPlainCell(value) };
}

function effectiveColumnType(column: Column): ColumnType {
  if (column.type) {
    return column.type;
  }
  if (column.map || column.colors) {
    return "enum";
  }
  if (column.currency) {
    return "currency";
  }
  if (typeof column.divide === "number" || typeof column.decimals === "number" || typeof column.thousands === "boolean") {
    return "number";
  }
  return "text";
}

function formatPlainCell(value: unknown): string {
  if (value == null) {
    return "";
  }
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value);
}

function formatNumber(value: unknown, column: Column): string {
  const parsed = toNumber(value);
  if (parsed == null) {
    return String(value);
  }
  const scaled = typeof column.divide === "number" && column.divide !== 0 ? parsed / column.divide : parsed;
  const decimals = typeof column.decimals === "number" && column.decimals >= 0 ? column.decimals : undefined;
  const useThousands = column.thousands !== false;
  if (decimals != null) {
    return useThousands
      ? scaled.toLocaleString("en-US", { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
      : scaled.toFixed(decimals);
  }
  return useThousands ? scaled.toLocaleString("en-US") : String(scaled);
}

function formatCurrency(value: unknown, column: Column): string {
  const decimals = typeof column.decimals === "number" ? column.decimals : 2;
  const merged = { ...column, decimals, thousands: column.thousands !== false };
  const text = formatNumber(value, merged);
  if (text === String(value)) {
    return text;
  }
  const code = (column.currency ?? "").toUpperCase();
  const symbol = code ? CURRENCY_SYMBOL[code] ?? `${code} ` : "";
  return `${symbol}${text}`;
}

const CURRENCY_SYMBOL: Record<string, string> = {
  CNY: "¥",
  RMB: "¥",
  USD: "$",
  EUR: "€",
  GBP: "£",
  JPY: "¥",
  HKD: "HK$",
  TWD: "NT$",
  SGD: "S$",
  KRW: "₩",
};

function toNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value.trim().replace(/,/g, ""));
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

function formatDateTime(value: unknown, format: string): string {
  const date = dateFromValue(value);
  if (!date) {
    return String(value);
  }
  const pad2 = (n: number) => String(n).padStart(2, "0");
  return format
    .replace(/YYYY/g, String(date.getFullYear()))
    .replace(/MM/g, pad2(date.getMonth() + 1))
    .replace(/DD/g, pad2(date.getDate()))
    .replace(/HH/g, pad2(date.getHours()))
    .replace(/mm/g, pad2(date.getMinutes()))
    .replace(/ss/g, pad2(date.getSeconds()));
}

function dateFromValue(value: unknown): Date | null {
  if (value instanceof Date && !Number.isNaN(value.getTime())) {
    return value;
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    const date = new Date(value < 1e12 ? value * 1000 : value);
    return Number.isNaN(date.getTime()) ? null : date;
  }
  if (typeof value !== "string" || !value.trim()) {
    return null;
  }
  const trimmed = value.trim();
  const date = /^-?\d+$/.test(trimmed)
    ? new Date(Number(trimmed) < 1e12 ? Number(trimmed) * 1000 : Number(trimmed))
    : new Date(trimmed);
  return Number.isNaN(date.getTime()) ? null : date;
}

function isSemanticColor(value: unknown): value is SemanticColor {
  return value === "red" || value === "amber" || value === "green" || value === "sky" || value === "violet" || value === "gray";
}

function hexWithAlpha(color: string, alpha: string): string {
  if (/^#[0-9a-f]{3}$/i.test(color)) {
    return `#${color
      .slice(1)
      .split("")
      .map((ch) => ch + ch)
      .join("")}${alpha}`;
  }
  return `${color}${alpha}`;
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
