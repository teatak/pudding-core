import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, GitBranch, Star, Trash } from "@/components/icons";
import { toast } from "sonner";

import {
  deleteProjectAppBinding,
  listAppConnections,
  listGitHubConnectionRepositories,
  listProjectAppBindings,
  putProjectAppBinding,
  type Project,
  type ProjectAppBinding,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppSelectContent } from "@/components/AppSelectContent";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Select, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/i18n";
import { openExternalURL } from "@/lib/desktopBridge";

export function ProjectGitHubDialog({ open, project, token, onOpenChange }: {
  open: boolean;
  project: Project;
  token: string;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [connectionID, setConnectionID] = useState("");
  const [repositoryValue, setRepositoryValue] = useState("");
  const [rootDir, setRootDir] = useState("");
  const connectionsQuery = useQuery({
    queryKey: queryKeys.appConnections(),
    queryFn: () => listAppConnections(token),
    enabled: open,
  });
  const bindingsQuery = useQuery({
    queryKey: queryKeys.projectAppBindings(project.id),
    queryFn: () => listProjectAppBindings(token, project.id),
    enabled: open,
  });
  const githubConnections = useMemo(
    () => (connectionsQuery.data?.connections || []).filter((connection) =>
      connection.appID === "github" && connection.authType === "oauth2" && !connection.reauthorizationRequired,
    ),
    [connectionsQuery.data?.connections],
  );
  const githubBindings = useMemo(
    () => (bindingsQuery.data?.bindings || []).filter((binding) => binding.appID === "github" && binding.resourceType === "repository"),
    [bindingsQuery.data?.bindings],
  );
  const repositoriesQuery = useQuery({
    queryKey: queryKeys.githubConnectionRepositories(connectionID),
    queryFn: () => listGitHubConnectionRepositories(token, connectionID),
    enabled: open && Boolean(connectionID),
  });

  useEffect(() => {
    if (!open) return;
    const primary = githubBindings.find((binding) => binding.primary);
    const nextConnectionID = primary?.connectionID || githubConnections[0]?.id || "";
    setConnectionID((current) => current || nextConnectionID);
    setRootDir((current) => current || project.rootDirs[0] || "");
  }, [githubBindings, githubConnections, open, project.rootDirs]);

  useEffect(() => setRepositoryValue(""), [connectionID]);

  const selectedRepository = useMemo(() => {
    const [installationID, repositoryID] = repositoryValue.split(":", 2);
    const installation = repositoriesQuery.data?.installations.find((item) => item.id === installationID);
    const repository = installation?.repositories.find((item) => item.id === repositoryID);
    return installation && repository ? { installation, repository } : null;
  }, [repositoriesQuery.data?.installations, repositoryValue]);
  const manageInstallation = selectedRepository?.installation || repositoriesQuery.data?.installations[0];

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: queryKeys.projectAppBindings(project.id) });
  };
  const saveMutation = useMutation({
    mutationFn: ({ binding, primary = false }: { binding?: ProjectAppBinding; primary?: boolean }) => {
      const selected = binding
        ? { connectionID: binding.connectionID, resourceID: binding.resourceID, resourceName: binding.resourceName, metadata: binding.metadata }
        : selectedRepository
          ? {
              connectionID,
              resourceID: selectedRepository.repository.id,
              resourceName: selectedRepository.repository.fullName,
              metadata: { rootDir },
            }
          : null;
      if (!selected) throw new Error("repository required");
      return putProjectAppBinding(token, project.id, {
        appID: "github",
        connectionID: selected.connectionID,
        resourceType: "repository",
        resourceID: selected.resourceID,
        resourceName: selected.resourceName,
        metadata: selected.metadata,
        primary,
      });
    },
    onSuccess: async () => {
      setRepositoryValue("");
      await invalidate();
      toast.success(t("project.githubSaved"));
    },
    onError: () => toast.error(t("project.githubSaveFailed")),
  });
  const deleteMutation = useMutation({
    mutationFn: (binding: ProjectAppBinding) => deleteProjectAppBinding(token, project.id, binding.id),
    onSuccess: async () => {
      await invalidate();
      toast.success(t("project.githubRemoved"));
    },
    onError: () => toast.error(t("project.githubRemoveFailed")),
  });

  const loading = connectionsQuery.isLoading || bindingsQuery.isLoading;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[min(760px,calc(100svh-2rem))] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><GitBranch className="size-4" />{t("project.githubTitle")}</DialogTitle>
          <DialogDescription>{t("project.githubDescription")}</DialogDescription>
        </DialogHeader>
        {loading ? <div className="grid place-items-center py-10"><Spinner /></div> : githubConnections.length === 0 ? (
          <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
            {t("project.githubConnectionRequired")}
          </div>
        ) : (
          <div className="grid gap-5">
            {githubBindings.length > 0 ? (
              <section className="grid gap-2">
                <Label>{t("project.githubBoundRepositories")}</Label>
                <div className="grid overflow-hidden rounded-lg border">
                  {githubBindings.map((binding, index) => (
                    <div key={binding.id} className={`flex items-center gap-3 px-3 py-2.5 ${index > 0 ? "border-t" : ""}`}>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-sm font-medium">{binding.resourceName}</span>
                          {binding.primary ? <Badge variant="secondary">{t("project.githubPrimary")}</Badge> : null}
                        </div>
                        {binding.metadata?.rootDir ? <div className="truncate text-xs text-muted-foreground">{binding.metadata.rootDir}</div> : null}
                      </div>
                      {!binding.primary ? (
                        <Button aria-label={t("project.githubMakePrimary")} disabled={saveMutation.isPending} size="icon-xs" type="button" variant="ghost" onClick={() => saveMutation.mutate({ binding, primary: true })}>
                          <Star className="size-3.5" />
                        </Button>
                      ) : null}
                      <Button aria-label={t("common.delete")} disabled={deleteMutation.isPending} size="icon-xs" type="button" variant="ghost" onClick={() => deleteMutation.mutate(binding)}>
                        <Trash className="size-3.5" />
                      </Button>
                    </div>
                  ))}
                </div>
              </section>
            ) : null}
            <section className="grid gap-3 rounded-lg border p-4">
              <div className="grid gap-1.5">
                <Label>{t("project.githubConnection")}</Label>
                <Select value={connectionID} onValueChange={setConnectionID}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <AppSelectContent>
                    {githubConnections.map((connection) => (
                      <SelectItem key={connection.id} value={connection.id}>
                        {connection.account?.login ? `@${connection.account.login}` : connection.name || connection.id}
                      </SelectItem>
                    ))}
                  </AppSelectContent>
                </Select>
              </div>
              <div className="grid gap-1.5">
                <div className="flex items-center justify-between gap-2">
                  <Label>{t("project.githubRepository")}</Label>
                  {manageInstallation?.htmlURL ? (
                    <Button className="h-7 px-2 text-xs" type="button" variant="ghost" onClick={() => void openExternalURL(manageInstallation.htmlURL)}>
                      <ExternalLink className="size-3.5" />{t("project.githubManageAccess")}
                    </Button>
                  ) : null}
                </div>
                {repositoriesQuery.isLoading ? <div className="flex h-9 items-center px-3"><Spinner /></div> : (
                  <Select value={repositoryValue} onValueChange={setRepositoryValue}>
                    <SelectTrigger><SelectValue placeholder={t("project.githubRepositoryPlaceholder")} /></SelectTrigger>
                    <AppSelectContent>
                      {(repositoriesQuery.data?.installations || []).flatMap((installation) =>
                        installation.repositories.map((repository) => (
                          <SelectItem key={`${installation.id}:${repository.id}`} value={`${installation.id}:${repository.id}`}>
                            {repository.fullName}{repository.private ? ` · ${t("project.githubPrivate")}` : ""}
                          </SelectItem>
                        )),
                      )}
                    </AppSelectContent>
                  </Select>
                )}
              </div>
              {project.rootDirs.length > 0 ? (
                <div className="grid gap-1.5">
                  <Label>{t("project.githubDirectory")}</Label>
                  <Select value={rootDir} onValueChange={setRootDir}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <AppSelectContent>
                      {project.rootDirs.map((path) => <SelectItem key={path} value={path}>{path}</SelectItem>)}
                    </AppSelectContent>
                  </Select>
                </div>
              ) : null}
              {repositoriesQuery.isError ? <p className="text-sm text-destructive">{t("project.githubRepositoriesFailed")}</p> : null}
              <Button disabled={!selectedRepository || saveMutation.isPending} type="button" onClick={() => saveMutation.mutate({ primary: githubBindings.length === 0 })}>
                {saveMutation.isPending ? <Spinner /> : null}{t("project.githubAddRepository")}
              </Button>
            </section>
          </div>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("common.close")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
