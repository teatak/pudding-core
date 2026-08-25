import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, FolderOpen, Hand, ShieldAlert, ShieldCheck } from "@/components/icons";
import { useState } from "react";
import { toast } from "sonner";

import { getProject, updateProject, type Project } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  AppPopoverContent as PopoverContent,
  appPopoverItemStateClassName,
  appPopoverSelectedItemStateClassName,
} from "@/components/AppPopover";
import { Spinner } from "@/components/Spinner";
import { composerControlStateClassName } from "@/components/composerControlStyles";
import { Button } from "@/components/ui/button";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

const projectApprovalModes: Project["approvalMode"][] = ["ask", "auto", "full"];
const projectApprovalModeIcons = {
  ask: Hand,
  auto: ShieldCheck,
  full: ShieldAlert,
};

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
          className="pudding-composer-project-name flex h-8 min-w-0 max-w-36 shrink items-center gap-1.5 rounded-full bg-muted/60 px-2 text-xs font-normal text-foreground"

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
        onChange={(approvalMode) => updateApprovalMutation.mutate(approvalMode)}
      />
    </>
  );
}

function ProjectApprovalControl({
  project,
  busy,
  onChange,
}: {
  project: Project | undefined;
  busy: boolean;
  onChange: (approvalMode: Project["approvalMode"]) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const value = project?.approvalMode;
  const CurrentApprovalIcon = value ? projectApprovalModeIcons[value] : null;
  const currentLabel = value ? t(`composer.projectApproval.${value}`) : t("composer.projectApproval");
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          aria-label={`${t("composer.projectApproval")}: ${currentLabel}`}
          className={cn(
            "pudding-composer-approval-control h-8 max-w-32 gap-1.5 rounded-full px-2 text-xs font-normal text-foreground/70 hover:text-foreground",
            composerControlStateClassName,
            value === "full" && "text-warning hover:text-warning",
          )}
          disabled={!project || busy}
          size="sm"
          type="button"
          variant="ghost"
        >
          {busy ? <Spinner className="size-4" /> : CurrentApprovalIcon ? (
            <CurrentApprovalIcon
              className={cn("size-4 shrink-0 text-muted-foreground", value === "full" && "text-warning")}
            />
          ) : null}
          <span className="pudding-composer-approval-label min-w-0 truncate">{currentLabel}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        aria-label={t("composer.projectApproval")}
        className="w-64 gap-0.5 p-1.5"
        collisionPadding={12}
        role="radiogroup"
        side="top"
        sideOffset={8}
      >
        {projectApprovalModes.map((mode) => {
          const ApprovalIcon = projectApprovalModeIcons[mode];
          const selected = value === mode;
          return (
            <button
              key={mode}
              aria-checked={selected}
              aria-label={t(`composer.projectApproval.${mode}`)}
              className={cn(
                "relative flex min-h-11 w-full items-start gap-2.5 rounded-md px-2 py-1.5 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden",
                appPopoverItemStateClassName,
                selected && appPopoverSelectedItemStateClassName,
              )}
              role="radio"
              type="button"
              onClick={() => {
                if (mode !== value) {
                  onChange(mode);
                }
                setOpen(false);
              }}
            >
              <ApprovalIcon
                className={cn("mt-0.5 size-4 shrink-0 text-muted-foreground", mode === "full" && "text-warning")}
              />
              <span className="grid min-w-0 flex-1">
                <span className={cn("text-[13px] leading-4 font-medium", selected && "pr-5", mode === "full" && "text-warning")}>
                  {t(`composer.projectApproval.${mode}`)}
                </span>
                <span className="text-[11px] leading-4 text-muted-foreground">
                  {t(`composer.projectApproval.${mode}.desc`)}
                </span>
              </span>
              {selected ? <Check className="absolute top-2 right-2 size-3.5" /> : null}
            </button>
          );
        })}
      </PopoverContent>
    </Popover>
  );
}

function basename(path: string) {
  const normalized = path.replace(/\/+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}
