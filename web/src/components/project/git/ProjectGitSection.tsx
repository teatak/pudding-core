import type { ProjectGitStatusFile } from "@/api/client";
import { ChevronRight, GitBranch, GitCommitHorizontal } from "lucide-react";
import { useState } from "react";

import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { projectFileName } from "../projectPaths";
import type { ProjectGitDiffSelection } from "../types";
import { projectBrowserError } from "../projectErrors";
import { isProjectGitStaged, isProjectGitWorking, projectGitStatusLabel, projectGitStatusTone } from "./gitStatus";
import type { ProjectGitRepositoryState } from "./types";

export function ProjectGitSection({ repositories, onOpenDiff }: {
  repositories: ProjectGitRepositoryState[];
  onOpenDiff: (selection: ProjectGitDiffSelection, pinned: boolean) => void;
}) {
  const { t } = useI18n();
  const available = repositories.filter((repository) => repository.status?.available);
  const firstError = repositories.find((repository) => repository.error)?.error;
  const loading = repositories.length > 0 && repositories.every((repository) => repository.loading);

  if (loading) {
    return <ProjectGitMessage><Spinner />{t("common.loading")}</ProjectGitMessage>;
  }
  if (available.length === 0) {
    return <ProjectGitMessage>{firstError ? projectBrowserError(firstError, t) : t("project.gitUnavailable")}</ProjectGitMessage>;
  }
  return (
    <div className="py-1">
      {available.map((repository) => {
        const status = repository.status!;
        const staged = status.files.filter(isProjectGitStaged);
        const working = status.files.filter(isProjectGitWorking);
        return (
          <div key={repository.root.id} className="pb-1">
            <div className="flex h-7 min-w-0 items-center gap-1.5 px-2 text-xs" title={repository.root.path}>
              <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate font-medium">{repository.root.name}</span>
              <span className="max-w-28 truncate text-[11px] text-muted-foreground">
                {status.detached ? status.head || t("project.gitDetached") : status.branch || t("project.gitDetached")}
              </span>
              {status.ahead ? <span className="text-[10px] text-muted-foreground">↑{status.ahead}</span> : null}
              {status.behind ? <span className="text-[10px] text-muted-foreground">↓{status.behind}</span> : null}
              {repository.fetching ? <Spinner className="size-3" /> : null}
            </div>
            {status.clean ? (
              <div className="px-7 py-2 text-[11px] text-muted-foreground">{t("project.gitClean")}</div>
            ) : (
              <>
                <ProjectGitChangeGroup
                  files={staged}
                  label={t("project.gitStagedChanges")}
                  rootID={repository.root.id}
                  staged
                  onOpenDiff={onOpenDiff}
                />
                <ProjectGitChangeGroup
                  files={working}
                  label={t("project.gitChanges")}
                  rootID={repository.root.id}
                  staged={false}
                  onOpenDiff={onOpenDiff}
                />
              </>
            )}
          </div>
        );
      })}
    </div>
  );
}

function ProjectGitChangeGroup({ files, label, rootID, staged, onOpenDiff }: {
  files: ProjectGitStatusFile[];
  label: string;
  rootID: string;
  staged: boolean;
  onOpenDiff: (selection: ProjectGitDiffSelection, pinned: boolean) => void;
}) {
  const [open, setOpen] = useState(true);
  if (files.length === 0) return null;
  return (
    <div>
      <button className="flex h-7 w-full items-center gap-1 px-2 text-left text-[11px] font-medium hover:bg-accent" type="button" onClick={() => setOpen((value) => !value)}>
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
        <span className="min-w-0 flex-1 truncate uppercase tracking-wide">{label}</span>
        <span className="text-muted-foreground">{files.length}</span>
      </button>
      {open ? files.map((file) => {
        const selection: ProjectGitDiffSelection = {
          rootID,
          path: file.path,
          originalPath: file.originalPath,
          staged,
          view: "git-diff",
        };
        return (
          <button
            key={`${staged ? "staged" : "working"}:${file.path}`}
            className="flex h-7 w-full min-w-0 select-none items-center gap-1.5 pl-7 pr-2 text-left text-xs hover:bg-accent hover:text-accent-foreground"
            title={file.path}
            type="button"
            onClick={() => onOpenDiff(selection, false)}
            onDoubleClick={() => onOpenDiff(selection, true)}
          >
            <GitCommitHorizontal className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">{projectFileName(file.path)}</span>
            <span className="min-w-0 max-w-28 truncate text-[10px] text-muted-foreground">{file.path.includes("/") ? file.path.slice(0, file.path.lastIndexOf("/")) : ""}</span>
            <span className={cn("w-3 shrink-0 text-center font-mono text-[11px] font-semibold", projectGitStatusTone(file))}>
              {projectGitStatusLabel(file, staged)}
            </span>
          </button>
        );
      }) : null}
    </div>
  );
}

function ProjectGitMessage({ children }: { children: React.ReactNode }) {
  return <div className="flex items-center gap-2 px-3 py-4 text-xs text-muted-foreground">{children}</div>;
}
