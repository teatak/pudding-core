import {
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";

import type { Session } from "@/api/client";
import {
  AppContextMenuContent as ContextMenuContent,
  AppContextMenuItem as ContextMenuItem,
  AppContextMenuLabel as ContextMenuLabel,
  AppContextMenuSeparator as ContextMenuSeparator,
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
  AppDropdownMenuLabel as DropdownMenuLabel,
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
  ContextMenu,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { RailIconAction } from "@/components/session-rail/RailIconAction";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useHasHoverInput } from "@/hooks/use-hover-input";
import { useI18n } from "@/i18n";
import { formatRelative } from "@/lib/time";
import { cn } from "@/lib/utils";

type SessionMenuCommand = "move-project" | "remove-project" | "toggle-pin" | "open-split" | "rename";

type SessionMenuEntry =
  | { type: "label"; label: string; icon?: ReactNode }
  | { type: "separator" }
  | {
      type: "item";
      id: SessionMenuCommand;
      label: string;
      disabled?: boolean;
      title?: string;
      icon?: ReactNode;
    };

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
  const [openMenu, setOpenMenu] = useState<"dropdown" | "context" | null>(null);
  const actionsOpen = openMenu !== null;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);
  const [pendingTitle, setPendingTitle] = useState<string | null>(null);
  const title = pendingTitle || session.title || t("session.untitled");
  const inputRef = useRef<HTMLInputElement>(null);
  const browserMenuCommandRef = useRef<SessionMenuCommand | null>(null);
  const pointerDragRef = useRef<{
    pointerID: number;
    startX: number;
    startY: number;
    dragging: boolean;
    cleanup?: () => void;
  } | null>(null);
  const suppressClickRef = useRef(false);
  const sessionMenuEntries: SessionMenuEntry[] = [
    ...(projectName
      ? [
          { type: "label" as const, label: projectName, icon: <FolderClosed className="size-3.5 shrink-0" /> },
          { type: "separator" as const },
        ]
      : []),
    {
      type: "item",
      id: "move-project",
      label: projectName ? t("session.moveToProject") : t("session.addToProject"),
      disabled: running || projectChangePending || !hasProjects,
      title: running ? t("session.projectChangeRunning") : undefined,
      icon: <FolderInput />,
    },
    ...(projectName
      ? [{
          type: "item" as const,
          id: "remove-project" as const,
          label: t("session.removeFromProject"),
          disabled: running || projectChangePending,
          title: running ? t("session.projectChangeRunning") : undefined,
          icon: <FolderMinus />,
        }]
      : []),
    { type: "separator" },
    {
      type: "item",
      id: "toggle-pin",
      label: session.pinned ? t("session.unpin") : t("session.pin"),
      icon: <Pin className="rotate-45" />,
    },
    { type: "item", id: "open-split", label: t("session.openSplit") },
    { type: "item", id: "rename", label: t("session.rename") },
  ];

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

  function runSessionMenuCommand(command: SessionMenuCommand) {
    switch (command) {
      case "move-project":
        onOpenProjectPicker();
        return;
      case "remove-project":
        void onRemoveProject().catch(() => undefined);
        return;
      case "toggle-pin":
        onPinChange(!session.pinned);
        return;
      case "open-split":
        onOpenSplit();
        return;
      case "rename":
        startEditing();
    }
  }

  function flushBrowserMenuCommand() {
    const command = browserMenuCommandRef.current;
    browserMenuCommandRef.current = null;
    if (command) {
      runSessionMenuCommand(command);
    }
  }

  function setMenuOpen(surface: "dropdown" | "context", open: boolean) {
    setOpenMenu((current) => open ? surface : current === surface ? null : current);
  }

  function actionsButton() {
    return (
      <RailIconAction
        aria-expanded={actionsOpen}
        aria-haspopup="menu"
        aria-label={t("session.actions")}
        className={cn(
          "absolute top-1 right-8 text-muted-foreground",
          actionsAlwaysVisible && "opacity-100",
          !actionsAlwaysVisible &&
            !suppressInteractiveState &&
            "group-focus-within/menu-item:opacity-100 group-hover/menu-item:opacity-100 md:opacity-0",
          actionsOpen && "opacity-100 md:opacity-100",
        )}
        data-state={actionsOpen ? "open" : "closed"}
        type="button"
      >
        <Ellipsis className="size-3.5!" />
      </RailIconAction>
    );
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

  const sessionItem = (
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
              actionsAlwaysVisible ? "right-16" : "right-2",
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
          <DropdownMenu open={openMenu === "dropdown"} onOpenChange={(open) => setMenuOpen("dropdown", open)}>
            <DropdownMenuTrigger asChild>{actionsButton()}</DropdownMenuTrigger>
            <DropdownMenuContent
              align="start"
              onCloseAutoFocus={(event) => {
                event.preventDefault();
                flushBrowserMenuCommand();
              }}
            >
              {sessionMenuEntries.map((entry, index) => {
                if (entry.type === "separator") {
                  return <DropdownMenuSeparator key={`separator-${index}`} />;
                }
                if (entry.type === "label") {
                  return (
                    <DropdownMenuLabel key={`label-${index}`} className="flex max-w-64 items-center">
                      {entry.icon}
                      <span className="min-w-0 truncate">{entry.label}</span>
                    </DropdownMenuLabel>
                  );
                }
                return (
                    <DropdownMenuItem
                      key={entry.id}
                    disabled={entry.disabled}
                    title={entry.title}
                    onSelect={() => {
                      browserMenuCommandRef.current = entry.id;
                    }}
                  >
                    {entry.icon}
                    {entry.label}
                  </DropdownMenuItem>
                );
              })}
            </DropdownMenuContent>
          </DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <RailIconAction
                aria-label={t("session.archive")}
                className={cn(
                  "absolute top-1 right-1 text-muted-foreground",
                  actionsAlwaysVisible && "opacity-100",
                  !actionsAlwaysVisible &&
                    !suppressInteractiveState &&
                    "group-focus-within/menu-item:opacity-100 group-hover/menu-item:opacity-100 md:opacity-0",
                  actionsOpen && "opacity-100 md:opacity-100",
                )}
                disabled={archivePending}
                type="button"
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  onArchive();
                }}
              >
                <Archive className="size-3.5!" />
              </RailIconAction>
            </TooltipTrigger>
            <TooltipContent className="pointer-events-none" side="right" sideOffset={4}>
              {t("session.archive")}
            </TooltipContent>
          </Tooltip>
        </>
      ) : null}
    </SidebarMenuItem>
  );

  return (
    <ContextMenu open={openMenu === "context"} onOpenChange={(open) => setMenuOpen("context", open)}>
      <ContextMenuTrigger asChild>{sessionItem}</ContextMenuTrigger>
      <ContextMenuContent
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          flushBrowserMenuCommand();
        }}
      >
        {sessionMenuEntries.map((entry, index) => {
          if (entry.type === "separator") {
            return <ContextMenuSeparator key={`separator-${index}`} />;
          }
          if (entry.type === "label") {
            return (
              <ContextMenuLabel key={`label-${index}`} className="flex max-w-64 items-center">
                {entry.icon}
                <span className="min-w-0 truncate">{entry.label}</span>
              </ContextMenuLabel>
            );
          }
          return (
            <ContextMenuItem
              key={entry.id}
              disabled={entry.disabled}
              title={entry.title}
              onSelect={() => {
                browserMenuCommandRef.current = entry.id;
              }}
            >
              {entry.icon}
              {entry.label}
            </ContextMenuItem>
          );
        })}
      </ContextMenuContent>
    </ContextMenu>
  );
}
