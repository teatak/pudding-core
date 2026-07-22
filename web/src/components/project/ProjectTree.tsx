import { useQuery } from "@tanstack/react-query";
import { ChevronRight, File } from "lucide-react";
import { useState, type ReactNode } from "react";

import {
  listProjectTree,
  type ProjectBrowserRoot,
  type ProjectGitStatusFile,
  type ProjectTreeEntry,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import {
  dataTransferHasProjectEntry,
  readProjectEntryDrag,
  writeProjectReferenceDrag,
} from "@/lib/projectReferences";
import { cn } from "@/lib/utils";

import { ProjectEntryContextMenu } from "./ProjectContextMenu";
import { ProjectFileTypeIcon, ProjectFolderTypeIcon } from "./ProjectFileTypeIcon";
import { projectGitFileKey, projectGitStatusLabel, projectGitStatusTone } from "./git/gitStatus";
import { projectBrowserError } from "./projectErrors";
import { projectAbsolutePath, projectParentPath } from "./projectPaths";
import type { ProjectEntryTarget, ProjectSelection } from "./types";

type TreeActions = {
  canPaste: boolean;
  onCopyAbsolutePath: (target: ProjectEntryTarget) => void;
  onCopyEntry: (target: ProjectEntryTarget) => void;
  onCopyPath: (target: ProjectEntryTarget) => void;
  onCreate: (target: ProjectEntryTarget, type: "dir" | "file") => void;
  onCutEntry: (target: ProjectEntryTarget) => void;
  onDelete: (target: ProjectEntryTarget) => void;
  onDuplicate: (target: ProjectEntryTarget) => void;
  onMove: (source: ProjectEntryTarget, destination: ProjectEntryTarget) => void;
  onOpenTerminal: (target: ProjectEntryTarget) => void;
  onPaste: (target: ProjectEntryTarget) => void;
  onReference: (target: ProjectEntryTarget) => void;
  onRename: (target: ProjectEntryTarget) => void;
  onRevealInFinder: (target: ProjectEntryTarget) => void;
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
    <div>
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
  const [dropActive, setDropActive] = useState(false);
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
          className={cn(
            "flex h-6 w-full min-w-0 items-center gap-1 pr-2 text-left text-xs hover:bg-[var(--workspace-tree-hover-background)] hover:text-accent-foreground",
            dropActive && "bg-primary/10 text-foreground ring-1 ring-inset ring-primary/40",
          )}
          style={{ paddingLeft: `${7 + depth * 13}px` }}

          draggable
          type="button"
          onClick={() => onToggle(root.id, path)}
          onDragStart={(event) => writeProjectReferenceDrag(event.dataTransfer, {
            name: target.name,
            path: target.path,
            sourcePath: projectAbsolutePath(root.path, target.path),
            rootID: target.rootID,
            kind: "dir",
          })}
          onDragEnd={() => setDropActive(false)}
          onDragEnter={(event) => {
            if (dataTransferHasProjectEntry(event.dataTransfer)) setDropActive(true);
          }}
          onDragLeave={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropActive(false);
          }}
          onDragOver={(event) => {
            if (!dataTransferHasProjectEntry(event.dataTransfer)) return;
            event.preventDefault();
            event.dataTransfer.dropEffect = "move";
            setDropActive(true);
          }}
          onDrop={(event) => {
            const source = readProjectEntryDrag(event.dataTransfer);
            setDropActive(false);
            if (!source) return;
            event.preventDefault();
            event.stopPropagation();
            const sourceTarget: ProjectEntryTarget = {
              name: source.name,
              path: source.path,
              rootID: source.rootID,
              type: source.kind,
            };
            if (canMoveProjectEntry(sourceTarget, target)) actions.onMove(sourceTarget, target);
          }}
        >
          <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", expanded && "rotate-90")} />
          <ProjectFolderTypeIcon name={label} open={expanded} />
          <span className="min-w-0 flex-1 truncate font-medium">{label}</span>
        </button>
      </ProjectEntryContextMenu>
      {expanded ? (
        <div className="relative">
          {!isRoot ? (
            <span
              aria-hidden="true"
              className="pointer-events-none absolute inset-y-0 z-10 w-px bg-foreground/15"
              style={{ left: `${14 + depth * 13}px` }}
            />
          ) : null}
          {treeQuery.isLoading ? (
            null
          ) : treeQuery.isError ? (
            <div className="px-3 py-1 text-[11px] text-destructive" style={{ paddingLeft: `${33 + depth * 13}px` }}>
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
                rootPath={root.path}
                selected={selected?.rootID === root.id && selected.path === entry.path}
                status={gitStatuses?.get(projectGitFileKey(root.id, entry.path))}
                onOpenPinned={() => onOpenPinned({ rootID: root.id, path: entry.path })}
                onOpenPreview={() => onOpenPreview({ rootID: root.id, path: entry.path })}
              />
            ))}
            {treeQuery.data?.truncated ? (
              <div className="px-3 py-1 text-[11px] text-warning" style={{ paddingLeft: `${33 + depth * 13}px` }}>
                {t("project.browserTreeTruncated")}
              </div>
            ) : null}
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}

function ProjectFileNode({
  depth,
  entry,
  rootID,
  rootPath,
  selected,
  status,
  onOpenPinned,
  onOpenPreview,
  ...actions
}: TreeActions & {
  depth: number;
  entry: ProjectTreeEntry;
  rootID: string;
  rootPath: string;
  selected: boolean;
  status?: ProjectGitStatusFile;
  onOpenPinned: () => void;
  onOpenPreview: () => void;
}) {
  const [dropActive, setDropActive] = useState(false);
  const disabled = entry.type !== "file";
  const target: ProjectEntryTarget = { rootID, path: entry.path, name: entry.name, type: "file" };
  const destination: ProjectEntryTarget = {
    rootID,
    path: projectParentPath(entry.path),
    name: projectParentPath(entry.path),
    type: "dir",
  };
  const button = (
    <button
      aria-current={selected ? "page" : undefined}
      className={cn(
        "flex h-6 w-full min-w-0 select-none items-center gap-1.5 pr-2 text-left text-xs hover:bg-[var(--workspace-tree-hover-background)] hover:text-accent-foreground aria-[current=page]:bg-[var(--workspace-tree-active-background)] aria-[current=page]:text-accent-foreground",
        disabled && "cursor-default text-muted-foreground/60 hover:bg-transparent",
        dropActive && "bg-primary/10 text-foreground ring-1 ring-inset ring-primary/40",
      )}
      disabled={disabled}
      draggable={!disabled}
      style={{ paddingLeft: `${25 + depth * 13}px` }}

      type="button"
      onClick={onOpenPreview}
      onDoubleClick={onOpenPinned}
      onDragStart={(event) => {
        if (!disabled) {
          writeProjectReferenceDrag(event.dataTransfer, {
            name: target.name,
            path: target.path,
            sourcePath: projectAbsolutePath(rootPath, target.path),
            rootID: target.rootID,
            kind: "file",
          });
        }
      }}
      onDragEnd={() => setDropActive(false)}
      onDragEnter={(event) => {
        if (dataTransferHasProjectEntry(event.dataTransfer)) setDropActive(true);
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropActive(false);
      }}
      onDragOver={(event) => {
        if (!dataTransferHasProjectEntry(event.dataTransfer)) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = "move";
        setDropActive(true);
      }}
      onDrop={(event) => {
        const source = readProjectEntryDrag(event.dataTransfer);
        setDropActive(false);
        if (!source) return;
        event.preventDefault();
        event.stopPropagation();
        const sourceTarget: ProjectEntryTarget = {
          name: source.name,
          path: source.path,
          rootID: source.rootID,
          type: source.kind,
        };
        if (canMoveProjectEntry(sourceTarget, destination)) actions.onMove(sourceTarget, destination);
      }}
    >
      {entry.type === "file" ? <ProjectFileTypeIcon path={entry.path} /> : <File className="size-3.5 shrink-0" />}
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
  return <ProjectEntryContextMenu {...actions} target={target}>{button}</ProjectEntryContextMenu>;
}

function ProjectTreeStatus({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">{children}</div>;
}

function canMoveProjectEntry(source: ProjectEntryTarget, destination: ProjectEntryTarget) {
  if (destination.type !== "dir" || source.path === ".") return false;
  if (source.rootID !== destination.rootID) return true;
  if (source.path === destination.path) return false;
  if (projectParentPath(source.path) === destination.path) return false;
  return source.type !== "dir" || !destination.path.startsWith(`${source.path}/`);
}
