import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown, Plus, TextCursorInput, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import {
  createProjectGitBranch,
  deleteProjectGitBranch,
  getProjectGitBranches,
  renameProjectGitBranch,
  switchProjectGitBranch,
  type ProjectGitBranch,
  type ProjectGitStatus,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppPopoverContent } from "@/components/AppPopover";
import { Spinner } from "@/components/Spinner";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";

import { projectGitOperationError, projectGitReadError } from "../projectErrors";

type BranchDialog = { mode: "create" | "rename"; name: string };

const branchSuccessMessage = {
  create: "project.gitBranchCreated",
  delete: "project.gitBranchDeleted",
  rename: "project.gitBranchRenamed",
  switch: "project.gitBranchSwitched",
} as const;

export function ProjectGitBranchPicker({
  branch,
  disabled,
  hasUnsavedChanges,
  rootID,
  sessionID,
  token,
  onStatus,
}: {
  branch: string;
  disabled: boolean;
  hasUnsavedChanges: boolean;
  rootID: string;
  sessionID: string;
  token: string;
  onStatus: (status: ProjectGitStatus, worktreeChanged?: boolean) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [dialog, setDialog] = useState<BranchDialog>();
  const [deleteBranch, setDeleteBranch] = useState<string>();
  const [switchBranch, setSwitchBranch] = useState<string>();
  const branchesQuery = useQuery({
    enabled: open,
    queryKey: queryKeys.projectGitBranches(sessionID, rootID),
    queryFn: () => getProjectGitBranches(token, sessionID, rootID),
    staleTime: 5_000,
  });
  const mutation = useMutation({
    mutationFn: async ({ mode, name }: { mode: "create" | "delete" | "rename" | "switch"; name: string }) => {
      if (mode === "create") return createProjectGitBranch(token, sessionID, rootID, name);
      if (mode === "delete") return deleteProjectGitBranch(token, sessionID, rootID, name);
      if (mode === "rename") return renameProjectGitBranch(token, sessionID, rootID, name);
      return switchProjectGitBranch(token, sessionID, rootID, name);
    },
    onSuccess: (status, variables) => {
      onStatus(status, variables.mode !== "delete" && variables.mode !== "rename");
      void queryClient.invalidateQueries({ queryKey: queryKeys.projectGitBranches(sessionID, rootID) });
      setDialog(undefined);
      setDeleteBranch(undefined);
      setSwitchBranch(undefined);
      setOpen(false);
      toast.success(t(branchSuccessMessage[variables.mode]));
    },
    onError: (error) => toast.error(projectGitOperationError(error, t)),
  });
  const filtered = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    return (branchesQuery.data?.branches || []).filter((candidate) => !query || candidate.name.toLocaleLowerCase().includes(query));
  }, [branchesQuery.data?.branches, search]);
  const local = filtered.filter((candidate) => !candidate.remote);
  const remote = filtered.filter((candidate) => candidate.remote);
  const requestSwitch = (name: string) => {
    if (hasUnsavedChanges) {
      setSwitchBranch(name);
      setOpen(false);
      return;
    }
    mutation.mutate({ mode: "switch", name });
  };

  return (
    <>
      <Popover open={open} onOpenChange={(next) => {
        if (!disabled && !mutation.isPending) setOpen(next);
        if (!next) setSearch("");
      }}>
        <PopoverTrigger asChild>
          <Button className="h-6 max-w-36 gap-1 px-1.5 text-[11px] font-normal" disabled={disabled || mutation.isPending} size="sm" type="button" variant="ghost">
            <span className="truncate">{branch}</span>
            <ChevronDown className="size-3" />
          </Button>
        </PopoverTrigger>
        <AppPopoverContent align="end" className="w-64 gap-1 p-1.5">
          <Input
            autoFocus
            className="mb-1 h-7 text-xs"
            placeholder={t("project.gitBranchSearch")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <div className="max-h-64 overflow-auto">
            {branchesQuery.isLoading ? (
              <div className="flex items-center gap-2 px-2 py-3 text-xs text-muted-foreground"><Spinner />{t("common.loading")}</div>
            ) : branchesQuery.isError ? (
              <div className="px-2 py-3 text-xs text-muted-foreground">{projectGitReadError(branchesQuery.error, t)}</div>
            ) : (
              <>
                <BranchGroup label={t("project.gitLocalBranches")} branches={local} pending={mutation.isPending} onDelete={setDeleteBranch} onSwitch={requestSwitch} />
                <BranchGroup label={t("project.gitRemoteBranches")} branches={remote} pending={mutation.isPending} onDelete={setDeleteBranch} onSwitch={requestSwitch} />
              </>
            )}
          </div>
          <div className="mt-1 border-t pt-1">
            <button className="flex h-7 w-full items-center gap-2 rounded-md px-2 text-left text-xs hover:bg-accent" type="button" onClick={() => setDialog({ mode: "create", name: "" })}>
              <Plus className="size-3.5" />{t("project.gitCreateBranch")}
            </button>
            <button className="flex h-7 w-full items-center gap-2 rounded-md px-2 text-left text-xs hover:bg-accent" type="button" onClick={() => setDialog({ mode: "rename", name: branch })}>
              <TextCursorInput className="size-3.5" />{t("project.gitRenameBranch")}
            </button>
          </div>
        </AppPopoverContent>
      </Popover>
      <BranchNameDialog dialog={dialog} pending={mutation.isPending} onCancel={() => setDialog(undefined)} onSubmit={(name) => dialog && mutation.mutate({ mode: dialog.mode, name })} />
      <AlertDialog open={Boolean(deleteBranch)} onOpenChange={(next) => !next && !mutation.isPending && setDeleteBranch(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("project.gitDeleteBranchTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("project.gitDeleteBranchDescription").replace("{name}", deleteBranch || "")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={mutation.isPending} variant="destructive" onClick={() => deleteBranch && mutation.mutate({ mode: "delete", name: deleteBranch })}>
              {mutation.isPending ? <Spinner /> : null}{t("project.gitDeleteBranch")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={Boolean(switchBranch)} onOpenChange={(next) => !next && !mutation.isPending && setSwitchBranch(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("project.gitSwitchBranchUnsavedTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("project.gitSwitchBranchUnsavedDescription").replace("{name}", switchBranch || "")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={mutation.isPending} onClick={() => switchBranch && mutation.mutate({ mode: "switch", name: switchBranch })}>
              {mutation.isPending ? <Spinner /> : null}{t("project.gitSwitchBranchUnsavedConfirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function BranchGroup({ label, branches, pending, onDelete, onSwitch }: {
  label: string;
  branches: ProjectGitBranch[];
  pending: boolean;
  onDelete: (name: string) => void;
  onSwitch: (name: string) => void;
}) {
  if (branches.length === 0) return null;
  return (
    <div className="pb-1">
      <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{label}</div>
      {branches.map((branch) => (
        <div key={`${branch.remote}:${branch.name}`} className="group/branch flex h-7 items-center rounded-md hover:bg-accent">
          <button className="flex h-full min-w-0 flex-1 items-center gap-2 px-2 text-left text-xs" disabled={pending || branch.current} type="button" onClick={() => onSwitch(branch.name)}>
            {branch.current ? <Check className="size-3.5 shrink-0" /> : null}
            <span className="min-w-0 flex-1 truncate">{branch.name}</span>
          </button>
          {!branch.remote && !branch.current ? (
            <Button className="mr-0.5 size-6 text-muted-foreground opacity-0 group-hover/branch:opacity-100 focus-visible:opacity-100" disabled={pending} size="icon-xs" type="button" variant="ghost" onClick={() => onDelete(branch.name)}>
              <Trash2 />
            </Button>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function BranchNameDialog({ dialog, pending, onCancel, onSubmit }: {
  dialog?: BranchDialog;
  pending: boolean;
  onCancel: () => void;
  onSubmit: (name: string) => void;
}) {
  const { t } = useI18n();
  const [name, setName] = useState("");
  const open = Boolean(dialog);
  useEffect(() => {
    setName(dialog?.name || "");
  }, [dialog]);
  return (
    <Dialog open={open} onOpenChange={(next) => {
      if (!next && !pending) {
        setName("");
        onCancel();
      }
    }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(dialog?.mode === "rename" ? "project.gitRenameBranch" : "project.gitCreateBranch")}</DialogTitle>
          <DialogDescription>{t("project.gitBranchNameHint")}</DialogDescription>
        </DialogHeader>
        <form className="space-y-4" onSubmit={(event) => {
          event.preventDefault();
          if (name.trim() && !pending) onSubmit(name.trim());
        }}>
          <Input autoFocus disabled={pending} value={name} onChange={(event) => setName(event.target.value)} />
          <DialogFooter>
            <Button disabled={pending} type="button" variant="outline" onClick={onCancel}>{t("common.cancel")}</Button>
            <Button disabled={!name.trim() || pending} type="submit">{pending ? <Spinner /> : null}{t("common.confirm")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
