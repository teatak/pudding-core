import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderClosed, FolderCog, FolderPlus, Search } from "@/components/icons";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { createProject, listProjects, type Project } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { PageHeader } from "@/components/PageHeader";
import { ProjectActionsMenu } from "@/components/ProjectActionsMenu";
import { ProjectFormDialog } from "@/components/ProjectFormDialog";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/i18n";
import { pickDirectories } from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";

type ProjectSort = "updated-desc" | "created-desc" | "name-asc" | "name-desc";

export function ProjectsPane({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const queryClient = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [rootDirs, setRootDirs] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<ProjectSort>("updated-desc");
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects(),
    queryFn: () => listProjects(token),
    enabled: Boolean(token),
  });
  const projects = projectsQuery.data?.projects || [];
  const visibleProjects = useMemo(() => {
    const normalizedSearch = search.trim().toLocaleLowerCase(locale);
    return [...projects]
      .filter((project) => {
        if (!normalizedSearch) {
          return true;
        }
        return [project.name, ...project.rootDirs].some((value) =>
          value.toLocaleLowerCase(locale).includes(normalizedSearch),
        );
      })
      .sort((left, right) => compareProjects(left, right, sort, locale));
  }, [locale, projects, search, sort]);

  const createMutation = useMutation({
    mutationFn: ({ projectName, paths }: { projectName: string; paths: string[] }) =>
      createProject(token, { name: projectName, rootDirs: paths }),
    onSuccess: (created) => {
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) => ({
        projects: [...(previous?.projects.filter((project) => project.id !== created.id) || []), created],
      }));
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects() });
      toast.success(t("project.added"));
      closeAddForm();
    },
    onError: () => toast.error(t("project.createFailed")),
  });

  function closeAddForm() {
    setAdding(false);
    setName("");
    setRootDirs([]);
  }

  async function chooseDirectories() {
    const picked = await pickDirectories({
      buttonLabel: t("project.createPickButton"),
      message: t("project.createPickMessage"),
      title: t("project.add"),
    });
    const nextPaths = Array.from(new Set([...rootDirs, ...picked].map((path) => path.trim()).filter(Boolean)));
    if (!name.trim() && nextPaths[0]) {
      setName(basename(nextPaths[0]));
    }
    setRootDirs(nextPaths);
  }

  function submitProject() {
    const projectName = name.trim();
    if (!projectName || rootDirs.length === 0) {
      return;
    }
    const normalizedPaths = normalizedDirectorySet(rootDirs);
    if (projects.some((project) => normalizedDirectorySet(project.rootDirs) === normalizedPaths)) {
      toast.error(t("project.alreadyExists"));
      return;
    }
    createMutation.mutate({ projectName, paths: rootDirs });
  }

  return (
    <main className="flex h-full w-full min-w-0 flex-1 flex-col overflow-hidden bg-background">
      <PageHeader
        icon={<FolderCog />}
        title={t("project.manage")}
      />
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="mx-auto grid w-full max-w-5xl gap-5 px-6 pt-4 pb-10">
          <section className="grid gap-4">
            <div className="flex flex-wrap items-center gap-2 border-b pb-4">
              <div className="relative min-w-48 flex-1">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  aria-label={t("project.searchPlaceholder")}
                  className="pl-8"
                  placeholder={t("project.searchPlaceholder")}
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                />
              </div>
              <Select value={sort} onValueChange={(value) => setSort(value as ProjectSort)}>
                <SelectTrigger aria-label={t("project.sortLabel")} className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="end">
                  <SelectItem value="updated-desc">{t("project.sortUpdated")}</SelectItem>
                  <SelectItem value="created-desc">{t("project.sortCreated")}</SelectItem>
                  <SelectItem value="name-asc">{t("project.sortNameAsc")}</SelectItem>
                  <SelectItem value="name-desc">{t("project.sortNameDesc")}</SelectItem>
                </SelectContent>
              </Select>
              <Button className="shrink-0" size="sm" type="button" onClick={() => setAdding(true)}>
                <FolderPlus className="size-3.5" />
                {t("project.add")}
              </Button>
            </div>
            <div className="text-xs text-muted-foreground">
              {t("project.resultCount").replace("{count}", String(visibleProjects.length))}
            </div>
            {projectsQuery.isLoading ? (
              <div className="flex justify-center py-12"><Spinner /></div>
            ) : projectsQuery.isError ? (
              <div className="grid justify-items-center gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-8 text-sm text-destructive">
                <span>{t("project.loadFailed")}</span>
                <Button size="sm" type="button" variant="outline" onClick={() => void projectsQuery.refetch()}>{t("common.refresh")}</Button>
              </div>
            ) : visibleProjects.length > 0 ? (
              <div className="grid gap-2">
                {visibleProjects.map((project) => (
                  <ProjectRow key={project.id} locale={locale} project={project} token={token} />
                ))}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed px-4 py-12 text-center text-sm text-muted-foreground">
                {projects.length > 0 ? t("project.noMatches") : t("project.empty")}
              </div>
            )}
          </section>
        </div>
      </div>
      <ProjectFormDialog
        description={t("project.addDescription")}
        directoryPaths={rootDirs}
        isPending={createMutation.isPending}
        name={name}
        open={adding}
        submitDisabled={!name.trim() || rootDirs.length === 0}
        submitLabel={t("project.add")}
        title={t("project.add")}
        onChooseDirectories={() => void chooseDirectories()}
        onDirectoryPathsChange={setRootDirs}
        onNameChange={setName}
        onOpenChange={(open) => {
          if (open) {
            setAdding(true);
          } else if (!createMutation.isPending) {
            closeAddForm();
          }
        }}
        onSubmit={submitProject}
      />
    </main>
  );
}

function ProjectRow({ locale, project, token }: { locale: string; project: Project; token: string }) {
  const { t } = useI18n();
  const [actionsOverlayOpen, setActionsOverlayOpen] = useState(false);
  return (
    <article
      className={cn(
        "group/project-label grid gap-3 rounded-xl border border-border/70 px-4 py-3 hover:bg-control-hover active:bg-control-active",
        actionsOverlayOpen && "bg-control-hover",
      )}
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
          <FolderClosed className="size-4.5" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-medium">{project.name}</h2>
          <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>{t("project.folderCount").replace("{count}", String(project.rootDirs.length))}</span>
            <span>{t("project.updatedAt").replace("{date}", formatProjectDate(project.updatedAt, locale))}</span>
          </div>
        </div>
        <ProjectActionsMenu
          alwaysVisible
          project={project}
          token={token}
          onOverlayOpenChange={setActionsOverlayOpen}
        />
      </div>
      <div className="grid gap-1 pl-12">
        {project.rootDirs.map((path) => (
          <div key={path} className="truncate text-xs text-muted-foreground" >{path}</div>
        ))}
      </div>
    </article>
  );
}

function compareProjects(left: Project, right: Project, sort: ProjectSort, locale: string) {
  if (sort === "name-asc") {
    return left.name.localeCompare(right.name, locale);
  }
  if (sort === "name-desc") {
    return right.name.localeCompare(left.name, locale);
  }
  const key = sort === "created-desc" ? "createdAt" : "updatedAt";
  return new Date(right[key]).getTime() - new Date(left[key]).getTime();
}

function formatProjectDate(value: string, locale: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function normalizedDirectorySet(paths: string[]) {
  return [...paths].map((path) => path.replace(/[\\/]+$/, "")).sort().join("\n");
}

function basename(path: string) {
  const normalized = path.replace(/[\\/]+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}
