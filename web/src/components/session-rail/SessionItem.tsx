import {
  type PointerEvent as ReactPointerEvent,
  useEffect,
  useRef,
  useState,
} from "react";

import type { Session } from "@/api/client";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
} from "@/components/AppMenu";
import {
  Archive,
  Ellipsis,
  FolderClosed,
  FolderInput,
  FolderMinus,
  Pin,
  SquareTerminal,
} from "@/components/icons";
import { useRailOverlayHold } from "@/components/session-rail/overlayHold";
import { Spinner } from "@/components/Spinner";
import {
  DropdownMenu,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  SidebarMenuAction,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useHasHoverInput } from "@/hooks/use-hover-input";
import { useI18n } from "@/i18n";
import { formatRelative } from "@/lib/time";
import { cn } from "@/lib/utils";

type SessionItemProps = {
  session: Session;
  projectName?: string;
  selected: boolean;
  running: boolean;
  completed: boolean;
  archivePending: boolean;
  hasProjects: boolean;
  projectChangePending: boolean;
  suppressInteractiveState: boolean;
  onSelect: () => void;
  onOpenSplit: () => void;
  onOpenProjectPicker: () => void;
  onPinChange: (pinned: boolean) => void;
  onRemoveProject: () => Promise<void>;
  onArchive: () => void;
  onRename: (title: string) => Promise<void>;
  onPointerDragStart: (clientX: number, clientY: number) => void;
  onPointerDragMove: (clientX: number, clientY: number) => void;
  onPointerDragEnd: (clientX: number, clientY: number) => void;
  onPointerDragCancel: () => void;
};

export function SessionItem({
  session,
  projectName,
  selected,
  running,
  completed,
  archivePending,
  hasProjects,
  projectChangePending,
  suppressInteractiveState,
  onSelect,
  onOpenSplit,
  onOpenProjectPicker,
  onPinChange,
  onRemoveProject,
  onArchive,
  onRename,
  onPointerDragStart,
  onPointerDragMove,
  onPointerDragEnd,
  onPointerDragCancel,
}: SessionItemProps) {
  const { t, locale } = useI18n();
  const actionsAlwaysVisible = !useHasHoverInput();
  const [actionsOpen, setActionsOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const [pendingTitle, setPendingTitle] = useState<string | null>(null);
  const title = pendingTitle || session.title || t("session.untitled");
  const inputRef = useRef<HTMLInputElement>(null);
  const editAfterMenuCloseRef = useRef(false);
  const pointerDragRef = useRef<{
    pointerID: number;
    startX: number;
    startY: number;
    dragging: boolean;
    cleanup?: () => void;
  } | null>(null);
  const suppressClickRef = useRef(false);

  useRailOverlayHold(actionsOpen);

  useEffect(() => {
    if (!editing) {
      setDraft(session.title);
    }
  }, [editing, session.title]);

  useEffect(() => {
    if (pendingTitle !== null && session.title === pendingTitle) {
      setPendingTitle(null);
    }
  }, [pendingTitle, session.title]);

  useEffect(() => {
    if (!editing) {
      return;
    }
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [editing]);

  function selectSession() {
    onSelect();
  }

  function startEditing() {
    setDraft(session.title);
    setEditing(true);
  }

  function cancelEditing() {
    setDraft(session.title);
    setEditing(false);
  }

  function saveEditing() {
    const nextTitle = draft.trim();
    if (!nextTitle) {
      cancelEditing();
      return;
    }
    if (nextTitle !== session.title) {
      setPendingTitle(nextTitle);
      void onRename(nextTitle).catch(() => setPendingTitle(null));
    }
    setEditing(false);
  }

  function handlePointerDown(event: ReactPointerEvent<HTMLButtonElement>) {
    if (event.button !== 0 || event.pointerType === "touch") {
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    pointerDragRef.current = {
      pointerID: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      dragging: false,
    };
  }

  function handlePointerMove(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = pointerDragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) {
      return;
    }
    const distance = Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY);
    if (!drag.dragging && distance < 6) {
      return;
    }
    if (!drag.dragging) {
      drag.dragging = true;
      bindWindowPointerDrag(event.pointerId);
      event.currentTarget.setPointerCapture(event.pointerId);
      onPointerDragStart(event.clientX, event.clientY);
    }
    event.preventDefault();
    onPointerDragMove(event.clientX, event.clientY);
  }

  function finishPointerDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = pointerDragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) {
      return;
    }
    pointerDragRef.current = null;
    drag.cleanup?.();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    if (!drag.dragging) {
      return;
    }
    event.preventDefault();
    suppressClickRef.current = true;
    onPointerDragEnd(event.clientX, event.clientY);
    window.setTimeout(() => {
      suppressClickRef.current = false;
    }, 0);
  }

  function cancelPointerDrag(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = pointerDragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) {
      return;
    }
    pointerDragRef.current = null;
    drag.cleanup?.();
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    onPointerDragCancel();
  }

  function releasePointerFocus(event: ReactPointerEvent<HTMLButtonElement>) {
    if (document.activeElement === event.currentTarget && !event.currentTarget.matches(":focus-visible")) {
      event.currentTarget.blur();
    }
  }

  function bindWindowPointerDrag(pointerID: number) {
    const drag = pointerDragRef.current;
    if (!drag || drag.cleanup) {
      return;
    }
    const handleMove = (event: PointerEvent) => {
      if (event.pointerId !== pointerID || !pointerDragRef.current?.dragging) {
        return;
      }
      event.preventDefault();
      onPointerDragMove(event.clientX, event.clientY);
    };
    const handleEnd = (event: PointerEvent) => {
      const current = pointerDragRef.current;
      if (event.pointerId !== pointerID || !current) {
        return;
      }
      pointerDragRef.current = null;
      current.cleanup?.();
      if (current.dragging) {
        event.preventDefault();
        suppressClickRef.current = true;
        onPointerDragEnd(event.clientX, event.clientY);
        window.setTimeout(() => {
          suppressClickRef.current = false;
        }, 0);
      } else {
        onPointerDragCancel();
      }
    };
    drag.cleanup = () => {
      window.removeEventListener("pointermove", handleMove);
      window.removeEventListener("pointerup", handleEnd);
      window.removeEventListener("pointercancel", handleEnd);
    };
    window.addEventListener("pointermove", handleMove, { passive: false });
    window.addEventListener("pointerup", handleEnd);
    window.addEventListener("pointercancel", handleEnd);
  }

  return (
    <SidebarMenuItem className={suppressInteractiveState ? "pointer-events-none" : undefined}>
      {editing ? (
        <SidebarMenuButton asChild className="h-8 px-2 py-1" isActive>
          <div className="cursor-text">
            <span aria-hidden="true" className="w-4 shrink-0" />
            <Input
              ref={inputRef}
              aria-label={t("session.rename")}
              className="h-5 min-w-0 flex-1 rounded-none border-0 bg-transparent px-0 py-0 text-sm shadow-none ring-0 focus-visible:border-0 focus-visible:ring-0 md:text-sm dark:bg-transparent"
              placeholder={t("session.untitled")}
              value={draft}
              onBlur={saveEditing}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  event.currentTarget.blur();
                }
                if (event.key === "Escape") {
                  event.preventDefault();
                  cancelEditing();
                }
              }}
            />
          </div>
        </SidebarMenuButton>
      ) : (
        <SidebarMenuButton
          asChild
          className={cn(
            "h-8 px-2 py-1 focus-visible:bg-sidebar-accent focus-visible:text-sidebar-accent-foreground focus-visible:ring-inset",
            running
              ? "pr-28 data-active:font-normal group-has-data-[sidebar=menu-action]/menu-item:pr-28"
              : actionsAlwaysVisible
                ? "pr-28 data-active:font-normal group-has-data-[sidebar=menu-action]/menu-item:pr-28"
                : "pr-16 data-active:font-normal group-has-data-[sidebar=menu-action]/menu-item:pr-16",
            suppressInteractiveState
              ? "hover:bg-transparent hover:text-sidebar-foreground active:bg-transparent active:text-sidebar-foreground"
              : "group-hover/menu-item:bg-sidebar-accent group-hover/menu-item:text-sidebar-accent-foreground group-focus-within/menu-item:bg-sidebar-accent group-focus-within/menu-item:text-sidebar-accent-foreground",
          )}
          isActive={selected || actionsOpen}
        >
          <button
            data-rail-session-action
            data-session-item-id={session.id}
            type="button"
            onClick={(event) => {
              if (suppressClickRef.current) {
                event.preventDefault();
                event.stopPropagation();
                return;
              }
              selectSession();
            }}
            onDoubleClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              startEditing();
            }}
            onPointerCancel={cancelPointerDrag}
            onPointerDown={handlePointerDown}
            onPointerLeave={releasePointerFocus}
            onPointerMove={handlePointerMove}
            onPointerUp={finishPointerDrag}
          >
            <span aria-hidden="true" className="w-4 shrink-0" />
            <span className="min-w-0 flex-1 truncate">
              {title}
            </span>
          </button>
        </SidebarMenuButton>
      )}
      {!editing ? (
        <>
          <SidebarMenuBadge
            className={cn(
              "min-w-0 px-0 font-normal text-muted-foreground",
              actionsAlwaysVisible ? "right-14" : "right-2",
              !actionsAlwaysVisible &&
                !suppressInteractiveState &&
                "group-focus-within/menu-item:opacity-0 group-hover/menu-item:opacity-0",
              actionsOpen && "opacity-0",
            )}
          >
            {running ? (
              <span aria-label={t("session.processing")} className="flex items-center justify-center">
                <Spinner className="size-3" />
              </span>
            ) : session.backgroundProcessCount > 0 ? (
              <span
                aria-label={t("session.backgroundTasks").replace("{count}", String(session.backgroundProcessCount))}
                className="flex items-center gap-1 whitespace-nowrap"
              >
                <SquareTerminal className="size-3" />
                <span className="tabular-nums">{session.backgroundProcessCount}</span>
              </span>
            ) : completed ? (
              <span aria-label={t("session.completed")} className="size-2 rounded-full bg-blue-500" />
            ) : (
              formatRelative(session.lastActivityAt || session.createdAt, locale)
            )}
          </SidebarMenuBadge>
          <DropdownMenu open={actionsOpen} onOpenChange={setActionsOpen}>
            <DropdownMenuTrigger asChild>
              <SidebarMenuAction
                aria-label={t("session.actions")}
                className={cn(
                  "right-7 rounded-sm bg-transparent text-muted-foreground after:hidden peer-hover/menu-button:text-muted-foreground peer-data-active/menu-button:text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:text-sidebar-accent-foreground",
                  actionsAlwaysVisible && "opacity-100",
                  !actionsAlwaysVisible &&
                    suppressInteractiveState &&
                    "group-hover/menu-item:opacity-0 hover:bg-transparent hover:text-muted-foreground md:opacity-0",
                  actionsOpen && "opacity-100 md:opacity-100",
                )}
                showOnHover={!actionsAlwaysVisible}
              >
                <Ellipsis className="size-3.5!" />
              </SidebarMenuAction>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="end"
              onCloseAutoFocus={(event) => {
                event.preventDefault();
                if (!editAfterMenuCloseRef.current) {
                  return;
                }
                editAfterMenuCloseRef.current = false;
                startEditing();
              }}
            >
              {projectName ? (
                <>
                  <DropdownMenuLabel className="flex min-h-7 max-w-64 items-center gap-1.5 px-1.5 py-1 text-[13px] font-normal">
                    <FolderClosed className="size-3.5 shrink-0" />
                    <span className="min-w-0 truncate">{projectName}</span>
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                </>
              ) : null}
              <DropdownMenuItem
                disabled={running || projectChangePending || !hasProjects}
                title={running ? t("session.projectChangeRunning") : undefined}
                onSelect={onOpenProjectPicker}
              >
                <FolderInput />
                {projectName ? t("session.moveToProject") : t("session.addToProject")}
              </DropdownMenuItem>
              {projectName ? (
                <DropdownMenuItem
                  disabled={running || projectChangePending}
                  title={running ? t("session.projectChangeRunning") : undefined}
                  onSelect={() => {
                    void onRemoveProject().catch(() => undefined);
                  }}
                >
                  <FolderMinus />
                  {t("session.removeFromProject")}
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => onPinChange(!session.pinned)}>
                <Pin className="rotate-45" />
                {session.pinned ? t("session.unpin") : t("session.pin")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onOpenSplit}>
                {t("session.openSplit")}
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={() => {
                  editAfterMenuCloseRef.current = true;
                }}
              >
                {t("session.rename")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <SidebarMenuAction
                aria-label={t("session.archive")}
                className={cn(
                  "right-1.5 rounded-sm bg-transparent text-muted-foreground after:hidden peer-hover/menu-button:text-muted-foreground peer-data-active/menu-button:text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:text-sidebar-accent-foreground",
                  actionsAlwaysVisible && "opacity-100",
                  !actionsAlwaysVisible &&
                    suppressInteractiveState &&
                    "group-hover/menu-item:opacity-0 hover:bg-transparent hover:text-muted-foreground md:opacity-0",
                  actionsOpen && "opacity-100 md:opacity-100",
                )}
                disabled={archivePending}
                showOnHover={!actionsAlwaysVisible}
                type="button"
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  onArchive();
                }}
              >
                <Archive className="size-3.5!" />
              </SidebarMenuAction>
            </TooltipTrigger>
            <TooltipContent className="pointer-events-none" side="right" sideOffset={4}>
              {t("session.archive")}
            </TooltipContent>
          </Tooltip>
        </>
      ) : null}
    </SidebarMenuItem>
  );
}
