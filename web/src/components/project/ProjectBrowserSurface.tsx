import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  copyProjectEntry,
  createProjectEntry,
  deleteProjectEntry,
  getProjectGitStatus,
  listProjectBrowserRoots,
  moveProjectEntry,
  renameProjectEntry,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { watchElectronProjectDirectories } from "@/desktop/projectFileWatcher";
import { useI18n } from "@/i18n";
import { revealDesktopPath } from "@/lib/desktopBridge";
import { layoutStorageKeys } from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { cn } from "@/lib/utils";
import { openFilePreview } from "@/state/filePreviewStore";
import { consumeProjectFileReveal, useProjectFileReveal } from "@/state/projectRevealStore";
import { addProjectReferenceToSessionDraft } from "@/state/sessionDraftStore";
import type { UIContextPart } from "@/state/uiContextStore";

import { ProjectDeleteDialog, ProjectMoveDialog, ProjectNameDialog, ProjectUnsavedCloseDialog } from "./ProjectDialogs";
import { ProjectFileViewer } from "./ProjectFileViewer";
import type { ProjectEditorSelection } from "./ProjectEditor";
import { ProjectGitSection } from "./git/ProjectGitSection";
import { projectGitFileKey } from "./git/gitStatus";
import type { ProjectGitRepositoryState } from "./git/types";
import { ProjectSidebar } from "./ProjectSidebar";
import { ProjectTree } from "./ProjectTree";
import { projectBrowserError } from "./projectErrors";
import { projectAbsolutePath, projectParentPath, projectPathContains, projectSelectionKey, projectTabKey } from "./projectPaths";
import { resolveProjectFileReveal, type ProjectEditorReveal } from "./projectReveal";
import { isProjectGitDiffTab, type ProjectEntryTarget, type ProjectSelection } from "./types";
import { useProjectWorkspace } from "./useProjectWorkspace";

type NameRequest =
  | { mode: "newFile" | "newFolder"; target: ProjectEntryTarget }
  | { mode: "rename"; target: ProjectEntryTarget };

type DiscardRequest = { id: number; keys: string[]; sessionID: string };
type ResourceClipboard = { mode: "copy" | "cut"; sessionID: string; target: ProjectEntryTarget };

export function ProjectBrowserSurface({
  active,
  sessionID,
  token,
  onOpenTerminal,
  onVisibleContextChange,
}: {
  active: boolean;
  sessionID: string;
  token: string;
  onOpenTerminal: (cwd: string) => void;
  onVisibleContextChange?: (context?: UIContextPart) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const workspace = useProjectWorkspace(sessionID);
  const fileReveal = useProjectFileReveal(sessionID);
  const currentSessionIDRef = useRef(sessionID);
  currentSessionIDRef.current = sessionID;
  const [nameRequest, setNameRequest] = useState<NameRequest>();
  const [deleteTarget, setDeleteTarget] = useState<ProjectEntryTarget>();
  const [pendingCloseKeys, setPendingCloseKeys] = useState<string[]>([]);
  const [discardRequest, setDiscardRequest] = useState<DiscardRequest>();
  const [dirtyBySession, setDirtyBySession] = useState<Record<string, string[]>>({});
  const [editorReveal, setEditorReveal] = useState<ProjectEditorReveal>();
  const [resourceClipboard, setResourceClipboard] = useState<ResourceClipboard>();
  const [dragMoveRequest, setDragMoveRequest] = useState<{ destination: ProjectEntryTarget; source: ProjectEntryTarget }>();
  const dirtyKeys = useMemo(() => new Set(dirtyBySession[sessionID] || []), [dirtyBySession, sessionID]);
  const rootsQuery = useQuery({
    enabled: (active || Boolean(fileReveal)) && Boolean(sessionID && token),
    queryKey: queryKeys.projectBrowserRoots(sessionID),
    queryFn: () => listProjectBrowserRoots(token, sessionID),
    staleTime: 10_000,
  });
  const roots = rootsQuery.data?.roots || [];
  const rootPaths = roots.map((root) => root.path);
  const gitQueries = useQueries({
    queries: roots.map((root) => ({
      enabled: active && Boolean(sessionID && token),
      queryKey: queryKeys.projectGitStatus(sessionID, root.id),
      queryFn: () => getProjectGitStatus(token, sessionID, root.id),
      refetchInterval: active ? 5_000 : false,
      refetchIntervalInBackground: false,
      retry: false,
      staleTime: 5_000,
    })),
  });
  const gitRepositories: ProjectGitRepositoryState[] = roots.map((root, index) => ({
    error: gitQueries[index]?.error,
    loading: gitQueries[index]?.isLoading || false,
    root,
    status: gitQueries[index]?.data,
  }));
  const gitStatuses = new Map(gitRepositories.flatMap((repository) => (
    repository.status?.available
      ? repository.status.files.map((file) => [projectGitFileKey(repository.root.id, file.path), file] as const)
      : []
  )));
  const selectedAbsolutePath = useMemo(() => {
    if (!workspace.selected || isProjectGitDiffTab(workspace.selected)) return undefined;
    const root = roots.find((candidate) => candidate.id === workspace.selected?.rootID);
    return root ? projectAbsolutePath(root.path, workspace.selected.path) : undefined;
  }, [roots, workspace.selected]);
  const visibleContext = useMemo<UIContextPart>(() => {
    const selected = workspace.selected;
    if (!selected) {
      return { type: "ui_context", surface: "project" };
    }
    const diff = isProjectGitDiffTab(selected);
    return {
      type: "ui_context",
      surface: "project",
      resource: diff ? "project_diff" : "project_file",
      id: projectSelectionKey(selected),
      name: projectFileName(selected.path),
      path: selected.path,
      rootID: selected.rootID,
      kind: diff ? (selected.staged ? "staged" : "unstaged") : undefined,
    };
  }, [workspace.selected]);

  useEffect(() => {
    onVisibleContextChange?.(active ? visibleContext : undefined);
  }, [active, onVisibleContextChange, visibleContext]);

  useEffect(() => workspace.ensureRootExpanded(roots), [roots.length, sessionID]);
  useEffect(() => workspace.removeUnavailableRoots(roots), [roots.map((root) => root.id).join("\n"), sessionID]);
  useEffect(() => {
    setNameRequest(undefined);
    setDeleteTarget(undefined);
    setPendingCloseKeys([]);
    setResourceClipboard((current) => current?.sessionID === sessionID ? current : undefined);
    setDragMoveRequest(undefined);
  }, [sessionID]);
  useEffect(() => {
    if (!active) return;
    const refreshGit = () => void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project", "git"] });
    window.addEventListener("focus", refreshGit);
    return () => window.removeEventListener("focus", refreshGit);
  }, [active, queryClient, sessionID]);
  useEffect(() => {
    if (!active || rootPaths.length === 0) return;
    return watchElectronProjectDirectories(rootPaths, () => {
      void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project"] });
    });
  }, [active, queryClient, sessionID, rootPaths.join("\n")]);
  useEffect(() => {
    if (!fileReveal || rootsQuery.isLoading || rootsQuery.isFetching) {
      return;
    }
    const selection = resolveProjectFileReveal(roots, fileReveal);
    if (selection) {
      workspace.openPreview(selection);
      setEditorReveal(fileReveal.line && fileReveal.line > 0 ? {
        column: fileReveal.column,
        key: projectSelectionKey(selection),
        line: fileReveal.line,
        serial: fileReveal.serial,
      } : undefined);
    } else if (fileReveal.fallback) {
      openFilePreview(fileReveal.fallback);
    } else {
      toast.warning(t("project.browserRevealUnavailable"));
    }
    consumeProjectFileReveal(sessionID, fileReveal.serial);
  }, [fileReveal?.serial, rootsQuery.isFetching, rootsQuery.isLoading, sessionID]);

  const invalidateProject = (targetSessionID: string) => queryClient.invalidateQueries({ queryKey: ["session", targetSessionID, "project"] });
  const createMutation = useMutation({
    mutationFn: ({ name, request, targetSessionID }: { name: string; request: NameRequest; targetSessionID: string }) => createProjectEntry(token, targetSessionID, {
      rootID: request.target.rootID,
      parentPath: request.target.path,
      name,
      type: request.mode === "newFolder" ? "dir" : "file",
    }),
    onSuccess: (entry, variables) => {
      if (currentSessionIDRef.current === variables.targetSessionID) {
        setNameRequest(undefined);
      }
      void invalidateProject(variables.targetSessionID);
      if (entry.type === "file") {
        workspace.openPinnedInSession(variables.targetSessionID, entry);
      }
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const renameMutation = useMutation({
    mutationFn: ({ name, target, targetSessionID }: { name: string; target: ProjectEntryTarget; targetSessionID: string }) => renameProjectEntry(token, targetSessionID, {
      rootID: target.rootID,
      path: target.path,
      name,
    }),
    onSuccess: (entry, variables) => {
      workspace.renameUnderInSession(variables.targetSessionID, variables.target, entry.path);
      if (currentSessionIDRef.current === variables.targetSessionID) {
        setNameRequest(undefined);
      }
      void invalidateProject(variables.targetSessionID);
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ target, targetSessionID }: { closingKeys: string[]; target: ProjectEntryTarget; targetSessionID: string }) => deleteProjectEntry(token, targetSessionID, target.rootID, target.path),
    onSuccess: (_result, variables) => {
      discardAndClose(variables.targetSessionID, variables.closingKeys);
      if (currentSessionIDRef.current === variables.targetSessionID) {
        setDeleteTarget(undefined);
      }
      void invalidateProject(variables.targetSessionID);
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const copyMutation = useMutation({
    mutationFn: ({ destination, source, targetSessionID, unique }: {
      destination: ProjectEntryTarget;
      open: boolean;
      source: ProjectEntryTarget;
      targetSessionID: string;
      unique: boolean;
    }) => copyProjectEntry(token, targetSessionID, {
      sourceRootID: source.rootID,
      sourcePath: source.path,
      targetRootID: destination.rootID,
      targetParentPath: destination.path,
      unique,
    }),
    onSuccess: (entry, variables) => {
      void invalidateProject(variables.targetSessionID);
      if (variables.open && entry.type === "file") workspace.openPinnedInSession(variables.targetSessionID, entry);
      toast.success(t("project.browserCopyDone"));
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const moveMutation = useMutation({
    mutationFn: ({ destination, source, targetSessionID }: {
      clearClipboard: boolean;
      destination: ProjectEntryTarget;
      fromDrag: boolean;
      source: ProjectEntryTarget;
      targetSessionID: string;
    }) => moveProjectEntry(token, targetSessionID, {
      sourceRootID: source.rootID,
      sourcePath: source.path,
      targetRootID: destination.rootID,
      targetParentPath: destination.path,
    }),
    onSuccess: (entry, variables) => {
      workspace.moveUnderInSession(variables.targetSessionID, variables.source, entry);
      if (variables.clearClipboard) setResourceClipboard(undefined);
      if (variables.fromDrag) setDragMoveRequest(undefined);
      void invalidateProject(variables.targetSessionID);
      toast.success(t("project.browserMoveDone"));
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });

  const setDirty = (targetSessionID: string, selection: ProjectSelection, dirty: boolean) => {
    const key = projectSelectionKey(selection);
    setDirtyBySession((current) => {
      const keys = new Set(current[targetSessionID] || []);
      if (dirty) keys.add(key); else keys.delete(key);
      return { ...current, [targetSessionID]: Array.from(keys) };
    });
  };

  function discardAndClose(targetSessionID: string, keys: string[]) {
    if (keys.length === 0) return;
    setDiscardRequest({ id: Date.now(), keys, sessionID: targetSessionID });
    setDirtyBySession((current) => ({
      ...current,
      [targetSessionID]: (current[targetSessionID] || []).filter((key) => !keys.includes(key)),
    }));
    workspace.closeKeysInSession(targetSessionID, keys);
  }

  const requestClose = (keys: string[]) => {
    if (keys.length === 0) return;
    const dirty = keys.filter((key) => dirtyKeys.has(key));
    if (dirty.length > 0) {
      setPendingCloseKeys(keys);
      return;
    }
    workspace.closeKeys(keys);
  };

  const requestRename = (target: ProjectEntryTarget) => {
    if (workspace.tabs.some((tab) => dirtyKeys.has(projectSelectionKey(tab)) && projectPathContains(target, tab))) {
      toast.warning(t("project.browserSaveBeforeRename"));
      return;
    }
    setNameRequest({ mode: "rename", target });
  };

  const hasDirtyUnder = (target: ProjectEntryTarget) => workspace.tabs.some(
    (tab) => dirtyKeys.has(projectSelectionKey(tab)) && projectPathContains(target, tab),
  );

  const copyEntry = (target: ProjectEntryTarget) => {
    setResourceClipboard({ mode: "copy", sessionID, target });
    toast.success(t("project.browserCopiedEntry"));
  };

  const cutEntry = (target: ProjectEntryTarget) => {
    if (hasDirtyUnder(target)) {
      toast.warning(t("project.browserSaveBeforeMove"));
      return;
    }
    setResourceClipboard({ mode: "cut", sessionID, target });
    toast.success(t("project.browserCutEntryReady"));
  };

  const pasteEntry = (destination: ProjectEntryTarget) => {
    if (!resourceClipboard || resourceClipboard.sessionID !== sessionID || destination.type !== "dir") return;
    const variables = { destination, source: resourceClipboard.target, targetSessionID: sessionID };
    if (resourceClipboard.mode === "cut") {
      if (hasDirtyUnder(resourceClipboard.target)) {
        toast.warning(t("project.browserSaveBeforeMove"));
        return;
      }
      moveMutation.mutate({ ...variables, clearClipboard: true, fromDrag: false });
    } else {
      copyMutation.mutate({ ...variables, open: false, unique: false });
    }
  };

  const duplicateEntry = (target: ProjectEntryTarget) => {
    const destination: ProjectEntryTarget = {
      name: projectParentPath(target.path),
      path: projectParentPath(target.path),
      rootID: target.rootID,
      type: "dir",
    };
    copyMutation.mutate({ destination, open: true, source: target, targetSessionID: sessionID, unique: true });
  };

  const moveEntry = (source: ProjectEntryTarget, destination: ProjectEntryTarget) => {
    if (hasDirtyUnder(source)) {
      toast.warning(t("project.browserSaveBeforeMove"));
      return;
    }
    setDragMoveRequest({ destination, source });
  };

  const revealEntry = (target: ProjectEntryTarget) => {
    const root = roots.find((candidate) => candidate.id === target.rootID);
    if (!root) return;
    void revealDesktopPath(projectAbsolutePath(root.path, target.path)).then((revealed) => {
      if (!revealed) toast.error(t("project.revealFailed"));
    });
  };

  const openEntryTerminal = (target: ProjectEntryTarget) => {
    const root = roots.find((candidate) => candidate.id === target.rootID);
    if (!root) return;
    const directory = target.type === "dir" ? target.path : projectParentPath(target.path);
    onOpenTerminal(projectAbsolutePath(root.path, directory));
  };

  const copyPath = (target: ProjectEntryTarget) => {
    void navigator.clipboard.writeText(target.path).then(
      () => toast.success(t("project.browserPathCopied")),
      () => toast.error(t("project.browserPathCopyFailed")),
    );
  };

  const copyAbsolutePath = (target: ProjectEntryTarget) => {
    const root = roots.find((candidate) => candidate.id === target.rootID);
    if (!root) {
      toast.error(t("project.browserPathCopyFailed"));
      return;
    }
    void navigator.clipboard.writeText(projectAbsolutePath(root.path, target.path)).then(
      () => toast.success(t("project.browserAbsolutePathCopied")),
      () => toast.error(t("project.browserPathCopyFailed")),
    );
  };

  const referenceEntry = (target: ProjectEntryTarget) => {
    const root = roots.find((candidate) => candidate.id === target.rootID);
    if (!root) {
      toast.error(t("project.browserPathCopyFailed"));
      return;
    }
    addProjectReferenceToSessionDraft(sessionID, {
      name: target.name,
      path: target.path,
      sourcePath: projectAbsolutePath(root.path, target.path),
      rootID: target.rootID,
      kind: target.type,
    });
  };

  const referenceSelection = (selection: ProjectSelection, range: ProjectEditorSelection) => {
    const root = roots.find((candidate) => candidate.id === selection.rootID);
    if (!root) {
      toast.error(t("project.browserPathCopyFailed"));
      return;
    }
    addProjectReferenceToSessionDraft(sessionID, {
      name: projectFileName(selection.path),
      path: selection.path,
      sourcePath: projectAbsolutePath(root.path, selection.path),
      rootID: selection.rootID,
      kind: "file",
      ...range,
    });
  };

  const namePending = (createMutation.isPending && createMutation.variables?.targetSessionID === sessionID)
    || (renameMutation.isPending && renameMutation.variables?.targetSessionID === sessionID);
  const deletePending = deleteMutation.isPending && deleteMutation.variables?.targetSessionID === sessionID;
  return (
    <div aria-hidden={!active} className={cn("absolute inset-0 z-20 min-h-0 overflow-hidden bg-[var(--workspace-background)] text-card-foreground dark:bg-[#1c1c1c]", !active && "pointer-events-none invisible opacity-0")}>
      <ResizablePanelGroup
        className="h-full min-h-0 overflow-hidden border-t bg-card dark:bg-[#1c1c1c]"
        defaultLayout={readPanelLayout(layoutStorageKeys.projectBrowserRatio, { tree: 28, viewer: 72 }, { minPercent: 15, maxPercent: 85 })}
        id="project-browser-layout"
        orientation="horizontal"
        onLayoutChanged={(layout) => savePanelLayout(layoutStorageKeys.projectBrowserRatio, layout)}
      >
        <ResizablePanel id="tree" className="min-w-0" minSize={180} maxSize="45%">
          <ProjectSidebar
            files={(
              <ProjectTree
                active={active}
                canPaste={resourceClipboard?.sessionID === sessionID}
                error={rootsQuery.error}
                expandedKeys={workspace.expandedKeys}
                gitStatuses={gitStatuses}
                loading={rootsQuery.isLoading}
                roots={roots}
                selected={workspace.selected}
                sessionID={sessionID}
                token={token}
                onCopyAbsolutePath={copyAbsolutePath}
                onCopyEntry={copyEntry}
                onCopyPath={copyPath}
                onCreate={(target, type) => setNameRequest({ mode: type === "dir" ? "newFolder" : "newFile", target })}
                onCutEntry={cutEntry}
                onDelete={setDeleteTarget}
                onDuplicate={duplicateEntry}
                onMove={moveEntry}
                onOpenPinned={workspace.openPinned}
                onOpenPreview={workspace.openPreview}
                onOpenTerminal={openEntryTerminal}
                onPaste={pasteEntry}
                onReference={referenceEntry}
                onRename={requestRename}
                onRevealInFinder={revealEntry}
                onToggle={workspace.toggleDirectory}
              />
            )}
            git={(
              <ProjectGitSection
                repositories={gitRepositories}
                sessionID={sessionID}
                token={token}
                onOpenDiff={workspace.openGitDiff}
              />
            )}
          />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel id="viewer" className="min-w-0" minSize={280}>
          <ProjectFileViewer
            active={active}
            absolutePath={selectedAbsolutePath}
            dirtyKeys={dirtyKeys}
            discardRequest={discardRequest}
            reveal={editorReveal}
            selection={workspace.selected}
            sessionID={sessionID}
            tabs={workspace.tabs}
            token={token}
            onActivate={workspace.activate}
            onDirtyChange={setDirty}
            onOpenPreview={workspace.openPreview}
            onPin={workspace.pinTab}
            onReference={referenceSelection}
            onRequestClose={requestClose}
            onReveal={workspace.reveal}
          />
        </ResizablePanel>
      </ResizablePanelGroup>

      <ProjectNameDialog
        initialName={nameRequest?.mode === "rename" ? nameRequest.target.name : ""}
        mode={nameRequest?.mode || "newFile"}
        open={Boolean(nameRequest)}
        pending={namePending}
        onOpenChange={(open) => !open && setNameRequest(undefined)}
        onSubmit={(name) => {
          if (!nameRequest) return;
          if (nameRequest.mode === "rename") {
            renameMutation.mutate({ name, target: nameRequest.target, targetSessionID: sessionID });
          } else {
            createMutation.mutate({ name, request: nameRequest, targetSessionID: sessionID });
          }
        }}
      />
      <ProjectDeleteDialog
        pending={deletePending}
        target={deleteTarget}
        onCancel={() => setDeleteTarget(undefined)}
        onConfirm={() => deleteTarget && deleteMutation.mutate({
          closingKeys: workspace.tabs.filter((tab) => projectPathContains(deleteTarget, tab)).map(projectTabKey),
          target: deleteTarget,
          targetSessionID: sessionID,
        })}
      />
      <ProjectMoveDialog
        destination={dragMoveRequest?.destination}
        pending={moveMutation.isPending && moveMutation.variables?.fromDrag === true}
        source={dragMoveRequest?.source}
        onCancel={() => setDragMoveRequest(undefined)}
        onConfirm={() => dragMoveRequest && moveMutation.mutate({
          clearClipboard: false,
          destination: dragMoveRequest.destination,
          fromDrag: true,
          source: dragMoveRequest.source,
          targetSessionID: sessionID,
        })}
      />
      <ProjectUnsavedCloseDialog
        count={pendingCloseKeys.filter((key) => dirtyKeys.has(key)).length}
        open={pendingCloseKeys.length > 0}
        onCancel={() => setPendingCloseKeys([])}
        onDiscard={() => {
          discardAndClose(sessionID, pendingCloseKeys);
          setPendingCloseKeys([]);
        }}
      />
    </div>
  );
}

function projectFileName(path: string) {
  return path.split(/[\\/]/).filter(Boolean).at(-1) || path;
}
