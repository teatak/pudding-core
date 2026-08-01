import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { createProject, type Project } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ProjectFormDialog } from "@/components/ProjectFormDialog";
import { useI18n } from "@/i18n";
import { getDesktopHomeDirectory, pickDirectories } from "@/lib/desktopBridge";

export function ProjectCreateDialog({
  open,
  token,
  onCreated,
  onOpenChange,
}: {
  open: boolean;
  token: string;
  onCreated: (projectID: string) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [rootDirs, setRootDirs] = useState<string[]>([]);
  const [homeDirectory, setHomeDirectory] = useState("");

  useEffect(() => {
    let current = true;
    void getDesktopHomeDirectory().then((next) => {
      if (current) {
        setHomeDirectory(next);
      }
    });
    return () => {
      current = false;
    };
  }, []);

  const createMutation = useMutation({
    mutationFn: ({ projectName, paths }: { projectName: string; paths: string[] }) =>
      createProject(token, { name: projectName, rootDirs: paths }),
    onSuccess: (created) => {
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) => ({
        projects: [...(previous?.projects.filter((project) => project.id !== created.id) || []), created],
      }));
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects() });
      toast.success(t("project.added"));
      resetForm();
      onOpenChange(false);
      onCreated(created.id);
    },
    onError: () => toast.error(t("project.createFailed")),
  });

  function resetForm() {
    setName("");
    setRootDirs([]);
  }

  async function chooseDirectories() {
    const picked = await pickDirectories({
      buttonLabel: t("project.createPickButton"),
      message: t("project.createPickMessage"),
      title: t("project.add"),
    });
    const nextPaths = Array.from(
      new Set([...rootDirs, ...picked].map((path) => path.trim()).filter(Boolean)),
    );
    if (!name.trim() && nextPaths[0]) {
      setName(basename(nextPaths[0]));
    }
    setRootDirs(nextPaths);
  }

  function submitProject() {
    const projectName = name.trim();
    if (!projectName) {
      return;
    }
    const projects = queryClient.getQueryData<{ projects: Project[] }>(queryKeys.projects())?.projects || [];
    const normalizedPaths = normalizedDirectorySet(rootDirs);
    if (
      rootDirs.length > 0 &&
      projects.some((project) => normalizedDirectorySet(project.rootDirs) === normalizedPaths)
    ) {
      toast.error(t("project.alreadyExists"));
      return;
    }
    createMutation.mutate({ projectName, paths: rootDirs });
  }

  return (
    <ProjectFormDialog
      description={t("project.emptyDescription")}
      directoryPaths={rootDirs}
      homeDirectory={homeDirectory}
      isPending={createMutation.isPending}
      name={name}
      open={open}
      submitDisabled={!name.trim()}
      submitLabel={t("project.add")}
      title={t("project.emptyTitle")}
      onChooseDirectories={() => void chooseDirectories()}
      onDirectoryPathsChange={setRootDirs}
      onNameChange={setName}
      onOpenChange={(nextOpen) => {
        if (nextOpen) {
          onOpenChange(true);
          return;
        }
        if (!createMutation.isPending) {
          resetForm();
          onOpenChange(false);
        }
      }}
      onSubmit={submitProject}
    />
  );
}

function normalizedDirectorySet(paths: string[]) {
  return [...paths].map((path) => path.replace(/[\\/]+$/, "")).sort().join("\n");
}

function basename(path: string) {
  const normalized = path.replace(/[\\/]+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}
