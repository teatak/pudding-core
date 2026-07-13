import { ChevronDown, FolderTree, GitBranch, RefreshCw } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { usePanelRef } from "react-resizable-panels";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useI18n } from "@/i18n";
import { layoutStorageKeys } from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { cn } from "@/lib/utils";

const collapsedPanelPixels = 32;
const minimumPanelPixels = 120;
const maximumRememberedPercent = 85;

export function ProjectSidebar({ files, git, refreshing, onRefresh }: {
  files: ReactNode;
  git: ReactNode;
  refreshing: boolean;
  onRefresh: () => void;
}) {
  const { t } = useI18n();
  const filesRef = usePanelRef();
  const gitRef = usePanelRef();
  const filesExpandedSize = useRef(62);
  const gitExpandedSize = useRef(38);
  const [filesCollapsed, setFilesCollapsed] = useState(false);
  const [gitCollapsed, setGitCollapsed] = useState(false);

  return (
    <div className="flex h-full min-h-0 flex-col bg-muted/20">
      <div className="flex h-10 shrink-0 items-center gap-2 border-b px-3">
        <FolderTree className="size-4 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate text-xs font-medium">{t("project.browser")}</span>
        <Button aria-label={t("common.refresh")} disabled={refreshing} size="icon-sm" type="button" variant="ghost" onClick={onRefresh}>
          {refreshing ? <Spinner /> : <RefreshCw />}
        </Button>
      </div>
      <ResizablePanelGroup
        className="min-h-0 flex-1"
        defaultLayout={readPanelLayout(layoutStorageKeys.projectSidebarRatio, { files: 62, git: 38 }, { minPercent: 4, maxPercent: 96 })}
        id="project-sidebar-layout"
        orientation="vertical"
        onLayoutChanged={(layout) => savePanelLayout(layoutStorageKeys.projectSidebarRatio, layout)}
      >
        <ResizablePanel
          id="files"
          className="min-h-0"
          collapsedSize="32px"
          collapsible
          minSize="120px"
          panelRef={filesRef}
          onResize={({ asPercentage, inPixels }) => {
            setFilesCollapsed(inPixels <= collapsedPanelPixels + 1);
            if (inPixels > collapsedPanelPixels + 1 && asPercentage <= maximumRememberedPercent) {
              filesExpandedSize.current = asPercentage;
            }
          }}
        >
          <ProjectSidebarSection collapsed={filesCollapsed} icon={<FolderTree />} label={t("project.browserFiles")} onToggle={() => togglePanel(filesRef.current, gitRef.current, filesCollapsed, filesExpandedSize.current)}>
            {files}
          </ProjectSidebarSection>
        </ResizablePanel>
        <ResizableHandle className="hover:bg-primary/50 data-[resize-handle-active]:bg-primary" />
        <ResizablePanel
          id="git"
          className="min-h-0"
          collapsedSize="32px"
          collapsible
          minSize="120px"
          panelRef={gitRef}
          onResize={({ asPercentage, inPixels }) => {
            setGitCollapsed(inPixels <= collapsedPanelPixels + 1);
            if (inPixels > collapsedPanelPixels + 1 && asPercentage <= maximumRememberedPercent) {
              gitExpandedSize.current = asPercentage;
            }
          }}
        >
          <ProjectSidebarSection collapsed={gitCollapsed} icon={<GitBranch />} label={t("project.git")} onToggle={() => togglePanel(gitRef.current, filesRef.current, gitCollapsed, gitExpandedSize.current)}>
            {git}
          </ProjectSidebarSection>
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

function ProjectSidebarSection({ children, collapsed, icon, label, onToggle }: {
  children: ReactNode;
  collapsed: boolean;
  icon: ReactNode;
  label: string;
  onToggle: () => void;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <button className={cn("flex h-8 w-full shrink-0 items-center gap-1.5 px-2 text-left text-[11px] font-semibold uppercase tracking-wide hover:bg-accent", !collapsed && "border-b")} type="button" onClick={onToggle}>
        <ChevronDown className={cn("size-3.5 shrink-0 transition-transform", collapsed && "-rotate-90")} />
        <span className="[&>svg]:size-3.5 [&>svg]:text-muted-foreground">{icon}</span>
        <span className="min-w-0 flex-1 truncate">{label}</span>
      </button>
      {!collapsed ? <div className="min-h-0 flex-1 overflow-auto">{children}</div> : null}
    </div>
  );
}

function togglePanel(
  panel: ReturnType<typeof usePanelRef>["current"],
  sibling: ReturnType<typeof usePanelRef>["current"],
  collapsed: boolean,
  expandedSize: number,
) {
  if (!panel) return;
  if (!collapsed) {
    panel.collapse();
    return;
  }
  const totalPixels = panel.getSize().inPixels + (sibling?.getSize().inPixels || 0);
  const maximumPercent = totalPixels > minimumPanelPixels * 2
    ? ((totalPixels - minimumPanelPixels) / totalPixels) * 100
    : 50;
  panel.resize(`${Math.min(expandedSize, maximumPercent)}%`);
}
