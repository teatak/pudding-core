import { AppTooltip } from "@/components/AppTooltip";
import type { ProjectBrowserRoot } from "@/api/client";
import { OpenTabsMenu } from "@/components/workspace/OpenTabsMenu";
import { useActiveTabVisibility } from "@/components/workspace/useActiveTabVisibility";
import { SortableTab, SortableTabList } from "@/components/workspace/SortableTabList";
import { filePreviewTitle } from "@/components/canvas/FilePreviewSurface";
import { FileDiff, X } from "@/components/icons";
import { useEffect, useRef, useState, type ReactNode, type UIEvent } from "react";

import { useI18n } from "@/i18n";
import { turnFileDiffChanges } from "@/lib/turnFileChanges";
import { cn } from "@/lib/utils";
import type { FilePreview } from "@/state/filePreviewStore";

import { ProjectTabContextMenu, ProjectVirtualTabContextMenu } from "./ProjectContextMenu";
import { ProjectFileTypeIcon } from "./ProjectFileTypeIcon";
import { projectAbsolutePath, projectFileName, projectSelectionKey, projectTabKey } from "./projectPaths";
import { isProjectGitDiffTab, type ProjectSelection, type ProjectTab } from "./types";

export function ProjectFileTabs({
  active,
  activePreviewID,
  dirtyKeys,
  leadingAction,
  tabs,
  roots,
  previewTabs,
  onActivate,
  onActivatePreview,
  onClosePreviews,
  onPin,
  onMoveTab,
  onRequestClose,
  onReveal,
}: {
  active?: ProjectTab;
  activePreviewID?: string;
  dirtyKeys: ReadonlySet<string>;
  leadingAction?: ReactNode;
  tabs: ProjectTab[];
  roots: ProjectBrowserRoot[];
  previewTabs: FilePreview[];
  onActivate: (selection: ProjectTab) => void;
  onActivatePreview: (previewID: string) => void;
  onClosePreviews: (previewIDs: string[]) => void;
  onPin: (selection: ProjectTab) => void;
  onMoveTab: (activeID: string, overID: string) => void;
  onRequestClose: (keys: string[]) => void;
  onReveal: (selection: ProjectSelection) => void;
}) {
  const { t } = useI18n();
  const activeKey = active ? projectTabKey(active) : undefined;
  const scrollRef = useRef<HTMLDivElement>(null);
  useActiveTabVisibility(scrollRef, activePreviewID || activeKey);
  const [scrolling, setScrolling] = useState(false);
  const [scrollIndicator, setScrollIndicator] = useState({ left: 0, width: 0 });
  const hideScrollTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => () => {
    if (hideScrollTimerRef.current) clearTimeout(hideScrollTimerRef.current);
  }, []);

  if (tabs.length === 0 && previewTabs.length === 0 && !leadingAction) return null;

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
    <div className="pudding-workspace-tab-strip no-drag-region">
      {leadingAction ? (
        <div className="h-full shrink-0 pr-1">
          {leadingAction}
        </div>
      ) : null}
      <div className="relative h-full min-w-0 flex-1">
        <SortableTabList ids={tabs.map(projectTabKey)} onMove={onMoveTab}>
        <div
          className="pudding-workspace-tabs-scroll flex h-full items-center gap-1 overflow-x-auto overscroll-x-contain"
          onScroll={handleScroll}
          ref={scrollRef}
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
                onClosePreviews(previewTabs.map((item) => item.id));
              }}
              onCloseRight={() => {
                onRequestClose(tabs.slice(index + 1).map(projectTabKey));
                onClosePreviews(previewTabs.map((item) => item.id));
              }}
              onReveal={onReveal}
            >
              <SortableTab id={key} selected={selected} disabled={tabs.length < 2} className="max-w-56 shrink-0">{(handleProps) => <>
                <AppTooltip content={tab.path}><button {...handleProps}
                  aria-pressed={selected}
                  className="pudding-workspace-tab-select"

                  type="button"
                  onClick={() => onActivate(tab)}
                  onDoubleClick={() => onPin(tab)}
                >
                  {gitDiff ? <FileDiff className="size-3.5 shrink-0 text-muted-foreground" /> : <ProjectFileTypeIcon path={tab.path} />}
                  <span className={cn("min-w-0 flex-1 truncate", !tab.pinned && "italic")}>{name}</span>
                  {dirty ? <span aria-label={t("project.browserUnsaved")} className="size-1.5 shrink-0 rounded-full bg-foreground/70" /> : null}
                </button></AppTooltip>
                <button
                  aria-label={`${t("project.browserCloseTab")} ${name}`}
                  className="pudding-workspace-tab-close"

                  type="button"
                  onClick={() => onRequestClose([key])}
                >
                  <X className="size-3" />
                </button>
              </>}</SortableTab>
            </ProjectTabContextMenu>
          );
        })}
        {previewTabs.map((preview, index) => {
          const selected = preview.id === activePreviewID;
          const label = preview.source !== "turn-diff" ? filePreviewTitle(preview.path) : replace(t("turnFiles.projectTab"), {
            count: String(turnFileDiffChanges(preview.fileChanges || []).length),
          });
          return (
            <ProjectVirtualTabContextMenu
              key={preview.id}
              onClose={() => onClosePreviews([preview.id])}
              onCloseOthers={() => {
                onRequestClose(tabs.map(projectTabKey));
                onClosePreviews(previewTabs.filter((item) => item.id !== preview.id).map((item) => item.id));
              }}
              onCloseRight={() => onClosePreviews(previewTabs.slice(index + 1).map((item) => item.id))}
            >
              <div
                className="pudding-workspace-content-tab group max-w-56 shrink-0"
                data-selected={selected}
              >
                <button
                  aria-pressed={selected}
                  className="pudding-workspace-tab-select"

                  type="button"
                  onClick={() => onActivatePreview(preview.id)}
                >
                  {preview.source === "turn-diff" ? <FileDiff className="size-3.5 shrink-0 text-muted-foreground" /> : <ProjectFileTypeIcon path={preview.path} />}
                  <span className="min-w-0 flex-1 truncate">{label}</span>
                </button>
                <button
                  aria-label={`${t("project.browserCloseTab")} ${label}`}
                  className="pudding-workspace-tab-close"

                  type="button"
                  onClick={() => onClosePreviews([preview.id])}
                >
                  <X className="size-3" />
                </button>
              </div>
            </ProjectVirtualTabContextMenu>
          );
        })}
        </div>
        </SortableTabList>
        <span
          aria-hidden="true"
          className={cn(
            "pointer-events-none absolute bottom-0 h-0.5 rounded-full bg-[var(--workspace-tab-border)] transition-opacity duration-150",
            scrolling ? "opacity-100" : "opacity-0",
          )}
          style={{ left: scrollIndicator.left, width: scrollIndicator.width }}
        />
      </div>
      <OpenTabsMenu activeID={activePreviewID ? `preview:${activePreviewID}` : activeKey} tabs={[
        ...tabs.map((tab) => {
          const root = roots.find((entry) => entry.id === tab.rootID);
          return { id: projectTabKey(tab), title: projectFileName(tab.path), description: root ? projectAbsolutePath(root.path, tab.path) : tab.path, icon: <ProjectFileTypeIcon path={tab.path} /> };
        }),
        ...previewTabs.map((preview) => ({ id: `preview:${preview.id}`, title: preview.source === "turn-diff" ? t("turnFiles.tab") : filePreviewTitle(preview.path), description: preview.path, icon: preview.source === "turn-diff" ? <FileDiff className="size-3.5" /> : <ProjectFileTypeIcon path={preview.path} /> })),
      ]} onSelect={(id) => {
        const preview = previewTabs.find((entry) => `preview:${entry.id}` === id);
        if (preview) { onActivatePreview(preview.id); return; }
        const tab = tabs.find((entry) => projectTabKey(entry) === id);
        if (tab) onActivate(tab);
      }} />
    </div>
  );
}

function replace(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce((text, [key, value]) => text.replaceAll(`{${key}}`, value), template);
}
