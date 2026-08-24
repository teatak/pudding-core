import { FileDiff, X } from "@/components/icons";
import { useEffect, useRef, useState, type ReactNode, type UIEvent } from "react";

import { useI18n } from "@/i18n";
import { turnFileDiffChanges } from "@/lib/turnFileChanges";
import { cn } from "@/lib/utils";
import type { FilePreview } from "@/state/filePreviewStore";

import { ProjectTabContextMenu, ProjectVirtualTabContextMenu } from "./ProjectContextMenu";
import { ProjectFileTypeIcon } from "./ProjectFileTypeIcon";
import { projectFileName, projectSelectionKey, projectTabKey } from "./projectPaths";
import { isProjectGitDiffTab, type ProjectSelection, type ProjectTab } from "./types";

export function ProjectFileTabs({
  active,
  activeTurnDiffID,
  dirtyKeys,
  leadingAction,
  tabs,
  turnDiffTabs,
  onActivate,
  onActivateTurnDiff,
  onCloseTurnDiffs,
  onPin,
  onRequestClose,
  onReveal,
}: {
  active?: ProjectTab;
  activeTurnDiffID?: string;
  dirtyKeys: ReadonlySet<string>;
  leadingAction?: ReactNode;
  tabs: ProjectTab[];
  turnDiffTabs: FilePreview[];
  onActivate: (selection: ProjectTab) => void;
  onActivateTurnDiff: (previewID: string) => void;
  onCloseTurnDiffs: (previewIDs: string[]) => void;
  onPin: (selection: ProjectTab) => void;
  onRequestClose: (keys: string[]) => void;
  onReveal: (selection: ProjectSelection) => void;
}) {
  const { t } = useI18n();
  const activeKey = active ? projectTabKey(active) : undefined;
  const [scrolling, setScrolling] = useState(false);
  const [scrollIndicator, setScrollIndicator] = useState({ left: 0, width: 0 });
  const hideScrollTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => () => {
    if (hideScrollTimerRef.current) clearTimeout(hideScrollTimerRef.current);
  }, []);

  if (tabs.length === 0 && turnDiffTabs.length === 0 && !leadingAction) return null;

  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const target = event.currentTarget;
    const scrollRange = target.scrollWidth - target.clientWidth;
    const indicatorWidth = scrollRange > 0
      ? Math.max(28, target.clientWidth * (target.clientWidth / target.scrollWidth))
      : 0;
    const indicatorRange = Math.max(0, target.clientWidth - indicatorWidth);
    const indicatorLeft = scrollRange > 0 ? (target.scrollLeft / scrollRange) * indicatorRange : 0;
    setScrollIndicator({ left: indicatorLeft, width: indicatorWidth });
    setScrolling(scrollRange > 0);
    if (hideScrollTimerRef.current) clearTimeout(hideScrollTimerRef.current);
    hideScrollTimerRef.current = setTimeout(() => setScrolling(false), 600);
  };

  return (
    <div className="flex h-(--workspace-subtoolbar-h) shrink-0 bg-[var(--workspace-file-tabs-background)]">
      {leadingAction ? (
        <div className="h-full shrink-0 border-r border-[var(--workspace-border)]">
          {leadingAction}
        </div>
      ) : null}
      <div className="relative min-w-0 flex-1">
        <div
          className="pudding-project-file-tabs flex h-full items-stretch overflow-x-auto overscroll-x-contain"
          onScroll={handleScroll}
        >
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
              onCloseOthers={() => {
                onRequestClose(tabs.filter((item) => projectTabKey(item) !== key).map(projectTabKey));
                onCloseTurnDiffs(turnDiffTabs.map((item) => item.id));
              }}
              onCloseRight={() => {
                onRequestClose(tabs.slice(index + 1).map(projectTabKey));
                onCloseTurnDiffs(turnDiffTabs.map((item) => item.id));
              }}
              onReveal={onReveal}
            >
              <div
                className={cn(
                  "group relative z-10 flex h-full min-w-28 max-w-56 shrink-0 items-center bg-[var(--workspace-file-tab-inactive-background)] text-xs text-[var(--workspace-tab-foreground)] hover:bg-[var(--workspace-file-tab-hover-background)] hover:text-foreground",
                  selected && "bg-[var(--workspace-file-tab-active-background)] text-foreground hover:bg-[var(--workspace-file-tab-active-background)]",
                )}
              >
                <button
                  aria-pressed={selected}
                  className="flex h-full min-w-0 flex-1 select-none items-center gap-1.5 pl-2.5 text-left"

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
                    "mr-1 inline-flex size-5 shrink-0 items-center justify-center rounded-[4px] opacity-0 hover:bg-[var(--workspace-file-tab-hover-background)] hover:text-foreground group-hover:opacity-100 focus-visible:opacity-100",
                    selected && "opacity-100",
                  )}

                  type="button"
                  onClick={() => onRequestClose([key])}
                >
                  <X className="size-3.5" />
                </button>
              </div>
            </ProjectTabContextMenu>
          );
        })}
        {turnDiffTabs.map((preview, index) => {
          const selected = preview.id === activeTurnDiffID;
          const label = replace(t("turnFiles.projectTab"), {
            count: String(turnFileDiffChanges(preview.fileChanges || []).length),
          });
          return (
            <ProjectVirtualTabContextMenu
              key={preview.id}
              onClose={() => onCloseTurnDiffs([preview.id])}
              onCloseOthers={() => {
                onRequestClose(tabs.map(projectTabKey));
                onCloseTurnDiffs(turnDiffTabs.filter((item) => item.id !== preview.id).map((item) => item.id));
              }}
              onCloseRight={() => onCloseTurnDiffs(turnDiffTabs.slice(index + 1).map((item) => item.id))}
            >
              <div
                className={cn(
                  "group relative z-10 flex h-full min-w-28 max-w-56 shrink-0 items-center bg-[var(--workspace-file-tab-inactive-background)] text-xs text-[var(--workspace-tab-foreground)] hover:bg-[var(--workspace-file-tab-hover-background)] hover:text-foreground",
                  selected && "bg-[var(--workspace-file-tab-active-background)] text-foreground hover:bg-[var(--workspace-file-tab-active-background)]",
                )}
              >
                <button
                  aria-pressed={selected}
                  className="flex h-full min-w-0 flex-1 select-none items-center gap-1.5 pl-2.5 text-left"

                  type="button"
                  onClick={() => onActivateTurnDiff(preview.id)}
                >
                  <FileDiff className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{label}</span>
                </button>
                <button
                  aria-label={`${t("project.browserCloseTab")} ${label}`}
                  className={cn(
                    "mr-1 inline-flex size-5 shrink-0 items-center justify-center rounded-[4px] opacity-0 hover:bg-[var(--workspace-file-tab-hover-background)] hover:text-foreground group-hover:opacity-100 focus-visible:opacity-100",
                    selected && "opacity-100",
                  )}

                  type="button"
                  onClick={() => onCloseTurnDiffs([preview.id])}
                >
                  <X className="size-3.5" />
                </button>
              </div>
            </ProjectVirtualTabContextMenu>
          );
        })}
        </div>
        <span
          aria-hidden="true"
          className={cn(
            "pointer-events-none absolute bottom-0 h-0.5 rounded-full bg-[var(--workspace-tab-border)] transition-opacity duration-150",
            scrolling ? "opacity-100" : "opacity-0",
          )}
          style={{ left: scrollIndicator.left, width: scrollIndicator.width }}
        />
      </div>
    </div>
  );
}

function replace(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce((text, [key, value]) => text.replaceAll(`{${key}}`, value), template);
}
