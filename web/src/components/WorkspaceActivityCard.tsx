import { Globe } from "@/components/icons";

import { BrowserFavicon } from "@/browser/BrowserFavicon";
import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import { ShellActionButton } from "@/components/ShellActionButton";
import { useI18n } from "@/i18n";
import { openBrowserReveal } from "@/state/browserRevealStore";
import { openCanvasReveal } from "@/state/canvasRevealStore";
import type { WorkspaceActivity } from "@/state/workspaceActivityStore";
import { cn } from "@/lib/utils";

export function WorkspaceActivityCard({
  activities,
  presentation = "rail",
}: {
  activities: WorkspaceActivity[];
  presentation?: "composer" | "rail";
}) {
  const { t } = useI18n();

  if (activities.length === 0) {
    return null;
  }

  return (
    <div
      aria-label={t("workspace.recentArtifacts")}
      className={cn(
        "pointer-events-auto flex min-w-0 gap-0.5 rounded-xl border border-border/70 bg-popover/95 text-popover-foreground shadow-lg backdrop-blur-md",
        presentation === "composer"
          ? "w-fit max-w-full overflow-x-auto rounded-lg p-0.5 shadow-sm overscroll-x-contain"
          : "w-full flex-col overflow-hidden",
      )}
      role="group"
    >
      {activities.map((activity) => (
        <WorkspaceActivityRow
          key={`${activity.kind}:${activity.resourceID || activity.serial}`}
          activity={activity}
          presentation={presentation}
          onOpen={() => openWorkspaceActivity(activity)}
        />
      ))}
    </div>
  );
}

function WorkspaceActivityRow({
  activity,
  presentation,
  onOpen,
}: {
  activity: WorkspaceActivity;
  presentation: "composer" | "rail";
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
    <ShellActionButton
      aria-label={`${t("workspace.openArtifact")}: ${title}`}
      className={cn(
        "no-drag-region group justify-start text-left",
        presentation === "composer"
          ? "h-7 max-w-48 shrink-0 gap-1.5 rounded-md px-1.5 pr-2"
          : "h-auto min-h-11 w-full gap-2 rounded-lg px-2.5 py-2",
      )}
      size="sm"
      title={`${title} · ${detail}`}
      onClick={onOpen}
    >
      <WorkspaceActivityIcon activity={activity} presentation={presentation} />
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
  activity,
  presentation,
}: {
  activity: WorkspaceActivity;
  presentation: "composer" | "rail";
}) {
  if (activity.kind === "canvas") {
    return <CanvasKindIcon kind={activity.resourceKind} size={presentation === "composer" ? "xs" : "md"} />;
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
