import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Compass, Plus, SquareTerminal } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  clearClosedCanvasItems,
  createClosedCanvasItem,
  deleteCanvasItem,
  deleteClosedCanvasItem,
  listClosedCanvasItems,
  listCanvasItems,
  getSession,
  putCanvasItem,
  type BrowserTab,
  type CanvasItemPayload,
  type Terminal,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { electronBrowserBridge } from "@/browser/electronBridge";
import { browserTabTitle } from "@/browser/helpers";
import {
  useElectronRequiredBrowserTabs,
  type ElectronBrowserSurfaceTab,
} from "@/browser/useElectronRequiredBrowserTabs";
import type { GalleryLayout } from "@/components/canvas/CanvasItemContent";
import { CanvasFreeformSurface } from "@/components/canvas/CanvasFreeformSurface";
import { titleForCanvasItem } from "@/components/canvas/CanvasKindIcon";
import { CanvasToolbar } from "@/components/canvas/CanvasToolbar";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
} from "@/components/AppMenu";
import { FilePreviewSurface, filePreviewTitle } from "@/components/canvas/FilePreviewSurface";
import { asRecord, numberValue, stringValue } from "@/components/canvas/canvasPayload";
import { Spinner } from "@/components/Spinner";
import { ProjectBrowserSurface } from "@/components/project/ProjectBrowserSurface";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import type { CanvasItem, ClosedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { useVisibleBrowserReveal } from "@/state/browserRevealStore";
import {
  closeFilePreview,
  consumeFilePreviewReveal,
  type FilePreview,
  useFilePreviews,
  useFilePreviewReveal,
} from "@/state/filePreviewStore";
import { useVisibleProjectFileReveal } from "@/state/projectRevealStore";
import {
  clearVisibleUIContext,
  setVisibleUIContext,
  type UIContextPart,
} from "@/state/uiContextStore";
import { TerminalSizeProbe, TerminalSurface } from "@/terminal/TerminalSurface";
import {
  DEFAULT_TERMINAL_DIMENSIONS,
  type TerminalDimensions,
} from "@/terminal/terminalDimensions";

import { BrowserWorkspaceSurface } from "./BrowserWorkspaceSurface";
import { WorkspaceEmpty } from "./WorkspaceEmpty";
import { WorkspaceResourceTabs } from "./WorkspaceResourceTabs";
import { CanvasLibraryControl, CanvasSurfaceControl, ProjectSurfaceControl } from "./WorkspaceSurfaceControls";
import { useWorkspaceBrowserSurface } from "./useWorkspaceBrowserSurface";
import { useWorkspaceTerminals } from "./useWorkspaceTerminals";

type WorkspacePaneProps = {
  token: string;
  sessionID?: string;
  secondarySessionID?: string;
};

export function WorkspacePane({ token, sessionID, secondarySessionID }: WorkspacePaneProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const terminalDimensionsRef = useRef<TerminalDimensions>({ ...DEFAULT_TERMINAL_DIMENSIONS });
  const actorSessionIDRef = useRef("");
  const canvasSessionStateRef = useRef("");
  const retainedTokenRef = useRef(token);
  const seenCanvasItemIDsRef = useRef<Set<string>>(new Set());
  const hasSeenCanvasItemsRef = useRef(false);
  const [activeCanvasItemIDs, setActiveCanvasItemIDs] = useState<Record<string, string>>({});
  const [canvasLibraryOpen, setCanvasLibraryOpen] = useState(false);
  const [retainedBrowserTabs, setRetainedBrowserTabs] = useState<Record<string, BrowserTab[]>>({});
  const [retainedTerminals, setRetainedTerminals] = useState<Record<string, Terminal[]>>({});
  const [retainedFilePreviews, setRetainedFilePreviews] = useState<Record<string, FilePreview>>({});
  const [projectUIContext, setProjectUIContext] = useState<UIContextPart>();
  const projectFileReveal = useVisibleProjectFileReveal(sessionID, secondarySessionID);
  const browserReveal = useVisibleBrowserReveal(sessionID, secondarySessionID);
  const filePreviewReveal = useFilePreviewReveal(sessionID, secondarySessionID);
  const [workspaceSessionID, setWorkspaceSessionID] = useState(sessionID || secondarySessionID || "");
  useEffect(() => {
    if (projectFileReveal?.sessionID) {
      setWorkspaceSessionID(projectFileReveal.sessionID);
    }
  }, [projectFileReveal?.serial]);
  useEffect(() => {
    if (browserReveal?.sessionID) {
      setWorkspaceSessionID(browserReveal.sessionID);
    }
  }, [browserReveal?.serial]);
  useEffect(() => {
    if (filePreviewReveal?.sessionID) {
      setWorkspaceSessionID(filePreviewReveal.sessionID);
    }
  }, [filePreviewReveal?.serial]);
  useEffect(() => {
    setWorkspaceSessionID((current) =>
      current && (current === sessionID || current === secondarySessionID)
        ? current
        : sessionID || secondarySessionID || "",
    );
  }, [secondarySessionID, sessionID]);
  const actorSessionID = workspaceSessionID || sessionID || actorSessionIDRef.current;
  useEffect(() => {
    if (actorSessionID) {
      actorSessionIDRef.current = actorSessionID;
    }
  }, [actorSessionID]);
  const primaryFilePreviews = useFilePreviews(sessionID);
  const secondaryFilePreviews = useFilePreviews(secondarySessionID);
  const requiredBrowserTabs = useElectronRequiredBrowserTabs(token);
  const allFilePreviews = useMemo(
    () =>
      [...primaryFilePreviews, ...secondaryFilePreviews].filter(
        (preview, index, all) => all.findIndex((entry) => entry.id === preview.id) === index,
      ),
    [primaryFilePreviews, secondaryFilePreviews],
  );
  const filePreviews = actorSessionID === secondarySessionID ? secondaryFilePreviews : primaryFilePreviews;
  const mountedFilePreviews = useMemo(() => {
    const activeSessionIDs = new Set([sessionID, secondarySessionID].filter(Boolean));
    return [
      ...Object.values(retainedFilePreviews).filter((preview) => !activeSessionIDs.has(preview.sessionID)),
      ...allFilePreviews,
    ];
  }, [allFilePreviews, retainedFilePreviews, secondarySessionID, sessionID]);
  const [activeFilePreviewIDs, setActiveFilePreviewIDs] = useState<Record<string, string>>(() => {
    const initial: Record<string, string> = {};
    const primaryPreviewID = primaryFilePreviews.at(-1)?.id;
    const secondaryPreviewID = secondaryFilePreviews.at(-1)?.id;
    if (sessionID && primaryPreviewID) initial[sessionID] = primaryPreviewID;
    if (secondarySessionID && secondaryPreviewID) initial[secondarySessionID] = secondaryPreviewID;
    return initial;
  });
  const activeFilePreviewID = actorSessionID ? activeFilePreviewIDs[actorSessionID] : undefined;
  const setActiveFilePreviewID = (previewID: string | undefined) => {
    if (!actorSessionID) {
      return;
    }
    setActiveFilePreviewIDs((current) => {
      if (previewID) {
        return current[actorSessionID] === previewID ? current : { ...current, [actorSessionID]: previewID };
      }
      if (!current[actorSessionID]) {
        return current;
      }
      const next = { ...current };
      delete next[actorSessionID];
      return next;
    });
  };
  useEffect(() => {
    const activeSessionIDs = new Set([sessionID, secondarySessionID].filter(Boolean));
    setRetainedFilePreviews((current) => {
      const next = Object.fromEntries(
        Object.entries(current).filter(([, preview]) => !activeSessionIDs.has(preview.sessionID)),
      );
      allFilePreviews.forEach((preview) => {
        next[preview.id] = preview;
      });
      return sameResourceRecord(current, next) ? current : next;
    });
  }, [allFilePreviews, secondarySessionID, sessionID]);
  const enabled = Boolean(token && actorSessionID);

  useEffect(() => {
    if (!actorSessionID || canvasSessionStateRef.current === actorSessionID) {
      return;
    }
    canvasSessionStateRef.current = actorSessionID;
    seenCanvasItemIDsRef.current = new Set();
    hasSeenCanvasItemsRef.current = false;
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

  const items = itemsQuery.data?.items ?? [];
  const closedItems = closedItemsQuery.data?.items ?? [];
  const selectedCanvasItemID = activeCanvasItemIDs[actorSessionID];
  const activeCanvasItem = items.find((item) => item.id === selectedCanvasItemID) || topCanvasItem(items);
  const selectCanvasItem = (itemID: string) => {
    if (!actorSessionID) return;
    setActiveCanvasItemIDs((current) => (
      current[actorSessionID] === itemID ? current : { ...current, [actorSessionID]: itemID }
    ));
  };
  const {
    activeBrowserTabID,
    activeSurface,
    browserActive,
    browserTabs,
    browserSurfacePending,
    browserSurfaceVisible,
    closeBrowserTab,
    closingBrowserTabID,
    createNewBrowserTab,
    creatingBrowserTab,
    selectCanvasSurface,
    selectBrowserTab,
    selectProjectSurface,
    selectTerminalSurface,
    selectWorkspaceSurface,
  } = useWorkspaceBrowserSurface({
    enabled,
    hasTransientSurface: filePreviews.length > 0,
    itemsLength: items.length,
    itemsPending: itemsQuery.isLoading,
    sessionID: actorSessionID,
    token,
  });
  const terminalActive = activeSurface === "terminal";
  const sessionQuery = useQuery({
    enabled,
    queryKey: queryKeys.session(actorSessionID),
    queryFn: () => getSession(token, actorSessionID),
    staleTime: 10_000,
  });
  const hasProject = Boolean(sessionQuery.data?.projectID);
  const projectActive = activeSurface === "project" && hasProject;
  const {
    activeTerminalID,
    closeTerminal,
    closingTerminalID,
    createNewTerminal,
    createNewTerminalAt,
    creatingTerminal,
    selectTerminal,
    terminals,
    terminalInitialDimensions,
  } = useWorkspaceTerminals({
    active: terminalActive,
    enabled,
    getInitialDimensions: () => terminalDimensionsRef.current,
    sessionID: actorSessionID,
    token,
    onActivate: () => {
      setActiveFilePreviewID(undefined);
      selectTerminalSurface();
    },
    onDeactivate: () => {
      if (browserTabs.length > 0) {
        selectBrowserTab(activeBrowserTabID || browserTabs[0].id);
      } else if (filePreviews.length > 0) {
        setActiveFilePreviewID(filePreviews[0].id);
        selectCanvasSurface();
      } else {
        if (items.length > 0) {
          selectCanvasSurface();
        } else {
          selectWorkspaceSurface();
        }
      }
    },
  });
  useEffect(() => {
    if (!actorSessionID) {
      return;
    }
    setRetainedBrowserTabs((current) =>
      sameResourceList(current[actorSessionID], browserTabs)
        ? current
        : { ...current, [actorSessionID]: browserTabs },
    );
  }, [actorSessionID, browserTabs]);
  useEffect(() => {
    const bridge = electronBrowserBridge();
    if (!bridge) {
      return;
    }
    return bridge.onUpdated((snapshot) => {
      if (snapshot.status !== "lost") {
        return;
      }
      setRetainedBrowserTabs((current) => {
        const tabs = current[snapshot.sessionID];
        if (!tabs?.some((tab) => tab.id === snapshot.tabID)) {
          return current;
        }
        const next = { ...current };
        const remaining = tabs.filter((tab) => tab.id !== snapshot.tabID);
        if (remaining.length > 0) {
          next[snapshot.sessionID] = remaining;
        } else {
          delete next[snapshot.sessionID];
        }
        return next;
      });
    });
  }, []);
  useEffect(() => {
    if (!actorSessionID) {
      return;
    }
    setRetainedTerminals((current) =>
      sameResourceList(current[actorSessionID], terminals)
        ? current
        : { ...current, [actorSessionID]: terminals },
    );
  }, [actorSessionID, terminals]);
  useEffect(() => {
    if (retainedTokenRef.current === token) {
      return;
    }
    retainedTokenRef.current = token;
    setRetainedBrowserTabs({});
    setRetainedTerminals({});
    setRetainedFilePreviews({});
    setActiveFilePreviewIDs({});
  }, [token]);
  const mountedBrowserTabs = useMemo(() => {
    const mounted: Record<string, ElectronBrowserSurfaceTab[]> = { ...retainedBrowserTabs };
    if (actorSessionID) {
      mounted[actorSessionID] = browserTabs;
    }
    Object.entries(requiredBrowserTabs).forEach(([targetSessionID, requiredTabs]) => {
      mounted[targetSessionID] = mergeBrowserSurfaceTabs(mounted[targetSessionID], requiredTabs);
    });
    return mounted;
  }, [actorSessionID, browserTabs, requiredBrowserTabs, retainedBrowserTabs]);
  const mountedTerminals = actorSessionID
    ? { ...retainedTerminals, [actorSessionID]: terminals }
    : retainedTerminals;
  const updateMountedTerminalStatus = (
    targetSessionID: string,
    terminalID: string,
    status: Terminal["status"],
    exitCode?: number,
  ) => {
    const update = (current: Terminal[] | undefined) =>
      (current || []).map((item) =>
        item.id === terminalID ? { ...item, status, exitCode, updatedAt: new Date().toISOString() } : item,
      );
    const retained = retainedTerminals[targetSessionID];
    queryClient.setQueryData<{ terminals: Terminal[] }>(queryKeys.terminals(targetSessionID), (current) => ({
      terminals: update(current?.terminals ?? retained),
    }));
    setRetainedTerminals((current) => ({
      ...current,
      [targetSessionID]: update(current[targetSessionID]),
    }));
  };
  const activeFilePreview = filePreviews.find((preview) => preview.id === activeFilePreviewID);
  const filePreviewActive = Boolean(activeFilePreview && activeSurface === "canvas");

  const visibleUIContext = useMemo<UIContextPart | undefined>(() => {
    if (!actorSessionID) {
      return undefined;
    }
    if (projectActive) {
      return projectUIContext || { type: "ui_context", surface: "project" };
    }
    if (filePreviewActive && activeFilePreview) {
      return {
        type: "ui_context",
        surface: "file_preview",
        resource: "file",
        id: activeFilePreview.id,
        name: filePreviewTitle(activeFilePreview.path),
        path: activeFilePreview.path,
        kind: activeFilePreview.source,
      };
    }
    if (activeSurface === "canvas") {
      if (!activeCanvasItem) {
        return { type: "ui_context", surface: "canvas" };
      }
      const payload = asRecord(activeCanvasItem.item);
      return {
        type: "ui_context",
        surface: "canvas",
        resource: "canvas_item",
        id: activeCanvasItem.id,
        name: titleForCanvasItem(activeCanvasItem, t),
        kind: stringValue(payload?.kind) || activeCanvasItem.kind,
      };
    }
    if (activeSurface === "browser") {
      const tab = browserTabs.find((entry) => entry.id === activeBrowserTabID);
      return tab
        ? {
            type: "ui_context",
            surface: "browser",
            resource: "browser_tab",
            id: tab.id,
            name: browserTabTitle(tab, t("browser.newTab"), t("browser.newTab")),
            url: tab.url,
            kind: tab.mode,
          }
        : { type: "ui_context", surface: "browser" };
    }
    if (activeSurface === "terminal") {
      const terminal = terminals.find((entry) => entry.id === activeTerminalID);
      return terminal
        ? {
            type: "ui_context",
            surface: "terminal",
            resource: "terminal",
            id: terminal.id,
            name: terminal.title || filePreviewTitle(terminal.cwd),
            path: terminal.cwd,
            kind: terminal.status,
          }
        : { type: "ui_context", surface: "terminal" };
    }
    return undefined;
  }, [
    activeBrowserTabID,
    activeCanvasItem,
    activeFilePreview,
    activeSurface,
    activeTerminalID,
    actorSessionID,
    browserTabs,
    filePreviewActive,
    projectActive,
    projectUIContext,
    t,
    terminals,
  ]);

  useEffect(() => {
    setVisibleUIContext(actorSessionID, visibleUIContext);
  }, [actorSessionID, visibleUIContext]);
  useEffect(() => () => clearVisibleUIContext(actorSessionID), [actorSessionID]);

  useEffect(() => {
    if (!filePreviewReveal || filePreviewReveal.sessionID !== actorSessionID) {
      return;
    }
    selectCanvasSurface();
    setActiveFilePreviewID(filePreviewReveal.previewID);
    consumeFilePreviewReveal(filePreviewReveal.serial);
  }, [actorSessionID, filePreviewReveal, selectCanvasSurface]);

  useEffect(() => {
    if (activeFilePreviewID && !filePreviews.some((preview) => preview.id === activeFilePreviewID)) {
      setActiveFilePreviewID(undefined);
    }
  }, [activeFilePreviewID, filePreviews]);

  const selectPersistentCanvasSurface = () => {
    setActiveFilePreviewID(undefined);
    selectCanvasSurface();
  };

  const createBrowserSurface = () => {
    setActiveFilePreviewID(undefined);
    createNewBrowserTab();
  };

  const activateProjectSurface = () => {
    if (!hasProject) {
      return;
    }
    setActiveFilePreviewID(undefined);
    selectProjectSurface();
  };

  useEffect(() => {
    if (activeSurface === "project" && !sessionQuery.isLoading && !hasProject) {
      if (items.length > 0) {
        selectCanvasSurface();
      } else {
        selectWorkspaceSurface();
      }
    }
  }, [activeSurface, hasProject, items.length, selectCanvasSurface, selectWorkspaceSurface, sessionQuery.isLoading]);

  useEffect(() => {
    if (projectFileReveal?.sessionID === actorSessionID && hasProject) {
      selectProjectSurface();
    }
  }, [actorSessionID, hasProject, projectFileReveal?.serial, selectProjectSurface]);

  const selectFilePreview = (previewID: string) => {
    selectCanvasSurface();
    setActiveFilePreviewID(previewID);
  };

  const removeFilePreview = (preview: FilePreview) => {
    closeFilePreview(preview.sessionID, preview.id);
    if (activeFilePreviewID !== preview.id) {
      return;
    }
    const closedIndex = filePreviews.findIndex((entry) => entry.id === preview.id);
    const next = filePreviews[closedIndex + 1] || filePreviews[closedIndex - 1];
    if (next) {
      setActiveFilePreviewID(next.id);
    } else if (terminals.length > 0) {
      setActiveFilePreviewID(undefined);
      selectTerminal(activeTerminalID || terminals[0].id);
    } else if (browserTabs.length > 0) {
      setActiveFilePreviewID(undefined);
      selectBrowserTab(activeBrowserTabID || browserTabs[0].id);
    } else {
      setActiveFilePreviewID(undefined);
      if (items.length === 0) {
        selectWorkspaceSurface();
      }
    }
  };

  const galleryLayoutMutation = useMutation({
    mutationFn: ({ item, layout }: { item: CanvasItem; layout: GalleryLayout }) => {
      const payload = asRecord(item.item) || {};
      const title = titleForCanvasItem(item, t);
      const kind = stringValue(payload.kind) || item.kind;
      return putCanvasItem(token, actorSessionID, item.id, {
        id: item.id,
        kind,
        title,
        item: { ...payload, kind, title, layout },
        window: item.window,
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
        title: titleForCanvasItem(item, t),
        item: item.item,
        window: item.window,
        closedAt: new Date().toISOString(),
      });
      await deleteCanvasItem(token, actorSessionID, item.id);
    },
    onMutate: async (item) => {
      const key = queryKeys.canvasItems(actorSessionID);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<{ items: CanvasItem[] }>(key);
      queryClient.setQueryData<{ items: CanvasItem[] }>(key, (current) => ({
        items: (current?.items || []).filter((entry) => entry.id !== item.id),
      }));
      if (activeCanvasItem?.id === item.id) {
        const next = topCanvasItem(items.filter((entry) => entry.id !== item.id));
        setActiveCanvasItemIDs((current) => {
          if (next) return { ...current, [actorSessionID]: next.id };
          return withoutKey(current, actorSessionID);
        });
      }
      return { previous };
    },
    onError: (_error, _item, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.canvasItems(actorSessionID), context.previous);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) });
      toast.error(t("canvas.closeFailed"));
    },
    onSuccess: (_result, item) => {
      if (actorSessionID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) });
      }
    },
  });

  const restoreMutation = useMutation({
    mutationFn: async (entry: ClosedCanvasItem) => {
      const item = await putCanvasItem(token, actorSessionID, entry.sourceItemID, canvasPayloadFromClosedItem(entry));
      await deleteClosedCanvasItem(token, actorSessionID, entry.id);
      return item;
    },
    onSuccess: (item) => {
      selectCanvasItem(item.id);
      selectPersistentCanvasSurface();
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
    if (items.some((item) => item.id === entry.sourceItemID)) {
      void deleteClosedCanvasItem(token, actorSessionID, entry.id).then(() =>
        queryClient.invalidateQueries({ queryKey: queryKeys.closedCanvasItems(actorSessionID) }),
      );
      selectCanvasItem(entry.sourceItemID);
      selectPersistentCanvasSurface();
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

  useEffect(() => {
    if (itemsQuery.isLoading) return;
    if (items.length === 0) {
      seenCanvasItemIDsRef.current = new Set();
      hasSeenCanvasItemsRef.current = true;
      setActiveCanvasItemIDs((current) => (
        current[actorSessionID] ? withoutKey(current, actorSessionID) : current
      ));
      return;
    }
    const seenIDs = seenCanvasItemIDsRef.current;
    const newItems = hasSeenCanvasItemsRef.current ? items.filter((item) => !seenIDs.has(item.id)) : [];
    seenCanvasItemIDsRef.current = new Set(items.map((item) => item.id));
    hasSeenCanvasItemsRef.current = true;
    const selected = activeCanvasItemIDs[actorSessionID];
    if (newItems.length > 0) {
      selectCanvasItem(newItems.at(-1)!.id);
      selectPersistentCanvasSurface();
    } else if (!selected || !items.some((item) => item.id === selected)) {
      selectCanvasItem(items[0].id);
    }
  }, [activeCanvasItemIDs, actorSessionID, items, itemsQuery.isLoading]);

  return (
    <aside className="relative flex h-full shrink-0 flex-col bg-[var(--workspace-background)] text-sidebar-foreground">
      <div className="relative z-30 flex h-(--toolbar-h) shrink-0 items-center gap-2 overflow-hidden pr-(--workspace-toolbar-pr) pl-(--workspace-toolbar-pl)">
        <WorkspaceResourceTabs
            activeBrowserTabID={activeBrowserTabID}
            activeFilePreviewID={activeFilePreviewID}
            activeSurface={activeSurface}
            activeTerminalID={activeTerminalID}
            browserTabs={browserTabs}
            closingBrowserTabID={closingBrowserTabID}
            closingTerminalID={closingTerminalID}
            filePreviewActive={filePreviewActive}
            filePreviewTabs={filePreviews.map((preview) => ({
              id: preview.id,
              label: filePreviewTitle(preview.path),
              openedAt: preview.openedAt,
              path: preview.path,
            }))}
            leadingTabs={(
              <>
                {hasProject ? <ProjectSurfaceControl active={projectActive} onActivate={activateProjectSurface} /> : null}
                <CanvasSurfaceControl
                  active={activeSurface === "canvas" && !filePreviewActive}
                  onActivate={selectPersistentCanvasSurface}
                />
              </>
            )}
            orderScope={actorSessionID || "workspace"}
            terminalTabs={terminals}
            onCloseBrowser={(tabID) => {
              const closingLastActiveBrowser =
                activeSurface === "browser" && activeBrowserTabID === tabID && browserTabs.length === 1;
              closeBrowserTab(tabID);
              if (closingLastActiveBrowser && terminals.length > 0) {
                selectTerminal(activeTerminalID || terminals[0].id);
              } else if (closingLastActiveBrowser && filePreviews.length > 0) {
                selectFilePreview(activeFilePreviewID || filePreviews[0].id);
              }
            }}
            onCloseFilePreview={(previewID) => {
              const preview = filePreviews.find((entry) => entry.id === previewID);
              if (preview) {
                removeFilePreview(preview);
              }
            }}
            onCloseTerminal={closeTerminal}
            onSelectBrowser={(tabID) => {
              setActiveFilePreviewID(undefined);
              selectBrowserTab(tabID);
            }}
            onSelectFilePreview={selectFilePreview}
            onSelectTerminal={selectTerminal}
        />
        {actorSessionID ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                aria-label={t("workspace.add")}
                className="no-drag-region h-(--workspace-toolbar-tab-h) w-(--workspace-toolbar-tab-h) shrink-0 rounded-md text-muted-foreground"
                disabled={creatingBrowserTab || creatingTerminal}
                size="icon-sm"
                title={t("workspace.add")}
                type="button"
                variant="ghost"
              >
                {creatingBrowserTab || creatingTerminal ? (
                  <Spinner className="h-3.5 w-3.5" />
                ) : (
                  <Plus className="h-3.5 w-3.5" />
                )}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-44 space-y-1">
              <DropdownMenuItem
                onSelect={createBrowserSurface}
              >
                <Compass />
                {t("browser.create")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={createNewTerminal}>
                <SquareTerminal />
                {t("terminal.create")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
        <div aria-hidden="true" className="pointer-events-none min-w-0 flex-1 self-stretch" />
        {secondarySessionID && sessionQuery.data?.title ? (
          <span
            className="no-drag-region max-w-32 shrink-0 truncate rounded-md bg-muted px-2 py-1 text-[11px] text-muted-foreground"
            title={sessionQuery.data.title}
          >
            {sessionQuery.data.title}
          </span>
        ) : null}
      </div>
      <div className="relative z-0 min-h-0 flex-1 overflow-hidden px-3 pb-3">
        <TerminalSizeProbe
          onDimensionsChange={(dimensions) => {
            terminalDimensionsRef.current = dimensions;
          }}
        />
        {activeSurface === "workspace" ? (
          <WorkspaceEmpty
            disabled={!actorSessionID}
            creatingBrowser={creatingBrowserTab}
            creatingTerminal={creatingTerminal}
            onCreateBrowser={createBrowserSurface}
            onCreateTerminal={createNewTerminal}
          />
        ) : null}
        {activeSurface === "canvas" && !filePreviewActive ? (
          <div className="absolute inset-0 z-10 flex min-h-0 flex-col overflow-hidden bg-[var(--workspace-background)]">
            <CanvasToolbar
              activeItemID={activeCanvasItem?.id}
              items={items}
              library={(
                <CanvasLibraryControl
                  closedItems={closedItems}
                  open={canvasLibraryOpen}
                  onClearClosed={() => clearClosedMutation.mutate()}
                  onOpenChange={setCanvasLibraryOpen}
                  onRemoveClosed={(entry) => removeClosedMutation.mutate(entry)}
                  onRestoreClosed={restoreClosedItem}
                />
              )}
              onClose={(item) => deleteMutation.mutate(item)}
              onSelect={(item) => selectCanvasItem(item.id)}
            />
            <div className="flex min-h-0 flex-1 p-3">
              <CanvasFreeformSurface
                key={actorSessionID}
                activeItemID={activeCanvasItem?.id}
                items={items}
                loading={itemsQuery.isLoading}
                sessionID={actorSessionID}
                token={token}
                onActiveItemChange={selectCanvasItem}
                onClose={(item) => deleteMutation.mutate(item)}
                onGalleryLayoutChange={(item, layout) => galleryLayoutMutation.mutate({ item, layout })}
              />
            </div>
          </div>
        ) : null}
        {actorSessionID ? (
          <ProjectBrowserSurface
            active={projectActive}
            sessionID={actorSessionID}
            token={token}
            onOpenTerminal={createNewTerminalAt}
            onVisibleContextChange={setProjectUIContext}
          />
        ) : null}
        {mountedFilePreviews.map((preview) => (
          <FilePreviewSurface
            key={preview.id}
            active={filePreviewActive && preview.id === activeFilePreview?.id}
            preview={preview}
          />
        ))}
        {Object.entries(mountedBrowserTabs).map(([targetSessionID, tabs]) =>
          tabs.length > 0 || (targetSessionID === actorSessionID && browserSurfaceVisible) ? (
            <BrowserWorkspaceSurface
              key={`browser:${targetSessionID}`}
              active={targetSessionID === actorSessionID && browserActive}
              activeTabID={targetSessionID === actorSessionID ? activeBrowserTabID : undefined}
              pending={targetSessionID === actorSessionID && browserSurfacePending}
              sessionID={targetSessionID}
              tabs={tabs}
              token={token}
            />
          ) : null,
        )}
        {Object.entries(mountedTerminals).map(([targetSessionID, sessionTerminals]) =>
          sessionTerminals.length > 0 ? (
            <TerminalSurface
              key={`terminal:${targetSessionID}`}
              active={targetSessionID === actorSessionID && terminalActive}
              activeTerminalID={targetSessionID === actorSessionID ? activeTerminalID : undefined}
              fallbackDimensions={terminalDimensionsRef.current}
              initialDimensionsByID={terminalInitialDimensions}
              sessionID={targetSessionID}
              terminals={sessionTerminals}
              token={token}
              onStatus={(terminalID, status, exitCode) =>
                updateMountedTerminalStatus(targetSessionID, terminalID, status, exitCode)
              }
            />
          ) : null,
        )}
      </div>
    </aside>
  );
}

function sameResourceList<T>(current: T[] | undefined, next: T[]) {
  return Boolean(current && current.length === next.length && current.every((item, index) => item === next[index]));
}

function mergeBrowserSurfaceTabs(
  current: ElectronBrowserSurfaceTab[] | undefined,
  required: ElectronBrowserSurfaceTab[],
) {
  const next = [...(current || [])];
  required.forEach((tab) => {
    const index = next.findIndex((entry) => entry.id === tab.id);
    if (index >= 0) {
      next[index] = tab;
    } else {
      next.push(tab);
    }
  });
  return next;
}

function sameResourceRecord<T>(current: Record<string, T>, next: Record<string, T>) {
  const currentKeys = Object.keys(current);
  const nextKeys = Object.keys(next);
  return currentKeys.length === nextKeys.length && nextKeys.every((key) => current[key] === next[key]);
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

function withoutKey<T>(record: Record<string, T>, key: string): Record<string, T> {
  const next = { ...record };
  delete next[key];
  return next;
}

function topCanvasItem(items: CanvasItem[]) {
  return items.reduce<CanvasItem | undefined>((top, item) => {
    if (!top) return item;
    const topZ = numberValue(asRecord(top.window)?.z, 0);
    const itemZ = numberValue(asRecord(item.window)?.z, 0);
    return itemZ >= topZ ? item : top;
  }, undefined);
}
