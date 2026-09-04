import { Fragment, useState, type ReactNode } from "react";

import { BrowserTabIcon } from "@/browser/BrowserTabIcon";
import { browserPageFaviconURL, browserPageTitle } from "@/browser/helpers";
import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import { ChevronDown, ChevronUp } from "@/components/icons";
import { ShellActionButton } from "@/components/ShellActionButton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { WorkspaceArtifact } from "@/components/workspace/types";
import { useI18n } from "@/i18n";
import { openBrowserReveal } from "@/state/browserRevealStore";
import { openCanvasReveal } from "@/state/canvasRevealStore";
import { cn } from "@/lib/utils";

export function WorkspaceActivityCard({
  artifacts,
  browserPreview,
  presentation = "rail",
}: {
  artifacts: WorkspaceArtifact[];
  browserPreview?: {
    content: ReactNode;
    resourceID: string;
  };
  presentation?: "dock" | "rail";
}) {
  const { t } = useI18n();
  const [dockCollapsed, setDockCollapsed] = useState(false);

  if (artifacts.length === 0) {
    return null;
  }
  return (
    <div
      aria-label={t("workspace.recentArtifacts")}
      className={cn(
        "pointer-events-auto flex min-w-0 gap-0.5 border border-border/70 bg-popover/95 text-popover-foreground shadow-lg backdrop-blur-md",
        presentation === "dock"
          ? "max-h-full w-10 flex-col overflow-hidden rounded-xl p-1 shadow-sm"
          : "w-full flex-col overflow-hidden rounded-xl py-1.5",
      )}
      role="group"
    >
      {presentation === "dock" ? (
        <DockCollapseButton
          collapsed={dockCollapsed}
          collapseLabel={t("workspace.collapseArtifacts")}
          expandLabel={t("workspace.expandArtifacts")}
          onToggle={() => setDockCollapsed((collapsed) => !collapsed)}
        />
      ) : null}
      {presentation === "dock" && !dockCollapsed ? (
        <div className="flex min-h-0 flex-col gap-0.5 overflow-x-hidden overflow-y-auto overscroll-y-contain border-t border-border/70 pt-1">
          {artifacts.map((artifact) => (
            <WorkspaceActivityRow
              key={`${artifact.kind}:${artifact.resourceID}`}
              artifact={artifact}
              presentation="dock"
              onOpen={() => openWorkspaceArtifact(artifact)}
            />
          ))}
        </div>
      ) : null}
      {presentation === "rail" ? artifacts.map((artifact) => {
        const key = `${artifact.kind}:${artifact.resourceID}`;
        const showPreview = artifact.kind === "browser"
          && artifact.resourceID === browserPreview?.resourceID;
        return (
          <Fragment key={key}>
            <WorkspaceActivityRow
              artifact={artifact}
              presentation="rail"
              onOpen={() => openWorkspaceArtifact(artifact)}
            />
            {showPreview ? <div className="mx-1.5">{browserPreview?.content}</div> : null}
          </Fragment>
        );
      }) : null}
    </div>
  );
}

function DockCollapseButton({
  collapsed,
  collapseLabel,
  expandLabel,
  onToggle,
}: {
  collapsed: boolean;
  collapseLabel: string;
  expandLabel: string;
  onToggle: () => void;
}) {
  const label = collapsed ? expandLabel : collapseLabel;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <ShellActionButton
          aria-expanded={!collapsed}
          aria-label={label}
          className="no-drag-region h-4 w-8 shrink-0 justify-center rounded-sm p-0"
          size="sm"
          onClick={onToggle}
        >
          {collapsed ? <ChevronDown className="size-3" /> : <ChevronUp className="size-3" />}
        </ShellActionButton>
      </TooltipTrigger>
      <TooltipContent side="left" sideOffset={6}>{label}</TooltipContent>
    </Tooltip>
  );
}

function WorkspaceActivityRow({
  artifact,
  presentation,
  onOpen,
}: {
  artifact: WorkspaceArtifact;
  presentation: "dock" | "rail";
  onOpen: () => void;
}) {
  const { t } = useI18n();
  const title = artifact.kind === "browser"
    ? browserPageTitle(artifact.title, artifact.url, t("browser.newTab"), t("browser.newTab"))
    : artifact.title?.trim() || t("canvas.untitled");
  const detail = artifact.kind === "browser"
    ? browserHost(artifact.url) || t("workspace.activity.browser")
    : t(canvasKindKey(artifact.resourceKind));

  const button = (
    <ShellActionButton
      aria-label={`${t("workspace.openArtifact")}: ${title}`}
      className={cn(
        "no-drag-region group justify-start text-left",
        presentation === "dock"
          ? "size-8 shrink-0 justify-center rounded-lg p-1"
          : "mx-1.5 h-auto min-h-11 w-auto self-stretch gap-2 rounded-lg px-2.5 py-2",
      )}
      size="sm"
      onClick={onOpen}
    >
      <WorkspaceActivityIcon artifact={artifact} presentation={presentation} />
      {presentation === "rail" ? (
        <span className="min-w-0 flex-1">
          <span className="block truncate text-xs font-medium">{title}</span>
          <span className="block truncate text-[11px] text-muted-foreground">{detail}</span>
        </span>
      ) : null}
    </ShellActionButton>
  );
  if (presentation === "dock") {
    return (
      <Tooltip>
        <TooltipTrigger asChild>{button}</TooltipTrigger>
        <TooltipContent side="left" sideOffset={6}>{title}</TooltipContent>
      </Tooltip>
    );
  }
  return button;
}

function WorkspaceActivityIcon({
  artifact,
  presentation,
}: {
  artifact: WorkspaceArtifact;
  presentation: "dock" | "rail";
}) {
  if (artifact.kind === "canvas") {
    return <CanvasKindIcon kind={artifact.resourceKind} size={presentation === "dock" ? "xs" : "md"} />;
  }
  return (
    <BrowserTabIcon
      className={cn(
        presentation === "dock"
          ? "size-(--workspace-toolbar-tab-icon) rounded-[5px]"
          : "size-6 rounded-md",
      )}
      faviconURL={browserPageFaviconURL(artifact.faviconURL, artifact.url)}
      pageURL={artifact.url || ""}
    />
  );
}

function openWorkspaceArtifact(artifact: WorkspaceArtifact) {
  if (artifact.kind === "browser") {
    openBrowserReveal(artifact.sessionID, artifact.resourceID);
    return;
  }
  openCanvasReveal(artifact.sessionID, artifact.resourceID);
}

function browserHost(url: string | undefined) {
  if (!url || url === "about:blank") {
    return "";
  }
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
}

function canvasKindKey(kind: string) {
  const keys: Record<string, string> = {
    chart: "workspace.activity.chart",
    gallery: "workspace.activity.gallery",
    grid: "workspace.activity.grid",
    markdown: "workspace.activity.markdown",
    metric: "workspace.activity.metric",
    table: "workspace.activity.table",
    timeline: "workspace.activity.timeline",
  };
  return keys[kind] || "workspace.activity.canvas";
}
