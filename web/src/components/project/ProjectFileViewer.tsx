import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Eye, FileCode2, FilePenLine, Folders, Maximize2, Minimize2, Minus, Plus, Save } from "@/components/icons";
import { lazy, Suspense, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";

import { APIError, getProjectFile, projectResourceURL, saveProjectFile, type ProjectFile } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { TurnFileDiffSurface } from "@/components/canvas/TurnFileDiffSurface";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { watchElectronProjectFile } from "@/desktop/projectFileWatcher";
import { languageFromPath } from "@/lib/fileLanguage";
import type { FilePreview } from "@/state/filePreviewStore";

import { ProjectFileTabs } from "./ProjectFileTabs";
import type { ProjectEditorSelection } from "./ProjectEditor";
import { ProjectGitDiffViewer } from "./git/ProjectGitDiffViewer";
import { projectBrowserError } from "./projectErrors";
import { projectSelectionKey } from "./projectPaths";
import { isProjectImagePath, isProjectPDFPath, isProjectSVGPath, projectDocumentPreviewKind } from "./projectPreviewKinds";
import type { ProjectEditorReveal } from "./projectReveal";
import { isProjectGitDiffTab, type ProjectSelection, type ProjectTab } from "./types";

const ProjectEditor = lazy(() => import("./ProjectEditor").then((module) => ({ default: module.ProjectEditor })));
const ProjectDocumentPreview = lazy(() => import("./ProjectDocumentPreview").then((module) => ({ default: module.ProjectDocumentPreview })));
const ProjectMarkdownEditor = lazy(() => import("./ProjectMarkdownEditor").then((module) => ({ default: module.ProjectMarkdownEditor })));

type FileViewMode = "preview" | "source";

const viewModeButtonClassName =
  "text-muted-foreground hover:bg-muted/60 hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground aria-pressed:shadow-none dark:hover:bg-muted/60 dark:aria-pressed:bg-input/50";

type FileDraft = {
  baseContent: string;
  baseRevision: string;
  content: string;
  externalRevision?: string;
};

type SaveDraftRequest = {
  expectedRevision: string;
  target: ProjectSelection;
  targetSessionID: string;
  value: string;
};

export function ProjectFileViewer({
  active,
  activeTurnDiff,
  activeTurnDiffSelection,
  absolutePath,
  dirtyKeys,
  discardRequest,
  reveal,
  selection,
  sessionID,
  showFilesAction = false,
  tabs,
  turnDiffTabs,
  token,
  onActivate,
  onActivateTurnDiff,
  onCloseTurnDiffs,
  onDirtyChange,
  onOpenPreview,
  onPin,
  onReference,
  onRequestClose,
  onReveal,
  onShowFiles,
}: {
  active: boolean;
  activeTurnDiff?: FilePreview;
  activeTurnDiffSelection?: ProjectSelection;
  absolutePath?: string;
  dirtyKeys: ReadonlySet<string>;
  discardRequest?: { id: number; keys: string[]; sessionID: string };
  reveal?: ProjectEditorReveal;
  selection?: ProjectTab;
  sessionID: string;
  showFilesAction?: boolean;
  tabs: ProjectTab[];
  turnDiffTabs: FilePreview[];
  token: string;
  onActivate: (selection: ProjectTab) => void;
  onActivateTurnDiff: (previewID: string) => void;
  onCloseTurnDiffs: (previewIDs: string[]) => void;
  onDirtyChange: (targetSessionID: string, selection: ProjectSelection, dirty: boolean) => void;
  onOpenPreview: (selection: ProjectSelection) => void;
  onPin: (selection: ProjectTab) => void;
  onReference: (selection: ProjectSelection, range: ProjectEditorSelection) => void;
  onRequestClose: (keys: string[]) => void;
  onReveal: (selection: ProjectSelection) => void;
  onShowFiles?: () => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [drafts, setDrafts] = useState<Record<string, FileDraft>>({});
  const draftsRef = useRef(drafts);
  draftsRef.current = drafts;
  const updateDrafts = (update: (current: Record<string, FileDraft>) => Record<string, FileDraft>) => {
    const next = update(draftsRef.current);
    draftsRef.current = next;
    setDrafts(next);
  };
  const [fileViewModes, setFileViewModes] = useState<Record<string, FileViewMode>>({});
  const [documentExpandedPaths, setDocumentExpandedPaths] = useState<Record<string, string[]>>({});
  const [resourceRevision, setResourceRevision] = useState(0);
  const gitDiffSelection = selection && isProjectGitDiffTab(selection) ? selection : undefined;
  const fileSelection = selection && !isProjectGitDiffTab(selection) ? selection : undefined;
  const selectionKey = fileSelection ? projectSelectionKey(fileSelection) : "";
  const draftKey = selectionKey ? `${sessionID}:${selectionKey}` : "";
  const isImage = Boolean(fileSelection && isProjectImagePath(fileSelection.path));
  const isSVG = Boolean(fileSelection && isProjectSVGPath(fileSelection.path));
  const isPDF = Boolean(fileSelection && isProjectPDFPath(fileSelection.path));
  const isResourcePreview = isImage || isPDF;
  const documentPreviewKind = fileSelection ? projectDocumentPreviewKind(fileSelection.path) : undefined;
  const fileQuery = useQuery({
    enabled: active && Boolean(fileSelection) && (!isResourcePreview || isSVG),
    queryKey: fileSelection
      ? queryKeys.projectFile(sessionID, fileSelection.rootID, fileSelection.path)
      : ["session", sessionID, "project", "file", "none"],
    queryFn: () => {
      if (!fileSelection) throw new Error("project file missing");
      return getProjectFile(token, sessionID, fileSelection.rootID, fileSelection.path);
    },
    retry: false,
    staleTime: 0,
  });
  const file = fileQuery.data;
  const draft = draftKey ? drafts[draftKey] : undefined;
  const content = draft?.content ?? file?.content ?? "";
  const dirty = Boolean(draft && draft.content !== draft.baseContent);
  const externalConflict = Boolean(draft?.externalRevision);
  const isMarkdown = file?.mime === "text/markdown" || /\.(?:md|markdown)$/i.test(file?.name || "");
  const supportsViewMode = isMarkdown || Boolean(documentPreviewKind) || isSVG;
  const fileViewMode = supportsViewMode && draftKey ? fileViewModes[draftKey] ?? "preview" : "source";
  const expandedDocumentPaths = useMemo(
    () => new Set(documentExpandedPaths[draftKey] ?? ["$"]),
    [documentExpandedPaths, draftKey],
  );
  const resourceURL = useMemo(() => {
    if (!isResourcePreview || !fileSelection) return "";
    const url = projectResourceURL(token, sessionID, fileSelection.rootID, fileSelection.path);
    return `${url}${url.includes("?") ? "&" : "?"}v=${resourceRevision}`;
  }, [fileSelection?.path, fileSelection?.rootID, isResourcePreview, resourceRevision, sessionID, token]);

  useEffect(() => {
    if (!reveal || reveal.key !== selectionKey || !draftKey) {
      return;
    }
    setFileViewModes((current) => ({ ...current, [draftKey]: documentPreviewKind || isSVG ? "source" : "preview" }));
  }, [documentPreviewKind, draftKey, isSVG, reveal?.serial, selectionKey]);

  useEffect(() => {
    if (!active || !absolutePath || !fileSelection) {
      return;
    }
    const refetch = () => {
      if (isResourcePreview) setResourceRevision((current) => current + 1);
      if (!isResourcePreview || isSVG) void fileQuery.refetch();
      void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project", "git"] });
    };
    return watchElectronProjectFile(absolutePath, refetch);
  }, [absolutePath, active, draftKey, isResourcePreview, isSVG]);

  useEffect(() => {
    if (!discardRequest) return;
    updateDrafts((current) => {
      const next = { ...current };
      discardRequest.keys.forEach((key) => delete next[`${discardRequest.sessionID}:${key}`]);
      return next;
    });
  }, [discardRequest]);

  useEffect(() => {
    if (!fileSelection || !file) {
      return;
    }
    const key = `${sessionID}:${projectSelectionKey(fileSelection)}`;
    const existing = draftsRef.current[key];
    if (!existing || existing.content === existing.baseContent) {
      updateDrafts((current) => ({
        ...current,
        [key]: { baseContent: file.content, baseRevision: file.revision, content: file.content },
      }));
      return;
    }
    if (existing.content === file.content) {
      updateDrafts((current) => ({
        ...current,
        [key]: { baseContent: file.content, baseRevision: file.revision, content: file.content },
      }));
      onDirtyChange(sessionID, fileSelection, false);
      return;
    }
    if (existing.baseRevision === file.revision) {
      if (existing.externalRevision) {
        updateDrafts((current) => ({ ...current, [key]: { ...existing, externalRevision: undefined } }));
      }
      return;
    }
    if (existing.externalRevision !== file.revision) {
      updateDrafts((current) => ({ ...current, [key]: { ...existing, externalRevision: file.revision } }));
    }
  }, [file, fileSelection, sessionID]);

  const saveMutation = useMutation({
    mutationFn: ({ expectedRevision, target, targetSessionID, value }: SaveDraftRequest) => {
      return saveProjectFile(token, targetSessionID, {
        rootID: target.rootID,
        path: target.path,
        content: value,
        expectedRevision,
      });
    },
    onSuccess: (saved, variables) => {
      const key = `${variables.targetSessionID}:${projectSelectionKey(saved)}`;
      const latestContent = draftsRef.current[key]?.content ?? saved.content;
      const stillDirty = latestContent !== saved.content;
      queryClient.setQueryData(queryKeys.projectFile(variables.targetSessionID, saved.rootID, saved.path), saved);
      void queryClient.invalidateQueries({ queryKey: ["session", variables.targetSessionID, "project", "git"] });
      if (isProjectSVGPath(saved.path)) setResourceRevision((current) => current + 1);
      updateDrafts((current) => ({
        ...current,
        [key]: { baseContent: saved.content, baseRevision: saved.revision, content: latestContent },
      }));
      onDirtyChange(variables.targetSessionID, saved, stillDirty);
      toast.success(t("project.browserSaved"));
    },
    onError: (error, variables) => {
      if (error instanceof APIError && error.code === "project_file_revision_conflict") {
        void queryClient.invalidateQueries({
          exact: true,
          queryKey: queryKeys.projectFile(variables.targetSessionID, variables.target.rootID, variables.target.path),
        });
        toast.warning(t("project.browserExternalChange"));
        return;
      }
      toast.error(projectBrowserError(error, t));
    },
  });

  const save = (overwrite = false) => {
    if (!fileSelection || !draft || !dirty || saveMutation.isPending || fileQuery.isError) {
      return;
    }
    if (externalConflict && !overwrite) {
      toast.warning(t("project.browserExternalChange"));
      return;
    }
    saveMutation.mutate({
      expectedRevision: overwrite && file ? file.revision : draft.baseRevision,
      target: { rootID: fileSelection.rootID, path: fileSelection.path },
      targetSessionID: sessionID,
      value: draft.content,
    });
  };

  const changeContent = (value: string) => {
    if (!fileSelection || !file) return;
    const key = `${sessionID}:${projectSelectionKey(fileSelection)}`;
    const previous = drafts[key] || { baseContent: file.content, baseRevision: file.revision, content: file.content };
    let nextDirty = value !== previous.baseContent;
    if (previous.externalRevision && (value === previous.baseContent || value === file.content)) {
      updateDrafts((current) => ({
        ...current,
        [key]: { baseContent: file.content, baseRevision: file.revision, content: file.content },
      }));
      nextDirty = false;
    } else {
      updateDrafts((current) => ({
        ...current,
        [key]: { ...previous, content: value, externalRevision: nextDirty ? previous.externalRevision : undefined },
      }));
    }
    onDirtyChange(sessionID, fileSelection, nextDirty);
    if (nextDirty) onPin(fileSelection);
  };

  const reloadExternal = () => {
    if (!fileSelection || !file) return;
    updateDrafts((current) => ({
      ...current,
      [draftKey]: { baseContent: file.content, baseRevision: file.revision, content: file.content },
    }));
    onDirtyChange(sessionID, fileSelection, false);
  };

  const previewFile = useMemo<ProjectFile | undefined>(
    () => file ? { ...file, content, size: new TextEncoder().encode(content).length } : undefined,
    [content, file],
  );
  const sourceEditorVisible = Boolean(
    previewFile
      && !(isMarkdown && fileViewMode === "preview")
      && !(documentPreviewKind && fileViewMode === "preview")
      && !(isSVG && fileViewMode === "preview"),
  );
  const openTurnDiffFile = (mode: FileViewMode) => {
    if (!activeTurnDiffSelection) return;
    const key = `${sessionID}:${projectSelectionKey(activeTurnDiffSelection)}`;
    setFileViewModes((current) => ({ ...current, [key]: mode }));
    onOpenPreview(activeTurnDiffSelection);
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--workspace-file-editor-background)]">
      <ProjectFileTabs
        active={selection}
        activeTurnDiffID={activeTurnDiff?.id}
        dirtyKeys={dirtyKeys}
        leadingAction={showFilesAction ? (
          <button
            aria-label={t("project.browserFiles")}
            className="inline-flex h-full w-8 items-center justify-center text-muted-foreground hover:bg-[var(--workspace-file-tab-hover-background)] hover:text-foreground"
            type="button"
            onClick={onShowFiles}
          >
            <Folders className="size-4" />
          </button>
        ) : undefined}
        tabs={tabs}
        turnDiffTabs={turnDiffTabs}
        onActivate={onActivate}
        onActivateTurnDiff={onActivateTurnDiff}
        onCloseTurnDiffs={onCloseTurnDiffs}
        onPin={onPin}
        onRequestClose={onRequestClose}
        onReveal={onReveal}
      />
      {activeTurnDiff ? (
        <div className="relative min-h-0 flex-1">
          <TurnFileDiffSurface
            active={active}
            preview={activeTurnDiff}
            token={token}
            onOpenPreview={activeTurnDiffSelection ? () => openTurnDiffFile("preview") : undefined}
            onOpenSource={activeTurnDiffSelection ? () => openTurnDiffFile("source") : undefined}
          />
        </div>
      ) : gitDiffSelection ? (
        <ProjectGitDiffViewer active={active} selection={gitDiffSelection} sessionID={sessionID} token={token} />
      ) : (
      <>
      {fileSelection && (!isImage || (isSVG && fileViewMode === "source")) ? (
        <div className="flex h-8 shrink-0 items-center gap-2 bg-[var(--workspace-file-editor-background)] px-2.5">
          <code className="min-w-0 flex-1 cursor-text select-text truncate font-mono text-xs" >{file?.path || fileSelection.path}</code>
          {!isResourcePreview || isSVG ? (
            <div className="flex shrink-0 items-center gap-1">
              {isSVG ? (
                <Button aria-label={t("project.browserPreview")} className={viewModeButtonClassName} size="icon-sm" type="button" variant="ghost" onClick={() => setFileViewModes((current) => ({ ...current, [draftKey]: "preview" }))}>
                  <Eye />
                </Button>
              ) : supportsViewMode ? (
                <>
                  <Button aria-label={isMarkdown ? t("project.browserMarkdownEditor") : t("project.browserPreview")} aria-pressed={fileViewMode === "preview"} className={viewModeButtonClassName} size="icon-sm" type="button" variant="ghost" onClick={() => setFileViewModes((current) => ({ ...current, [draftKey]: "preview" }))}>
                    {isMarkdown ? <FilePenLine /> : <Eye />}
                  </Button>
                  <Button aria-label={t("project.browserSource")} aria-pressed={fileViewMode === "source"} className={viewModeButtonClassName} size="icon-sm" type="button" variant="ghost" onClick={() => setFileViewModes((current) => ({ ...current, [draftKey]: "source" }))}>
                    <FileCode2 />
                  </Button>
                </>
              ) : null}
              <Button aria-label={t("project.browserSave")} disabled={!dirty || saveMutation.isPending || externalConflict || fileQuery.isError} size="icon-sm"  type="button" variant="ghost" onClick={() => save()}>
                {saveMutation.isPending ? <Spinner /> : <Save />}
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
      {externalConflict && !fileQuery.isError ? (
        <div className="flex shrink-0 items-center gap-2 border-b border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning-foreground">
          <AlertTriangle className="size-4 shrink-0" />
          <span className="min-w-0 flex-1">{t("project.browserExternalChange")}</span>
          <Button size="sm" type="button" variant="outline" onClick={reloadExternal}>{t("project.browserReloadExternal")}</Button>
          <Button size="sm" type="button" variant="outline" onClick={() => save(true)}>{t("project.browserOverwriteExternal")}</Button>
        </div>
      ) : null}
      <div className={sourceEditorVisible ? "min-h-0 flex-1 overflow-hidden" : "min-h-0 flex-1 overflow-auto"}>
        {!fileSelection ? (
          <ProjectViewerStatus icon={<Folders className="size-8" />}>{t("project.browserSelectFile")}</ProjectViewerStatus>
        ) : isImage && (!isSVG || fileViewMode === "preview") ? (
          <ProjectImagePreview
            key={resourceURL}
            active={active}
            alt={fileSelection.path}
            src={resourceURL}
            onShowSource={isSVG ? () => setFileViewModes((current) => ({ ...current, [draftKey]: "source" })) : undefined}
          />
        ) : isPDF ? (
          <ProjectPDFPreview key={resourceURL} src={resourceURL} title={fileSelection.path} />
        ) : fileQuery.isError ? (
          <ProjectViewerStatus>{projectBrowserError(fileQuery.error, t)}</ProjectViewerStatus>
        ) : fileQuery.isLoading && !file ? (
          <ProjectViewerStatus icon={<Spinner className="size-6" />}>{t("common.loading")}</ProjectViewerStatus>
        ) : previewFile && isMarkdown && fileViewMode === "preview" ? (
          <Suspense fallback={<ProjectViewerStatus icon={<Spinner className="size-6" />}>{t("common.loading")}</ProjectViewerStatus>}>
            <ProjectMarkdownEditor
              key={draftKey}
              path={previewFile.path}
              reveal={reveal?.key === selectionKey ? reveal : undefined}
              value={content}
              onChange={changeContent}
              onSave={() => save()}
              onReferenceSelection={(range) => fileSelection && onReference(fileSelection, range)}
            />
          </Suspense>
        ) : previewFile && documentPreviewKind && fileViewMode === "preview" ? (
          <Suspense fallback={<ProjectViewerStatus icon={<Spinner className="size-6" />}>{t("common.loading")}</ProjectViewerStatus>}>
            <ProjectDocumentPreview
              key={draftKey}
              expandedPaths={expandedDocumentPaths}
              kind={documentPreviewKind}
              path={previewFile.path}
              value={content}
              onExpandedPathChange={(path, expanded) => {
                setDocumentExpandedPaths((current) => {
                  const next = new Set(current[draftKey] ?? ["$"]);
                  if (expanded) next.add(path);
                  else next.delete(path);
                  return { ...current, [draftKey]: Array.from(next) };
                });
              }}
            />
          </Suspense>
        ) : previewFile ? (
          <Suspense fallback={<ProjectViewerStatus icon={<Spinner className="size-6" />}>{t("common.loading")}</ProjectViewerStatus>}>
            <ProjectEditor
              key={draftKey}
              path={previewFile.path}
              reveal={reveal?.key === selectionKey ? reveal : undefined}
              value={content}
              onChange={changeContent}
              onSave={() => save()}
              onReferenceSelection={(range) => fileSelection && onReference(fileSelection, range)}
            />
          </Suspense>
        ) : null}
      </div>
      {file && !fileQuery.isError && (!isSVG || fileViewMode === "source") ? (
        <div className="flex h-8 shrink-0 items-center gap-2 border-t border-[var(--workspace-resize-border)] px-3 text-[10px] text-muted-foreground">
          <span>{dirty ? t("project.browserUnsaved") : t("project.browserSavedState")}</span>
          <span aria-hidden="true">·</span>
          <span>{formatBytes(new TextEncoder().encode(content).length)}</span>
          {languageFromPath(file.path) ? <><span aria-hidden="true">·</span><span>{languageFromPath(file.path)}</span></> : null}
        </div>
      ) : null}
      </>
      )}
    </div>
  );
}

function ProjectViewerStatus({ children, icon }: { children: ReactNode; icon?: ReactNode }) {
  return <div className="flex h-full min-h-64 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-muted-foreground">{icon}<span>{children}</span></div>;
}

function ProjectImagePreview({ active, alt, src, onShowSource }: { active: boolean; alt: string; src: string; onShowSource?: () => void }) {
  const { t } = useI18n();
  const [failed, setFailed] = useState(false);
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const [viewportSize, setViewportSize] = useState({ height: 0, width: 0 });
  const [imageSize, setImageSize] = useState({ height: 0, width: 0 });
  const [zoomMode, setZoomMode] = useState<"fit" | "custom">("fit");
  const [customScale, setCustomScale] = useState(1);

  useEffect(() => {
    if (!active) return;
    const viewport = viewportRef.current;
    if (!viewport) return;
    let resizeFrame = 0;
    const updateSize = () => {
      resizeFrame = 0;
      const height = viewport.clientHeight;
      const width = viewport.clientWidth;
      setViewportSize((current) =>
        current.height === height && current.width === width ? current : { height, width },
      );
    };
    const scheduleSizeUpdate = () => {
      if (resizeFrame) return;
      resizeFrame = window.requestAnimationFrame(updateSize);
    };
    scheduleSizeUpdate();
    const observer = new ResizeObserver(scheduleSizeUpdate);
    observer.observe(viewport);
    return () => {
      observer.disconnect();
      if (resizeFrame) window.cancelAnimationFrame(resizeFrame);
    };
  }, [active]);

  const fitScale = useMemo(() => {
    if (!viewportSize.width || !viewportSize.height || !imageSize.width || !imageSize.height) return 1;
    return Math.max(
      0.01,
      Math.min(
        1,
        (viewportSize.width - 48) / imageSize.width,
        (viewportSize.height - 48) / imageSize.height,
      ),
    );
  }, [imageSize.height, imageSize.width, viewportSize.height, viewportSize.width]);
  const layoutReady = Boolean(viewportSize.width && viewportSize.height && imageSize.width && imageSize.height);
  const scale = zoomMode === "fit" ? fitScale : customScale;
  const imageWidth = imageSize.width * scale;
  const imageHeight = imageSize.height * scale;

  const changeScale = (factor: number) => {
    setCustomScale(clampImageScale(scale * factor));
    setZoomMode("custom");
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--workspace-file-editor-background)]">
      <div className="flex h-8 shrink-0 items-center gap-2 px-2.5">
        <code className="min-w-0 flex-1 cursor-text select-text truncate font-mono text-xs">{alt}</code>
        {!failed ? (
          <div className="flex shrink-0 items-center gap-0.5 text-muted-foreground">
            {onShowSource ? (
              <Button
                aria-label={t("project.browserSource")}
                className={viewModeButtonClassName}
                size="icon-sm"
                title={t("project.browserSource")}
                type="button"
                variant="ghost"
                onClick={onShowSource}
              >
                <FileCode2 />
              </Button>
            ) : null}
            <Button
              aria-label={t("project.browserZoomOut")}
              disabled={scale <= 0.1}
              size="icon-sm"
              title={t("project.browserZoomOut")}
              type="button"
              variant="ghost"
              onClick={() => changeScale(1 / 1.2)}
            >
              <Minus />
            </Button>
            <span className="min-w-12 px-1.5 text-center text-xs tabular-nums">
              {layoutReady ? `${Math.round(scale * 100)}%` : null}
            </span>
            <Button
              aria-label={t("project.browserZoomIn")}
              disabled={scale >= 8}
              size="icon-sm"
              title={t("project.browserZoomIn")}
              type="button"
              variant="ghost"
              onClick={() => changeScale(1.2)}
            >
              <Plus />
            </Button>
            <Button
              aria-label={t(zoomMode === "fit" ? "project.browserZoomReset" : "project.browserZoomFit")}
              aria-pressed={zoomMode === "fit"}
              className={viewModeButtonClassName}
              size="icon-sm"
              title={t(zoomMode === "fit" ? "project.browserZoomReset" : "project.browserZoomFit")}
              type="button"
              variant="ghost"
              onClick={() => {
                if (zoomMode === "fit") {
                  setCustomScale(1);
                  setZoomMode("custom");
                  return;
                }
                setZoomMode("fit");
              }}
            >
              {zoomMode === "fit" ? <Maximize2 /> : <Minimize2 />}
            </Button>
          </div>
        ) : null}
      </div>
      <div className="relative min-h-0 flex-1 overflow-hidden">
        {failed ? (
          <ProjectViewerStatus>{t("project.browserUnsupportedFile")}</ProjectViewerStatus>
        ) : (
          <div
            ref={viewportRef}
            className="h-full min-h-0 overflow-auto"
            onWheel={(event) => {
              if (!event.ctrlKey && !event.metaKey) return;
              event.preventDefault();
              changeScale(event.deltaY < 0 ? 1.1 : 1 / 1.1);
            }}
          >
            <div
              className="flex min-h-full min-w-full items-center justify-center p-6"
              style={{
                height: layoutReady ? Math.max(viewportSize.height, imageHeight + 48) : undefined,
                width: layoutReady ? Math.max(viewportSize.width, imageWidth + 48) : undefined,
              }}
            >
              <img
                alt={alt}
                className="block shrink-0 object-contain"
                draggable={false}
                src={src}
                style={{
                  height: layoutReady ? imageHeight : 0,
                  width: layoutReady ? imageWidth : 0,
                }}
                onError={() => setFailed(true)}
                onLoad={(event) => {
                  setImageSize({
                    height: event.currentTarget.naturalHeight,
                    width: event.currentTarget.naturalWidth,
                  });
                }}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function clampImageScale(scale: number) {
  return Math.min(8, Math.max(0.1, scale));
}

function ProjectPDFPreview({ src, title }: { src: string; title: string }) {
  return <iframe className="h-full min-h-0 w-full border-0 bg-[var(--workspace-file-editor-background)]" src={src} title={title} />;
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
