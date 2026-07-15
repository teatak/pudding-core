import { FileDiff, X } from "lucide-react";

import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { ProjectTabContextMenu } from "./ProjectContextMenu";
import { ProjectFileTypeIcon } from "./ProjectFileTypeIcon";
import { projectFileName, projectSelectionKey, projectTabKey } from "./projectPaths";
import { isProjectGitDiffTab, type ProjectSelection, type ProjectTab } from "./types";

export function ProjectFileTabs({
  active,
  dirtyKeys,
  tabs,
  onActivate,
  onPin,
  onRequestClose,
  onReveal,
}: {
  active?: ProjectTab;
  dirtyKeys: ReadonlySet<string>;
  tabs: ProjectTab[];
  onActivate: (selection: ProjectTab) => void;
  onPin: (selection: ProjectTab) => void;
  onRequestClose: (keys: string[]) => void;
  onReveal: (selection: ProjectSelection) => void;
}) {
  const { t } = useI18n();
  const activeKey = active ? projectTabKey(active) : undefined;

  return (
    <div className="flex h-10 shrink-0 items-stretch overflow-x-auto border-b bg-muted/20 dark:bg-[#1c1c1c]">
      {tabs.map((tab, index) => {
        const key = projectTabKey(tab);
        const selected = key === activeKey;
        const gitDiff = isProjectGitDiffTab(tab);
        const dirty = !gitDiff && dirtyKeys.has(projectSelectionKey(tab));
        const name = projectFileName(tab.path);
        return (
          <ProjectTabContextMenu
            key={key}
            tab={tab}
            onClose={() => onRequestClose([key])}
            onCloseOthers={() => onRequestClose(tabs.filter((item) => projectTabKey(item) !== key).map(projectTabKey))}
            onCloseRight={() => onRequestClose(tabs.slice(index + 1).map(projectTabKey))}
            onReveal={onReveal}
          >
            <div
              className={cn(
                "group flex h-full min-w-28 max-w-56 shrink-0 items-center border-r text-xs text-muted-foreground",
                selected && "bg-card text-foreground dark:bg-[#242424]",
              )}
            >
              <button
                aria-pressed={selected}
                className="flex h-full min-w-0 flex-1 select-none items-center gap-1.5 pl-3 text-left"
                title={gitDiff ? `${tab.path} (${tab.staged ? t("project.gitStaged") : t("project.gitWorkingTree")})` : tab.path}
                type="button"
                onClick={() => onActivate(tab)}
                onDoubleClick={() => onPin(tab)}
              >
                {gitDiff ? <FileDiff className="size-3.5 shrink-0 text-muted-foreground" /> : <ProjectFileTypeIcon path={tab.path} />}
                <span className={cn("min-w-0 flex-1 truncate", !tab.pinned && "italic")}>{name}</span>
                {dirty ? <span aria-label={t("project.browserUnsaved")} className="size-1.5 shrink-0 rounded-full bg-foreground/70" /> : null}
              </button>
              <button
                aria-label={`${t("project.browserCloseTab")} ${name}`}
                className={cn(
                  "mr-1 inline-flex size-6 shrink-0 items-center justify-center rounded-sm opacity-0 hover:bg-accent hover:text-accent-foreground group-hover:opacity-100 focus-visible:opacity-100",
                  selected && "opacity-100",
                )}
                title={t("project.browserCloseTab")}
                type="button"
                onClick={() => onRequestClose([key])}
              >
                <X className="size-3.5" />
              </button>
            </div>
          </ProjectTabContextMenu>
        );
      })}
    </div>
  );
}
