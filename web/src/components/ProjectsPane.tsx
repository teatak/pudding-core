import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderClosed, FolderPlus, Folders, Search, X } from "@/components/icons";
import {
  useEffect,
  useMemo,
  useState,
  type DragEvent,
  type FormEvent,
} from "react";
import { toast } from "sonner";

import { createProject, listProjects, type Project } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppSelectContent } from "@/components/AppSelectContent";
import { ProjectEmptyIllustration } from "@/components/illustrations/ProjectEmptyIllustration";
import { PageHeader } from "@/components/PageHeader";
import { ProjectActionsMenu } from "@/components/ProjectActionsMenu";
import { ProjectFormDialog } from "@/components/ProjectFormDialog";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useI18n } from "@/i18n";
import { getDesktopHomeDirectory, pickDirectories } from "@/lib/desktopBridge";
import { displayUserPath } from "@/lib/displayPath";
import { droppedLocalItemsFromDataTransfer } from "@/lib/localFolders";
import { cn } from "@/lib/utils";

type ProjectSort = "updated-desc" | "created-desc" | "name-asc" | "name-desc";

const projectToolsThreshold = 6;

export function ProjectsPane({
  token,
  onOpenProjectDraft,
}: {
  token: string;
  onOpenProjectDraft: (projectID: string) => void;
}) {
  const { locale, t } = useI18n();
  const queryClient = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [rootDirs, setRootDirs] = useState<string[]>([]);
  const [homeDirectory, setHomeDirectory] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<ProjectSort>("updated-desc");

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

  const projectsQuery = useQuery({
    queryKey: queryKeys.projects(),
    queryFn: () => listProjects(token),
    enabled: Boolean(token),
  });
  const projects = projectsQuery.data?.projects || [];
  const showProjectTools = projects.length >= projectToolsThreshold;
  const visibleProjects = useMemo(() => {
    const normalizedSearch = showProjectTools ? search.trim().toLocaleLowerCase(locale) : "";
    return [...projects]
      .filter((project) => {
        if (!normalizedSearch) {
          return true;
        }
        return [project.name, ...project.rootDirs].some((value) =>
          value.toLocaleLowerCase(locale).includes(normalizedSearch),
        );
      })
      .sort((left, right) =>
        compareProjects(left, right, showProjectTools ? sort : "updated-desc", locale),
      );
  }, [locale, projects, search, showProjectTools, sort]);

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
      onOpenProjectDraft(created.id);
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
    if (!projectName) {
      return;
    }
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
    <main className="flex h-full w-full min-w-0 flex-1 flex-col overflow-hidden bg-background">
      <PageHeader
        icon={<Folders aria-hidden="true" />}
        title={t("project.manage")}
      />
      <div className="min-h-0 flex-1 overflow-auto">
        {projectsQuery.isLoading ? (
          <div className="grid min-h-full place-items-center">
            <Spinner />
          </div>
        ) : projectsQuery.isError ? (
          <div className="mx-auto w-full max-w-5xl px-6 pt-4 pb-10">
            <div className="grid justify-items-center gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-8 text-sm text-destructive">
              <span>{t("project.loadFailed")}</span>
              <Button size="sm" type="button" variant="outline" onClick={() => void projectsQuery.refetch()}>
                {t("common.refresh")}
              </Button>
            </div>
          </div>
        ) : projects.length === 0 ? (
          <ProjectEmptyState
            isPending={createMutation.isPending}
            homeDirectory={homeDirectory}
            name={name}
            rootDirs={rootDirs}
            onChooseDirectories={() => void chooseDirectories()}
            onDirectoryPathsChange={setRootDirs}
            onNameChange={setName}
            onRemoveDirectory={(path) =>
              setRootDirs((current) => current.filter((entry) => entry !== path))
            }
            onSubmit={submitProject}
          />
        ) : (
          <div className="mx-auto grid w-full max-w-5xl gap-5 px-6 pt-4 pb-10">
            <section className="grid gap-4">
              <div className="flex flex-wrap items-center gap-2 pb-1">
                <div className="mr-2 shrink-0 text-xs text-muted-foreground">
                  {t("project.resultCount").replace("{count}", String(visibleProjects.length))}
                </div>
                <div className="ml-auto flex min-w-0 flex-1 flex-wrap items-center justify-end gap-2">
                  {showProjectTools ? (
                    <>
                      <div className="relative min-w-48 w-full max-w-md">
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
                        <AppSelectContent>
                          <SelectGroup>
                            <SelectLabel>{t("project.sortLabel")}</SelectLabel>
                            <SelectItem value="updated-desc">{t("project.sortUpdated")}</SelectItem>
                            <SelectItem value="created-desc">{t("project.sortCreated")}</SelectItem>
                            <SelectItem value="name-asc">{t("project.sortNameAsc")}</SelectItem>
                            <SelectItem value="name-desc">{t("project.sortNameDesc")}</SelectItem>
                          </SelectGroup>
                        </AppSelectContent>
                      </Select>
                    </>
                  ) : null}
                  <Button
                    className="shrink-0"
                    type="button"
                    variant="secondary"
                    onClick={() => setAdding(true)}
                  >
                    <FolderPlus className="size-4" />
                    {t("project.add")}
                  </Button>
                </div>
              </div>
              {visibleProjects.length > 0 ? (
                <div className="grid grid-cols-2 gap-2">
                  {visibleProjects.map((project) => (
                    <ProjectRow
                      key={project.id}
                      homeDirectory={homeDirectory}
                      locale={locale}
                      project={project}
                      token={token}
                      onOpenProjectDraft={onOpenProjectDraft}
                    />
                  ))}
                </div>
              ) : (
                <div className="rounded-lg border border-dashed px-4 py-12 text-center text-sm text-muted-foreground">
                  {t("project.noMatches")}
                </div>
              )}
            </section>
          </div>
        )}
      </div>
      <ProjectFormDialog
        description={t("project.emptyDescription")}
        directoryPaths={rootDirs}
        homeDirectory={homeDirectory}
        isPending={createMutation.isPending}
        name={name}
        open={adding}
        submitDisabled={!name.trim()}
        submitLabel={t("project.add")}
        title={t("project.emptyTitle")}
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

function ProjectEmptyState({
  homeDirectory,
  isPending,
  name,
  rootDirs,
  onChooseDirectories,
  onDirectoryPathsChange,
  onNameChange,
  onRemoveDirectory,
  onSubmit,
}: {
  homeDirectory: string;
  isPending: boolean;
  name: string;
  rootDirs: string[];
  onChooseDirectories: () => void;
  onDirectoryPathsChange: (paths: string[]) => void;
  onNameChange: (name: string) => void;
  onRemoveDirectory: (path: string) => void;
  onSubmit: () => void;
}) {
  const { t } = useI18n();
  const [dropActive, setDropActive] = useState(false);
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSubmit();
  };

  useEffect(() => {
    const resetDropState = () => setDropActive(false);
    window.addEventListener("dragend", resetDropState);
    window.addEventListener("drop", resetDropState);
    window.addEventListener("blur", resetDropState);
    return () => {
      window.removeEventListener("dragend", resetDropState);
      window.removeEventListener("drop", resetDropState);
      window.removeEventListener("blur", resetDropState);
    };
  }, []);

  const handleDragEnter = (event: DragEvent<HTMLDivElement>) => {
    if (isPending || !dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    setDropActive(true);
  };

  const handleDragOver = (event: DragEvent<HTMLDivElement>) => {
    if (isPending || !dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
    setDropActive(true);
  };

  const handleDragLeave = (event: DragEvent<HTMLDivElement>) => {
    if (!dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    const nextTarget = event.relatedTarget;
    if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) return;
    setDropActive(false);
  };

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    if (isPending || !dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    setDropActive(false);
    const dropped = droppedLocalItemsFromDataTransfer(event.dataTransfer);
    const paths = Array.from(
      new Set(
        [...rootDirs, ...dropped.folderPaths]
          .map((path) => path.trim())
          .filter(Boolean),
      ),
    );
    if (paths.length > rootDirs.length) {
      onDirectoryPathsChange(paths);
      if (!name.trim() && paths[0]) {
        onNameChange(basename(paths[0]));
      }
    }
    if (dropped.folderPathUnavailable) {
      toast.error(t("project.folderDropPathUnavailable"));
    }
  };

  return (
    <section className="flex min-h-full items-start justify-center px-6 pt-[clamp(3rem,8vh,6rem)] pb-16">
      <div className="grid w-full max-w-lg justify-items-center text-center">
        <ProjectEmptyIllustration />
        <h2 className="mt-2 text-2xl font-semibold tracking-tight">{t("project.emptyTitle")}</h2>
        <p className="mt-2 max-w-md text-sm leading-6 text-muted-foreground">
          {t("project.emptyDescription")}
        </p>
        <form className="mt-7 grid w-full gap-3 text-left" onSubmit={submit}>
          <label>
            <span className="sr-only">{t("project.name")}</span>
            <Input
              autoFocus
              className="h-11 rounded-xl bg-background px-4"
              disabled={isPending}
              maxLength={120}
              placeholder={t("project.namePlaceholder")}
              value={name}
              onChange={(event) => onNameChange(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && event.nativeEvent.isComposing) {
                  event.preventDefault();
                }
              }}
            />
          </label>
          <div
            className={cn(
              "relative overflow-hidden rounded-xl border bg-background transition-colors",
              rootDirs.length === 0 && "border-dashed",
              dropActive && "border-info bg-accent/40",
            )}
            onDragEnter={handleDragEnter}
            onDragLeave={handleDragLeave}
            onDragOver={handleDragOver}
            onDrop={handleDrop}
          >
            {rootDirs.length > 0 ? (
              <>
                {rootDirs.map((path) => (
                  <div className="flex min-w-0 items-center gap-2 border-b px-3 py-2.5" key={path}>
                    <FolderClosed className="size-4 shrink-0 text-foreground" />
                    <span className="min-w-0 flex-1 truncate text-sm font-normal">
                      {displayUserPath(path, homeDirectory)}
                    </span>
                    <Button
                      aria-label={t("project.removeDirectory").replace("{name}", basename(path))}
                      disabled={isPending}
                      size="icon-xs"
                      type="button"
                      variant="ghost"
                      onClick={() => onRemoveDirectory(path)}
                    >
                      <X />
                    </Button>
                  </div>
                ))}
                <button
                  className="flex h-12 w-full items-center justify-center gap-2 bg-transparent px-3 text-sm font-normal text-foreground transition-colors hover:bg-interactive-hover active:bg-interactive-pressed focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50"
                  disabled={isPending}
                  type="button"
                  onClick={onChooseDirectories}
                >
                  <FolderPlus className="size-4" />
                  {t("project.addFolder")}
                </button>
              </>
            ) : (
              <button
                className="flex min-h-20 w-full flex-col items-center justify-center gap-1.5 bg-transparent px-4 py-3 text-center text-sm transition-colors hover:bg-interactive-hover active:bg-interactive-pressed focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50"
                disabled={isPending}
                type="button"
                onClick={onChooseDirectories}
              >
                <span className="flex items-center gap-2 font-normal">
                  <FolderPlus className="size-4" />
                  {t("project.addFolder")}
                </span>
                <span className="max-w-sm text-xs leading-5 font-normal text-muted-foreground">
                  {t("project.addFolderEmptyDescription")}
                </span>
              </button>
            )}
            {dropActive ? (
              <div className="pointer-events-none absolute inset-1 z-10 grid place-items-center rounded-lg bg-background/95 px-4 text-center text-sm font-medium">
                {t("project.folderDropHint")}
              </div>
            ) : null}
          </div>
          <Button className="h-11 rounded-xl" disabled={isPending || !name.trim()} type="submit">
            {isPending ? <Spinner /> : null}
            {t("project.add")}
          </Button>
        </form>
      </div>
    </section>
  );
}

function ProjectRow({
  homeDirectory,
  locale,
  project,
  token,
  onOpenProjectDraft,
}: {
  homeDirectory: string;
  locale: string;
  project: Project;
  token: string;
  onOpenProjectDraft: (projectID: string) => void;
}) {
  const { t } = useI18n();
  const newConversationLabel = t("project.newConversation");
  return (
    <article className="group/project-label relative grid gap-2 rounded-xl border border-border/70 px-4 py-2.5 transition-colors hover:border-border hover:bg-accent/25">
      <button
        aria-label={`${newConversationLabel}：${project.name}`}
        className="absolute inset-0 cursor-pointer rounded-xl focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
        type="button"
        onClick={() => onOpenProjectDraft(project.id)}
      />
      <div className="pointer-events-none relative z-10 flex min-w-0 items-center gap-3">
        <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
          <FolderClosed className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-medium">{project.name}</h2>
          <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>
              {formatProjectActivityAt(project.lastActivityAt || project.updatedAt, locale, t)}
            </span>
            {project.rootDirs.length !== 1 ? (
              <span>
                {t("project.folderCount").replace("{count}", String(project.rootDirs.length))}
              </span>
            ) : null}
          </div>
        </div>
        <div className="pointer-events-auto relative z-20 flex shrink-0 items-center gap-1">
          <ProjectActionsMenu
            alwaysVisible
            homeDirectory={homeDirectory}
            project={project}
            token={token}
          />
        </div>
      </div>
      {project.rootDirs.length > 0 ? (
        <div className="pointer-events-none relative z-10 grid gap-1 pt-0.5 pl-11">
          {project.rootDirs.map((path) => (
            <div key={path} className="truncate text-xs text-muted-foreground/80">
              {displayUserPath(path, homeDirectory)}
            </div>
          ))}
        </div>
      ) : null}
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
  const leftValue =
    sort === "updated-desc" ? left.lastActivityAt || left.updatedAt : left[key];
  const rightValue =
    sort === "updated-desc" ? right.lastActivityAt || right.updatedAt : right[key];
  return new Date(rightValue).getTime() - new Date(leftValue).getTime();
}

function formatProjectActivityAt(value: string, locale: string, t: (key: string) => string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const now = new Date();
  const elapsed = now.getTime() - date.getTime();
  if (elapsed >= -60_000 && elapsed < 60_000) {
    return t("project.activityJustNow");
  }
  if (elapsed >= 0 && elapsed < 60 * 60_000) {
    return t("project.activityMinutesAgo").replace(
      "{count}",
      String(Math.max(1, Math.floor(elapsed / 60_000))),
    );
  }
  const time = new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
  if (isSameCalendarDay(date, now)) {
    return t("project.activityToday").replace("{time}", time);
  }
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (isSameCalendarDay(date, yesterday)) {
    return t("project.activityYesterday").replace("{time}", time);
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function isSameCalendarDay(left: Date, right: Date) {
  return (
    left.getFullYear() === right.getFullYear() &&
    left.getMonth() === right.getMonth() &&
    left.getDate() === right.getDate()
  );
}

function normalizedDirectorySet(paths: string[]) {
  return [...paths].map((path) => path.replace(/[\\/]+$/, "")).sort().join("\n");
}

function basename(path: string) {
  const normalized = path.replace(/[\\/]+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}

function dataTransferHasFiles(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types || []).includes("Files");
}
