import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, BookOpen, FileCode2, Folders, Save } from "lucide-react";
import { lazy, Suspense, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";

import { APIError, getProjectFile, projectResourceURL, saveProjectFile, type ProjectFile } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { watchElectronProjectFile } from "@/desktop/projectFileWatcher";
import { languageFromPath } from "@/lib/fileLanguage";

import { ProjectFileTabs } from "./ProjectFileTabs";
import { ProjectFileTypeIcon } from "./ProjectFileTypeIcon";
import type { ProjectEditorSelection } from "./ProjectEditor";
import { ProjectGitDiffViewer } from "./git/ProjectGitDiffViewer";
import { ProjectMarkdownPreview } from "./ProjectMarkdownPreview";
import { projectBrowserError } from "./projectErrors";
import { projectSelectionKey } from "./projectPaths";
import type { ProjectEditorReveal } from "./projectReveal";
import { isProjectGitDiffTab, type ProjectSelection, type ProjectTab } from "./types";

const ProjectEditor = lazy(() => import("./ProjectEditor").then((module) => ({ default: module.ProjectEditor })));

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
  absolutePath,
  dirtyKeys,
  discardRequest,
  reveal,
  selection,
  sessionID,
  tabs,
  token,
  onActivate,
  onDirtyChange,
  onOpenPreview,
  onPin,
  onReference,
  onRequestClose,
  onReveal,
}: {
  active: boolean;
  absolutePath?: string;
  dirtyKeys: ReadonlySet<string>;
  discardRequest?: { id: number; keys: string[]; sessionID: string };
  reveal?: ProjectEditorReveal;
  selection?: ProjectTab;
  sessionID: string;
  tabs: ProjectTab[];
  token: string;
  onActivate: (selection: ProjectTab) => void;
  onDirtyChange: (targetSessionID: string, selection: ProjectSelection, dirty: boolean) => void;
  onOpenPreview: (selection: ProjectSelection) => void;
  onPin: (selection: ProjectTab) => void;
  onReference: (selection: ProjectSelection, range: ProjectEditorSelection) => void;
  onRequestClose: (keys: string[]) => void;
  onReveal: (selection: ProjectSelection) => void;
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
  const [previewMode, setPreviewMode] = useState<Record<string, boolean>>({});
  const [imageRevision, setImageRevision] = useState(0);
  const gitDiffSelection = selection && isProjectGitDiffTab(selection) ? selection : undefined;
  const fileSelection = selection && !isProjectGitDiffTab(selection) ? selection : undefined;
  const selectionKey = fileSelection ? projectSelectionKey(fileSelection) : "";
  const draftKey = selectionKey ? `${sessionID}:${selectionKey}` : "";
  const isImage = Boolean(fileSelection && isProjectImagePath(fileSelection.path));
  const fileQuery = useQuery({
    enabled: active && Boolean(fileSelection) && !isImage,
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
  const showPreview = Boolean(isMarkdown && draftKey && previewMode[draftKey] !== false);
  const imageURL = useMemo(() => {
    if (!isImage || !fileSelection) return "";
    const url = projectResourceURL(token, sessionID, fileSelection.rootID, fileSelection.path);
    return `${url}${url.includes("?") ? "&" : "?"}v=${imageRevision}`;
  }, [fileSelection?.path, fileSelection?.rootID, imageRevision, isImage, sessionID, token]);

  useEffect(() => {
    if (!reveal || reveal.key !== selectionKey || !draftKey) {
      return;
    }
    setPreviewMode((current) => ({ ...current, [draftKey]: false }));
  }, [draftKey, reveal?.serial, selectionKey]);

  useEffect(() => {
    if (!active || !absolutePath || !fileSelection) {
      return;
    }
    const refetch = () => {
      if (isImage) setImageRevision((current) => current + 1);
      else void fileQuery.refetch();
      void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project", "git"] });
    };
    return watchElectronProjectFile(absolutePath, refetch, refetch);
  }, [absolutePath, active, draftKey, isImage]);

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

  return (
    <div className="flex h-full min-h-0 flex-col bg-card dark:bg-[#1c1c1c]">
      <ProjectFileTabs
        active={selection}
        dirtyKeys={dirtyKeys}
        tabs={tabs}
        onActivate={onActivate}
        onPin={onPin}
        onRequestClose={onRequestClose}
        onReveal={onReveal}
      />
      {gitDiffSelection ? (
        <ProjectGitDiffViewer active={active} selection={gitDiffSelection} sessionID={sessionID} token={token} />
      ) : (
      <>
      {fileSelection ? (
        <div className="flex h-9 shrink-0 items-center gap-2 bg-background px-3 dark:bg-[#171717]">
          <ProjectFileTypeIcon path={file?.path || fileSelection.path} />
          <code className="min-w-0 flex-1 cursor-text select-text truncate font-mono text-xs" title={file?.path || fileSelection.path}>{file?.path || fileSelection.path}</code>
          {!isImage ? (
            <div className="flex shrink-0 items-center gap-1">
              {isMarkdown ? (
                <>
                  <Button aria-label={t("project.browserPreview")} aria-pressed={showPreview} className="text-muted-foreground hover:bg-muted/60 hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground" size="icon-sm" type="button" variant="ghost" onClick={() => setPreviewMode((current) => ({ ...current, [draftKey]: true }))}>
                    <BookOpen />
                  </Button>
                  <Button aria-label={t("project.browserSource")} aria-pressed={!showPreview} className="text-muted-foreground hover:bg-muted/60 hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground" size="icon-sm" type="button" variant="ghost" onClick={() => setPreviewMode((current) => ({ ...current, [draftKey]: false }))}>
                    <FileCode2 />
                  </Button>
                </>
              ) : null}
              <Button aria-label={t("project.browserSave")} disabled={!dirty || saveMutation.isPending || externalConflict || fileQuery.isError} size="icon-sm" title={`${t("project.browserSave")} (⌘S)`} type="button" variant="ghost" onClick={() => save()}>
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
      <div className="min-h-0 flex-1 overflow-auto">
        {!fileSelection ? (
          <ProjectViewerStatus icon={<Folders className="size-8" />}>{t("project.browserSelectFile")}</ProjectViewerStatus>
        ) : isImage ? (
          <ProjectImagePreview key={imageURL} alt={fileSelection.path} src={imageURL} />
        ) : fileQuery.isError ? (
          <ProjectViewerStatus>{projectBrowserError(fileQuery.error, t)}</ProjectViewerStatus>
        ) : fileQuery.isLoading && !file ? (
          <ProjectViewerStatus icon={<Spinner className="size-6" />}>{t("common.loading")}</ProjectViewerStatus>
        ) : previewFile && showPreview ? (
          <ProjectMarkdownPreview
            file={previewFile}
            sessionID={sessionID}
            token={token}
            onOpenPreview={onOpenPreview}
          />
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
      {file && !fileQuery.isError ? (
        <div className="flex h-8 shrink-0 items-center gap-2 border-t px-3 text-[10px] text-muted-foreground">
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

function ProjectImagePreview({ alt, src }: { alt: string; src: string }) {
  const { t } = useI18n();
  const [failed, setFailed] = useState(false);
  if (failed) return <ProjectViewerStatus>{t("project.browserUnsupportedFile")}</ProjectViewerStatus>;
  return (
    <div className="flex h-full min-h-0 items-center justify-center overflow-auto p-6">
      <img alt={alt} className="max-h-full max-w-full object-contain" src={src} onError={() => setFailed(true)} />
    </div>
  );
}

function isProjectImagePath(path: string) {
  return /\.(?:avif|gif|jpe?g|png|svg|webp)$/i.test(path);
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
