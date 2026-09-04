import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  APIError,
  clearClosedCanvasItems,
  createClosedCanvasItem,
  deleteCanvasItem,
  deleteClosedCanvasItem,
  deleteSavedCanvasItem,
  listClosedCanvasItems,
  listCanvasItems,
  listSavedCanvasItems,
  getSession,
  listProjectBrowserRoots,
  putCanvasItem,
  openSavedCanvasItem,
  saveCanvasItem,
  type CanvasItemPayload,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useRetainBrowserRuntimeTabs } from "@/browser/BrowserRuntimeProvider";
import { browserTabTitle } from "@/browser/helpers";
import { activateBrowserPageFindRegion } from "@/browser/pageFindTarget";
import type { GalleryLayout } from "@/components/canvas/CanvasItemContent";
import {
  CanvasItemActions,
  CanvasItemSurface,
} from "@/components/canvas/CanvasItemSurface";
import { titleForCanvasItem } from "@/components/canvas/CanvasKindIcon";
import { FilePreviewSurface, filePreviewTitle } from "@/components/canvas/FilePreviewSurface";
import { asRecord, numberValue, stringValue } from "@/components/canvas/canvasPayload";
import { Spinner } from "@/components/Spinner";
import { ProjectBrowserSurface } from "@/components/project/ProjectBrowserSurface";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ConfirmationDialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { CanvasItem, ClosedCanvasItem, SavedCanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { turnFileChangeFullPath, turnFileChangeLabel, turnFileDiffChanges } from "@/lib/turnFileChanges";
import { consumeCanvasReveal, useVisibleCanvasReveal } from "@/state/canvasRevealStore";
import {
  closeFilePreview,
  consumeFilePreviewReveal,
  type FilePreview,
  useFilePreviews,
  useFilePreviewReveal,
} from "@/state/filePreviewStore";
import { consumeProjectFileReveal, useVisibleProjectFileReveal } from "@/state/projectRevealStore";
import { setProjectTabClosed, useProjectTabClosed } from "@/state/workspaceProjectTabStore";
import { setWorkspaceOpen } from "@/state/workspaceStore";
import {
  clearVisibleUIContext,
  setVisibleUIContext,
  type UIContextPart,
} from "@/state/uiContextStore";
import { BrowserWorkspaceSurface } from "./BrowserWorkspaceSurface";
import { WorkspaceEmpty } from "./WorkspaceEmpty";
import { WorkspaceResourceMenu } from "./WorkspaceResourceMenu";
import { WorkspaceResourceTabs } from "./WorkspaceResourceTabs";
import { useWorkspaceBrowserSurface } from "./useWorkspaceBrowserSurface";

type WorkspacePaneProps = {
  token: string;
  activeSessionID?: string;
  presented: boolean;
  sessionID?: string;
  secondarySessionID?: string;
  reserveTopRightActions?: 0 | 1 | 2;
};

export const WorkspacePane = memo(function WorkspacePane({
  token,
  activeSessionID,
  presented,
  sessionID,
  secondarySessionID,
  reserveTopRightActions = 0,
}: WorkspacePaneProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const actorSessionIDRef = useRef("");
  const canvasSessionStateRef = useRef("");
  const retainedTokenRef = useRef(token);
  const seenCanvasItemIDsRef = useRef<Set<string>>(new Set());
  const hasSeenCanvasItemsRef = useRef(false);
  const [activeCanvasItemIDs, setActiveCanvasItemIDs] = useState<Record<string, string>>({});
  const [canvasGalleryActiveIndices, setCanvasGalleryActiveIndices] = useState<Record<string, number>>({});
  const [pendingSavedClose, setPendingSavedClose] = useState<CanvasItem>();
  const [retainedFilePreviews, setRetainedFilePreviews] = useState<Record<string, FilePreview>>({});
  const [projectUIContext, setProjectUIContext] = useState<UIContextPart>();
  const [validatedProjectReveal, setValidatedProjectReveal] = useState<{ serial: number; sessionID: string }>();
  const hadResourcesRef = useRef(false);
  const resourceSessionIDRef = useRef("");
  const projectFileReveal = useVisibleProjectFileReveal(sessionID, secondarySessionID);
  const canvasReveal = useVisibleCanvasReveal(sessionID, secondarySessionID);
  const filePreviewReveal = useFilePreviewReveal(sessionID, secondarySessionID);
  const actorSessionID = activeSessionID || sessionID || secondarySessionID || actorSessionIDRef.current;
  useEffect(() => {
    if (actorSessionID) {
      actorSessionIDRef.current = actorSessionID;
    }
  }, [actorSessionID]);
  const primaryFilePreviews = useFilePreviews(sessionID);
  const secondaryFilePreviews = useFilePreviews(secondarySessionID);
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
  const setActiveFilePreviewID = useCallback((previewID: string | undefined) => {
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
  }, [actorSessionID]);
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
  const projectTabClosed = useProjectTabClosed(actorSessionID);

  const sessionQuery = useQuery({
    enabled,
    queryKey: queryKeys.session(actorSessionID),
    queryFn: () => getSession(token, actorSessionID),
    staleTime: 10_000,
  });
  const hasProject = Boolean(sessionQuery.data?.projectID);
  const workspaceRootsQuery = useQuery({
    enabled,
    queryKey: queryKeys.projectBrowserRoots(actorSessionID),
    queryFn: () => listProjectBrowserRoots(token, actorSessionID),
    staleTime: 10_000,
  });
  const hasFileWorkspace = hasProject || Boolean(workspaceRootsQuery.data?.roots.length);
  const temporaryFileWorkspace = !hasProject && Boolean(workspaceRootsQuery.data?.temporary);
  const fileWorkspaceLabel = temporaryFileWorkspace ? t("workspace.sessionFiles") : t("workspace.project");
  const projectTabVisible = hasFileWorkspace && !projectTabClosed;
  const projectRevealReady = !projectFileReveal || (
    validatedProjectReveal?.serial === projectFileReveal.serial
    && validatedProjectReveal.sessionID === projectFileReveal.sessionID
  );
  useEffect(() => {
    if (!actorSessionID || projectFileReveal?.sessionID !== actorSessionID) {
      return;
    }
    let cancelled = false;
    void Promise.all([
      sessionQuery.refetch(),
      queryClient.invalidateQueries({ queryKey: queryKeys.projectBrowserRoots(actorSessionID) }),
    ]).then(([result]) => {
      if (cancelled) {
        return;
      }
      if (result.isSuccess) {
        setValidatedProjectReveal({ serial: projectFileReveal.serial, sessionID: actorSessionID });
      } else {
        toast.warning(t("project.browserLoadFailed"));
      }
    }).catch(() => {
      if (!cancelled) {
        toast.warning(t("project.browserLoadFailed"));
      }
    });
    return () => {
      cancelled = true;
    };
  }, [actorSessionID, projectFileReveal?.serial, queryClient, sessionQuery.refetch]);
  const projectTurnDiffPreviews = useMemo(
    () => hasFileWorkspace ? filePreviews.filter((preview) => preview.source === "turn-diff") : [],
    [filePreviews, hasFileWorkspace],
  );
  const surfaceFilePreviews = useMemo(
    () => filePreviews.filter((preview) => (
      preview.source !== "turn-diff" || (!sessionQuery.isLoading && !hasFileWorkspace)
    )),
    [filePreviews, hasFileWorkspace, sessionQuery.isLoading],
  );
  const workspaceFilePreviewTabs = useMemo(
    () => surfaceFilePreviews.map((preview) => ({
      id: preview.id,
      kind: preview.source === "turn-diff" ? "diff" as const : "file" as const,
      label: preview.source === "turn-diff" ? t("turnFiles.tab") : filePreviewTitle(preview.path),
      openedAt: preview.openedAt,
      path: preview.source === "turn-diff"
        ? (() => {
            const changes = turnFileDiffChanges(preview.fileChanges || []);
            const change = changes.find((item) => item.id === preview.selectedFileChangeID) || changes[0];
            return change ? turnFileChangeLabel(change, changes) : t("turnFiles.tab");
          })()
        : preview.path,
    })),
    [surfaceFilePreviews, t],
  );

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
  const savedItemsQuery = useQuery({
    enabled,
    queryKey: queryKeys.savedCanvasItems(),
    queryFn: () => listSavedCanvasItems(token, actorSessionID),
    staleTime: 30_000,
  });

  const items = useMemo(() => itemsQuery.data?.items ?? [], [itemsQuery.data?.items]);
  const closedItems = useMemo(() => closedItemsQuery.data?.items ?? [], [closedItemsQuery.data?.items]);
  const savedItems = useMemo(() => savedItemsQuery.data?.items ?? [], [savedItemsQuery.data?.items]);
  const selectedCanvasItemID = activeCanvasItemIDs[actorSessionID];
  const activeCanvasItem = items.find((item) => item.id === selectedCanvasItemID) || topCanvasItem(items);
  const selectCanvasItem = useCallback((itemID: string) => {
    if (!actorSessionID) return;
    setActiveCanvasItemIDs((current) => (
      current[actorSessionID] === itemID ? current : { ...current, [actorSessionID]: itemID }
    ));
  }, [actorSessionID]);
  const {
    activeBrowserTabID,
    activeBrowserSelection,
    activeSurface,
    browserActive,
    browserTabsReady,
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
    selectWorkspaceSurface,
  } = useWorkspaceBrowserSurface({
    enabled,
    hasProjectSurface: projectTabVisible,
    hasTransientSurface: surfaceFilePreviews.length > 0,
    itemsLength: items.length,
    itemsPending: itemsQuery.isLoading,
    sessionID: actorSessionID,
    token,
  });
  useRetainBrowserRuntimeTabs(actorSessionID, browserTabs, browserTabsReady);
  const browserSurfaceTabs = browserTabsReady ? browserTabs : [];
  const projectActive = activeSurface === "project" && projectTabVisible;
  useEffect(() => {
    if (retainedTokenRef.current === token) {
      return;
    }
    retainedTokenRef.current = token;
    setRetainedFilePreviews({});
    setActiveFilePreviewIDs({});
    setCanvasGalleryActiveIndices({});
  }, [token]);
  const activeFilePreview = filePreviews.find((preview) => preview.id === activeFilePreviewID);
  const filePreviewActive = Boolean(
    activeFilePreview
      && surfaceFilePreviews.some((preview) => preview.id === activeFilePreview.id)
      && activeSurface === "canvas",
  );

  const visibleUIContext = useMemo<UIContextPart | undefined>(() => {
    if (!actorSessionID) {
      return undefined;
    }
    if (projectActive) {
      return projectUIContext || { type: "ui_context", surface: "project" };
    }
    if (filePreviewActive && activeFilePreview) {
      const fileChanges = turnFileDiffChanges(activeFilePreview.fileChanges || []);
      const selectedChange = fileChanges.find((change) => change.id === activeFilePreview.selectedFileChangeID) || fileChanges[0];
      if (activeFilePreview.source === "turn-diff" && selectedChange) {
        return {
          type: "ui_context",
          surface: "file_preview",
          resource: "project_diff",
          id: selectedChange.id,
          name: turnFileChangeLabel(selectedChange, fileChanges),
          path: turnFileChangeFullPath(selectedChange),
          kind: selectedChange.kind,
        };
      }
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
            selectionText: activeBrowserSelection || undefined,
          }
        : { type: "ui_context", surface: "browser" };
    }
    return undefined;
  }, [
    activeBrowserTabID,
    activeBrowserSelection,
    activeCanvasItem,
    activeFilePreview,
    activeSurface,
    actorSessionID,
    browserTabs,
    filePreviewActive,
    projectActive,
    projectUIContext,
    t,
  ]);

  useEffect(() => {
    setVisibleUIContext(actorSessionID, visibleUIContext);
  }, [actorSessionID, visibleUIContext]);
  useEffect(() => () => clearVisibleUIContext(actorSessionID), [actorSessionID]);

  useEffect(() => {
    if (!filePreviewReveal || filePreviewReveal.sessionID !== actorSessionID) {
      return;
    }
    const preview = filePreviews.find((entry) => entry.id === filePreviewReveal.previewID);
    if (!preview || (preview.source === "turn-diff" && sessionQuery.isLoading)) {
      return;
    }
    setActiveFilePreviewID(filePreviewReveal.previewID);
    if (preview.source === "turn-diff" && hasFileWorkspace) {
      setProjectTabClosed(actorSessionID, false);
      selectProjectSurface();
    } else {
      selectCanvasSurface();
    }
    consumeFilePreviewReveal(filePreviewReveal.serial);
  }, [actorSessionID, filePreviewReveal, filePreviews, hasFileWorkspace, selectCanvasSurface, selectProjectSurface, sessionQuery.isLoading]);

  useEffect(() => {
    if (activeFilePreviewID && !filePreviews.some((preview) => preview.id === activeFilePreviewID)) {
      setActiveFilePreviewID(undefined);
    }
  }, [activeFilePreviewID, filePreviews]);

  const selectPersistentCanvasSurface = useCallback(() => {
    setActiveFilePreviewID(undefined);
    selectCanvasSurface();
  }, [selectCanvasSurface, setActiveFilePreviewID]);

  useEffect(() => {
    if (!canvasReveal || canvasReveal.sessionID !== actorSessionID) {
      return;
    }
    if (!items.some((item) => item.id === canvasReveal.itemID)) {
      return;
    }
    selectCanvasItem(canvasReveal.itemID);
    selectPersistentCanvasSurface();
    consumeCanvasReveal(canvasReveal.serial);
  }, [actorSessionID, canvasReveal, items]);

  const createBrowserSurface = useCallback(() => {
    setActiveFilePreviewID(undefined);
    createNewBrowserTab();
  }, [createNewBrowserTab, setActiveFilePreviewID]);

  const activateProjectSurface = useCallback(() => {
    if (!hasFileWorkspace) {
      return;
    }
    setProjectTabClosed(actorSessionID, false);
    if (activeFilePreview?.source !== "turn-diff") {
      setActiveFilePreviewID(undefined);
    }
    selectProjectSurface();
  }, [activeFilePreview?.source, actorSessionID, hasFileWorkspace, selectProjectSurface, setActiveFilePreviewID]);

  const selectProjectFallbackSurface = useCallback(() => {
    if (browserTabs.length > 0) {
      selectBrowserTab(activeBrowserTabID || browserTabs[0].id);
    } else if (surfaceFilePreviews.length > 0) {
      setActiveFilePreviewID(surfaceFilePreviews.at(-1)!.id);
      selectCanvasSurface();
    } else if (items.length > 0) {
      setActiveFilePreviewID(undefined);
      selectCanvasSurface();
    } else {
      selectWorkspaceSurface();
    }
  }, [
    activeBrowserTabID,
    browserTabs,
    items.length,
    selectBrowserTab,
    selectCanvasSurface,
    selectWorkspaceSurface,
    setActiveFilePreviewID,
    surfaceFilePreviews,
  ]);

  const closeProjectSurface = useCallback(() => {
    if (!actorSessionID) return;
    if (projectFileReveal?.sessionID === actorSessionID) {
      consumeProjectFileReveal(actorSessionID, projectFileReveal.serial);
    }
    if (filePreviewReveal?.sessionID === actorSessionID) {
      const pendingPreview = filePreviews.find((preview) => preview.id === filePreviewReveal.previewID);
      if (pendingPreview?.source === "turn-diff") {
        consumeFilePreviewReveal(filePreviewReveal.serial);
      }
    }
    setProjectTabClosed(actorSessionID, true);
    if (!projectActive) return;
    selectProjectFallbackSurface();
  }, [
    actorSessionID,
    filePreviewReveal,
    filePreviews,
    projectActive,
    projectFileReveal,
    selectProjectFallbackSurface,
  ]);

  useEffect(() => {
    if (activeSurface === "project" && projectTabClosed) {
      selectProjectFallbackSurface();
    }
  }, [activeSurface, projectTabClosed, selectProjectFallbackSurface]);

  useEffect(() => {
    if (
      activeSurface === "project"
      && enabled
      && !sessionQuery.isPending
      && !workspaceRootsQuery.isPending
      && !hasFileWorkspace
    ) {
      if (items.length > 0) {
        selectCanvasSurface();
      } else {
        selectWorkspaceSurface();
      }
    }
  }, [
    activeSurface,
    enabled,
    hasFileWorkspace,
    items.length,
    selectCanvasSurface,
    selectWorkspaceSurface,
    sessionQuery.isPending,
    workspaceRootsQuery.isPending,
  ]);

  useEffect(() => {
    if (projectFileReveal?.sessionID === actorSessionID && hasFileWorkspace) {
      setProjectTabClosed(actorSessionID, false);
      selectProjectSurface();
    }
  }, [actorSessionID, hasFileWorkspace, projectFileReveal?.serial, selectProjectSurface]);

  const selectFilePreview = useCallback((previewID: string) => {
    selectCanvasSurface();
    setActiveFilePreviewID(previewID);
  }, [selectCanvasSurface, setActiveFilePreviewID]);

  const selectProjectTurnDiff = useCallback((previewID: string) => {
    setProjectTabClosed(actorSessionID, false);
    setActiveFilePreviewID(previewID);
    selectProjectSurface();
  }, [actorSessionID, selectProjectSurface, setActiveFilePreviewID]);

  const deactivateProjectTurnDiff = useCallback(() => {
    if (activeFilePreview?.source === "turn-diff") {
      setActiveFilePreviewID(undefined);
    }
  }, [activeFilePreview?.source, setActiveFilePreviewID]);

  const closeProjectTurnDiffs = useCallback((previewIDs: string[]) => {
    const closing = new Set(previewIDs);
    projectTurnDiffPreviews
      .filter((preview) => closing.has(preview.id))
      .forEach((preview) => closeFilePreview(preview.sessionID, preview.id));
    if (activeFilePreviewID && closing.has(activeFilePreviewID)) {
      const remaining = projectTurnDiffPreviews.filter((preview) => !closing.has(preview.id));
      setActiveFilePreviewID(remaining.at(-1)?.id);
    }
  }, [activeFilePreviewID, projectTurnDiffPreviews, setActiveFilePreviewID]);

  const removeFilePreview = useCallback((preview: FilePreview) => {
    closeFilePreview(preview.sessionID, preview.id);
    if (activeFilePreviewID !== preview.id) {
      return;
    }
    const closedIndex = surfaceFilePreviews.findIndex((entry) => entry.id === preview.id);
    const next = surfaceFilePreviews[closedIndex + 1] || surfaceFilePreviews[closedIndex - 1];
    if (next) {
      setActiveFilePreviewID(next.id);
    } else if (browserTabs.length > 0) {
      setActiveFilePreviewID(undefined);
      selectBrowserTab(activeBrowserTabID || browserTabs[0].id);
    } else {
      setActiveFilePreviewID(undefined);
      if (items.length === 0 && projectTabVisible) {
        selectProjectSurface();
      } else if (items.length === 0) {
        selectWorkspaceSurface();
      }
    }
  }, [
    activeBrowserTabID,
    activeFilePreviewID,
    browserTabs,
    items.length,
    projectTabVisible,
    selectBrowserTab,
    selectProjectSurface,
    selectWorkspaceSurface,
    setActiveFilePreviewID,
    surfaceFilePreviews,
  ]);

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
  const changeCanvasActiveIndex = useCallback((itemID: string, activeIndex: number) => {
    setCanvasGalleryActiveIndices((current) => {
      const key = canvasGalleryStateKey(actorSessionID, itemID);
      return current[key] === activeIndex ? current : { ...current, [key]: activeIndex };
    });
  }, [actorSessionID]);
  const changeCanvasGalleryLayout = useCallback((item: CanvasItem, layout: GalleryLayout) => {
    galleryLayoutMutation.mutate({ item, layout });
  }, [galleryLayoutMutation.mutate]);

  const saveItemMutation = useMutation({
    mutationFn: (item: CanvasItem) => saveCanvasItem(token, actorSessionID, item.id),
    onSuccess: (result) => {
      queryClient.setQueryData<{ items: CanvasItem[] }>(queryKeys.canvasItems(actorSessionID), (current) => ({
        items: (current?.items || []).map((item) => item.id === result.item.id ? result.item : item),
      }));
      queryClient.setQueryData<{ items: SavedCanvasItem[] }>(queryKeys.savedCanvasItems(), (current) => ({
        items: [result.savedItem, ...(current?.items || []).filter((item) => item.id !== result.savedItem.id)],
      }));
      toast.success(t("canvas.saveDone"));
    },
    onError: (error) => {
      toast.error(error instanceof APIError && error.code === "saved_canvas_conflict"
        ? t("canvas.saveConflict")
        : t("canvas.saveFailed"));
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

  const closeSavedMutation = useMutation({
    mutationFn: async ({ item, saveChanges }: { item: CanvasItem; saveChanges: boolean }) => {
      if (saveChanges) {
        await saveCanvasItem(token, actorSessionID, item.id);
      }
      await deleteCanvasItem(token, actorSessionID, item.id);
    },
    onMutate: async ({ item }) => {
      const key = queryKeys.canvasItems(actorSessionID);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<{ items: CanvasItem[] }>(key);
      queryClient.setQueryData<{ items: CanvasItem[] }>(key, (current) => ({
        items: (current?.items || []).filter((entry) => entry.id !== item.id),
      }));
      if (activeCanvasItem?.id === item.id) {
        const next = topCanvasItem(items.filter((entry) => entry.id !== item.id));
        setActiveCanvasItemIDs((current) => next
          ? { ...current, [actorSessionID]: next.id }
          : withoutKey(current, actorSessionID));
      }
      return { previous };
    },
    onError: (error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.canvasItems(actorSessionID), context.previous);
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.savedCanvasItems() });
      toast.error(error instanceof APIError && error.code === "saved_canvas_conflict"
        ? t("canvas.saveConflict")
        : t("canvas.closeFailed"));
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(actorSessionID) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.savedCanvasItems() });
    },
  });

  const requestCloseCanvasItem = useCallback((item: CanvasItem) => {
    if (!item.sourceSavedItemID) {
      deleteMutation.mutate(item);
      return;
    }
    if (item.savedDirty) {
      setPendingSavedClose(item);
      return;
    }
    closeSavedMutation.mutate({ item, saveChanges: false });
  }, [closeSavedMutation.mutate, deleteMutation.mutate]);

  const restoreMutation = useMutation({
    mutationFn: async (entry: ClosedCanvasItem) => {
      const item = await putCanvasItem(token, actorSessionID, entry.sourceItemID, canvasPayloadFromClosedItem(entry));
      await deleteClosedCanvasItem(token, actorSessionID, entry.id);
      return item;
    },
    onSuccess: (item) => {
      queryClient.setQueryData<{ items: CanvasItem[] }>(queryKeys.canvasItems(actorSessionID), (current) => ({
        items: [...(current?.items || []).filter((entry) => entry.id !== item.id), item],
      }));
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

  const openSavedMutation = useMutation({
    mutationFn: (entry: SavedCanvasItem) => openSavedCanvasItem(token, actorSessionID, entry.id),
    onSuccess: (item) => {
      queryClient.setQueryData<{ items: CanvasItem[] }>(queryKeys.canvasItems(actorSessionID), (current) => ({
        items: [...(current?.items || []).filter((entry) => entry.id !== item.id), item],
      }));
      selectCanvasItem(item.id);
      selectPersistentCanvasSurface();
    },
    onError: () => toast.error(t("canvas.openSavedFailed")),
  });

  const removeSavedMutation = useMutation({
    mutationFn: (entry: SavedCanvasItem) => deleteSavedCanvasItem(token, actorSessionID, entry.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.savedCanvasItems() });
      void queryClient.invalidateQueries({
        predicate: (query) => query.queryKey[0] === "session"
          && query.queryKey[2] === "canvas"
          && query.queryKey[3] === "items",
      });
    },
    onError: () => toast.error(t("canvas.deleteSavedFailed")),
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

  const totalResourceCount = items.length + browserTabs.length + surfaceFilePreviews.length + (projectTabVisible ? 1 : 0);
  useEffect(() => {
    if (resourceSessionIDRef.current !== actorSessionID) {
      resourceSessionIDRef.current = actorSessionID;
      hadResourcesRef.current = totalResourceCount > 0;
      return;
    }
    if (totalResourceCount > 0) {
      hadResourcesRef.current = true;
      return;
    }
    if (hadResourcesRef.current) {
      hadResourcesRef.current = false;
      setWorkspaceOpen(actorSessionID, false);
    }
  }, [actorSessionID, totalResourceCount]);

  useEffect(() => {
    if (itemsQuery.isLoading || itemsQuery.isFetching || activeSurface !== "canvas" || filePreviewActive || items.length > 0) return;
    if (surfaceFilePreviews.length > 0) {
      setActiveFilePreviewID(surfaceFilePreviews.at(-1)!.id);
    } else if (browserTabs.length > 0) {
      selectBrowserTab(activeBrowserTabID || browserTabs[0].id);
    } else if (projectTabVisible) {
      selectProjectSurface();
    } else {
      selectWorkspaceSurface();
    }
  }, [
    activeBrowserTabID,
    activeSurface,
    browserTabs,
    filePreviewActive,
    surfaceFilePreviews,
    items.length,
    itemsQuery.isFetching,
    itemsQuery.isLoading,
    projectTabVisible,
  ]);

  const closeWorkspaceBrowser = useCallback((tabID: string) => {
    const closingLastActiveBrowser =
      activeSurface === "browser" && activeBrowserTabID === tabID && browserTabs.length === 1;
    closeBrowserTab(tabID);
    if (closingLastActiveBrowser && surfaceFilePreviews.length > 0) {
      const fallbackPreviewID = surfaceFilePreviews.some((preview) => preview.id === activeFilePreviewID)
        ? activeFilePreviewID!
        : surfaceFilePreviews[0].id;
      selectFilePreview(fallbackPreviewID);
    }
  }, [
    activeBrowserTabID,
    activeFilePreviewID,
    activeSurface,
    browserTabs.length,
    closeBrowserTab,
    selectFilePreview,
    surfaceFilePreviews,
  ]);
  const closeWorkspaceCanvasItem = useCallback((itemID: string) => {
    const item = items.find((entry) => entry.id === itemID);
    if (item) requestCloseCanvasItem(item);
  }, [items, requestCloseCanvasItem]);
  const closeWorkspaceFilePreview = useCallback((previewID: string) => {
    const preview = surfaceFilePreviews.find((entry) => entry.id === previewID);
    if (preview) removeFilePreview(preview);
  }, [removeFilePreview, surfaceFilePreviews]);
  const activateWorkspaceBrowser = useCallback((tabID: string) => {
    activateBrowserPageFindRegion();
    setActiveFilePreviewID(undefined);
    selectBrowserTab(tabID);
  }, [selectBrowserTab, setActiveFilePreviewID]);
  const activateWorkspaceCanvasItem = useCallback((itemID: string) => {
    setActiveFilePreviewID(undefined);
    selectCanvasItem(itemID);
    selectCanvasSurface();
  }, [selectCanvasItem, selectCanvasSurface, setActiveFilePreviewID]);

  return (
    <aside className="pudding-workspace-pane relative flex h-full shrink-0 flex-col bg-[var(--workspace-chrome-background)] text-sidebar-foreground">
      <div
        className={cn(
          "relative z-30 flex h-(--toolbar-h) shrink-0 items-center gap-1.5 overflow-hidden pl-(--workspace-toolbar-pl)",
          reserveTopRightActions === 2
            ? "pr-[calc(var(--workspace-toggle-right)+var(--toolbar-icon-button-size)+var(--toolbar-icon-button-size)+0.875rem)]"
            : reserveTopRightActions === 1
              ? "pr-[calc(var(--workspace-toggle-right)+var(--toolbar-icon-button-size)+0.375rem)]"
              : "pr-(--workspace-toolbar-pr)",
        )}
      >
        <WorkspaceResourceTabs
          activeBrowserTabID={activeBrowserTabID}
          activeCanvasItemID={activeCanvasItem?.id}
          activeFilePreviewID={activeFilePreviewID}
          activeSurface={activeSurface}
          browserTabs={browserSurfaceTabs}
          canvasItems={items}
          closingCanvasItemID={deleteMutation.isPending
            ? deleteMutation.variables?.id
            : closeSavedMutation.isPending
              ? closeSavedMutation.variables?.item.id
              : undefined}
          closingBrowserTabID={closingBrowserTabID}
          filePreviewActive={filePreviewActive}
          filePreviewTabs={workspaceFilePreviewTabs}
          orderScope={actorSessionID || "workspace"}
          projectLabel={fileWorkspaceLabel}
          projectTabVisible={projectTabVisible}
          onCloseBrowser={closeWorkspaceBrowser}
          onCloseCanvasItem={closeWorkspaceCanvasItem}
          onCloseFilePreview={closeWorkspaceFilePreview}
          onCloseProject={closeProjectSurface}
          onSelectBrowser={activateWorkspaceBrowser}
          onSelectCanvasItem={activateWorkspaceCanvasItem}
          onSelectFilePreview={selectFilePreview}
          onSelectProject={activateProjectSurface}
        />
        {actorSessionID && activeSurface !== "workspace" ? (
          <WorkspaceResourceMenu
            closedItems={closedItems}
            creatingBrowser={creatingBrowserTab}
            hasProject={hasFileWorkspace}
            projectLabel={fileWorkspaceLabel}
            projectTabVisible={projectTabVisible}
            savedItems={savedItems}
            onClearClosed={() => clearClosedMutation.mutate()}
            onCreateBrowser={createBrowserSurface}
            onOpenProject={activateProjectSurface}
            onOpenSaved={(entry) => openSavedMutation.mutate(entry)}
            onRemoveClosed={(entry) => removeClosedMutation.mutate(entry)}
            onRemoveSaved={(entry) => removeSavedMutation.mutate(entry)}
            onRestoreClosed={restoreClosedItem}
          />
        ) : null}
        <div aria-hidden="true" className="pointer-events-none min-w-0 flex-1 self-stretch" />
        {secondarySessionID && sessionQuery.data?.title ? (
          <span
            className="no-drag-region max-w-32 shrink-0 truncate rounded-md bg-muted px-2 py-1 text-[11px] text-muted-foreground"
          >
            {sessionQuery.data.title}
          </span>
        ) : null}
      </div>
      <div className="relative z-0 min-h-0 flex-1 overflow-hidden">
        {activeSurface === "workspace" ? (
          <WorkspaceEmpty
            closedItems={closedItems}
            disabled={!actorSessionID}
            creatingBrowser={creatingBrowserTab}
            hasProject={hasFileWorkspace}
            projectLabel={fileWorkspaceLabel}
            savedItems={savedItems}
            onClearClosed={() => clearClosedMutation.mutate()}
            onCreateBrowser={createBrowserSurface}
            onOpenProject={activateProjectSurface}
            onRemoveClosed={(entry) => removeClosedMutation.mutate(entry)}
            onRemoveSaved={(entry) => removeSavedMutation.mutate(entry)}
            onOpenSaved={(entry) => openSavedMutation.mutate(entry)}
            onRestoreClosed={restoreClosedItem}
          />
        ) : null}
        {items.map((item) => (
          <CanvasItemSurface
            key={`${actorSessionID}:${item.id}`}
            active={activeSurface === "canvas" && !filePreviewActive && activeCanvasItem?.id === item.id}
            activeIndex={canvasGalleryActiveIndices[canvasGalleryStateKey(actorSessionID, item.id)] || 0}
            item={item}
            token={token}
            onActiveIndexChange={changeCanvasActiveIndex}
            onGalleryLayoutChange={changeCanvasGalleryLayout}
          />
        ))}
        {activeSurface === "canvas" && !filePreviewActive && activeCanvasItem ? (
          <div className="absolute top-3 right-3 z-20">
            <CanvasItemActions
              item={activeCanvasItem}
              saving={saveItemMutation.isPending && saveItemMutation.variables?.id === activeCanvasItem.id}
              token={token}
              onSave={() => saveItemMutation.mutate(activeCanvasItem)}
              onGalleryLayoutChange={(layout) => galleryLayoutMutation.mutate({ item: activeCanvasItem, layout })}
            />
          </div>
        ) : null}
        {activeSurface === "canvas" && !filePreviewActive && itemsQuery.isLoading && items.length === 0 ? (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-[var(--workspace-background)] text-muted-foreground">
            <Spinner className="size-5" />
          </div>
        ) : null}
        {actorSessionID ? (
          <ProjectBrowserSurface
            active={projectActive}
            activeTurnDiffID={activeFilePreview?.source === "turn-diff" ? activeFilePreview.id : undefined}
            hasProject={hasProject}
            projectStateReady={projectRevealReady}
            sessionID={actorSessionID}
            token={token}
            turnDiffTabs={projectTurnDiffPreviews}
            onActivateTurnDiff={selectProjectTurnDiff}
            onCloseTurnDiffs={closeProjectTurnDiffs}
            onDeactivateTurnDiff={deactivateProjectTurnDiff}
            onVisibleContextChange={setProjectUIContext}
          />
        ) : null}
        {mountedFilePreviews.filter((preview) => !(
          hasFileWorkspace && preview.sessionID === actorSessionID && preview.source === "turn-diff"
        )).map((preview) => (
          <FilePreviewSurface
            key={preview.id}
            active={filePreviewActive && preview.id === activeFilePreview?.id}
            preview={preview}
            token={token}
          />
        ))}
        {actorSessionID && (browserSurfaceTabs.length > 0 || browserSurfaceVisible) ? (
          <BrowserWorkspaceSurface
            key={`browser:${actorSessionID}`}
            active={browserActive && presented}
            activeTabID={activeBrowserTabID}
            pending={browserSurfacePending}
            sessionID={actorSessionID}
            tabs={browserSurfaceTabs}
            token={token}
          />
        ) : null}
      </div>
      <AlertDialog open={Boolean(pendingSavedClose)} onOpenChange={(open) => !open && setPendingSavedClose(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("canvas.closeSavedTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("canvas.closeSavedDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="outline"
              onClick={() => {
                if (pendingSavedClose) closeSavedMutation.mutate({ item: pendingSavedClose, saveChanges: false });
              }}
            >
              {t("canvas.closeWithoutSaving")}
            </AlertDialogAction>
            <AlertDialogAction
              onClick={() => {
                if (pendingSavedClose) closeSavedMutation.mutate({ item: pendingSavedClose, saveChanges: true });
              }}
            >
              {t("canvas.saveAndClose")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </aside>
  );
});

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

function canvasGalleryStateKey(sessionID: string, itemID: string) {
  return `${sessionID}\u0000${itemID}`;
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
