import { Fragment, useState } from "react";

import type { Session } from "@/api/client";
import { MessageSquareText } from "@/components/icons";
import { isSessionTurnRunning } from "@/components/session-rail/activity";
import { SessionItem } from "@/components/session-rail/SessionItem";
import { Button } from "@/components/ui/button";
import { SidebarMenu, SidebarMenuItem } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/i18n";
import { useOverlayStore } from "@/state/overlayStore";

const sessionCollapseThreshold = 6;
const collapsedSessionDisplayLimit = 5;

type SessionItemsProps = {
  sessions: Session[];
  projectNamesByID: ReadonlyMap<string, string>;
  selectedSessionID: string | undefined;
  archivePending: boolean;
  hasProjects: boolean;
  projectChangePendingID: string | undefined;
  showEmptyState?: boolean;
  draggingSessionID: string | null;
  dropIndex: number | null;
  showEmptyDropTarget: boolean;
  onSelect: (id: string) => void;
  onOpenSplit: (id: string) => void;
  onOpenProjectPicker: (id: string) => void;
  onPinChange: (id: string, pinned: boolean, pinnedOrder?: number) => void;
  onProjectChange: (id: string, projectID: string) => Promise<void>;
  onArchive: (id: string) => void;
  onRename: (id: string, title: string) => Promise<void>;
  onPointerDragStart: (id: string, clientX: number, clientY: number) => void;
  onPointerDragMove: (clientX: number, clientY: number) => void;
  onPointerDragEnd: (id: string, clientX: number, clientY: number) => void;
  onPointerDragCancel: () => void;
};

export function SessionListSkeleton() {
  return (
    <div className="grid gap-0.5 p-2">
      <Skeleton className="h-8 rounded-md" />
      <Skeleton className="h-8 rounded-md" />
      <Skeleton className="h-8 rounded-md" />
    </div>
  );
}

export function SessionListError({ onRefetch }: { onRefetch: () => void }) {
  const { t } = useI18n();
  return (
    <div className="grid justify-items-center gap-2 px-3 py-6 text-center text-xs text-muted-foreground">
      <div>{t("session.loadFailed")}</div>
      <Button className="h-7 px-2 text-xs" size="sm" type="button" variant="outline" onClick={onRefetch}>
        {t("common.refresh")}
      </Button>
    </div>
  );
}

export function SessionItems({
  sessions,
  projectNamesByID,
  selectedSessionID,
  archivePending,
  hasProjects,
  projectChangePendingID,
  showEmptyState = true,
  draggingSessionID,
  dropIndex,
  showEmptyDropTarget,
  onSelect,
  onOpenSplit,
  onOpenProjectPicker,
  onPinChange,
  onProjectChange,
  onArchive,
  onRename,
  onPointerDragStart,
  onPointerDragMove,
  onPointerDragEnd,
  onPointerDragCancel,
}: SessionItemsProps) {
  const { t } = useI18n();
  const [showAll, setShowAll] = useState(false);
  // 实时运行态:sessions 快照(15s 兜底)与 SSE overlay 双源取或
  const runningTurns = useOverlayStore((state) => state.runningTurns);
  const turnPhases = useOverlayStore((state) => state.turnPhases);
  const completedSessions = useOverlayStore((state) => state.completedSessions);

  if (sessions.length === 0 && showEmptyState) {
    return <SessionEmptyState />;
  }
  const canCollapse = sessions.length > sessionCollapseThreshold;
  const cappedSessions = showAll || !canCollapse
    ? sessions
    : sessions.slice(0, collapsedSessionDisplayLimit);
  const visibleSessions = cappedSessions.filter((session) => session.id !== draggingSessionID);
  const hiddenSessionCount = canCollapse ? sessions.length - collapsedSessionDisplayLimit : 0;
  if (visibleSessions.length === 0 && (dropIndex !== null || showEmptyDropTarget)) {
    return (
      <SidebarMenu className="gap-0.5">
        <SessionDropIndicator active={dropIndex !== null} />
      </SidebarMenu>
    );
  }
  if (visibleSessions.length === 0) {
    return null;
  }

  return (
    <SidebarMenu className="gap-0.5">
      {visibleSessions.map((session, index) => (
        <Fragment key={session.id}>
          {dropIndex === index ? <SessionDropIndicator active /> : null}
          <SessionItem
            completed={Boolean(completedSessions[session.id])}
            archivePending={archivePending}
            hasProjects={hasProjects}
            projectName={session.projectID ? projectNamesByID.get(session.projectID) : undefined}
            projectChangePending={projectChangePendingID === session.id}
            running={isSessionTurnRunning(session, runningTurns, turnPhases)}
            selected={session.id === selectedSessionID}
            session={session}
            suppressInteractiveState={Boolean(draggingSessionID)}
            onArchive={() => onArchive(session.id)}
            onOpenSplit={() => onOpenSplit(session.id)}
            onOpenProjectPicker={() => onOpenProjectPicker(session.id)}
            onPinChange={(pinned) => onPinChange(session.id, pinned)}
            onRemoveProject={() => onProjectChange(session.id, "")}
            onPointerDragCancel={onPointerDragCancel}
            onPointerDragEnd={(clientX, clientY) => onPointerDragEnd(session.id, clientX, clientY)}
            onPointerDragMove={onPointerDragMove}
            onPointerDragStart={(clientX, clientY) => onPointerDragStart(session.id, clientX, clientY)}
            onRename={(title) => onRename(session.id, title)}
            onSelect={() => onSelect(session.id)}
          />
        </Fragment>
      ))}
      {dropIndex === visibleSessions.length ? <SessionDropIndicator active /> : null}
      {hiddenSessionCount > 0 ? (
        <SidebarMenuItem>
          <button
            aria-expanded={showAll}
            className="flex h-8 w-full items-center rounded-md pr-2 pl-[34px] text-left text-xs text-sidebar-foreground/50 hover:text-sidebar-foreground/80"
            type="button"
            onClick={() => setShowAll((current) => !current)}
          >
            <span>
              {showAll
                ? t("session.showLess")
                : t("session.showMore").replace("{count}", String(hiddenSessionCount))}
            </span>
          </button>
        </SidebarMenuItem>
      ) : null}
    </SidebarMenu>
  );
}

export function SessionEmptyState() {
  const { t } = useI18n();
  return (
    <div className="grid justify-items-center gap-2 px-2 py-10 text-center text-sm text-muted-foreground">
      <MessageSquareText className="h-5 w-5" />
      <div>{t("session.empty")}</div>
    </div>
  );
}

function SessionDropIndicator({ active }: { active: boolean }) {
  return (
    <li aria-hidden="true" className="relative z-20 h-0 -my-px overflow-visible">
      {active ? (
        <>
          <span className="absolute top-0 right-1 left-2 h-0.5 -translate-y-1/2 rounded-full bg-primary" />
          <span className="absolute top-0 left-0.5 size-2.5 -translate-y-1/2 rounded-full border-2 border-primary bg-sidebar" />
        </>
      ) : null}
    </li>
  );
}
