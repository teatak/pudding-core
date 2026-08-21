import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Ellipsis, FolderCog, FolderMinus } from "@/components/icons";
import { type MouseEvent as ReactMouseEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  deleteProject,
  revealDesktopPath,
  updateProject,
  type Project,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ProjectFormDialog } from "@/components/ProjectFormDialog";
import { RailIconAction } from "@/components/session-rail/RailIconAction";
import { Spinner } from "@/components/Spinner";
import {
  AppContextMenuContent as ContextMenuContent,
  AppContextMenuItem as ContextMenuItem,
  AppContextMenuSeparator as ContextMenuSeparator,
  AppContextMenuSubContent as ContextMenuSubContent,
  AppContextMenuSubTrigger as ContextMenuSubTrigger,
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
  AppDropdownMenuSubContent as DropdownMenuSubContent,
  AppDropdownMenuSubTrigger as DropdownMenuSubTrigger,
} from "@/components/AppMenu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ConfirmationDialog";
import { Button } from "@/components/ui/button";
import { ContextMenu, ContextMenuSub, ContextMenuTrigger } from "@/components/ui/context-menu";
import { DropdownMenu, DropdownMenuSub, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useI18n } from "@/i18n";
import { pickDirectories } from "@/lib/desktopBridge";
import type { AppSearch } from "@/lib/route";
import { cn } from "@/lib/utils";

type ProjectMenuCommand = "edit" | "delete" | `reveal:${number}` | `copy:${number}`;

type ProjectMenuEntry =
  | { type: "separator" }
  | {
      type: "item";
      id: ProjectMenuCommand;
      label: string;
      disabled?: boolean;
      icon?: ReactNode;
    }
  | { type: "submenu"; label: string; items: ProjectMenuEntry[] };

export function ProjectActionsMenu({
  alwaysVisible = false,
  homeDirectory = "",
  project,
  surface = "default",
  token,
  onOverlayOpenChange,
}: {
  alwaysVisible?: boolean;
  homeDirectory?: string;
  project: Project;
  surface?: "default" | "sidebar";
  token: string;
  onOverlayOpenChange?: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const [openMenu, setOpenMenu] = useState<"dropdown" | "context" | null>(null);
  const menuOpen = openMenu !== null;
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [name, setName] = useState(project.name);
  const [directoryPaths, setDirectoryPaths] = useState(project.rootDirs);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const overlayOpen = menuOpen || editOpen || deleteOpen;
  const isMac =
    (typeof document !== "undefined" && document.documentElement.dataset.shell === "electron-mac") ||
    (typeof navigator !== "undefined" && /Mac/i.test(navigator.platform));

  useEffect(() => {
    onOverlayOpenChange?.(overlayOpen);
  }, [onOverlayOpenChange, overlayOpen]);
  useEffect(() => () => onOverlayOpenChange?.(false), [onOverlayOpenChange]);
  useEffect(() => {
    if (surface !== "sidebar") {
      return;
    }
    const trigger = triggerRef.current;
    const target = trigger?.closest<HTMLElement>("[data-project-menu-context-target=true]");
    if (!trigger || !target) {
      return;
    }
    const openFromContextMenu = (rawEvent: Event) => {
      const event = rawEvent as MouseEvent;
      if (event.target instanceof Node && trigger.contains(event.target)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      trigger.dispatchEvent(new MouseEvent("contextmenu", {
        bubbles: true,
        button: 2,
        buttons: 2,
        cancelable: true,
        clientX: event.clientX,
        clientY: event.clientY,
        screenX: event.screenX,
        screenY: event.screenY,
      }));
    };
    target.addEventListener("contextmenu", openFromContextMenu);
    return () => target.removeEventListener("contextmenu", openFromContextMenu);
  }, [surface]);

  const revealMutation = useMutation({
    mutationFn: (path: string) => revealDesktopPath(token, path),
    onError: () => toast.error(t("project.revealFailed")),
  });
  const cacheProject = (updated: Project) => {
    queryClient.setQueryData(queryKeys.project(updated.id), updated);
    queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) =>
      previous
        ? { projects: previous.projects.map((entry) => (entry.id === updated.id ? updated : entry)) }
        : { projects: [updated] },
    );
  };
  const editMutation = useMutation({
    mutationFn: ({ nextName, paths }: { nextName: string; paths: string[] }) =>
      updateProject(token, project.id, {
        name: nextName,
        rootDirs: paths,
      }),
    onSuccess: async (updated) => {
      cacheProject(updated);
      setEditOpen(false);
      toast.success(t("project.updated"));
      const sessions =
        queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions())?.sessions || [];
      await Promise.all(
        sessions
          .filter((session) => session.projectID === project.id)
          .map((session) =>
            queryClient.invalidateQueries({
              queryKey: ["session", session.id, "project"],
            }),
          ),
      );
    },
    onError: () => toast.error(t("project.updateFailed")),
  });
  const deleteMutation = useMutation({
    mutationFn: () => deleteProject(token, project.id),
    onSuccess: async () => {
      const previousSessions = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      const attachedSessions = previousSessions?.sessions.filter((session) => session.projectID === project.id) || [];
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) => ({
        projects: previous?.projects.filter((entry) => entry.id !== project.id) || [],
      }));
      if (previousSessions) {
        queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), {
          sessions: previousSessions.sessions.map((session) =>
            session.projectID === project.id ? { ...session, projectID: undefined } : session,
          ),
        });
      }
      for (const session of attachedSessions) {
        queryClient.setQueryData<Session>(queryKeys.session(session.id), (previous) =>
          previous ? { ...previous, projectID: undefined } : previous,
        );
      }
      queryClient.removeQueries({ queryKey: queryKeys.project(project.id), exact: true });
      setDeleteOpen(false);
      await navigate({
        to: "/",
        search: (previous) => {
          const next = { ...(previous as AppSearch) };
          if (next.project === project.id) {
            delete next.project;
          }
          return next;
        },
        replace: true,
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projects() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
      ]);
    },
    onError: () => toast.error(t("project.deleteFailed")),
  });

  const openEdit = () => {
    setName(project.name);
    setDirectoryPaths(project.rootDirs);
    setEditOpen(true);
  };
  const chooseDirectories = async () => {
    const picked = await pickDirectories({
      buttonLabel: t("project.createPickButton"),
      message: t("project.editPickMessage"),
      title: t("project.edit"),
    });
    setDirectoryPaths((current) =>
      Array.from(new Set([...current, ...picked].map((path) => path.trim()).filter(Boolean))),
    );
  };
  const saveProject = () => {
    const nextName = name.trim();
    if (!nextName) return;
    const projects =
      queryClient.getQueryData<{ projects: Project[] }>(queryKeys.projects())?.projects || [];
    if (
      directoryPaths.length > 0 &&
      projects.some(
        (entry) =>
          entry.id !== project.id &&
          normalizedDirectorySet(entry.rootDirs) === normalizedDirectorySet(directoryPaths),
      )
    ) {
      toast.error(t("project.alreadyExists"));
      return;
    }
    editMutation.mutate({ nextName, paths: directoryPaths });
  };
  const copyPaths = (paths: string[]) => {
    void navigator.clipboard.writeText(paths.join("\n")).then(
      () => toast.success(t(paths.length > 1 ? "project.pathsCopied" : "project.pathCopied")),
      () => toast.error(t("project.pathCopyFailed")),
    );
  };
  const pathMenuEntries = (index: number): ProjectMenuEntry[] => [
    ...(isMac
      ? [{
          type: "item" as const,
          id: `reveal:${index}` as const,
          label: t("project.revealFinder"),
          disabled: revealMutation.isPending,
          icon: revealMutation.isPending ? <Spinner /> : undefined,
        }]
      : []),
    {
      type: "item",
      id: `copy:${index}`,
      label: t("project.copyPath"),
    },
  ];
  const menuEntries: ProjectMenuEntry[] = [
    ...(project.rootDirs.length === 1
      ? pathMenuEntries(0)
      : project.rootDirs.map((path, index) => ({
          type: "submenu" as const,
          label: projectDirectoryLabel(path, project.rootDirs),
          items: pathMenuEntries(index),
        }))),
    {
      type: "item",
      id: "edit",
      label: t("project.edit"),
      icon: <FolderCog />,
    },
    { type: "separator" },
    {
      type: "item",
      id: "delete",
      label: t("project.delete"),
      disabled: deleteMutation.isPending,
      icon: <FolderMinus />,
    },
  ];

  const runMenuCommand = (command: ProjectMenuCommand) => {
    if (command === "edit") {
      openEdit();
      return;
    }
    if (command === "delete") {
      setDeleteOpen(true);
      return;
    }
    const match = /^(reveal|copy):(\d+)$/.exec(command);
    const path = match ? project.rootDirs[Number(match[2])] : undefined;
    if (!match || !path) {
      return;
    }
    if (match[1] === "reveal") {
      revealMutation.mutate(path);
      return;
    }
    copyPaths([path]);
  };

  const setMenuSurfaceOpen = (menu: "dropdown" | "context", open: boolean) => {
    setOpenMenu((current) => open ? menu : current === menu ? null : current);
  };

  const actionsButton = () => {
    const props = {
      "aria-expanded": menuOpen,
      "aria-haspopup": "menu" as const,
      "aria-label": t("project.actions"),
      "data-project-actions-open": menuOpen,
      "data-state": menuOpen ? "open" : "closed",
      onClick: (event: ReactMouseEvent<HTMLButtonElement>) => {
        event.stopPropagation();
      },
    };
    const icon = <Ellipsis className="size-3.5" />;
    if (surface === "sidebar") {
      return (
        <RailIconAction
          ref={triggerRef}
          className={cn(
            "group-hover/project-label:opacity-100 group-has-[[data-state=open]]/project-label:opacity-100",
            alwaysVisible && "opacity-100",
          )}
          {...props}
        >
          {icon}
        </RailIconAction>
      );
    }
    return (
      <Button
        ref={triggerRef}
        className={cn("data-[state=open]:bg-muted", alwaysVisible && "opacity-100")}
        size="icon"
        variant="ghost"
        {...props}
      >
        {icon}
      </Button>
    );
  };

  const renderDropdownEntry = (entry: ProjectMenuEntry, key: string): ReactNode => {
    if (entry.type === "separator") {
      return <DropdownMenuSeparator key={key} />;
    }
    if (entry.type === "submenu") {
      return (
        <DropdownMenuSub key={key}>
          <DropdownMenuSubTrigger>
            <span className="min-w-0 truncate">{entry.label}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-48 max-w-[calc(100vw-2rem)]">
            {entry.items.map((item, index) => renderDropdownEntry(item, `${key}-${index}`))}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      );
    }
    return (
      <DropdownMenuItem
        key={key}
        disabled={entry.disabled}
        onSelect={() => runMenuCommand(entry.id)}
      >
        {entry.icon}
        {entry.label}
      </DropdownMenuItem>
    );
  };

  const renderContextEntry = (entry: ProjectMenuEntry, key: string): ReactNode => {
    if (entry.type === "separator") {
      return <ContextMenuSeparator key={key} />;
    }
    if (entry.type === "submenu") {
      return (
        <ContextMenuSub key={key}>
          <ContextMenuSubTrigger>
            <span className="min-w-0 truncate">{entry.label}</span>
          </ContextMenuSubTrigger>
          <ContextMenuSubContent className="w-48 max-w-[calc(100vw-2rem)]">
            {entry.items.map((item, index) => renderContextEntry(item, `${key}-${index}`))}
          </ContextMenuSubContent>
        </ContextMenuSub>
      );
    }
    return (
      <ContextMenuItem
        key={key}
        disabled={entry.disabled}
        onSelect={() => runMenuCommand(entry.id)}
      >
        {entry.icon}
        {entry.label}
      </ContextMenuItem>
    );
  };

  const dropdownContent = (
    <DropdownMenuContent align="start" className="w-48">
      {menuEntries.map((entry, index) => renderDropdownEntry(entry, String(index)))}
    </DropdownMenuContent>
  );
  const menu = surface === "sidebar" ? (
    <ContextMenu
      open={openMenu === "context"}
      onOpenChange={(open) => setMenuSurfaceOpen("context", open)}
    >
      <DropdownMenu
        open={openMenu === "dropdown"}
        onOpenChange={(open) => setMenuSurfaceOpen("dropdown", open)}
      >
        <ContextMenuTrigger asChild>
          <DropdownMenuTrigger asChild>{actionsButton()}</DropdownMenuTrigger>
        </ContextMenuTrigger>
        {dropdownContent}
      </DropdownMenu>
      <ContextMenuContent className="w-48">
        {menuEntries.map((entry, index) => renderContextEntry(entry, String(index)))}
      </ContextMenuContent>
    </ContextMenu>
  ) : (
    <DropdownMenu
      open={openMenu === "dropdown"}
      onOpenChange={(open) => setMenuSurfaceOpen("dropdown", open)}
    >
      <DropdownMenuTrigger asChild>{actionsButton()}</DropdownMenuTrigger>
      {dropdownContent}
    </DropdownMenu>
  );

  return (
    <>
      {menu}
      <ProjectFormDialog
        description={t("project.editDescription")}
        directoryPaths={directoryPaths}
        homeDirectory={homeDirectory}
        isPending={editMutation.isPending}
        name={name}
        open={editOpen}
        submitDisabled={
          !name.trim() ||
          (name.trim() === project.name &&
            normalizedDirectoryList(directoryPaths) === normalizedDirectoryList(project.rootDirs))
        }
        submitLabel={t("common.save")}
        title={t("project.edit")}
        onChooseDirectories={() => void chooseDirectories()}
        onDirectoryPathsChange={setDirectoryPaths}
        onNameChange={setName}
        onOpenChange={(open) => {
          if (open) {
            openEdit();
          } else if (!editMutation.isPending) {
            setEditOpen(false);
          }
        }}
        onSubmit={saveProject}
      />
      <AlertDialog open={deleteOpen} onOpenChange={(open) => !deleteMutation.isPending && setDeleteOpen(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("project.deleteTitle").replace("{name}", project.name)}</AlertDialogTitle>
            <AlertDialogDescription>{t("project.deleteDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={deleteMutation.isPending}
              variant="destructive"
              onClick={() => deleteMutation.mutate()}
            >
              {deleteMutation.isPending ? <Spinner /> : null}
              {t("project.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

export function projectDirectoryLabel(path: string, paths: string[]) {
  const name = basename(path);
  if (paths.filter((candidate) => basename(candidate) === name).length < 2) {
    return name;
  }
  const normalized = path.replace(/[\\/]+$/, "");
  const parts = normalized.split(/[\\/]/).filter(Boolean);
  return parts.slice(-2).join("/") || path;
}

function basename(path: string) {
  const normalized = path.replace(/[\\/]+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}

function normalizedDirectorySet(paths: string[]) {
  return [...paths]
    .map((path) => path.trim().replace(/[\\/]+$/, ""))
    .filter(Boolean)
    .sort()
    .join("\n");
}

function normalizedDirectoryList(paths: string[]) {
  return paths
    .map((path) => path.trim().replace(/[\\/]+$/, ""))
    .filter(Boolean)
    .join("\n");
}
