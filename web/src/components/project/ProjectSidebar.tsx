import { ChevronDown, Folders, GitBranch } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { usePanelRef } from "react-resizable-panels";

import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useI18n } from "@/i18n";
import { layoutStorageKeys } from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { cn } from "@/lib/utils";

const collapsedPanelPixels = 39;
const minimumPanelPixels = 120;
const maximumRememberedPercent = 85;

export function ProjectSidebar({ files, git }: {
  files: ReactNode;
  git: ReactNode;
}) {
  const { t } = useI18n();
  const filesRef = usePanelRef();
  const gitRef = usePanelRef();
  const filesExpandedSize = useRef(62);
  const gitExpandedSize = useRef(38);
  const [filesCollapsed, setFilesCollapsed] = useState(false);
  const [gitCollapsed, setGitCollapsed] = useState(false);

  return (
    <ResizablePanelGroup
      className="h-full min-h-0 bg-muted/20 dark:bg-[#1c1c1c]"
      defaultLayout={readPanelLayout(layoutStorageKeys.projectSidebarRatio, { files: 62, git: 38 }, { minPercent: 4, maxPercent: 96 })}
      id="project-sidebar-layout"
      orientation="vertical"
      onLayoutChanged={(layout) => savePanelLayout(layoutStorageKeys.projectSidebarRatio, layout)}
    >
        <ResizablePanel
          id="files"
          className="min-h-0"
          collapsedSize="39px"
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
          <ProjectSidebarSection
            collapsed={filesCollapsed}
            icon={<Folders />}
            label={t("project.browserFiles")}
            onToggle={() => togglePanel(filesRef.current, gitRef.current, filesCollapsed, filesExpandedSize.current)}
          >
            {files}
          </ProjectSidebarSection>
        </ResizablePanel>
        <ResizableHandle className="hover:bg-primary/50 data-[resize-handle-active]:bg-primary" />
        <ResizablePanel
          id="git"
          className="min-h-0"
          collapsedSize="39px"
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
  );
}

function ProjectSidebarSection({ action, children, collapsed, icon, label, onToggle }: {
  action?: ReactNode;
  children: ReactNode;
  collapsed: boolean;
  icon: ReactNode;
  label: string;
  onToggle: () => void;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <div className="flex h-[39px] shrink-0 items-center hover:bg-accent dark:hover:bg-white/[0.05]">
        <button className="flex h-full min-w-0 flex-1 items-center gap-1.5 px-2 text-left text-xs font-medium" type="button" onClick={onToggle}>
          <ChevronDown className={cn("size-4 shrink-0 transition-transform", collapsed && "-rotate-90")} />
          <span className="[&>svg]:size-4 [&>svg]:text-muted-foreground">{icon}</span>
          <span className="min-w-0 flex-1 truncate">{label}</span>
        </button>
        {action ? <div className="mr-2 shrink-0">{action}</div> : null}
      </div>
      {!collapsed ? <div className="min-h-0 flex-1 overflow-auto border-t">{children}</div> : null}
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
