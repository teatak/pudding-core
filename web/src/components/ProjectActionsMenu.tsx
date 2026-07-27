import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Ellipsis, FolderCog, Trash } from "lucide-react";
import { useEffect, useState } from "react";
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
import { Spinner } from "@/components/Spinner";
import {
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
} from "@/components/ui/alert-dialog";
import { DropdownMenu, DropdownMenuSub, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useI18n } from "@/i18n";
import { pickDirectories } from "@/lib/desktopBridge";
import type { AppSearch } from "@/lib/route";
import { cn } from "@/lib/utils";

export function ProjectActionsMenu({
  alwaysVisible = false,
  project,
  surface = "default",
  token,
  onOverlayOpenChange,
}: {
  alwaysVisible?: boolean;
  project: Project;
  surface?: "default" | "sidebar";
  token: string;
  onOverlayOpenChange?: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const navigate = useNavigate({ from: "/" });
  const queryClient = useQueryClient();
  const [menuOpen, setMenuOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [name, setName] = useState(project.name);
  const [directoryPaths, setDirectoryPaths] = useState(project.rootDirs);
  const overlayOpen = menuOpen || editOpen || deleteOpen;
  const isMac =
    (typeof document !== "undefined" && document.documentElement.dataset.shell === "electron-mac") ||
    (typeof navigator !== "undefined" && /Mac/i.test(navigator.platform));

  useEffect(() => {
    onOverlayOpenChange?.(overlayOpen);
  }, [onOverlayOpenChange, overlayOpen]);
  useEffect(() => () => onOverlayOpenChange?.(false), [onOverlayOpenChange]);

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
    if (!nextName || directoryPaths.length === 0) return;
    const projects =
      queryClient.getQueryData<{ projects: Project[] }>(queryKeys.projects())?.projects || [];
    if (
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

  return (
    <>
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild>
          <button
            aria-label={t("project.actions")}
            className={cn(
              "flex size-6 shrink-0 items-center justify-center rounded-md opacity-0 group-hover/project-label:opacity-100 data-[state=open]:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:outline-hidden",
              surface === "sidebar"
                ? "text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground focus-visible:ring-sidebar-ring"
                : "text-muted-foreground hover:bg-accent hover:text-accent-foreground data-[state=open]:bg-accent data-[state=open]:text-accent-foreground focus-visible:ring-ring",
              alwaysVisible && "opacity-100",
            )}
            data-project-actions-open={menuOpen}
            type="button"
            onClick={(event) => event.stopPropagation()}
          >
            <Ellipsis className="size-3.5" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48 space-y-1">
          {project.rootDirs.length === 1 ? (
            <>
              {isMac ? (
                <DropdownMenuItem
                  disabled={revealMutation.isPending}
                  onSelect={() => revealMutation.mutate(project.rootDirs[0])}
                >
                  {revealMutation.isPending ? <Spinner /> : null}
                  {t("project.revealFinder")}
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuItem onSelect={() => copyPaths([project.rootDirs[0]])}>
                {t("project.copyPath")}
              </DropdownMenuItem>
            </>
          ) : (
            project.rootDirs.map((path) => (
              <DropdownMenuSub key={path}>
                <DropdownMenuSubTrigger>
                  <span className="min-w-0 truncate">
                    {projectDirectoryLabel(path, project.rootDirs)}
                  </span>
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-48 max-w-[calc(100vw-2rem)]">
                  {isMac ? (
                    <DropdownMenuItem
                      disabled={revealMutation.isPending}
                      onSelect={() => revealMutation.mutate(path)}
                    >
                      {revealMutation.isPending ? <Spinner /> : null}
                      {t("project.revealFinder")}
                    </DropdownMenuItem>
                  ) : null}
                  <DropdownMenuItem
                    onSelect={() => copyPaths([path])}
                  >
                    {t("project.copyPath")}
                  </DropdownMenuItem>
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            ))
          )}
          <DropdownMenuItem onSelect={openEdit}>
            <FolderCog />
            {t("project.edit")}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={deleteMutation.isPending} onSelect={() => setDeleteOpen(true)}>
            <Trash />
            {t("project.delete")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ProjectFormDialog
        description={t("project.editDescription")}
        directoryPaths={directoryPaths}
        isPending={editMutation.isPending}
        name={name}
        open={editOpen}
        submitDisabled={
          !name.trim() ||
          directoryPaths.length === 0 ||
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
            <AlertDialogAction disabled={deleteMutation.isPending} onClick={() => deleteMutation.mutate()}>
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
