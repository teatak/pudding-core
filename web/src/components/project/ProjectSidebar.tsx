import { ChevronDown, Folders, GitBranch } from "@/components/icons";
import { useState, type ReactNode } from "react";
import { usePanelRef } from "react-resizable-panels";

import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useI18n } from "@/i18n";
import { layoutStorageKeys } from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { cn } from "@/lib/utils";

const collapsedPanelPixels = 31;

export function ProjectSidebar({ files, filesAction, git }: {
  files: ReactNode;
  filesAction?: ReactNode;
  git?: ReactNode;
}) {
  const { t } = useI18n();
  const filesRef = usePanelRef();
  const gitRef = usePanelRef();
  const [filesCollapsed, setFilesCollapsed] = useState(false);
  const [gitCollapsed, setGitCollapsed] = useState(false);

  if (!git) {
    return (
      <ResizablePanelGroup className="h-full min-h-0 bg-[var(--workspace-chrome-background)]" orientation="vertical">
        <ResizablePanel
          id="files"
          className="min-h-0"
          collapsedSize="31px"
          collapsible
          minSize="120px"
          panelRef={filesRef}
          onResize={({ inPixels }) => setFilesCollapsed(inPixels <= collapsedPanelPixels + 1)}
        >
          <ProjectSidebarSection
            action={filesAction}
            collapsed={filesCollapsed}
            icon={<Folders />}
            label={t("project.browserFiles")}
            topBorder={false}
            onToggle={() => togglePanel(filesRef.current, filesCollapsed)}
          >
            {files}
          </ProjectSidebarSection>
        </ResizablePanel>
      </ResizablePanelGroup>
    );
  }

  return (
    <ResizablePanelGroup
      className="h-full min-h-0 bg-[var(--workspace-chrome-background)]"
      defaultLayout={readPanelLayout(layoutStorageKeys.projectSidebarRatio, { files: 62, git: 38 }, { minPercent: 4, maxPercent: 96 })}
      id="project-sidebar-layout"
      orientation="vertical"
      onLayoutChanged={(layout) => savePanelLayout(layoutStorageKeys.projectSidebarRatio, layout)}
    >
        <ResizablePanel
          id="files"
          className="min-h-0"
          collapsedSize="31px"
          collapsible
          minSize="120px"
          panelRef={filesRef}
          onResize={({ inPixels }) => {
            setFilesCollapsed(inPixels <= collapsedPanelPixels + 1);
          }}
        >
          <ProjectSidebarSection
            action={filesAction}
            collapsed={filesCollapsed}
            icon={<Folders />}
            label={t("project.browserFiles")}
            topBorder={false}
            onToggle={() => togglePanel(filesRef.current, filesCollapsed)}
          >
            {files}
          </ProjectSidebarSection>
        </ResizablePanel>
        <ResizableHandle className="pudding-project-sidebar-handle" />
        <ResizablePanel
          id="git"
          className="min-h-0"
          collapsedSize="31px"
          collapsible
          minSize="120px"
          panelRef={gitRef}
          onResize={({ inPixels }) => {
            setGitCollapsed(inPixels <= collapsedPanelPixels + 1);
          }}
        >
          <ProjectSidebarSection collapsed={gitCollapsed} icon={<GitBranch />} label={t("project.git")} onToggle={() => togglePanel(gitRef.current, gitCollapsed)}>
            {git}
          </ProjectSidebarSection>
        </ResizablePanel>
    </ResizablePanelGroup>
  );
}

function ProjectSidebarSection({ action, children, collapsed, icon, label, topBorder = true, onToggle }: {
  action?: ReactNode;
  children: ReactNode;
  collapsed: boolean;
  icon: ReactNode;
  label: string;
  topBorder?: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <div className={cn(
        "flex h-[calc(var(--workspace-subtoolbar-h)-1px)] shrink-0 items-center hover:bg-[var(--workspace-tree-hover-background)]",
        topBorder && "border-t border-[var(--workspace-border-subtle)]",
      )}>
        <button className="flex h-full min-w-0 flex-1 items-center gap-1 pl-[7px] pr-2 text-left text-xs font-medium" type="button" onClick={onToggle}>
          <ChevronDown className={cn("size-3.5 shrink-0 transition-transform", collapsed && "-rotate-90")} />
          <span className="[&>svg]:size-4 [&>svg]:text-muted-foreground">{icon}</span>
          <span className="min-w-0 flex-1 truncate">{label}</span>
        </button>
        {action ? <div className="mr-2 shrink-0">{action}</div> : null}
      </div>
      {!collapsed ? <div className="min-h-0 flex-1 overflow-auto border-t border-[var(--workspace-border-subtle)] bg-[var(--workspace-tree-background)]">{children}</div> : null}
    </div>
  );
}

function togglePanel(
  panel: ReturnType<typeof usePanelRef>["current"],
  collapsed: boolean,
) {
  if (!panel) return;
  if (collapsed) {
    panel.expand();
    return;
  }
  panel.collapse();
}
