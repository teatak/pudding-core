import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Ellipsis, FolderCog, FolderOpen, FolderPlus, Trash, X } from "lucide-react";
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
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuSub, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { pickDirectories } from "@/lib/desktopBridge";
import type { AppSearch } from "@/lib/route";
import { cn } from "@/lib/utils";

export function ProjectActionsMenu({
  allowDirectoryEditing = false,
  alwaysVisible = false,
  project,
  surface = "default",
  token,
  onOverlayOpenChange,
}: {
  allowDirectoryEditing?: boolean;
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
  const [renameOpen, setRenameOpen] = useState(false);
  const [directoriesOpen, setDirectoriesOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [name, setName] = useState(project.name);
  const [directoryPaths, setDirectoryPaths] = useState(project.rootDirs);
  const overlayOpen = menuOpen || renameOpen || directoriesOpen || deleteOpen;
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
  const renameMutation = useMutation({
    mutationFn: (nextName: string) => updateProject(token, project.id, { name: nextName }),
    onSuccess: (updated) => {
      cacheProject(updated);
      setRenameOpen(false);
    },
    onError: () => toast.error(t("project.renameFailed")),
  });
  const directoriesMutation = useMutation({
    mutationFn: (paths: string[]) => updateProject(token, project.id, { rootDirs: paths }),
    onSuccess: async (updated) => {
      cacheProject(updated);
      setDirectoriesOpen(false);
      toast.success(t("project.directoriesUpdated"));
      const sessions = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions())?.sessions || [];
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
    onError: () => toast.error(t("project.updateDirectoriesFailed")),
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

  const openRename = () => {
    setName(project.name);
    setRenameOpen(true);
  };
  const openDirectories = () => {
    setDirectoryPaths(project.rootDirs);
    setDirectoriesOpen(true);
  };
  const chooseDirectories = async () => {
    const picked = await pickDirectories({
      buttonLabel: t("project.createPickButton"),
      message: t("project.editDirectoriesPickMessage"),
      title: t("project.editDirectoriesTitle"),
    });
    setDirectoryPaths((current) =>
      Array.from(new Set([...current, ...picked].map((path) => path.trim()).filter(Boolean))),
    );
  };
  const saveDirectories = () => {
    if (directoryPaths.length === 0) return;
    const projects = queryClient.getQueryData<{ projects: Project[] }>(queryKeys.projects())?.projects || [];
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
    directoriesMutation.mutate(directoryPaths);
  };
  const saveRename = () => {
    const nextName = name.trim();
    if (!nextName || nextName === project.name) {
      setRenameOpen(false);
      return;
    }
    renameMutation.mutate(nextName);
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
          {allowDirectoryEditing ? (
            <DropdownMenuItem onSelect={openDirectories}>
              <FolderCog />
              {t("project.editDirectories")}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuItem onSelect={openRename}>{t("project.rename")}</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={deleteMutation.isPending} onSelect={() => setDeleteOpen(true)}>
            <Trash />
            {t("project.delete")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <form
            className="contents"
            onSubmit={(event) => {
              event.preventDefault();
              saveRename();
            }}
          >
            <DialogHeader>
              <DialogTitle>{t("project.renameTitle")}</DialogTitle>
              <DialogDescription>{t("project.renameDescription")}</DialogDescription>
            </DialogHeader>
            <Input
              autoFocus
              aria-label={t("project.name")}
              disabled={renameMutation.isPending}
              maxLength={120}
              value={name}
              onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && event.nativeEvent.isComposing) {
                  event.preventDefault();
                }
              }}
            />
            <DialogFooter>
              <Button disabled={renameMutation.isPending} type="button" variant="outline" onClick={() => setRenameOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button disabled={renameMutation.isPending || !name.trim()} type="submit">
                {renameMutation.isPending ? <Spinner /> : null}
                {t("common.save")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog
        open={directoriesOpen}
        onOpenChange={(open) => {
          if (open) {
            openDirectories();
          } else if (!directoriesMutation.isPending) {
            setDirectoriesOpen(false);
          }
        }}
      >
        <DialogContent className="max-h-[min(680px,calc(100svh-2rem))] sm:max-w-xl">
          <form
            className="contents"
            onSubmit={(event) => {
              event.preventDefault();
              saveDirectories();
            }}
          >
            <DialogHeader>
              <DialogTitle>{t("project.editDirectoriesTitle")}</DialogTitle>
              <DialogDescription>
                {t("project.editDirectoriesDescription").replace("{name}", project.name)}
              </DialogDescription>
            </DialogHeader>
            <div className="grid min-h-0 gap-2 overflow-y-auto pr-1">
              <div className="flex items-center justify-between gap-3">
                <span className="text-sm">{t("project.directories")}</span>
                <Button
                  disabled={directoriesMutation.isPending}
                  size="sm"
                  type="button"
                  variant="outline"
                  onClick={() => void chooseDirectories()}
                >
                  <FolderPlus className="size-3.5" />
                  {t("project.chooseFolders")}
                </Button>
              </div>
              <div className="grid gap-1 rounded-lg border p-1">
                {directoryPaths.map((path) => (
                  <div
                    key={path}
                    className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted/70 focus-within:bg-muted/70"
                  >
                    <FolderOpen className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate text-sm">{path}</span>
                    <Button
                      aria-label={t("project.removeDirectory").replace("{name}", basename(path))}
                      disabled={directoriesMutation.isPending || directoryPaths.length === 1}
                      size="icon-xs"
                      type="button"
                      variant="ghost"
                      onClick={() =>
                        setDirectoryPaths((current) => current.filter((entry) => entry !== path))
                      }
                    >
                      <X />
                    </Button>
                  </div>
                ))}
              </div>
            </div>
            <DialogFooter>
              <Button
                disabled={directoriesMutation.isPending}
                type="button"
                variant="outline"
                onClick={() => setDirectoriesOpen(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button
                disabled={
                  directoriesMutation.isPending ||
                  directoryPaths.length === 0 ||
                  normalizedDirectorySet(directoryPaths) ===
                    normalizedDirectorySet(project.rootDirs)
                }
                type="submit"
              >
                {directoriesMutation.isPending ? <Spinner /> : null}
                {t("common.save")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
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
