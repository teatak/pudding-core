import { Globe } from "lucide-react";

import { BrowserFavicon } from "@/browser/BrowserFavicon";
import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import { ChatColumn } from "@/components/ChatColumn";
import { useI18n } from "@/i18n";
import { openBrowserReveal } from "@/state/browserRevealStore";
import { openCanvasReveal } from "@/state/canvasRevealStore";
import type { WorkspaceActivity } from "@/state/workspaceActivityStore";

export function WorkspaceActivityCard({ activities }: { activities: WorkspaceActivity[] }) {
  const { t } = useI18n();

  if (activities.length === 0) {
    return null;
  }

  return (
    <aside
      aria-label={t("workspace.recentArtifacts")}
      className="pointer-events-none absolute inset-x-0 top-0 z-30 h-9"
    >
      <ChatColumn className="flex h-full items-center justify-center">
        <div className="pointer-events-auto flex w-fit max-w-full min-w-0 gap-0.5 overflow-x-auto rounded-lg border border-border/70 bg-popover/95 p-0.5 text-popover-foreground shadow-sm backdrop-blur-md overscroll-x-contain">
          {activities.map((activity) => (
            <WorkspaceActivityRow
              key={`${activity.kind}:${activity.resourceID || activity.serial}`}
              activity={activity}
              onOpen={() => openWorkspaceActivity(activity)}
            />
          ))}
        </div>
      </ChatColumn>
    </aside>
  );
}

function WorkspaceActivityRow({
  activity,
  onOpen,
}: {
  activity: WorkspaceActivity;
  onOpen: () => void;
}) {
  const { t } = useI18n();
  const title = activity.title?.trim()
    || (activity.kind === "browser" ? browserHost(activity.url) : "")
    || t(activity.kind === "browser" ? "browser.noTitle" : "canvas.untitled");
  const detail = activity.kind === "browser"
    ? browserHost(activity.url) || t("workspace.activity.browser")
    : t(canvasKindKey(activity.resourceKind));

  return (
    <button
      aria-label={`${t("workspace.openArtifact")}: ${title}`}
      className="no-drag-region group flex h-7 max-w-48 shrink-0 items-center gap-1.5 rounded-md px-1.5 pr-2 text-left transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      title={`${title} · ${detail}`}
      type="button"
      onClick={onOpen}
    >
      <WorkspaceActivityIcon activity={activity} />
      <span className="min-w-0 truncate text-xs font-medium">{title}</span>
    </button>
  );
}

function WorkspaceActivityIcon({ activity }: { activity: WorkspaceActivity }) {
  if (activity.kind === "canvas") {
    return <CanvasKindIcon kind={activity.resourceKind} size="xs" />;
  }
  return (
    <span className="grid size-(--workspace-toolbar-tab-icon) shrink-0 place-items-center overflow-hidden rounded-[5px] bg-blue-500/15 text-blue-600 dark:text-blue-300">
      <BrowserFavicon
        className="size-full rounded-sm object-cover"
        fallback={<Globe className="size-3.5" />}
        faviconURL={activity.faviconURL}
        pageURL={activity.url || ""}
      />
    </span>
  );
}

function openWorkspaceActivity(activity: WorkspaceActivity) {
  if (activity.kind === "browser") {
    openBrowserReveal(activity.sessionID, activity.resourceID);
    return;
  }
  openCanvasReveal(activity.sessionID, activity.resourceID);
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
