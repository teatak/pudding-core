import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Ellipsis, FolderOpen, Trash } from "lucide-react";
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
  const [renameOpen, setRenameOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [name, setName] = useState(project.name);
  const rootDir = project.rootDirs[0];
  const overlayOpen = menuOpen || renameOpen || deleteOpen;
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
  const renameMutation = useMutation({
    mutationFn: (nextName: string) => updateProject(token, project.id, { name: nextName }),
    onSuccess: (updated) => {
      queryClient.setQueryData(queryKeys.project(updated.id), updated);
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) =>
        previous
          ? { projects: previous.projects.map((entry) => (entry.id === updated.id ? updated : entry)) }
          : { projects: [updated] },
      );
      setRenameOpen(false);
    },
    onError: () => toast.error(t("project.renameFailed")),
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
          {project.rootDirs.length > 1 ? (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <FolderOpen />
                {t("project.directories")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-64 max-w-[calc(100vw-2rem)]">
                {project.rootDirs.map((path) => (
                  <DropdownMenuItem
                    key={path}
                    disabled={!isMac || revealMutation.isPending}

                    onSelect={() => revealMutation.mutate(path)}
                  >
                    <span className="min-w-0 truncate">{projectDirectoryLabel(path, project.rootDirs)}</span>
                  </DropdownMenuItem>
                ))}
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => copyPaths(project.rootDirs)}>
                  {t("project.copyAllPaths")}
                </DropdownMenuItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          ) : (
            <>
              {isMac && rootDir ? (
                <DropdownMenuItem disabled={revealMutation.isPending} onSelect={() => revealMutation.mutate(rootDir)}>
                  {revealMutation.isPending ? <Spinner /> : null}
                  {t("project.revealFinder")}
                </DropdownMenuItem>
              ) : null}
              {rootDir ? (
                <DropdownMenuItem  onSelect={() => copyPaths([rootDir])}>
                  {t("project.copyPath")}
                </DropdownMenuItem>
              ) : null}
            </>
          )}
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
