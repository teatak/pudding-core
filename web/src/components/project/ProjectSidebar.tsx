import { Files, GitBranch, Search } from "@/components/icons";
import type { ReactNode } from "react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export type ProjectSidebarView = "files" | "git" | "search";

export function ProjectSidebar({
  activeView,
  files,
  git,
  gitChangeCount,
  search,
  onViewChange,
}: {
  activeView: ProjectSidebarView;
  files: ReactNode;
  git?: ReactNode;
  gitChangeCount: number;
  search: ReactNode;
  onViewChange: (view: ProjectSidebarView) => void;
}) {
  const { t } = useI18n();
  const visibleView = activeView === "git" && !git ? "files" : activeView;

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--workspace-tree-background)]">
      <nav
        aria-label={t("workspace.project")}
        className="flex h-(--workspace-subtoolbar-h) shrink-0 items-center gap-1 bg-sidebar px-1"
      >
        <ProjectActivityButton
          active={visibleView === "files"}
          icon={<Files />}
          label={t("project.browserFiles")}
          onClick={() => onViewChange("files")}
        />
        <ProjectActivityButton
          active={visibleView === "search"}
          icon={<Search />}
          label={t("project.browserSearch")}
          onClick={() => onViewChange("search")}
        />
        {git ? (
          <ProjectActivityButton
            active={visibleView === "git"}
            badge={gitChangeCount}
            icon={<GitBranch />}
            label={t("project.git")}
            onClick={() => onViewChange("git")}
          />
        ) : null}
      </nav>
      <div className="min-h-0 min-w-0 flex-1 overflow-hidden">
        {visibleView === "search" ? search : visibleView === "git" ? git : files}
      </div>
    </div>
  );
}

function ProjectActivityButton({
  active,
  badge,
  icon,
  label,
  onClick,
}: {
  active: boolean;
  badge?: number;
  icon: ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          aria-label={label}
          aria-pressed={active}
          className={cn(
            "relative inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-[var(--control-hover-background)] hover:text-foreground [&>svg]:size-[18px]",
            active && "bg-[var(--control-active-background)] text-foreground hover:bg-[var(--control-active-background)]",
          )}
          type="button"
          onClick={onClick}
        >
          {icon}
          {badge ? (
            <span className="absolute top-0 right-0 inline-flex h-3.5 min-w-3.5 translate-x-1/4 -translate-y-1/4 items-center justify-center rounded-full bg-primary px-0.5 text-[8px] font-semibold leading-none tabular-nums text-primary-foreground">
              {badge > 99 ? "99+" : badge}
            </span>
          ) : null}
        </button>
      </TooltipTrigger>
      <TooltipContent side="bottom" sideOffset={4}>{label}</TooltipContent>
    </Tooltip>
  );
}
