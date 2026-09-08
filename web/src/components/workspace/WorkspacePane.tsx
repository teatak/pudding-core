import { useLibrary } from "./useLibrary";
import { AppTooltip } from "@/components/AppTooltip";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderClosed, Plus } from "@/components/icons";
import { Button } from "@/components/ui/button";
import { BrowserTabIcon } from "@/browser/BrowserTabIcon";
import { browserTabFaviconURL } from "@/browser/helpers";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  APIError,
  deleteCanvasItem,
  deleteSavedCanvasItem,
  listCanvasItems,
  getSession,
  putCanvasItem,
  openSavedCanvasItem,
  saveCanvasItem,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useRetainBrowserRuntimeTabs } from "@/browser/BrowserRuntimeProvider";
import { browserTabTitle } from "@/browser/helpers";
import type { GalleryLayout } from "@/components/canvas/CanvasItemContent";
import {
  CanvasItemActions,
  CanvasItemSurface,
} from "@/components/canvas/CanvasItemSurface";
import { CanvasKindIcon, titleForCanvasItem } from "@/components/canvas/CanvasKindIcon";
import { filePreviewTitle } from "@/components/canvas/FilePreviewSurface";
import { asRecord, stringValue } from "@/components/canvas/canvasPayload";
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
import type { CanvasItem } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { turnFileChangeFullPath, turnFileChangeLabel, turnFileDiffChanges } from "@/lib/turnFileChanges";
import { consumeCanvasReveal, useVisibleCanvasReveal } from "@/state/canvasRevealStore";
import {
  closeFilePreview,
  consumeFilePreviewReveal,
  useFilePreviews,
  useFilePreviewReveal,
} from "@/state/filePreviewStore";
import { useVisibleProjectFileReveal } from "@/state/projectRevealStore";
import {
  canvasWorkspaceTabKey,
  fileWorkspaceTabKey,
  closeWorkspaceTab,
  closeWorkspaceTabs,
  reconcileWorkspaceTabs,
  mergeWorkspaceTabOrder,
  useWorkspaceSessionUI,
  resolveCanvasTabs,
  updateWorkspaceSessionUI,
  openWorkspaceView,
  openWorkspaceTab,
  setWorkspaceActiveTab,
  workspaceTabResourceID,
  type WorkspaceTabKey,
} from "@/state/workspaceStore";
import {
  clearVisibleUIContext,
  setVisibleUIContext,
  type UIContextPart,
} from "@/state/uiContextStore";
import { BrowserWorkspaceSurface } from "./BrowserWorkspaceSurface";
import { WorkspaceContentTabs, type WorkspaceContentTab } from "./WorkspaceContentTabs";
import { WorkspaceLibrary } from "./WorkspaceLibrary";
import { useWorkspaceBrowserSurface } from "./useWorkspaceBrowserSurface";

type WorkspacePaneProps = {
  token: string;
  activeSessionID?: string;
  presented: boolean;
  sessionID?: string;
  secondarySessionID?: string;
  reserveWorkspaceControl?: boolean;
};

export const WorkspacePane = memo(function WorkspacePane({
  token,
  activeSessionID,
  presented,
  sessionID,
  secondarySessionID,
  reserveWorkspaceControl = false,
}: WorkspacePaneProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const actorSessionIDRef = useRef("");
  const librarySearchRef = useRef<HTMLInputElement>(null);
  const [canvasGalleryActiveIndices, setCanvasGalleryActiveIndices] = useState<Record<string, number>>({});
  const [pendingDelete, setPendingDelete] = useState<({ kind: "canvas"; item: CanvasItem } | { kind: "saved"; item: { id: string; title?: string } }) & { sessionID: string }>();
  const [pendingCanvasClose, setPendingCanvasClose] = useState<{ sessionID: string; keys: WorkspaceTabKey[] }>();
  const [projectUIContext, setProjectUIContext] = useState<UIContextPart>();
  const [validatedProjectReveal, setValidatedProjectReveal] = useState<{ serial: number; sessionID: string }>();
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
  const sessionFilePreviews = actorSessionID === secondarySessionID ? secondaryFilePreviews : primaryFilePreviews;
  const workspaceUI = useWorkspaceSessionUI(actorSessionID);
  const projectTabOrder = workspaceUI.project.tabOrder;
  const filePreviews = useMemo(() => {
    const byKey = new Map(sessionFilePreviews.map((preview) => [fileWorkspaceTabKey(preview.id), preview]));
    return mergeWorkspaceTabOrder(projectTabOrder, [...byKey.keys()]).map((key) => byKey.get(key)!);
  }, [sessionFilePreviews, projectTabOrder]);
  const enabled = Boolean(token && actorSessionID);
  const activeTab = workspaceUI.activeTab;
  const activeApp = activeTab.startsWith("browser:") ? "browser" : activeTab.startsWith("canvas:") ? "artifacts" : activeTab;
  const projectActiveTab = workspaceUI.project.activeTab;

  const sessionQuery = useQuery({
    enabled,
    queryKey: queryKeys.session(actorSessionID),
    queryFn: () => getSession(token, actorSessionID),
    staleTime: 10_000,
  });
  const hasProject = Boolean(sessionQuery.data?.projectID);
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

  const itemsQuery = useQuery({
    enabled,
    queryKey: queryKeys.canvasItems(actorSessionID),
    queryFn: () => listCanvasItems(token, actorSessionID),
    staleTime: Infinity,
  });
  const libraryQuery = useLibrary(token, actorSessionID, enabled);

  const items = useMemo(() => itemsQuery.data?.items ?? [], [itemsQuery.data?.items]);
  const canvasTabs = useMemo(() => resolveCanvasTabs(workspaceUI, items), [workspaceUI, items]);
  const openCanvasItems = useMemo(() => items.filter((item) => !canvasTabs.closedCanvasTabs?.[canvasWorkspaceTabKey(item.id)]), [items, canvasTabs]);
  const {
    activeBrowserTabID,
    activeBrowserSelection,
    browserActive,
    browserTabsReady,
    browserTabsResolved,
    browserTabs,
    browserSurfacePending,
    browserSurfaceError,
    retryBrowserTabs,
    closeBrowserTabs,
    closingBrowserTabIDs,
    createNewBrowserTab,
    creatingBrowserTab,
    openBrowserLink,
  } = useWorkspaceBrowserSurface({
    enabled,
    sessionID: actorSessionID,
    token,
  });
  useRetainBrowserRuntimeTabs(actorSessionID, browserTabs, browserTabsReady);
  const browserSurfaceTabs = browserTabsReady ? browserTabs : [];
  const availableWorkspaceTabs = useMemo<WorkspaceTabKey[]>(() => [
    "project", ...filePreviews.map((preview) => fileWorkspaceTabKey(preview.id)),
    ...items.map((item) => canvasWorkspaceTabKey(item.id)),
    ...browserTabs.map((tab) => `browser:${tab.id}` as const),
  ], [filePreviews, items, browserTabs]);
  const activeCanvasItemID = workspaceTabResourceID(activeTab, "canvas");
  const activeCanvasItem = items.find((item) => item.id === activeCanvasItemID);
  const activeFilePreviewID = workspaceTabResourceID(projectActiveTab, "file");
  const activeFilePreview = filePreviews.find((preview) => preview.id === activeFilePreviewID);
  const projectActive = activeApp === "project";
  const filePreviewActive = projectActive && Boolean(activeFilePreview);

  const visibleUIContext = useMemo<UIContextPart | undefined>(() => {
    if (!actorSessionID) {
      return undefined;
    }
    if (projectActive && !filePreviewActive) {
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
    if (activeApp === "artifacts") {
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
    if (activeApp === "browser") {
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
    activeApp,
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
    if (!preview) return;
    openWorkspaceTab(actorSessionID, fileWorkspaceTabKey(preview.id));
    consumeFilePreviewReveal(filePreviewReveal.serial);
  }, [
    actorSessionID,
    filePreviewReveal,
    filePreviews,
  ]);

  useEffect(() => {
    if (!canvasReveal || canvasReveal.sessionID !== actorSessionID) {
      return;
    }
    if (!items.some((item) => item.id === canvasReveal.itemID)) {
      return;
    }
    setWorkspaceActiveTab(actorSessionID, canvasWorkspaceTabKey(canvasReveal.itemID));
    consumeCanvasReveal(canvasReveal.serial);
  }, [actorSessionID, canvasReveal, items]);

  const closeLocalWorkspaceTab = useCallback((closingTab: WorkspaceTabKey) => {
    closeWorkspaceTab(actorSessionID, closingTab);
  }, [actorSessionID]);
  const selectFilePreview = useCallback((previewID: string) => setWorkspaceActiveTab(actorSessionID, fileWorkspaceTabKey(previewID)), [actorSessionID]);
  const deactivateFilePreview = useCallback(() => setWorkspaceActiveTab(actorSessionID, "project"), [actorSessionID]);
  const closeFilePreviews = useCallback((previewIDs: string[]) => {
    filePreviews.filter((preview) => previewIDs.includes(preview.id)).forEach((preview) => {
      closeFilePreview(preview.sessionID, preview.id);
      closeLocalWorkspaceTab(fileWorkspaceTabKey(preview.id));
    });
  }, [filePreviews, closeLocalWorkspaceTab]);

  const galleryLayoutMutation = useMutation({
    mutationFn: ({ item, layout }: { item: CanvasItem; layout: GalleryLayout }) => {
      const payload = asRecord(item.item) || {};
      const title = titleForCanvasItem(item, t);
      const kind = stringValue(payload.kind) || item.kind;
      return putCanvasItem(token, item.sessionID, item.id, {
        id: item.id,
        kind,
        title,
        item: { ...payload, kind, title, layout },
        window: item.window,
      });
    },
    onSuccess: (_result, { item }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.canvasItems(item.sessionID) });
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

  const closeContentTabsNow = async (targetSessionID: string, keys: WorkspaceTabKey[]) => {
    const browserIDs = keys.flatMap((key) => { const id = workspaceTabResourceID(key, "browser"); return id ? [id] : []; });
    try {
      if (browserIDs.length) await closeBrowserTabs(targetSessionID, browserIDs);
      closeWorkspaceTabs(targetSessionID, keys.filter((key) => !key.startsWith("browser:")));
      setPendingCanvasClose(undefined);
    } catch {
      // Browser release reports the failure; keep the remaining canvas tabs open.
    }
  };
  const saveItemMutation = useMutation({
    mutationFn: async ({ targetSessionID, itemIDs, closeAfterSave }: { targetSessionID: string; itemIDs: string[]; closeAfterSave?: WorkspaceTabKey[] }) => {
      for (const id of itemIDs) {
        const key = queryKeys.canvasItems(targetSessionID);
        const item = queryClient.getQueryData<{ items: CanvasItem[] }>(key)?.items.find((entry) => entry.id === id);
        if (!item || (closeAfterSave && !(item.sourceSavedItemID && item.savedDirty))) continue;
        const result = await saveCanvasItem(token, targetSessionID, id);
        queryClient.setQueryData<{ items: CanvasItem[] }>(key, (current) => ({
          items: (current?.items || []).map((entry) => entry.id === result.item.id ? result.item : entry),
        }));
      }
    },
    onSuccess: (_result, { targetSessionID, closeAfterSave }) => {
      if (closeAfterSave) void closeContentTabsNow(targetSessionID, closeAfterSave);
      toast.success(t("canvas.saveDone"));
    },
    onSettled: () => { void queryClient.invalidateQueries({ queryKey: ["library"] }); },
    onError: (error) => {
      toast.error(error instanceof APIError && error.code === "saved_canvas_conflict"
        ? t("canvas.saveConflict")
        : t("canvas.saveFailed"));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (item: CanvasItem) => deleteCanvasItem(token, item.sessionID, item.id),
    onSuccess: (_result, item) => {
      const key = queryKeys.canvasItems(item.sessionID);
      queryClient.setQueryData<{ items: CanvasItem[] }>(key, (current) => ({
        items: (current?.items || []).filter((entry) => entry.id !== item.id),
      }));
      const remaining = queryClient.getQueryData<{ items: CanvasItem[] }>(key)?.items || [];
      updateWorkspaceSessionUI(item.sessionID, (current) => resolveCanvasTabs(current, remaining));
      setPendingDelete(undefined);
      void queryClient.invalidateQueries({ queryKey: key });
    },
    onError: () => toast.error(t("canvas.deleteFailed")),
  });
  const requestCloseContentTabs = (keys: WorkspaceTabKey[]) => {
    if (saveItemMutation.isPending || pendingCanvasClose || closingBrowserTabIDs.length) return;
    const selected = openCanvasItems.filter((item) => keys.includes(canvasWorkspaceTabKey(item.id)));
    if (selected.some((item) => item.sourceSavedItemID && item.savedDirty)) {
      setPendingCanvasClose({ sessionID: actorSessionID, keys });
    } else void closeContentTabsNow(actorSessionID, keys);
  };
  const pendingCanvasItems = pendingCanvasClose?.sessionID === actorSessionID
    ? items.filter((item) => pendingCanvasClose.keys.includes(canvasWorkspaceTabKey(item.id))) : [];

  const openSavedMutation = useMutation({
    mutationFn: ({ entry, targetSessionID }: { entry: { id: string }; targetSessionID: string }) => openSavedCanvasItem(token, targetSessionID, entry.id),
    onSuccess: (item) => {
      queryClient.setQueryData<{ items: CanvasItem[] }>(queryKeys.canvasItems(item.sessionID), (current) => ({
        items: [...(current?.items || []).filter((entry) => entry.id !== item.id), item],
      }));
      openWorkspaceTab(item.sessionID, canvasWorkspaceTabKey(item.id));
    },
    onError: () => toast.error(t("canvas.openSavedFailed")),
  });

  const removeSavedMutation = useMutation({
    mutationFn: ({ entry, targetSessionID }: { entry: { id: string }; targetSessionID: string }) => deleteSavedCanvasItem(token, targetSessionID, entry.id),
    onSuccess: () => {
      setPendingDelete(undefined);
      void queryClient.invalidateQueries({ queryKey: ["library"] });
      void queryClient.invalidateQueries({
        predicate: (query) => query.queryKey[0] === "session"
          && query.queryKey[2] === "canvas"
          && query.queryKey[3] === "items",
      });
    },
    onError: () => toast.error(t("canvas.deleteSavedFailed")),
  });

  useEffect(() => {
    if (!enabled) return;
    reconcileWorkspaceTabs(actorSessionID, "project", availableWorkspaceTabs);
    if (browserTabsResolved) reconcileWorkspaceTabs(actorSessionID, "browser", availableWorkspaceTabs);
    if (itemsQuery.isSuccess && !itemsQuery.isFetching) updateWorkspaceSessionUI(actorSessionID, (current) => resolveCanvasTabs(current, items));
  }, [activeTab, actorSessionID, availableWorkspaceTabs, browserTabsResolved, enabled, itemsQuery.isFetching, itemsQuery.isSuccess, items, workspaceUI]);

  const contentTabs: WorkspaceContentTab[] = [
    ...(workspaceUI.tabOrder.includes("project") ? [{ id: "project" as const, title: t("workspace.app.project"), kind: "project" as const, icon: <FolderClosed className="size-4" /> }] : []),
    ...browserSurfaceTabs.map((tab) => ({
        id: `browser:${tab.id}` as const, title: browserTabTitle(tab, t("browser.newTab"), t("browser.newTab")), kind: "browser" as const,
        closing: closingBrowserTabIDs.includes(tab.id),
        icon: <BrowserTabIcon className="size-4" faviconURL={browserTabFaviconURL(tab)} pageURL={tab.url} />,
      })),
    ...openCanvasItems.map((item) => ({
        id: canvasWorkspaceTabKey(item.id), title: titleForCanvasItem(item, t), kind: "canvas" as const,
        closing: (deleteMutation.isPending && deleteMutation.variables?.id === item.id) || (saveItemMutation.isPending && saveItemMutation.variables?.closeAfterSave && saveItemMutation.variables.targetSessionID === item.sessionID && saveItemMutation.variables.itemIDs.includes(item.id)),
        icon: <CanvasKindIcon kind={item.kind} size="xs" />,
      })),
  ];
  return (
    <aside className="pudding-workspace-pane relative flex h-full shrink-0 flex-col bg-[var(--workspace-chrome-background)] text-sidebar-foreground">
      <WorkspaceContentTabs
        sessionID={actorSessionID}
        tabs={contentTabs}
        closing={closingBrowserTabIDs.length > 0 || saveItemMutation.isPending || Boolean(pendingCanvasClose)}
        onClose={requestCloseContentTabs}
        className={cn(
          "pudding-workspace-topbar relative z-30 !pl-(--workspace-toolbar-pl)",
          activeApp === "library" && "pudding-workspace-topbar-empty",
          reserveWorkspaceControl
            ? "!pr-[calc(var(--workspace-toggle-right)+var(--workspace-control-width)+0.5rem)]"
            : "!pr-(--workspace-toolbar-pr)",
        )}
        trailingAction={contentTabs.length > 0 ? <AppTooltip content={t("workspace.app.library")}>
          <Button data-workspace-add aria-label={t("workspace.app.library")} className="size-7 shrink-0 self-center rounded-md text-muted-foreground" size="icon-sm" variant="ghost" onClick={() => { openWorkspaceView(actorSessionID, "library"); librarySearchRef.current?.focus({ preventScroll: true }); }}><Plus className="size-4" /></Button>
        </AppTooltip> : null}
        actions={secondarySessionID && sessionQuery.data?.title ? <span className="pudding-workspace-session-label max-w-24 truncate text-[11px] text-muted-foreground">{sessionQuery.data.title}</span> : null}
      />
      <div className="relative z-0 min-h-0 flex-1 overflow-hidden">
        <WorkspaceLibrary key={actorSessionID} searchRef={librarySearchRef}
          onOpenProject={() => openWorkspaceView(actorSessionID, "project")}
          onNewBrowserTab={createNewBrowserTab} creatingBrowserTab={creatingBrowserTab}
          active={activeApp === "library"} sessionID={actorSessionID} token={token} entries={libraryQuery.data?.entries || []}
          savedQuery={libraryQuery} canvasItems={items} canvasQuery={itemsQuery}
          onOpenSaved={(entry) => openSavedMutation.mutate({ entry, targetSessionID: actorSessionID })}
          onRemoveSaved={(item) => setPendingDelete({ kind: "saved", item, sessionID: actorSessionID })} onRemoveCanvas={(item) => setPendingDelete({ kind: "canvas", item, sessionID: actorSessionID })}
          onOpenBrowserURL={(url) => openBrowserLink(actorSessionID, url)}
        />
        {activeApp === "browser" && !browserSurfaceTabs.length && !browserSurfacePending && browserSurfaceError ? (
          <div role="alert" className="flex h-full flex-col items-center justify-center gap-3 p-6 text-sm text-muted-foreground">
            <p>{t("browser.loadFailed")}</p>
            <Button variant="outline" onClick={() => void retryBrowserTabs()}>{t("common.refresh")}</Button>
          </div>
        ) : null}
        {openCanvasItems.map((item) => (
          <CanvasItemSurface
            key={`${actorSessionID}:${item.id}`}
            active={activeApp === "artifacts" && !filePreviewActive && activeCanvasItem?.id === item.id}
            activeIndex={canvasGalleryActiveIndices[canvasGalleryStateKey(actorSessionID, item.id)] || 0}
            item={item}
            token={token}
            onActiveIndexChange={changeCanvasActiveIndex}
            onGalleryLayoutChange={changeCanvasGalleryLayout}
          />
        ))}
        {activeApp === "artifacts" && !filePreviewActive && activeCanvasItem ? (
          <div className="absolute top-3 right-3 z-20">
            <CanvasItemActions
              item={activeCanvasItem}
              saving={saveItemMutation.isPending && saveItemMutation.variables?.targetSessionID === activeCanvasItem.sessionID && saveItemMutation.variables.itemIDs.includes(activeCanvasItem.id)}
              token={token}
              onSave={() => saveItemMutation.mutate({ targetSessionID: activeCanvasItem.sessionID, itemIDs: [activeCanvasItem.id] })}
              onDelete={() => setPendingDelete({ kind: "canvas", item: activeCanvasItem, sessionID: activeCanvasItem.sessionID })}
              onGalleryLayoutChange={(layout) => galleryLayoutMutation.mutate({ item: activeCanvasItem, layout })}
            />
          </div>
        ) : null}
        {activeApp === "artifacts" && !filePreviewActive && itemsQuery.isLoading && items.length === 0 ? (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-[var(--workspace-background)] text-muted-foreground">
            <Spinner className="size-5" />
          </div>
        ) : null}
        {actorSessionID ? (
          <ProjectBrowserSurface
            onOpenBrowserURL={openBrowserLink}
            active={projectActive && presented}
            activePreviewID={activeFilePreview?.id}
            hasProject={hasProject}
            projectStateReady={projectRevealReady}
            sessionID={actorSessionID}
            token={token}
            previewTabs={filePreviews}
            onActivatePreview={selectFilePreview}
            onClosePreviews={closeFilePreviews}
            onDeactivatePreview={deactivateFilePreview}
            onVisibleContextChange={setProjectUIContext}
          />
        ) : null}
        {actorSessionID && (browserSurfaceTabs.length > 0 || browserSurfacePending) ? (
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
      <AlertDialog open={Boolean(pendingDelete && pendingDelete.sessionID === actorSessionID)} onOpenChange={(open) => !open && !deleteMutation.isPending && !removeSavedMutation.isPending && setPendingDelete(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(pendingDelete?.kind === "saved" ? "canvas.deleteSavedWidget" : "canvas.delete")}</AlertDialogTitle>
            <AlertDialogDescription>{t(pendingDelete?.kind === "saved" ? "canvas.deleteSavedDescription" : "canvas.deleteDescription").replace("{title}", pendingDelete?.item.title || t("canvas.untitled"))}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending || removeSavedMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={deleteMutation.isPending || removeSavedMutation.isPending} onClick={(event) => {
              event.preventDefault();
              if (pendingDelete?.kind === "canvas") deleteMutation.mutate(pendingDelete.item);
              else if (pendingDelete?.kind === "saved") removeSavedMutation.mutate({ entry: pendingDelete.item, targetSessionID: pendingDelete.sessionID });
            }}>{t("common.delete")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={Boolean(pendingCanvasClose && pendingCanvasClose.sessionID === actorSessionID)} onOpenChange={(open) => !open && !saveItemMutation.isPending && setPendingCanvasClose(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("canvas.closeSavedTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("canvas.closeSavedDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <ul className="max-h-48 overflow-y-auto text-sm text-muted-foreground">
            {pendingCanvasItems.map((item) => <li className="flex items-center gap-2 py-1" key={item.id}><CanvasKindIcon kind={item.kind} size="xs" /><span className="min-w-0 flex-1 truncate">{titleForCanvasItem(item, t)}</span>{item.sourceSavedItemID && item.savedDirty ? <span aria-label={t("project.browserUnsaved")} className="size-1.5 shrink-0 rounded-full bg-foreground/60" /> : null}</li>)}
          </ul>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={saveItemMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="outline"
              disabled={saveItemMutation.isPending}
              onClick={() => {
                if (pendingCanvasClose) void closeContentTabsNow(pendingCanvasClose.sessionID, pendingCanvasClose.keys);
              }}
            >
              {t("canvas.closeWithoutSaving")}
            </AlertDialogAction>
            <AlertDialogAction
              disabled={saveItemMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                if (pendingCanvasClose) saveItemMutation.mutate({ targetSessionID: pendingCanvasClose.sessionID, itemIDs: pendingCanvasItems.map((item) => item.id), closeAfterSave: pendingCanvasClose.keys });
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

function canvasGalleryStateKey(sessionID: string, itemID: string) {
  return `${sessionID}\u0000${itemID}`;
}
