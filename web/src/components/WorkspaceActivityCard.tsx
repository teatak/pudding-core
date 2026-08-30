import { Globe } from "@/components/icons";
import { Fragment, type ReactNode } from "react";

import { BrowserFavicon } from "@/browser/BrowserFavicon";
import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import { ShellActionButton } from "@/components/ShellActionButton";
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
  presentation?: "composer" | "rail";
}) {
  const { t } = useI18n();

  if (artifacts.length === 0) {
    return null;
  }
  return (
    <div
      aria-label={t("workspace.recentArtifacts")}
      className={cn(
        "pointer-events-auto flex min-w-0 gap-0.5 border border-border/70 bg-popover/95 text-popover-foreground shadow-lg backdrop-blur-md",
        presentation === "composer"
          ? "w-fit max-w-full overflow-x-auto rounded-lg p-0.5 shadow-sm overscroll-x-contain"
          : "w-full flex-col overflow-hidden rounded-xl py-1.5",
      )}
      role="group"
    >
      {artifacts.map((artifact) => {
        const key = `${artifact.kind}:${artifact.resourceID}`;
        const showPreview = presentation === "rail"
          && artifact.kind === "browser"
          && artifact.resourceID === browserPreview?.resourceID;
        return (
          <Fragment key={key}>
            <WorkspaceActivityRow
              artifact={artifact}
              presentation={presentation}
              onOpen={() => openWorkspaceArtifact(artifact)}
            />
            {showPreview ? <div className="mx-1.5">{browserPreview?.content}</div> : null}
          </Fragment>
        );
      })}
    </div>
  );
}

function WorkspaceActivityRow({
  artifact,
  presentation,
  onOpen,
}: {
  artifact: WorkspaceArtifact;
  presentation: "composer" | "rail";
  onOpen: () => void;
}) {
  const { t } = useI18n();
  const title = artifact.title?.trim()
    || (artifact.kind === "browser" ? browserHost(artifact.url) : "")
    || t(artifact.kind === "browser" ? "browser.noTitle" : "canvas.untitled");
  const detail = artifact.kind === "browser"
    ? browserHost(artifact.url) || t("workspace.activity.browser")
    : t(canvasKindKey(artifact.resourceKind));

  return (
    <ShellActionButton
      aria-label={`${t("workspace.openArtifact")}: ${title}`}
      className={cn(
        "no-drag-region group justify-start text-left",
        presentation === "composer"
          ? "h-7 max-w-48 shrink-0 gap-1.5 rounded-md px-1.5 pr-2"
          : "mx-1.5 h-auto min-h-11 w-auto self-stretch gap-2 rounded-lg px-2.5 py-2",
      )}
      size="sm"
      onClick={onOpen}
    >
      <WorkspaceActivityIcon artifact={artifact} presentation={presentation} />
      {presentation === "composer" ? (
        <span className="min-w-0 truncate text-xs font-medium">{title}</span>
      ) : (
        <span className="min-w-0 flex-1">
          <span className="block truncate text-xs font-medium">{title}</span>
          <span className="block truncate text-[11px] text-muted-foreground">{detail}</span>
        </span>
      )}
    </ShellActionButton>
  );
}

function WorkspaceActivityIcon({
  artifact,
  presentation,
}: {
  artifact: WorkspaceArtifact;
  presentation: "composer" | "rail";
}) {
  if (artifact.kind === "canvas") {
    return <CanvasKindIcon kind={artifact.resourceKind} size={presentation === "composer" ? "xs" : "md"} />;
  }
  return (
    <span
      className={cn(
        "grid shrink-0 place-items-center overflow-hidden bg-info/15 text-info",
        presentation === "composer"
          ? "size-(--workspace-toolbar-tab-icon) rounded-[5px]"
          : "size-6 rounded-md",
      )}
    >
      <BrowserFavicon
        className="size-full rounded-sm object-cover"
        fallback={<Globe className="size-3.5" />}
        faviconURL={artifact.faviconURL}
        pageURL={artifact.url || ""}
      />
    </span>
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
