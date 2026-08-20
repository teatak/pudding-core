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
  ArrowRight,
  ArrowUpDown,
  ChevronRight,
  FolderClosed,
  FolderOpen,
  FolderPlus,
  MessageCirclePlus,
  Package,
} from "@/components/icons";
import { ProjectActionsMenu } from "@/components/ProjectActionsMenu";
import type { SessionGroupActivity } from "@/components/session-rail/activity";
import type {
  ProjectSessionGroup,
  ProjectSortMode,
} from "@/components/session-rail/model";
import { useRailOverlayHold } from "@/components/session-rail/overlayHold";
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
  return (
    <Button
      className="relative mb-1 h-10 w-full justify-start gap-2 overflow-hidden px-2 font-normal disabled:opacity-100"
      disabled={downloading || installing}
      variant="secondary"
      onClick={downloaded ? restart : download}
    >
      <span className="truncate">
        {label}
        {available && state.version ? ` ${state.version}` : ""}
      </span>
      {downloading ? (
        <>
          <Progress aria-label={label} className="ml-auto h-1.5 w-24 shrink-0 bg-primary/10" value={state.percent ?? 0} />
          <span className="w-8 shrink-0 text-right text-xs tabular-nums">{state.percent ?? 0}%</span>
        </>
      ) : installing ? (
        <Spinner className="ml-auto size-4 shrink-0" />
      ) : (
        <ArrowRight className="ml-auto size-4 shrink-0" />
      )}
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
      data-project-dragging={dragging || undefined}
      className={cn(
        "group/project-label relative h-8 min-h-8 gap-1 px-0 text-sm",
        !interactionDisabled && "hover:bg-sidebar-accent has-[:focus-visible]:bg-sidebar-accent has-[:focus-visible]:text-sidebar-accent-foreground has-[[data-project-actions-open=true]]:bg-sidebar-accent has-[[data-project-actions-open=true]]:text-sidebar-accent-foreground",
        icon === "project" && "font-normal text-sidebar-foreground!",
        icon !== "project" && "font-normal text-sidebar-foreground/45!",
        dragging && "opacity-45",
        active && "text-sidebar-accent-foreground",
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
            <button
              aria-label={actionLabel || label}
              className="flex size-6 shrink-0 items-center justify-center rounded-md text-sidebar-foreground/60 opacity-0 transition-opacity hover:bg-sidebar-accent hover:text-sidebar-accent-foreground group-hover/project-label:opacity-100 group-has-[[data-state=open]]/project-label:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-hidden"
              type="button"
              onClick={(event) => {
                event.stopPropagation();
                onAction();
              }}
            >
              <MessageCirclePlus className="size-3.5" />
            </button>
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
  const modeLabel =
    mode === "activity"
      ? t("project.sortRecentUsage")
      : mode === "name"
        ? t("project.sortNameAsc")
        : mode === "name-desc"
          ? t("project.sortNameDesc")
          : t("project.sortCustom");

  return (
    <SidebarGroup className="px-2 py-0">
      <SidebarGroupLabel
        className={cn(
          "group/project-section h-8 min-h-8 gap-1 px-0 text-sm font-normal text-sidebar-foreground/45!",
          !interactionDisabled && "hover:bg-sidebar-accent has-[:focus-visible]:bg-sidebar-accent has-[:focus-visible]:text-sidebar-accent-foreground",
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
              <DropdownMenuTrigger asChild>
                <button
                  aria-label={`${t("project.sortLabel")}：${modeLabel}`}
                  className="flex size-6 shrink-0 items-center justify-center rounded-md text-sidebar-foreground/55 opacity-0 outline-none transition-opacity hover:bg-sidebar-accent hover:text-sidebar-accent-foreground group-hover/project-section:opacity-100 group-has-[[data-state=open]]/project-section:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
                  type="button"
                >
                  <ArrowUpDown className="size-3.5" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuRadioGroup
                  value={mode}
                  onValueChange={(value) => onModeChange(value as ProjectSortMode)}
                >
                  <DropdownMenuRadioItem value="activity">
                    {t("project.sortRecentUsage")}
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="name">
                    {t("project.sortNameAsc")}
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="name-desc">
                    {t("project.sortNameDesc")}
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="custom">
                    {t("project.sortCustom")}
                  </DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  aria-label={t("project.add")}
                  className="flex size-6 shrink-0 items-center justify-center rounded-md text-sidebar-foreground/55 opacity-0 outline-none transition-opacity hover:bg-sidebar-accent hover:text-sidebar-accent-foreground group-hover/project-section:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sidebar-ring"
                  type="button"
                  onClick={onCreateProject}
                >
                  <FolderPlus className="size-3.5" />
                </button>
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
