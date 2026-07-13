import { useQuery } from "@tanstack/react-query";
import { ChevronRight, File, FileCode2, Folder, FolderOpen } from "lucide-react";
import type { ReactNode } from "react";

import {
  listProjectTree,
  type ProjectBrowserRoot,
  type ProjectGitStatusFile,
  type ProjectTreeEntry,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { ProjectEntryContextMenu } from "./ProjectContextMenu";
import { projectGitFileKey, projectGitStatusLabel, projectGitStatusTone } from "./git/gitStatus";
import { projectBrowserError } from "./projectErrors";
import type { ProjectEntryTarget, ProjectSelection } from "./types";

type TreeActions = {
  onCopyAbsolutePath: (target: ProjectEntryTarget) => void;
  onCopyPath: (target: ProjectEntryTarget) => void;
  onCreate: (target: ProjectEntryTarget, type: "dir" | "file") => void;
  onDelete: (target: ProjectEntryTarget) => void;
  onRefresh: (target: ProjectEntryTarget) => void;
  onRename: (target: ProjectEntryTarget) => void;
};

export function ProjectTree({
  active,
  expandedKeys,
  loading,
  gitStatuses,
  roots,
  selected,
  sessionID,
  token,
  error,
  onOpenPinned,
  onOpenPreview,
  onToggle,
  ...actions
}: TreeActions & {
  active: boolean;
  error?: unknown;
  expandedKeys: string[];
  gitStatuses?: ReadonlyMap<string, ProjectGitStatusFile>;
  loading: boolean;
  roots: ProjectBrowserRoot[];
  selected?: ProjectSelection;
  sessionID: string;
  token: string;
  onOpenPinned: (selection: ProjectSelection) => void;
  onOpenPreview: (selection: ProjectSelection) => void;
  onToggle: (rootID: string, path: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="py-1.5">
        {loading ? (
          <ProjectTreeStatus><Spinner />{t("common.loading")}</ProjectTreeStatus>
        ) : error ? (
          <ProjectTreeStatus>{projectBrowserError(error, t)}</ProjectTreeStatus>
        ) : roots.length === 0 ? (
          <ProjectTreeStatus>{t("project.browserEmpty")}</ProjectTreeStatus>
        ) : roots.map((root) => (
          <ProjectDirectoryNode
            key={root.id}
            {...actions}
            active={active}
            depth={0}
            expandedKeys={expandedKeys}
            gitStatuses={gitStatuses}
            isRoot
            label={root.name}
            path="."
            root={root}
            selected={selected}
            sessionID={sessionID}
            token={token}
            onOpenPinned={onOpenPinned}
            onOpenPreview={onOpenPreview}
            onToggle={onToggle}
          />
        ))}
    </div>
  );
}

function ProjectDirectoryNode({
  active,
  depth,
  expandedKeys,
  gitStatuses,
  isRoot = false,
  label,
  path,
  root,
  selected,
  sessionID,
  token,
  onOpenPinned,
  onOpenPreview,
  onToggle,
  ...actions
}: TreeActions & {
  active: boolean;
  depth: number;
  expandedKeys: string[];
  gitStatuses?: ReadonlyMap<string, ProjectGitStatusFile>;
  isRoot?: boolean;
  label: string;
  path: string;
  root: ProjectBrowserRoot;
  selected?: ProjectSelection;
  sessionID: string;
  token: string;
  onOpenPinned: (selection: ProjectSelection) => void;
  onOpenPreview: (selection: ProjectSelection) => void;
  onToggle: (rootID: string, path: string) => void;
}) {
  const { t } = useI18n();
  const key = `${root.id}:${path}`;
  const expanded = expandedKeys.includes(key);
  const target: ProjectEntryTarget = { rootID: root.id, path, name: label, type: "dir" };
  const treeQuery = useQuery({
    enabled: active && expanded,
    queryKey: queryKeys.projectTree(sessionID, root.id, path),
    queryFn: () => listProjectTree(token, sessionID, root.id, path),
    staleTime: 5_000,
  });

  return (
    <div>
      <ProjectEntryContextMenu {...actions} isRoot={isRoot} target={target}>
        <button
          className="flex h-7 w-full min-w-0 items-center gap-1 pr-2 text-left text-xs hover:bg-accent hover:text-accent-foreground"
          style={{ paddingLeft: `${8 + depth * 14}px` }}
          title={isRoot ? root.path : path}
          type="button"
          onClick={() => onToggle(root.id, path)}
        >
          <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", expanded && "rotate-90")} />
          {expanded ? <FolderOpen className="size-4 shrink-0 text-amber-500" /> : <Folder className="size-4 shrink-0 text-amber-500" />}
          <span className="min-w-0 flex-1 truncate font-medium">{label}</span>
        </button>
      </ProjectEntryContextMenu>
      {expanded ? (
        treeQuery.isLoading ? (
          null
        ) : treeQuery.isError ? (
          <div className="px-3 py-1.5 text-[11px] text-destructive" style={{ paddingLeft: `${36 + depth * 14}px` }}>
            {projectBrowserError(treeQuery.error, t)}
          </div>
        ) : (
          <>
            {treeQuery.data?.entries.map((entry) => entry.type === "dir" ? (
              <ProjectDirectoryNode
                key={entry.path}
                {...actions}
                active={active}
                depth={depth + 1}
                expandedKeys={expandedKeys}
                gitStatuses={gitStatuses}
                label={entry.name}
                path={entry.path}
                root={root}
                selected={selected}
                sessionID={sessionID}
                token={token}
                onOpenPinned={onOpenPinned}
                onOpenPreview={onOpenPreview}
                onToggle={onToggle}
              />
            ) : (
              <ProjectFileNode
                key={entry.path}
                {...actions}
                depth={depth + 1}
                entry={entry}
                rootID={root.id}
                selected={selected?.rootID === root.id && selected.path === entry.path}
                status={gitStatuses?.get(projectGitFileKey(root.id, entry.path))}
                onOpenPinned={() => onOpenPinned({ rootID: root.id, path: entry.path })}
                onOpenPreview={() => onOpenPreview({ rootID: root.id, path: entry.path })}
              />
            ))}
            {treeQuery.data?.truncated ? (
              <div className="px-3 py-1.5 text-[11px] text-warning" style={{ paddingLeft: `${36 + depth * 14}px` }}>
                {t("project.browserTreeTruncated")}
              </div>
            ) : null}
          </>
        )
      ) : null}
    </div>
  );
}

function ProjectFileNode({
  depth,
  entry,
  rootID,
  selected,
  status,
  onOpenPinned,
  onOpenPreview,
  ...actions
}: TreeActions & {
  depth: number;
  entry: ProjectTreeEntry;
  rootID: string;
  selected: boolean;
  status?: ProjectGitStatusFile;
  onOpenPinned: () => void;
  onOpenPreview: () => void;
}) {
  const disabled = entry.type !== "file";
  const button = (
    <button
      aria-current={selected ? "page" : undefined}
      className={cn(
        "flex h-7 w-full min-w-0 select-none items-center gap-1.5 pr-2 text-left text-xs hover:bg-accent hover:text-accent-foreground aria-[current=page]:bg-accent aria-[current=page]:text-accent-foreground",
        disabled && "cursor-default text-muted-foreground/60 hover:bg-transparent",
      )}
      disabled={disabled}
      style={{ paddingLeft: `${26 + depth * 14}px` }}
      title={entry.path}
      type="button"
      onClick={onOpenPreview}
      onDoubleClick={onOpenPinned}
    >
      {entry.type === "file" ? <FileCode2 className="size-3.5 shrink-0 text-muted-foreground" /> : <File className="size-3.5 shrink-0" />}
      <span className="min-w-0 flex-1 truncate">{entry.name}</span>
      {status ? (
        <span className={cn("w-3 shrink-0 text-center font-mono text-[11px] font-semibold", projectGitStatusTone(status))}>
          {projectGitStatusLabel(status, status.worktreeStatus === ".")}
        </span>
      ) : null}
    </button>
  );
  if (disabled) {
    return button;
  }
  const target: ProjectEntryTarget = { rootID, path: entry.path, name: entry.name, type: "file" };
  return <ProjectEntryContextMenu {...actions} target={target}>{button}</ProjectEntryContextMenu>;
}

function ProjectTreeStatus({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">{children}</div>;
}
