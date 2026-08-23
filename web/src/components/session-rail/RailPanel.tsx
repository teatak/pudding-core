import { useNavigate } from "@tanstack/react-router";
import {
  type DragEvent as ReactDragEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import type { Project, Session } from "@/api/client";
import {
  Folders,
  MessageCirclePlus,
  Package,
  Search,
  Settings,
} from "@/components/icons";
import { isSessionTurnRunning, sessionGroupActivity } from "@/components/session-rail/activity";
import {
  CollapsibleSessionGroupLabel,
  dragPreviewTransform,
  ProjectSortHeader,
  RailProjectActionsMenu,
  RailUpdateButton,
  SessionDragPreview,
} from "@/components/session-rail/RailControls";
import {
  groupProjectSessions,
  normalizeProjectOrder,
  readCollapsedSessionGroups,
  readCustomProjectOrder,
  readProjectSortMode,
  sortPinnedSessions,
  sortProjectGroups,
  sortSessionsByActivity,
  type ProjectSortMode,
  writeCollapsedSessionGroups,
  writeCustomProjectOrder,
  writeProjectSortMode,
} from "@/components/session-rail/model";
import { RailOverlayHoldContext } from "@/components/session-rail/overlayHold";
import { ShellActionButton } from "@/components/ShellActionButton";
import {
  SessionEmptyState,
  SessionItems,
  SessionListError,
  SessionListSkeleton,
} from "@/components/session-rail/SessionItems";
import { SessionProjectPickerDialog } from "@/components/session-rail/SessionProjectPickerDialog";
import { Collapsible, CollapsibleContent } from "@/components/ui/collapsible";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { useI18n } from "@/i18n";
import { getDesktopHomeDirectory } from "@/lib/desktopBridge";
import type { AppSearch } from "@/lib/route";
import { openSettingsDialog } from "@/lib/settingsDialog";
import { cn } from "@/lib/utils";
import { isTurnPhaseActive, useOverlayStore } from "@/state/overlayStore";

const dragAutoScrollEdgePx = 44;
const dragAutoScrollMaxStepPx = 14;

function handleVerticalMenuNavigation(event: ReactKeyboardEvent<HTMLElement>, selector: string) {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    return;
  }
  const target = event.target instanceof HTMLElement ? event.target.closest<HTMLElement>(selector) : null;
  if (!target || !event.currentTarget.contains(target)) {
    return;
  }
  const items = Array.from(event.currentTarget.querySelectorAll<HTMLElement>(selector)).filter(
    (item) => !item.matches(":disabled,[aria-disabled=true]"),
  );
  const currentIndex = items.indexOf(target);
  if (currentIndex < 0 || items.length < 2) {
    return;
  }
  event.preventDefault();
  const nextIndex =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? items.length - 1
        : event.key === "ArrowDown"
          ? (currentIndex + 1) % items.length
          : (currentIndex - 1 + items.length) % items.length;
  const next = items[nextIndex];
  next.focus({ preventScroll: true });
  next.scrollIntoView({ block: "nearest", inline: "nearest" });
}

type RailPanelProps = {
  token: string;
  sessions: Session[];
  projects: Project[];
  selectedSessionID: string | undefined;
  appsActive: boolean;
  projectsActive: boolean;
  isLoading: boolean;
  isError: boolean;
  draftActive: boolean;
  draftProjectID?: string;
  archivePending: boolean;
  projectChangePendingID: string | undefined;
  onCreate: () => void;
  onCreateProject: () => void;
  onSearch: () => void;
  onCreateProjectSession: (projectID: string) => void;
  onSelect: (id: string) => void;
  onOpenSplit: (id: string) => void;
  onArchive: (id: string) => void;
  onPinChange: (id: string, pinned: boolean, pinnedOrder?: number) => Promise<void>;
  onPlacementChange: (id: string, projectID: string) => Promise<void>;
  onProjectChange: (id: string, projectID: string) => Promise<void>;
  onRename: (id: string, title: string) => Promise<void>;
  onOverlayOpenChange?: (open: boolean) => void;
  onRefetch: () => void;
};

type SessionDropTarget =
  | { kind: "pinned"; index: number }
  | { kind: "recent" }
  | { kind: "project"; projectID: string };

type ProjectDropTarget = {
  index: number;
  top: number;
};

// 面板三段:新建 / 列表 / 脚部。四边间距由外层容器(aside / popover)统一给 8px,
// 内部不再叠加水平 margin,保证两种形态边缘视觉一致。
export function RailPanel({
  token,
  sessions,
  projects,
  selectedSessionID,
  appsActive,
  projectsActive,
  isLoading,
  isError,
  draftActive,
  draftProjectID,
  archivePending,
  projectChangePendingID,
  onCreate,
  onCreateProject,
  onSearch,
  onCreateProjectSession,
  onSelect,
  onOpenSplit,
  onArchive,
  onPinChange,
  onPlacementChange,
  onProjectChange,
  onRename,
  onOverlayOpenChange,
  onRefetch,
}: RailPanelProps) {
  const { t } = useI18n();
  const navigate = useNavigate({ from: "/" });
  const completedSessions = useOverlayStore((state) => state.completedSessions);
  const runningTurns = useOverlayStore((state) => state.runningTurns);
  const turnPhases = useOverlayStore((state) => state.turnPhases);
  const [draggingSessionID, setDraggingSessionID] = useState<string | null>(null);
  const [projectPickerSessionID, setProjectPickerSessionID] = useState<string | null>(null);
  const [dragTarget, setDragTarget] = useState<SessionDropTarget | null>(null);
  const [dragPreview, setDragPreview] = useState<{
    id: string;
    title: string;
    x: number;
    y: number;
  } | null>(null);
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(() => readCollapsedSessionGroups());
  const [projectSortMode, setProjectSortMode] = useState<ProjectSortMode>(() => readProjectSortMode());
  const [customProjectOrder, setCustomProjectOrder] = useState<string[]>(() => readCustomProjectOrder());
  const [draggingProjectKey, setDraggingProjectKey] = useState<string | null>(null);
  const [projectDropTarget, setProjectDropTarget] = useState<ProjectDropTarget | null>(null);
  const [optimisticPinnedOrder, setOptimisticPinnedOrder] = useState<string[] | null>(null);
  const [contentFade, setContentFade] = useState({ top: false, bottom: false });
  const [homeDirectory, setHomeDirectory] = useState("");
  const dragPreviewRef = useRef<HTMLDivElement | null>(null);
  const dragPreviewPointRef = useRef({ x: 0, y: 0 });
  const dragPreviewFrameRef = useRef<number | null>(null);
  const scrollContainerRef = useRef<HTMLDivElement | null>(null);
  const autoScrollFrameRef = useRef<number | null>(null);
  const autoScrollPointerRef = useRef<{ x: number; y: number } | null>(null);
  const optimisticPinnedIndexes = optimisticPinnedOrder === null
    ? null
    : new Map(optimisticPinnedOrder.map((sessionID, index) => [sessionID, index]));
  const displayedSessions = optimisticPinnedIndexes === null
    ? sessions
    : sessions.map((session) => {
        const optimisticIndex = optimisticPinnedIndexes.get(session.id);
        if (optimisticIndex !== undefined) {
          return { ...session, pinned: true, pinnedOrder: optimisticIndex + 1 };
        }
        return session.pinned ? { ...session, pinned: false, pinnedOrder: 0 } : session;
      });
  const pinnedSessions = sortPinnedSessions(displayedSessions.filter((session) => session.pinned));
  const unpinnedSessions = sortSessionsByActivity(displayedSessions.filter((session) => !session.pinned));
  const chatSessions = unpinnedSessions.filter((session) => !session.projectID);
  const groupedProjects = groupProjectSessions(
    projects,
    unpinnedSessions.filter((session) => Boolean(session.projectID)),
  );
  const projectGroups = sortProjectGroups(groupedProjects, projectSortMode, customProjectOrder);
  const projectNamesByID = new Map(projects.map((project) => [project.id, project.name]));
  const projectPickerSession = projectPickerSessionID
    ? displayedSessions.find((session) => session.id === projectPickerSessionID)
    : undefined;
  const showPinnedGroup = pinnedSessions.length > 0 || Boolean(draggingSessionID);
  const draggedSession = draggingSessionID
    ? displayedSessions.find((session) => session.id === draggingSessionID)
    : undefined;
  const draggedSessionRunning = Boolean(
    draggedSession && isSessionTurnRunning(draggedSession, runningTurns, turnPhases),
  );
  const recentDropEnabled = Boolean(
    draggedSession && !draggedSessionRunning && (draggedSession.pinned || draggedSession.projectID),
  );
  const pinnedCollapsed = collapsedGroups.has("pinned");
  const chatCollapsed = collapsedGroups.has("chat");
  const projectsCollapsed = collapsedGroups.has("projects");
  const onOverlayOpenChangeRef = useRef(onOverlayOpenChange);
  const overlayHoldIDsRef = useRef(new Set<string>());

  useEffect(() => {
    let current = true;
    void getDesktopHomeDirectory().then((directory) => {
      if (current) setHomeDirectory(directory);
    });
    return () => {
      current = false;
    };
  }, []);

  function toggleGroupCollapsed(groupID: string) {
    setCollapsedGroups((previous) => {
      const next = new Set(previous);
      if (next.has(groupID)) {
        next.delete(groupID);
      } else {
        next.add(groupID);
      }
      writeCollapsedSessionGroups(next);
      return next;
    });
  }

  function changeProjectSortMode(mode: ProjectSortMode) {
    if (mode === "custom") {
      setCustomProjectOrder((previous) => {
        const next = normalizeProjectOrder(projectGroups, previous);
        writeCustomProjectOrder(next);
        return next;
      });
    }
    setProjectSortMode(mode);
    writeProjectSortMode(mode);
  }

  function clearProjectDragState() {
    setDraggingProjectKey(null);
    setProjectDropTarget(null);
  }

  function handleProjectDragStart(event: ReactDragEvent<HTMLButtonElement>, key: string) {
    if (projectSortMode !== "custom") {
      event.preventDefault();
      return;
    }
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", key);
    setDraggingProjectKey(key);
    setProjectDropTarget(null);
  }

  function resolveProjectDropTarget(
    container: HTMLElement,
    clientY: number,
    sourceKey: string,
  ): ProjectDropTarget {
    const containerRect = container.getBoundingClientRect();
    const projectRows = Array.from(
      container.querySelectorAll<HTMLElement>("[data-project-group-key]"),
    ).filter((row) => row.dataset.projectGroupKey !== sourceKey);
    const nextRowIndex = projectRows.findIndex((row) => {
      const rect = row.getBoundingClientRect();
      return clientY < rect.top + rect.height / 2;
    });
    const index = nextRowIndex < 0 ? projectRows.length : nextRowIndex;
    const anchorRect = projectRows[index]?.getBoundingClientRect();
    const lastRect = projectRows.at(-1)?.getBoundingClientRect();

    return {
      index,
      top: anchorRect
        ? anchorRect.top - containerRect.top
        : lastRect
          ? lastRect.bottom - containerRect.top
          : 0,
    };
  }

  function handleProjectDragOver(event: ReactDragEvent<HTMLDivElement>) {
    if (!draggingProjectKey) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    const nextTarget = resolveProjectDropTarget(
      event.currentTarget,
      event.clientY,
      draggingProjectKey,
    );
    setProjectDropTarget((previous) =>
      previous?.index === nextTarget.index && Math.abs(previous.top - nextTarget.top) < 0.5
        ? previous
        : nextTarget,
    );
  }

  function handleProjectDragLeave(event: ReactDragEvent<HTMLDivElement>) {
    if (event.target !== event.currentTarget) {
      return;
    }
    const relatedTarget = event.relatedTarget;
    if (relatedTarget instanceof Node && event.currentTarget.contains(relatedTarget)) {
      return;
    }
    setProjectDropTarget(null);
  }

  function handleProjectDrop(event: ReactDragEvent<HTMLDivElement>) {
    event.preventDefault();
    const sourceKey = draggingProjectKey || event.dataTransfer.getData("text/plain");
    if (!sourceKey) {
      clearProjectDragState();
      return;
    }
    const target = resolveProjectDropTarget(event.currentTarget, event.clientY, sourceKey);
    const order = projectGroups.map((group) => group.key).filter((groupKey) => groupKey !== sourceKey);
    order.splice(Math.min(target.index, order.length), 0, sourceKey);
    setCustomProjectOrder(order);
    writeCustomProjectOrder(order);
    clearProjectDragState();
  }

  useEffect(() => {
    onOverlayOpenChangeRef.current = onOverlayOpenChange;
  });

  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) {
      return;
    }
    const frame = window.requestAnimationFrame(() => updateContentFade(container));
    return () => window.cancelAnimationFrame(frame);
  }, [collapsedGroups, isError, isLoading, projects.length, sessions.length]);

  function updateContentFade(container: HTMLDivElement) {
    const next = {
      top: container.scrollTop > 1,
      bottom: container.scrollTop + container.clientHeight < container.scrollHeight - 1,
    };
    setContentFade((previous) =>
      previous.top === next.top && previous.bottom === next.bottom ? previous : next,
    );
  }

  const setOverlayHold = useCallback((id: string, open: boolean) => {
    const holds = overlayHoldIDsRef.current;
    if (open) {
      holds.add(id);
    } else {
      holds.delete(id);
    }
    onOverlayOpenChangeRef.current?.(holds.size > 0);
  }, []);

  useEffect(() => {
    return () => {
      overlayHoldIDsRef.current.clear();
      onOverlayOpenChangeRef.current?.(false);
    };
  }, []);

  useEffect(() => {
    if (!draggingSessionID) {
      return;
    }
    const cursorLock = document.createElement("style");
    cursorLock.textContent = "* { cursor: grabbing !important; }";
    document.head.append(cursorLock);
    return () => {
      cursorLock.remove();
    };
  }, [draggingSessionID]);

  useEffect(() => {
    return () => {
      if (dragPreviewFrameRef.current !== null) {
        window.cancelAnimationFrame(dragPreviewFrameRef.current);
      }
      if (autoScrollFrameRef.current !== null) {
        window.cancelAnimationFrame(autoScrollFrameRef.current);
      }
    };
  }, []);

  function clearDragState() {
    if (dragPreviewFrameRef.current !== null) {
      window.cancelAnimationFrame(dragPreviewFrameRef.current);
      dragPreviewFrameRef.current = null;
    }
    if (autoScrollFrameRef.current !== null) {
      window.cancelAnimationFrame(autoScrollFrameRef.current);
      autoScrollFrameRef.current = null;
    }
    autoScrollPointerRef.current = null;
    setDraggingSessionID(null);
    setDragTarget(null);
    setDragPreview(null);
  }

  function moveDragPreview(clientX: number, clientY: number) {
    dragPreviewPointRef.current = { x: clientX, y: clientY };
    if (dragPreviewFrameRef.current !== null) {
      window.cancelAnimationFrame(dragPreviewFrameRef.current);
    }
    dragPreviewFrameRef.current = window.requestAnimationFrame(() => {
      dragPreviewFrameRef.current = null;
      if (dragPreviewRef.current) {
        dragPreviewRef.current.style.transform = dragPreviewTransform(clientX, clientY);
      }
    });
  }

  function scheduleDragAutoScroll(clientX: number, clientY: number) {
    autoScrollPointerRef.current = { x: clientX, y: clientY };
    if (autoScrollFrameRef.current !== null) {
      return;
    }
    autoScrollFrameRef.current = window.requestAnimationFrame(runDragAutoScroll);
  }

  function runDragAutoScroll() {
    autoScrollFrameRef.current = null;
    const pointer = autoScrollPointerRef.current;
    const container = scrollContainerRef.current;
    if (!pointer || !container) {
      return;
    }
    const rect = container.getBoundingClientRect();
    if (pointer.x < rect.left - dragAutoScrollEdgePx || pointer.x > rect.right + dragAutoScrollEdgePx) {
      return;
    }
    let delta = 0;
    if (pointer.y < rect.top + dragAutoScrollEdgePx) {
      const strength = Math.min(1, (rect.top + dragAutoScrollEdgePx - pointer.y) / dragAutoScrollEdgePx);
      delta = -Math.ceil(strength * dragAutoScrollMaxStepPx);
    } else if (pointer.y > rect.bottom - dragAutoScrollEdgePx) {
      const strength = Math.min(1, (pointer.y - (rect.bottom - dragAutoScrollEdgePx)) / dragAutoScrollEdgePx);
      delta = Math.ceil(strength * dragAutoScrollMaxStepPx);
    }
    if (delta === 0) {
      return;
    }
    const previousScrollTop = container.scrollTop;
    container.scrollTop += delta;
    if (container.scrollTop !== previousScrollTop) {
      setDragTarget(findDropTarget(pointer.x, pointer.y));
      autoScrollFrameRef.current = window.requestAnimationFrame(runDragAutoScroll);
    }
  }

  function findDropTarget(clientX: number, clientY: number): SessionDropTarget | null {
    const target = document.elementFromPoint(clientX, clientY);
    const zone = target?.closest<HTMLElement>("[data-session-drop-target]");
    const kind = zone?.dataset.sessionDropTarget;
    if (!zone || !kind) {
      return null;
    }
    if (kind === "recent") {
      return { kind };
    }
    if (kind === "project") {
      const projectID = zone.dataset.sessionProjectId;
      return projectID ? { kind, projectID } : null;
    }
    if (kind !== "pinned") {
      return null;
    }
    const itemElements = Array.from(zone.querySelectorAll<HTMLElement>("[data-session-item-id]")).filter(
      (item) => item.dataset.sessionItemId !== draggingSessionID,
    );
    const index = itemElements.findIndex((item) => {
      const rect = item.getBoundingClientRect();
      return clientY < rect.top + rect.height / 2;
    });
    return {
      kind,
      index: index === -1 ? itemElements.length : index,
    };
  }

  function handlePointerDragStart(sessionID: string, clientX: number, clientY: number) {
    const session = displayedSessions.find((item) => item.id === sessionID);
    dragPreviewPointRef.current = { x: clientX, y: clientY };
    setDraggingSessionID(sessionID);
    setDragPreview({
      id: sessionID,
      title: session?.title || t("session.untitled"),
      x: clientX,
      y: clientY,
    });
  }

  function handlePointerDragMove(clientX: number, clientY: number) {
    moveDragPreview(clientX, clientY);
    scheduleDragAutoScroll(clientX, clientY);
    setDragTarget(findDropTarget(clientX, clientY));
  }

  function releaseOptimisticPinnedOrder(expectedOrder: string[]) {
    setOptimisticPinnedOrder((current) => {
      if (
        current?.length === expectedOrder.length &&
        current.every((sessionID, index) => sessionID === expectedOrder[index])
      ) {
        return null;
      }
      return current;
    });
  }

  function handlePointerDrop(sessionID: string, clientX: number, clientY: number) {
    const target = findDropTarget(clientX, clientY);
    const session = displayedSessions.find((item) => item.id === sessionID);
    if (!session) {
      clearDragState();
      return;
    }
    if (!target) {
      clearDragState();
      return;
    }
    if (target.kind !== "pinned") {
      const nextOrder = session.pinned
        ? pinnedSessions.filter((item) => item.id !== sessionID).map((item) => item.id)
        : null;
      if (nextOrder) {
        setOptimisticPinnedOrder(nextOrder);
      }
      const projectID = target.kind === "project" ? target.projectID : "";
      void onPlacementChange(session.id, projectID).finally(() => {
        if (nextOrder) {
          releaseOptimisticPinnedOrder(nextOrder);
        }
      });
      clearDragState();
      return;
    }

    const pinnedWithoutDragged = pinnedSessions.filter((item) => item.id !== sessionID);
    const insertIndex = Math.max(0, Math.min(target.index, pinnedWithoutDragged.length));
    const nextPinned = [
      ...pinnedWithoutDragged.slice(0, insertIndex),
      { ...session, pinned: true },
      ...pinnedWithoutDragged.slice(insertIndex),
    ];
    const nextOrder = nextPinned.map((item) => item.id);
    setOptimisticPinnedOrder(nextOrder);
    const updates = nextPinned.flatMap((item, index) => {
      const pinnedOrder = index + 1;
      if (!item.pinned || item.pinnedOrder !== pinnedOrder || item.id === sessionID) {
        return [onPinChange(item.id, true, pinnedOrder)];
      }
      return [];
    });
    void Promise.all(updates).finally(() => {
      releaseOptimisticPinnedOrder(nextOrder);
    });
    clearDragState();
  }

  return (
    <RailOverlayHoldContext.Provider value={setOverlayHold}>
      <SidebarProvider className="pudding-session-rail !contents">
        <Sidebar className="min-h-0 w-full flex-1 bg-transparent" collapsible="none">
          <SidebarHeader className="px-2 pt-0 pb-2">
            <SidebarMenu
              className="gap-0.5"
              onKeyDown={(event) => handleVerticalMenuNavigation(event, "[data-rail-header-action]")}
            >
              <SidebarMenuItem>
                <SidebarMenuButton
                  className="h-8 px-2 py-1"
                  data-rail-header-action
                  isActive={draftActive && !draftProjectID}
                  onClick={onCreate}
                >
                  <MessageCirclePlus />
                  <span>{t("session.create")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton
                  className="h-8 px-2 py-1"
                  data-rail-header-action
                  isActive={projectsActive}
                  onClick={() => {
                    void navigate({
                      to: "/",
                      search: (prev) => {
                        const next = { ...(prev as AppSearch), view: "projects" as const };
                        delete next.session;
                        delete next.split;
                        delete next.draft;
                        delete next.project;
                        return next;
                      },
                    });
                  }}
                >
                  <Folders />
                  <span>{t("project.manage")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton className="h-8 px-2 py-1" data-rail-header-action onClick={onSearch}>
                  <Search />
                  <span>{t("rail.search")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton
                  className="h-8 px-2 py-1"
                  data-rail-header-action
                  isActive={appsActive}
                  onClick={() => {
                    void navigate({
                      to: "/",
                      search: (prev) => {
                        const next = { ...(prev as AppSearch), view: "apps" as const };
                        delete next.session;
                        delete next.split;
                        delete next.draft;
                        delete next.project;
                        return next;
                      },
                    });
                  }}
                >
                  <Package />
                  <span>{t("rail.automations")}</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarHeader>
          <SidebarContent
            ref={scrollContainerRef}
            className={cn(
              "touch-pan-y gap-2 pt-2 pb-2 overscroll-contain",
              contentFade.top && contentFade.bottom
                ? "pudding-session-rail-content-fade-both"
                : contentFade.top
                  ? "pudding-session-rail-content-fade-top"
                  : contentFade.bottom
                    ? "pudding-session-rail-content-fade-bottom"
                    : undefined,
            )}
            onKeyDown={(event) => handleVerticalMenuNavigation(event, "[data-rail-session-action]")}
            onScroll={(event) => updateContentFade(event.currentTarget)}
          >
            {isLoading ? (
              <SessionListSkeleton />
            ) : isError ? (
              <SessionListError onRefetch={onRefetch} />
            ) : (
              <>
                {showPinnedGroup ? (
                  <Collapsible asChild open={Boolean(draggingSessionID) || !pinnedCollapsed}>
                    <SidebarGroup
                      className="rounded-md px-2 py-0"
                      data-session-drop-target={draggingSessionID ? "pinned" : undefined}
                    >
                      <CollapsibleSessionGroupLabel
                        activity={pinnedCollapsed
                          ? sessionGroupActivity(pinnedSessions, runningTurns, turnPhases, completedSessions)
                          : undefined}
                        collapsed={draggingSessionID ? false : pinnedCollapsed}
                        icon="pinned"
                        interactionDisabled={Boolean(draggingSessionID)}
                        label={t("session.pinned")}
                        onToggle={() => toggleGroupCollapsed("pinned")}
                      />
                      <CollapsibleContent
                        className={cn(
                          "pudding-session-group-content",
                          draggingSessionID ? "overflow-visible" : "overflow-hidden",
                        )}
                      >
                        <SidebarGroupContent className="pt-0.5">
                          <SessionItems
                            archivePending={archivePending}
                            hasProjects={projects.length > 0}
                            projectChangePendingID={projectChangePendingID}
                            projectNamesByID={projectNamesByID}
                            selectedSessionID={selectedSessionID}
                            sessions={pinnedSessions}
                            showEmptyState={false}
                            draggingSessionID={draggingSessionID}
                            dropIndex={dragTarget?.kind === "pinned" ? dragTarget.index : null}
                            showEmptyDropTarget={Boolean(draggingSessionID && pinnedSessions.length === 0)}
                            onArchive={onArchive}
                            onOpenSplit={onOpenSplit}
                            onOpenProjectPicker={setProjectPickerSessionID}
                            onPinChange={onPinChange}
                            onProjectChange={onProjectChange}
                            onPointerDragCancel={clearDragState}
                            onPointerDragEnd={handlePointerDrop}
                            onPointerDragMove={handlePointerDragMove}
                            onPointerDragStart={handlePointerDragStart}
                            onRename={onRename}
                            onSelect={onSelect}
                          />
                        </SidebarGroupContent>
                      </CollapsibleContent>
                    </SidebarGroup>
                  </Collapsible>
                ) : null}
                <div className="flex flex-col gap-2 rounded-md">
                  {sessions.length === 0 && projects.length === 0 ? (
                    <SessionEmptyState />
                  ) : chatSessions.length > 0 || recentDropEnabled ? (
                    <Collapsible asChild open={recentDropEnabled || !chatCollapsed}>
                      <SidebarGroup className="rounded-md px-2 py-0">
                        <CollapsibleSessionGroupLabel
                          activity={chatCollapsed
                            ? sessionGroupActivity(chatSessions, runningTurns, turnPhases, completedSessions)
                            : undefined}
                          collapsed={recentDropEnabled ? false : chatCollapsed}
                          icon="chat"
                          interactionDisabled={Boolean(draggingSessionID)}
                          label={t("session.chats")}
                          onToggle={() => toggleGroupCollapsed("chat")}
                        />
                        <CollapsibleContent className="pudding-session-group-content overflow-hidden">
                          <SidebarGroupContent
                            className={cn(
                              "rounded-md pt-0.5",
                              recentDropEnabled && "min-h-8",
                              dragTarget?.kind === "recent" &&
                                "pudding-session-drop-row pudding-session-drop-row-active",
                            )}
                            data-session-drop-target={recentDropEnabled ? "recent" : undefined}
                          >
                            <SessionItems
                              archivePending={archivePending}
                              hasProjects={projects.length > 0}
                              projectChangePendingID={projectChangePendingID}
                              projectNamesByID={projectNamesByID}
                              selectedSessionID={selectedSessionID}
                              sessions={chatSessions}
                              showEmptyState={sessions.length === 0}
                              draggingSessionID={draggingSessionID}
                              dropIndex={null}
                              showEmptyDropTarget={Boolean(recentDropEnabled && chatSessions.length === 0)}
                              onArchive={onArchive}
                              onOpenSplit={onOpenSplit}
                              onOpenProjectPicker={setProjectPickerSessionID}
                              onPinChange={onPinChange}
                              onProjectChange={onProjectChange}
                              onPointerDragCancel={clearDragState}
                              onPointerDragEnd={handlePointerDrop}
                              onPointerDragMove={handlePointerDragMove}
                              onPointerDragStart={handlePointerDragStart}
                              onRename={onRename}
                              onSelect={onSelect}
                            />
                          </SidebarGroupContent>
                        </CollapsibleContent>
                      </SidebarGroup>
                    </Collapsible>
                  ) : null}
                  {projectGroups.length > 0 ? (
                    <Collapsible open={Boolean(draggingSessionID) || !projectsCollapsed}>
                      <ProjectSortHeader
                        collapsed={draggingSessionID ? false : projectsCollapsed}
                        interactionDisabled={Boolean(draggingSessionID)}
                        mode={projectSortMode}
                        onCreateProject={onCreateProject}
                        onModeChange={changeProjectSortMode}
                        onToggle={() => toggleGroupCollapsed("projects")}
                      />
                      <CollapsibleContent
                        className="pudding-session-group-content overflow-hidden"
                        style={draggingProjectKey ? { overflow: "visible" } : undefined}
                      >
                        <div
                          className="relative flex flex-col gap-0.5 py-1"
                          onDragLeave={handleProjectDragLeave}
                          onDragOver={handleProjectDragOver}
                          onDrop={handleProjectDrop}
                        >
                          {projectDropTarget ? (
                            <span
                              aria-hidden="true"
                              className="pointer-events-none absolute right-3 left-3 z-20 h-0 overflow-visible"
                              style={{ top: projectDropTarget.top }}
                            >
                              <span className="absolute top-0 right-0 left-2 h-0.5 -translate-y-1/2 rounded-full bg-primary" />
                              <span className="absolute top-0 left-0.5 size-2.5 -translate-y-1/2 rounded-full border-2 border-primary bg-sidebar" />
                            </span>
                          ) : null}
                          {projectGroups.map((group) => {
                            const projectCollapsed = collapsedGroups.has(`project:${group.key}`);
                            const hasSessions = group.sessions.length > 0;
                            const isDraggedSessionSourceProject = Boolean(
                              draggedSession &&
                              !draggedSession.pinned &&
                              group.project &&
                              draggedSession.projectID === group.project.id,
                            );
                            const projectDropEnabled = Boolean(
                              draggedSession &&
                              !draggedSessionRunning &&
                              group.project &&
                              (draggedSession.pinned || draggedSession.projectID !== group.project.id),
                            );
                            return (
                              <Collapsible
                                key={group.key}
                                asChild
                                open={draggingProjectKey || draggingSessionID ? false : !projectCollapsed}
                              >
                                <SidebarGroup
                                  className="rounded-md px-2 py-0"
                                  data-project-group-key={group.key}
                                >
                                  <div
                                    className="rounded-md"
                                    data-session-drop-target={projectDropEnabled ? "project" : undefined}
                                    data-session-project-id={projectDropEnabled ? group.project?.id : undefined}
                                  >
                                    <CollapsibleSessionGroupLabel
                                      active={draftActive && draftProjectID === group.projectID}
                                      activity={projectCollapsed || draggingSessionID
                                        ? sessionGroupActivity(group.sessions, runningTurns, turnPhases, completedSessions)
                                        : undefined}
                                      actions={group.project ? (
                                        <RailProjectActionsMenu
                                          homeDirectory={homeDirectory}
                                          project={group.project}
                                          token={token}
                                        />
                                      ) : undefined}
                                      collapsed={draggingSessionID ? true : projectCollapsed}
                                      dropTargetActive={
                                        dragTarget?.kind === "project" &&
                                        dragTarget.projectID === group.project?.id
                                      }
                                      interactionDisabled={isDraggedSessionSourceProject}
                                      icon="project"
                                      label={group.label}
                                      reorderable={projectSortMode === "custom"}
                                      dragging={draggingProjectKey === group.key}
                                      actionLabel={t("session.create")}
                                      onAction={group.projectID ? () => onCreateProjectSession(group.projectID!) : undefined}
                                      onDragEnd={clearProjectDragState}
                                      onDragStart={(event) => handleProjectDragStart(event, group.key)}
                                      onToggle={() => toggleGroupCollapsed(`project:${group.key}`)}
                                    />
                                  </div>
                                  {hasSessions ? (
                                    <CollapsibleContent className="pudding-session-group-content overflow-hidden">
                                      <SidebarGroupContent className="pt-0.5">
                                        <SessionItems
                                          archivePending={archivePending}
                                          hasProjects={projects.length > 0}
                                          projectChangePendingID={projectChangePendingID}
                                          projectNamesByID={projectNamesByID}
                                          selectedSessionID={selectedSessionID}
                                          sessions={group.sessions}
                                          showEmptyState={false}
                                          draggingSessionID={draggingSessionID}
                                          dropIndex={null}
                                          showEmptyDropTarget={false}
                                          onArchive={onArchive}
                                          onOpenSplit={onOpenSplit}
                                          onOpenProjectPicker={setProjectPickerSessionID}
                                          onPinChange={onPinChange}
                                          onProjectChange={onProjectChange}
                                          onPointerDragCancel={clearDragState}
                                          onPointerDragEnd={handlePointerDrop}
                                          onPointerDragMove={handlePointerDragMove}
                                          onPointerDragStart={handlePointerDragStart}
                                          onRename={onRename}
                                          onSelect={onSelect}
                                        />
                                      </SidebarGroupContent>
                                    </CollapsibleContent>
                                  ) : null}
                                </SidebarGroup>
                              </Collapsible>
                            );
                          })}
                        </div>
                      </CollapsibleContent>
                    </Collapsible>
                  ) : null}
                </div>
              </>
            )}
            {dragPreview ? (
              <SessionDragPreview
                point={dragPreviewPointRef.current}
                preview={dragPreview}
                previewRef={dragPreviewRef}
              />
            ) : null}
          </SidebarContent>
          <SidebarFooter className="flex-row items-center gap-1">
            <ShellActionButton
              aria-label={t("settings.title")}
              className="opacity-100"
              size="icon"
              tabIndex={-1}
              onClick={() => openSettingsDialog()}
            >
              <Settings />
            </ShellActionButton>
            <RailUpdateButton serverTurnRunning={sessions.some((session) => session.running)} />
          </SidebarFooter>
        </Sidebar>
        <SessionProjectPickerDialog
          open={Boolean(projectPickerSessionID)}
          pending={projectChangePendingID === projectPickerSessionID}
          projects={projects}
          session={projectPickerSession}
          onOpenChange={(open) => {
            if (!open) {
              setProjectPickerSessionID(null);
            }
          }}
          onSelect={(projectID) => {
            if (!projectPickerSessionID) {
              return Promise.resolve();
            }
            return onProjectChange(projectPickerSessionID, projectID);
          }}
        />
      </SidebarProvider>
    </RailOverlayHoldContext.Provider>
  );
}
