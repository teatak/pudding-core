import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, FolderOpen, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { getProject, updateProject, type Project } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

const projectApprovalModes: Project["approvalMode"][] = ["ask", "auto", "full"];

export function ProjectComposerControls({
  projectID,
  showProjectName = false,
  token,
}: {
  projectID: string;
  showProjectName?: boolean;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const projectQuery = useQuery({
    queryKey: queryKeys.project(projectID),
    queryFn: () => getProject(token, projectID),
    enabled: Boolean(token && projectID),
  });
  const updateApprovalMutation = useMutation({
    mutationFn: (approvalMode: Project["approvalMode"]) => {
      if (!projectID) {
        throw new Error("missing project");
      }
      return updateProject(token, projectID, { approvalMode });
    },
    onSuccess: (updated) => {
      queryClient.setQueryData(queryKeys.project(updated.id), updated);
      queryClient.setQueryData<{ projects: Project[] }>(queryKeys.projects(), (previous) => {
        if (!previous) {
          return previous;
        }
        return {
          projects: previous.projects.map((project) => (project.id === updated.id ? updated : project)),
        };
      });
    },
    onError: () => toast.error(t("composer.projectApprovalSaveFailed")),
    onSettled: () => {
      if (projectID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.project(projectID) });
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects() });
    },
  });

  if (!projectID) {
    return null;
  }

  const projectName = projectQuery.data?.name || basename(projectQuery.data?.rootDirs[0] || "") || t("composer.projectLoading");
  return (
    <>
      {showProjectName ? (
        <span
          className="pudding-composer-project-name flex h-7 min-w-0 max-w-36 shrink items-center gap-1.5 rounded-full bg-muted/60 px-2 text-xs font-normal text-foreground"

        >
          {projectQuery.isLoading ? (
            <Spinner className="size-3" />
          ) : (
            <FolderOpen className="size-3 shrink-0 text-muted-foreground" />
          )}
          <span className="min-w-0 truncate">{projectName}</span>
        </span>
      ) : null}
      <ProjectApprovalControl
        busy={projectQuery.isLoading || updateApprovalMutation.isPending}
        project={projectQuery.data}
        projectID={projectID}
        onChange={(approvalMode) => updateApprovalMutation.mutate(approvalMode)}
      />
    </>
  );
}

function ProjectApprovalControl({
  project,
  projectID,
  busy,
  onChange,
}: {
  project: Project | undefined;
  projectID: string;
  busy: boolean;
  onChange: (approvalMode: Project["approvalMode"]) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  if (!projectID) {
    return null;
  }
  const value = project?.approvalMode || "auto";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          aria-label={`${t("composer.projectApproval")}: ${t(`composer.projectApproval.${value}`)}`}
          className="pudding-composer-approval-control h-8 max-w-32 gap-1.5 rounded-full px-2 text-[13px] font-normal"
          disabled={!project || busy}
          size="sm"

          type="button"
          variant="ghost"
        >
          {busy ? <Spinner className="size-4" /> : <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />}
          <span className="pudding-composer-approval-label min-w-0 truncate">{t(`composer.projectApproval.${value}`)}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-56 gap-1 p-1" collisionPadding={12} side="top" sideOffset={8}>
        {projectApprovalModes.map((mode) => (
          <button
            key={mode}
            aria-label={t("composer.projectApproval")}
            className={cn(
              "flex w-full items-start gap-1.5 rounded-md px-2 py-0.5 text-left text-sm hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden",
              value === mode && "bg-muted",
            )}
            type="button"
            onClick={() => {
              if (mode !== value) {
                onChange(mode);
              }
              setOpen(false);
            }}
          >
            <span className="mt-0.5 grid size-4 shrink-0 place-items-center">
              {value === mode ? <Check className="size-3.5" /> : null}
            </span>
            <span className="grid min-w-0">
              <span className="font-medium leading-5">{t(`composer.projectApproval.${mode}`)}</span>
              <span className="text-[10px] leading-3.5 text-muted-foreground">
                {t(`composer.projectApproval.${mode}.desc`)}
              </span>
            </span>
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}

function basename(path: string) {
  const normalized = path.replace(/\/+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}
