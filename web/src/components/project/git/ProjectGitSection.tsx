import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ChevronRight,
  GitBranch,
  GitCommitHorizontal,
  Minus,
  Plus,
  RotateCcw,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import {
  commitProjectGit,
  discardProjectGit,
  initializeProjectGit,
  stageProjectGit,
  unstageProjectGit,
  type ProjectGitStatus,
  type ProjectGitStatusFile,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppContextMenuContent, AppContextMenuItem, AppContextMenuSeparator } from "@/components/AppMenu";
import { Spinner } from "@/components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { ContextMenu, ContextMenuTrigger } from "@/components/ui/context-menu";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { projectFileName } from "../projectPaths";
import type { ProjectGitDiffSelection } from "../types";
import { projectBrowserError } from "../projectErrors";
import { isProjectGitStaged, isProjectGitWorking, projectGitStatusLabel, projectGitStatusTone } from "./gitStatus";
import type { ProjectGitRepositoryState } from "./types";

type GitChangeGroup = "conflicted" | "staged" | "working";
type GitPathMutation = { paths: string[]; rootID: string };
type GitDiscardRequest = { files: ProjectGitStatusFile[]; rootID: string };

export function ProjectGitSection({ repositories, sessionID, token, onOpenDiff }: {
  repositories: ProjectGitRepositoryState[];
  sessionID: string;
  token: string;
  onOpenDiff: (selection: ProjectGitDiffSelection, pinned: boolean) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [commitMessages, setCommitMessages] = useState<Record<string, string>>({});
  const [discardRequest, setDiscardRequest] = useState<GitDiscardRequest>();
  const loading = repositories.length > 0 && repositories.every((repository) => repository.loading);

  const applyStatus = (status: ProjectGitStatus, worktreeChanged = false) => {
    queryClient.setQueryData(queryKeys.projectGitStatus(sessionID, status.rootID), status);
    void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project", "git", "diff", status.rootID] });
    if (worktreeChanged) {
      void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project", "file"] });
      void queryClient.invalidateQueries({ queryKey: ["session", sessionID, "project", "tree"] });
    }
  };

  const initializeMutation = useMutation({
    mutationFn: (rootID: string) => initializeProjectGit(token, sessionID, rootID),
    onSuccess: (status) => {
      applyStatus(status);
      toast.success(t("project.gitInitialized"));
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const stageMutation = useMutation({
    mutationFn: (request: GitPathMutation) => runGitPathMutation(request.paths, (paths) => stageProjectGit(token, sessionID, request.rootID, paths)),
    onSuccess: (status) => applyStatus(status),
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const unstageMutation = useMutation({
    mutationFn: (request: GitPathMutation) => runGitPathMutation(request.paths, (paths) => unstageProjectGit(token, sessionID, request.rootID, paths)),
    onSuccess: (status) => applyStatus(status),
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const discardMutation = useMutation({
    mutationFn: (request: GitPathMutation) => runGitPathMutation(request.paths, (paths) => discardProjectGit(token, sessionID, request.rootID, paths)),
    onSuccess: (status) => {
      applyStatus(status, true);
      setDiscardRequest(undefined);
      toast.success(t("project.gitDiscarded"));
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });
  const commitMutation = useMutation({
    mutationFn: ({ message, rootID }: { message: string; rootID: string }) => commitProjectGit(token, sessionID, rootID, message),
    onSuccess: (status) => {
      applyStatus(status);
      setCommitMessages((current) => ({ ...current, [status.rootID]: "" }));
      toast.success(t("project.gitCommitted"));
    },
    onError: (error) => toast.error(projectBrowserError(error, t)),
  });

  if (loading) {
    return <ProjectGitMessage><Spinner />{t("common.loading")}</ProjectGitMessage>;
  }
  if (repositories.length === 0) {
    return <ProjectGitMessage>{t("project.gitUnavailable")}</ProjectGitMessage>;
  }

  return (
    <>
      <div className="py-1">
        {repositories.map((repository) => {
          const status = repository.status;
          const repositoryPending = [
            initializeMutation,
            stageMutation,
            unstageMutation,
            discardMutation,
            commitMutation,
          ].some((mutation) => mutation.isPending && mutation.variables && (
            typeof mutation.variables === "string"
              ? mutation.variables === repository.root.id
              : mutation.variables.rootID === repository.root.id
          ));
          if (repository.error) {
            return (
              <div key={repository.root.id}>
                <ProjectGitRepositoryHeader repository={repository} />
                <ProjectGitMessage>{projectBrowserError(repository.error, t)}</ProjectGitMessage>
              </div>
            );
          }
          if (!status?.available) {
            return (
              <div key={repository.root.id} className="pb-2">
                <ProjectGitRepositoryHeader repository={repository} />
                <div className="space-y-2 px-3 py-3">
                  <p className="text-[11px] text-muted-foreground">{t("project.gitUnavailable")}</p>
                  <Button
                    className="h-7 w-full text-xs"
                    disabled={repositoryPending}
                    size="sm"
                    type="button"
                    variant="secondary"
                    onClick={() => initializeMutation.mutate(repository.root.id)}
                  >
                    {initializeMutation.isPending && initializeMutation.variables === repository.root.id ? <Spinner /> : <GitBranch />}
                    {t("project.gitInitialize")}
                  </Button>
                </div>
              </div>
            );
          }
          const conflicted = status.files.filter((file) => file.kind === "conflicted");
          const staged = status.files.filter(isProjectGitStaged);
          const working = status.files.filter((file) => file.kind !== "conflicted" && isProjectGitWorking(file));
          const message = commitMessages[repository.root.id] || "";
          return (
            <div key={repository.root.id} className="pb-1">
              <ProjectGitRepositoryHeader repository={repository} />
              <form
                className="space-y-1.5 border-y px-2 py-2"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (message.trim() && status.stagedCount > 0 && status.conflictedCount === 0 && !repositoryPending) {
                    commitMutation.mutate({ message: message.trim(), rootID: repository.root.id });
                  }
                }}
              >
                <Input
                  aria-label={t("project.gitCommitMessage")}
                  className="h-7 text-xs"
                  disabled={commitMutation.isPending && commitMutation.variables?.rootID === repository.root.id}
                  maxLength={16 * 1024}
                  placeholder={t("project.gitCommitPlaceholder")}
                  value={message}
                  onChange={(event) => setCommitMessages((current) => ({ ...current, [repository.root.id]: event.target.value }))}
                />
                <Button
                  className="h-7 w-full text-xs"
                  disabled={!message.trim() || status.stagedCount === 0 || status.conflictedCount > 0 || repositoryPending}
                  size="sm"
                  type="submit"
                >
                  {commitMutation.isPending && commitMutation.variables?.rootID === repository.root.id ? <Spinner /> : <GitCommitHorizontal />}
                  {t("project.gitCommit")}
                </Button>
              </form>
              {status.clean ? (
                <div className="px-7 py-3 text-[11px] text-muted-foreground">{t("project.gitClean")}</div>
              ) : (
                <>
                  <ProjectGitChangeGroup
                    files={conflicted}
                    group="conflicted"
                    label={t("project.gitConflicts")}
                    pending={repositoryPending}
                    rootID={repository.root.id}
                    onDiscard={setDiscardRequest}
                    onOpenDiff={onOpenDiff}
                    onStage={(paths) => stageMutation.mutate({ paths, rootID: repository.root.id })}
                    onUnstage={(paths) => unstageMutation.mutate({ paths, rootID: repository.root.id })}
                  />
                  <ProjectGitChangeGroup
                    files={staged}
                    group="staged"
                    label={t("project.gitStagedChanges")}
                    pending={repositoryPending}
                    rootID={repository.root.id}
                    onDiscard={setDiscardRequest}
                    onOpenDiff={onOpenDiff}
                    onStage={(paths) => stageMutation.mutate({ paths, rootID: repository.root.id })}
                    onUnstage={(paths) => unstageMutation.mutate({ paths, rootID: repository.root.id })}
                  />
                  <ProjectGitChangeGroup
                    files={working}
                    group="working"
                    label={t("project.gitChanges")}
                    pending={repositoryPending}
                    rootID={repository.root.id}
                    onDiscard={setDiscardRequest}
                    onOpenDiff={onOpenDiff}
                    onStage={(paths) => stageMutation.mutate({ paths, rootID: repository.root.id })}
                    onUnstage={(paths) => unstageMutation.mutate({ paths, rootID: repository.root.id })}
                  />
                </>
              )}
            </div>
          );
        })}
      </div>
      <GitDiscardDialog
        pending={discardMutation.isPending}
        request={discardRequest}
        onCancel={() => setDiscardRequest(undefined)}
        onConfirm={() => discardRequest && discardMutation.mutate({
          paths: discardRequest.files.map((file) => file.path),
          rootID: discardRequest.rootID,
        })}
      />
    </>
  );
}

function ProjectGitRepositoryHeader({ repository }: { repository: ProjectGitRepositoryState }) {
  const { t } = useI18n();
  const status = repository.status;
  return (
    <div className="flex h-8 min-w-0 items-center gap-1.5 px-2 text-xs" title={repository.root.path}>
      <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate font-medium">{repository.root.name}</span>
      {status?.available ? (
        <>
          <span className="max-w-28 truncate text-[11px] text-muted-foreground">
            {status.detached ? status.head || t("project.gitDetached") : status.branch || t("project.gitDetached")}
          </span>
          {status.ahead ? <span className="text-[10px] text-muted-foreground">↑{status.ahead}</span> : null}
          {status.behind ? <span className="text-[10px] text-muted-foreground">↓{status.behind}</span> : null}
        </>
      ) : null}
    </div>
  );
}

function ProjectGitChangeGroup({ files, group, label, pending, rootID, onDiscard, onOpenDiff, onStage, onUnstage }: {
  files: ProjectGitStatusFile[];
  group: GitChangeGroup;
  label: string;
  pending: boolean;
  rootID: string;
  onDiscard: (request: GitDiscardRequest) => void;
  onOpenDiff: (selection: ProjectGitDiffSelection, pinned: boolean) => void;
  onStage: (paths: string[]) => void;
  onUnstage: (paths: string[]) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(true);
  if (files.length === 0) return null;
  const staged = group === "staged";
  const paths = files.map((file) => file.path);
  return (
    <div>
      <div className="group/git-heading flex h-7 items-center hover:bg-accent">
        <button className="flex h-full min-w-0 flex-1 items-center gap-1 px-2 text-left text-[11px] font-medium" type="button" onClick={() => setOpen((value) => !value)}>
          <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
          <span className="min-w-0 flex-1 truncate uppercase tracking-wide">{label}</span>
          <span className="text-muted-foreground">{files.length}</span>
        </button>
        <Button
          aria-label={staged ? t("project.gitUnstageAll") : t("project.gitStageAll")}
          className="mr-1 size-6 opacity-0 group-hover/git-heading:opacity-100 focus-visible:opacity-100"
          disabled={pending}
          size="icon-xs"
          title={staged ? t("project.gitUnstageAll") : t("project.gitStageAll")}
          type="button"
          variant="ghost"
          onClick={() => staged ? onUnstage(paths) : onStage(paths)}
        >
          {staged ? <Minus /> : <Plus />}
        </Button>
      </div>
      {open ? files.map((file) => {
        const selection: ProjectGitDiffSelection = {
          rootID,
          path: file.path,
          originalPath: file.originalPath,
          staged,
          view: "git-diff",
        };
        return (
          <ContextMenu key={`${group}:${file.path}`}>
            <ContextMenuTrigger asChild>
              <div className="group/git-file flex h-7 min-w-0 items-center hover:bg-accent hover:text-accent-foreground">
                <button
                  className="flex h-full min-w-0 flex-1 select-none items-center gap-1.5 pl-7 text-left text-xs"
                  title={file.path}
                  type="button"
                  onClick={() => onOpenDiff(selection, false)}
                  onDoubleClick={() => onOpenDiff(selection, true)}
                >
                  <GitCommitHorizontal className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{projectFileName(file.path)}</span>
                  <span className="min-w-0 max-w-20 truncate text-[10px] text-muted-foreground">{file.path.includes("/") ? file.path.slice(0, file.path.lastIndexOf("/")) : ""}</span>
                </button>
                <div className="hidden shrink-0 items-center group-hover/git-file:flex group-focus-within/git-file:flex">
                  <Button
                    aria-label={staged ? t("project.gitUnstage") : t("project.gitStage")}
                    className="size-6"
                    disabled={pending}
                    size="icon-xs"
                    title={staged ? t("project.gitUnstage") : t("project.gitStage")}
                    type="button"
                    variant="ghost"
                    onClick={() => staged ? onUnstage([file.path]) : onStage([file.path])}
                  >
                    {staged ? <Minus /> : <Plus />}
                  </Button>
                  {group === "working" ? (
                    <Button
                      aria-label={t("project.gitDiscard")}
                      className="size-6"
                      disabled={pending}
                      size="icon-xs"
                      title={t("project.gitDiscard")}
                      type="button"
                      variant="ghost"
                      onClick={() => onDiscard({ files: [file], rootID })}
                    >
                      <RotateCcw />
                    </Button>
                  ) : null}
                </div>
                <span className={cn("mr-2 w-3 shrink-0 text-center font-mono text-[11px] font-semibold", projectGitStatusTone(file))}>
                  {projectGitStatusLabel(file, staged)}
                </span>
              </div>
            </ContextMenuTrigger>
            <AppContextMenuContent>
              <AppContextMenuItem disabled={pending} onSelect={() => staged ? onUnstage([file.path]) : onStage([file.path])}>
                {staged ? <Minus /> : <Plus />}
                {staged ? t("project.gitUnstage") : t("project.gitStage")}
              </AppContextMenuItem>
              {group === "working" ? (
                <>
                  <AppContextMenuSeparator />
                  <AppContextMenuItem disabled={pending} variant="destructive" onSelect={() => onDiscard({ files: [file], rootID })}>
                    <RotateCcw />
                    {t("project.gitDiscard")}
                  </AppContextMenuItem>
                </>
              ) : null}
            </AppContextMenuContent>
          </ContextMenu>
        );
      }) : null}
      {open && group === "working" && files.length > 1 ? (
        <button
          className="flex h-7 w-full items-center gap-1.5 pl-7 pr-2 text-left text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
          disabled={pending}
          type="button"
          onClick={() => onDiscard({ files, rootID })}
        >
          <RotateCcw className="size-3.5" />
          {t("project.gitDiscardAll")}
        </button>
      ) : null}
    </div>
  );
}

function GitDiscardDialog({ pending, request, onCancel, onConfirm }: {
  pending: boolean;
  request?: GitDiscardRequest;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useI18n();
  const hasUntracked = request?.files.some((file) => file.kind === "untracked");
  return (
    <AlertDialog open={Boolean(request)} onOpenChange={(open) => !open && !pending && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("project.gitDiscardTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(hasUntracked ? "project.gitDiscardUntrackedDescription" : "project.gitDiscardDescription")
              .replace("{count}", String(request?.files.length || 0))}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="max-h-32 overflow-auto rounded-md bg-muted/50 px-2 py-1.5 font-mono text-[11px]">
          {request?.files.map((file) => <div key={file.path} className="truncate" title={file.path}>{file.path}</div>)}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction disabled={pending} variant="destructive" onClick={onConfirm}>
            {pending ? <Spinner /> : null}{t("project.gitDiscardConfirm")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function ProjectGitMessage({ children }: { children: React.ReactNode }) {
  return <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">{children}</div>;
}

async function runGitPathMutation(
  paths: string[],
  operation: (batch: string[]) => Promise<ProjectGitStatus>,
): Promise<ProjectGitStatus> {
  let status: ProjectGitStatus | undefined;
  for (let offset = 0; offset < paths.length; offset += 512) {
    status = await operation(paths.slice(offset, offset + 512));
  }
  if (!status) throw new Error("Git operation requires at least one path");
  return status;
}
