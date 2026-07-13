import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  createProjectEntry,
  deleteProjectEntry,
  getProjectGitStatus,
  listProjectBrowserRoots,
  renameProjectEntry,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useI18n } from "@/i18n";
import { layoutStorageKeys } from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { cn } from "@/lib/utils";

import { ProjectDeleteDialog, ProjectNameDialog, ProjectUnsavedCloseDialog } from "./ProjectDialogs";
import { ProjectFileViewer } from "./ProjectFileViewer";
import { ProjectGitSection } from "./git/ProjectGitSection";
import { projectGitFileKey } from "./git/gitStatus";
import type { ProjectGitRepositoryState } from "./git/types";
import { ProjectSidebar } from "./ProjectSidebar";
import { ProjectTree } from "./ProjectTree";
import { projectBrowserError } from "./projectErrors";
import { projectAbsolutePath, projectParentPath, projectPathContains, projectSelectionKey, projectTabKey } from "./projectPaths";
import { isProjectGitDiffTab, type ProjectEntryTarget, type ProjectSelection } from "./types";
import { useProjectWorkspace } from "./useProjectWorkspace";

type NameRequest =
  | { mode: "newFile" | "newFolder"; target: ProjectEntryTarget }
  | { mode: "rename"; target: ProjectEntryTarget };

type DiscardRequest = { id: number; keys: string[]; sessionID: string };

export function ProjectBrowserSurface({ active, sessionID, token }: { active: boolean; sessionID: string; token: string }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const workspace = useProjectWorkspace(sessionID);
  const currentSessionIDRef = useRef(sessionID);
  currentSessionIDRef.current = sessionID;
  const [nameRequest, setNameRequest] = useState<NameRequest>();
  const [deleteTarget, setDeleteTarget] = useState<ProjectEntryTarget>();
  const [pendingCloseKeys, setPendingCloseKeys] = useState<string[]>([]);
  const [discardRequest, setDiscardRequest] = useState<DiscardRequest>();
  const [dirtyBySession, setDirtyBySession] = useState<Record<string, string[]>>({});
  const dirtyKeys = useMemo(() => new Set(dirtyBySession[sessionID] || []), [dirtyBySession, sessionID]);
  const rootsQuery = useQuery({
    enabled: active && Boolean(sessionID && token),
    queryKey: queryKeys.projectBrowserRoots(sessionID),
    queryFn: () => listProjectBrowserRoots(token, sessionID),
    staleTime: 10_000,
  });
  const roots = rootsQuery.data?.roots || [];
  const gitQueries = useQueries({
    queries: roots.map((root) => ({
      enabled: active && Boolean(sessionID && token),
      queryKey: queryKeys.projectGitStatus(sessionID, root.id),
      queryFn: () => getProjectGitStatus(token, sessionID, root.id),
      retry: false,
      staleTime: 5_000,
    })),
  });
  const gitRepositories: ProjectGitRepositoryState[] = roots.map((root, index) => ({
    error: gitQueries[index]?.error,
    fetching: gitQueries[index]?.isFetching || false,
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

  useEffect(() => workspace.ensureRootExpanded(roots), [roots.length, sessionID]);
  useEffect(() => workspace.removeUnavailableRoots(roots), [roots.map((root) => root.id).join("\n"), sessionID]);
  useEffect(() => {
    setNameRequest(undefined);
    setDeleteTarget(undefined);
    setPendingCloseKeys([]);
  }, [sessionID]);
  useEffect(() => {
    if (!active) return;
    const refreshGit = () => void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project", "git"] });
    window.addEventListener("focus", refreshGit);
    return () => window.removeEventListener("focus", refreshGit);
  }, [active, queryClient, sessionID]);

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

  const refreshEntry = (target: ProjectEntryTarget) => {
    const path = target.type === "dir" ? target.path : projectParentPath(target.path);
    void queryClient.invalidateQueries({ queryKey: queryKeys.projectTree(sessionID, target.rootID, path) });
    if (target.type === "file") {
      void queryClient.invalidateQueries({ queryKey: queryKeys.projectFile(sessionID, target.rootID, target.path) });
    }
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

  const namePending = (createMutation.isPending && createMutation.variables?.targetSessionID === sessionID)
    || (renameMutation.isPending && renameMutation.variables?.targetSessionID === sessionID);
  const deletePending = deleteMutation.isPending && deleteMutation.variables?.targetSessionID === sessionID;
  return (
    <div aria-hidden={!active} className={cn("absolute inset-0 z-20 min-h-0 overflow-hidden bg-[var(--canvas-background)] text-card-foreground", !active && "pointer-events-none invisible opacity-0")}>
      <ResizablePanelGroup
        className="h-full min-h-0 overflow-hidden border-t bg-card"
        defaultLayout={readPanelLayout(layoutStorageKeys.projectBrowserRatio, { tree: 28, viewer: 72 }, { minPercent: 15, maxPercent: 85 })}
        id="project-browser-layout"
        orientation="horizontal"
        onLayoutChanged={(layout) => savePanelLayout(layoutStorageKeys.projectBrowserRatio, layout)}
      >
        <ResizablePanel id="tree" className="min-w-0" minSize={180} maxSize="45%">
          <ProjectSidebar
            refreshing={rootsQuery.isFetching || gitQueries.some((query) => query.isFetching)}
            onRefresh={() => void invalidateProject(sessionID)}
            files={(
              <ProjectTree
                active={active}
                error={rootsQuery.error}
                expandedKeys={workspace.expandedKeys}
                gitStatuses={gitStatuses}
                loading={rootsQuery.isLoading}
                roots={roots}
                selected={workspace.selected}
                sessionID={sessionID}
                token={token}
                onCopyAbsolutePath={copyAbsolutePath}
                onCopyPath={copyPath}
                onCreate={(target, type) => setNameRequest({ mode: type === "dir" ? "newFolder" : "newFile", target })}
                onDelete={setDeleteTarget}
                onOpenPinned={workspace.openPinned}
                onOpenPreview={workspace.openPreview}
                onRefresh={refreshEntry}
                onRename={requestRename}
                onToggle={workspace.toggleDirectory}
              />
            )}
            git={<ProjectGitSection repositories={gitRepositories} onOpenDiff={workspace.openGitDiff} />}
          />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel id="viewer" className="min-w-0" minSize={280}>
          <ProjectFileViewer
            active={active}
            absolutePath={selectedAbsolutePath}
            dirtyKeys={dirtyKeys}
            discardRequest={discardRequest}
            selection={workspace.selected}
            sessionID={sessionID}
            tabs={workspace.tabs}
            token={token}
            onActivate={workspace.activate}
            onDirtyChange={setDirty}
            onOpenPreview={workspace.openPreview}
            onPin={workspace.pinTab}
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
