import {
  type DragEvent as ReactDragEvent,
  type RefObject,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

import type { Project } from "@/api/client";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuRadioItem as DropdownMenuRadioItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
} from "@/components/AppMenu";
import {
  ArrowUpDown,
  ChevronRight,
  Download,
  FolderClosed,
  FolderOpen,
  FolderPlus,
  MessageCirclePlus,
  Package,
  RotateCcw,
} from "@/components/icons";
import { ProjectActionsMenu } from "@/components/ProjectActionsMenu";
import type { SessionGroupActivity } from "@/components/session-rail/activity";
import type {
  ProjectSessionGroup,
  ProjectSortMode,
} from "@/components/session-rail/model";
import { useRailOverlayHold } from "@/components/session-rail/overlayHold";
import { RailIconAction } from "@/components/session-rail/RailIconAction";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent } from "@/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuRadioGroup,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Progress } from "@/components/ui/progress";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import {
  type DesktopUpdateState,
  activateDesktopUpdate,
  downloadDesktopUpdate,
  getDesktopUpdateState,
  onDesktopUpdateState,
} from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";
import { isTurnPhaseActive, useOverlayStore } from "@/state/overlayStore";

export function dragPreviewTransform(clientX: number, clientY: number) {
  return `translate3d(${clientX + 12}px, ${clientY}px, 0) translateY(-50%)`;
}

export function RailUpdateButton({ serverTurnRunning }: { serverTurnRunning: boolean }) {
  const { t } = useI18n();
  const [state, setState] = useState<DesktopUpdateState | null>(null);
  const handledDownloadedVersionRef = useRef("");
  const runningTurns = useOverlayStore((current) => current.runningTurns);
  const turnPhases = useOverlayStore((current) => current.turnPhases);

  useEffect(() => {
    let active = true;
    let receivedEvent = false;
    const unsubscribe = onDesktopUpdateState((next) => {
      if (active) {
        receivedEvent = true;
        setState(next);
      }
    });
    void getDesktopUpdateState().then((next) => {
      if (active && !receivedEvent) {
        setState(next);
      }
    });
    return () => {
      active = false;
      unsubscribe();
    };
  }, []);

  const hasActiveTurn =
    serverTurnRunning ||
    Object.values(runningTurns).some(Boolean) ||
    Object.values(turnPhases).some((phase) => isTurnPhaseActive(phase));
  const downloaded = state?.status === "downloaded";

  useEffect(() => {
    if (!downloaded) {
      handledDownloadedVersionRef.current = "";
      return;
    }
    const version = state?.version || "downloaded";
    if (handledDownloadedVersionRef.current === version || hasActiveTurn) {
      return;
    }
    handledDownloadedVersionRef.current = version;
    void activateDesktopUpdate();
  }, [downloaded, hasActiveTurn, state?.version]);

  if (
    state?.status !== "available" &&
    state?.status !== "downloading" &&
    state?.status !== "downloaded" &&
    state?.status !== "installing"
  ) {
    return null;
  }
  const available = state.status === "available";
  const downloading = state.status === "downloading";
  const installing = state.status === "installing";
  const download = () => void downloadDesktopUpdate();
  const restart = () => void activateDesktopUpdate();
  const label = available
    ? t("update.update")
    : downloading
      ? t("update.downloading")
      : t(installing ? "update.restarting" : "update.restart");
  const content = (
    <>
      <span className="truncate">
        {label}
        {available && state.version ? ` ${state.version}` : ""}
      </span>
      {downloading ? (
        <>
          <Progress
            aria-label={label}
            className="h-1 w-14 shrink-0 bg-foreground/10 dark:bg-foreground/20"
            value={state.percent ?? 0}
          />
          <span className="w-8 shrink-0 text-right text-xs tabular-nums">{state.percent ?? 0}%</span>
        </>
      ) : installing ? (
        <Spinner className="size-4 shrink-0" />
      ) : available ? (
        <Download className="size-4 shrink-0" />
      ) : (
        <RotateCcw className="size-4 shrink-0" />
      )}
    </>
  );
  const className =
    "relative ml-auto h-8 min-w-0 items-center justify-start gap-2 overflow-hidden rounded-md px-2 text-[13px] font-normal";
  if (downloading || installing) {
    return <div className={cn("flex", className)}>{content}</div>;
  }
  return (
    <Button className={cn(className, "w-auto max-w-full shrink-0")} variant="ghost" onClick={downloaded ? restart : download}>
      {content}
    </Button>
  );
}

export function SessionDragPreview({
  point,
  preview,
  previewRef,
}: {
  point: {
    x: number;
    y: number;
  };
  preview: {
    title: string;
    x: number;
    y: number;
  };
  previewRef: RefObject<HTMLDivElement | null>;
}) {
  if (typeof document === "undefined") {
    return null;
  }
  return createPortal(
    <div
      ref={previewRef}
      aria-hidden="true"
      className="pointer-events-none fixed top-0 left-0 z-50 max-w-[220px] select-none rounded-md bg-popover px-3 py-2 text-sm text-popover-foreground shadow-lg ring-1 ring-border/70 will-change-transform"
      style={{
        transform: dragPreviewTransform(point.x || preview.x, point.y || preview.y),
      }}
    >
      <div className="truncate">{preview.title}</div>
    </div>,
    document.body,
  );
}

export function CollapsibleSessionGroupLabel({
  active = false,
  activity,
  collapsed,
  dropTargetActive = false,
  interactionDisabled = false,
  icon,
  label,
  actions,
  actionLabel,
  reorderable = false,
  dragging = false,
  onAction,
  onDragEnd,
  onDragStart,
  onToggle,
}: {
  active?: boolean;
  activity?: SessionGroupActivity;
  collapsed: boolean;
  dropTargetActive?: boolean;
  interactionDisabled?: boolean;
  icon: "chat" | "pinned" | "project";
  label: string;
  actions?: ReactNode;
  actionLabel?: string;
  reorderable?: boolean;
  dragging?: boolean;
  onAction?: () => void;
  onDragEnd?: () => void;
  onDragStart?: (event: ReactDragEvent<HTMLButtonElement>) => void;
  onToggle: () => void;
}) {
  const { t } = useI18n();
  const Icon = collapsed ? FolderClosed : FolderOpen;
  return (
    <SidebarGroupLabel
      data-active={active}
      data-project-menu-context-target={actions ? "true" : undefined}
      data-project-dragging={dragging || undefined}
      className={cn(
        "group/project-label relative gap-1 px-0",
        icon === "project" ? "h-8 min-h-8 text-sm" : "h-7 min-h-7 text-[13px]",
        !interactionDisabled && "has-[:focus-visible]:bg-sidebar-accent has-[:focus-visible]:text-sidebar-accent-foreground has-[[data-project-actions-open=true]]:bg-sidebar-accent has-[[data-project-actions-open=true]]:text-sidebar-accent-foreground",
        !interactionDisabled && icon === "project" && "hover:bg-sidebar-accent",
        icon === "project" && "font-normal text-sidebar-foreground!",
        icon !== "project" && "font-normal text-sidebar-foreground/45!",
        dragging && "opacity-45",
        active && "bg-[var(--sidebar-selected-background)] text-sidebar-accent-foreground",
        dropTargetActive && "pudding-session-drop-project-active",
      )}
    >
      <button
        className="group flex h-full min-w-0 flex-1 cursor-default items-center gap-2 rounded-md px-2 pr-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-sidebar-ring"
        draggable={reorderable}
        type="button"
        onDragEnd={onDragEnd}
        onDragStart={onDragStart}
        onClick={interactionDisabled ? undefined : onToggle}
      >
        {icon === "project" ? (
          <Icon
            className="size-4 shrink-0 text-sidebar-foreground/80"
          />
        ) : null}
        <span className="min-w-0 truncate">{label}</span>
        {!dropTargetActive && !interactionDisabled ? (
          <ChevronRight
            className={cn(
              "size-3.5 shrink-0 opacity-0 transition-[opacity,transform] group-hover:opacity-100 group-focus-visible:opacity-100",
              !collapsed && "rotate-90",
            )}
          />
        ) : null}
      </button>
      {!dropTargetActive && !interactionDisabled ? actions : null}
      {onAction && !dropTargetActive && !interactionDisabled ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <RailIconAction
              aria-label={actionLabel || label}
              className="mr-1 group-hover/project-label:opacity-100 group-has-[[data-state=open]]/project-label:opacity-100"
              onClick={(event) => {
                event.stopPropagation();
                onAction();
              }}
            >
              <MessageCirclePlus className="size-3.5" />
            </RailIconAction>
          </TooltipTrigger>
          <TooltipContent side="right">{actionLabel || label}</TooltipContent>
        </Tooltip>
      ) : null}
      {activity && !dropTargetActive && !interactionDisabled ? (
        <span
          aria-label={activity === "running" ? t("session.processing") : t("session.completed")}
          className="flex size-6 shrink-0 items-center justify-center text-sidebar-foreground/55"
        >
          {activity === "running" ? (
            <Spinner className="size-3" />
          ) : (
            <span aria-hidden="true" className="size-2 rounded-full bg-blue-500" />
          )}
        </span>
      ) : null}
    </SidebarGroupLabel>
  );
}

export function ProjectSortHeader({
  collapsed,
  interactionDisabled = false,
  mode,
  onCreateProject,
  onModeChange,
  onToggle,
}: {
  collapsed: boolean;
  interactionDisabled?: boolean;
  mode: ProjectSortMode;
  onCreateProject: () => void;
  onModeChange: (mode: ProjectSortMode) => void;
  onToggle: () => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  useRailOverlayHold(open);
  const sortOptions: Array<{ value: ProjectSortMode; label: string }> = [
    { value: "activity", label: t("project.sortRecentUsage") },
    { value: "name", label: t("project.sortNameAsc") },
    { value: "name-desc", label: t("project.sortNameDesc") },
    { value: "custom", label: t("project.sortCustom") },
  ];
  const modeLabel = sortOptions.find((option) => option.value === mode)?.label || t("project.sortCustom");

  const sortButton = (
    <RailIconAction
      aria-expanded={open}
      aria-haspopup="menu"
      aria-label={`${t("project.sortLabel")}：${modeLabel}`}
      className="text-sidebar-foreground/55 group-hover/project-section:opacity-100 group-has-[[data-state=open]]/project-section:opacity-100"
      data-state={open ? "open" : "closed"}
      onContextMenu={(event) => {
        event.preventDefault();
        event.stopPropagation();
        event.currentTarget.click();
      }}
    >
      <ArrowUpDown className="size-3.5" />
    </RailIconAction>
  );

  return (
    <SidebarGroup className="px-2 py-0">
      <SidebarGroupLabel
        className={cn(
          "group/project-section h-7 min-h-7 gap-1 px-0 text-[13px] font-normal text-sidebar-foreground/45!",
          !interactionDisabled && "has-[:focus-visible]:bg-sidebar-accent has-[:focus-visible]:text-sidebar-accent-foreground has-[[data-state=open]]:text-sidebar-accent-foreground",
        )}
      >
        <button
          className="group flex h-full min-w-0 flex-1 cursor-default items-center gap-1 rounded-md px-2 pr-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-sidebar-ring"
          type="button"
          onClick={interactionDisabled ? undefined : onToggle}
        >
          <span className="min-w-0 truncate">{t("project.manage")}</span>
          {!interactionDisabled ? (
            <ChevronRight
              className={cn(
                "size-3.5 shrink-0 opacity-0 transition-[opacity,transform] group-hover:opacity-100 group-focus-visible:opacity-100",
                !collapsed && "rotate-90",
              )}
            />
          ) : null}
        </button>
        {!interactionDisabled ? (
          <>
            <DropdownMenu open={open} onOpenChange={setOpen}>
              <DropdownMenuTrigger asChild>{sortButton}</DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuRadioGroup
                  value={mode}
                  onValueChange={(value) => onModeChange(value as ProjectSortMode)}
                >
                  {sortOptions.map((option) => (
                    <DropdownMenuRadioItem key={option.value} value={option.value}>
                      {option.label}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <RailIconAction
                  aria-label={t("project.add")}
                  className="mr-1 text-sidebar-foreground/55 group-hover/project-section:opacity-100 group-has-[[data-state=open]]/project-section:opacity-100"
                  onClick={onCreateProject}
                >
                  <FolderPlus className="size-3.5" />
                </RailIconAction>
              </TooltipTrigger>
              <TooltipContent side="right">{t("project.add")}</TooltipContent>
            </Tooltip>
          </>
        ) : null}
      </SidebarGroupLabel>
    </SidebarGroup>
  );
}

export function RailProjectActionsMenu({
  homeDirectory,
  project,
  token,
}: {
  homeDirectory: string;
  project: Project;
  token: string;
}) {
  const [overlayOpen, setOverlayOpen] = useState(false);

  useRailOverlayHold(overlayOpen);
  return (
    <ProjectActionsMenu
      project={project}
      surface="sidebar"
      token={token}
      homeDirectory={homeDirectory}
      onOverlayOpenChange={setOverlayOpen}
    />
  );
}
