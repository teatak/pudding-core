import { Files, GitBranch, Search } from "@/components/icons";
import type { ReactNode } from "react";

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
  const views: Array<{
    badge?: number;
    icon: ReactNode;
    id: ProjectSidebarView;
    label: string;
    shortLabel: string;
  }> = [
    {
      icon: <Files />,
      id: "files",
      label: t("project.browserFiles"),
      shortLabel: t("project.browserFilesShort"),
    },
    {
      icon: <Search />,
      id: "search",
      label: t("project.browserSearch"),
      shortLabel: t("project.browserSearchShort"),
    },
    ...(git
      ? [{
          badge: gitChangeCount,
          icon: <GitBranch />,
          id: "git" as const,
          label: t("project.git"),
          shortLabel: t("project.gitShort"),
        }]
      : []),
  ];
  const activeIndex = Math.max(0, views.findIndex((view) => view.id === visibleView));

  return (
    <div className="flex h-full min-h-0 flex-col bg-[var(--workspace-tree-background)]">
      <nav
        aria-label={t("workspace.project")}
        className="flex h-11 shrink-0 items-center p-2"
      >
        <div
          className="relative grid h-full min-w-0 flex-1 rounded-lg bg-[var(--workspace-view-switch-background)] p-0.5"
          style={{ gridTemplateColumns: `repeat(${views.length}, minmax(0, 1fr))` }}
        >
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-y-0.5 left-0.5 rounded-md bg-[var(--workspace-view-switch-active-background)] shadow-sm transition-transform duration-150 ease-out"
            style={{
              transform: `translateX(${activeIndex * 100}%)`,
              width: `calc((100% - 0.25rem) / ${views.length})`,
            }}
          />
          {views.map((view) => (
            <ProjectActivityButton
              active={visibleView === view.id}
              badge={view.badge}
              icon={view.icon}
              key={view.id}
              label={view.label}
              shortLabel={view.shortLabel}
              onClick={() => onViewChange(view.id)}
            />
          ))}
        </div>
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
  shortLabel,
  onClick,
}: {
  active: boolean;
  badge?: number;
  icon: ReactNode;
  label: string;
  shortLabel: string;
  onClick: () => void;
}) {
  return (
    <button
      aria-label={label}
      aria-pressed={active}
      className={cn(
        "relative z-10 inline-flex min-w-0 items-center justify-center gap-1 rounded-md px-1 text-[11px] font-medium text-muted-foreground hover:text-foreground [&>svg]:size-3.5 [&>svg]:shrink-0",
        active && "text-foreground",
      )}
      type="button"
      onClick={onClick}
    >
      {icon}
      <span className="truncate">{shortLabel}</span>
      {badge ? (
        <span className="inline-flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-primary px-0.5 text-[8px] font-semibold leading-none tabular-nums text-primary-foreground">
          {badge > 99 ? "99+" : badge}
        </span>
      ) : null}
    </button>
  );
}
