import { useQueryClient } from "@tanstack/react-query";
import { Check, FileText, FolderOpen, ShieldCheck, X, type LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { approveApproval, denyApproval } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { ChoiceMenu, type ChoiceMenuItem } from "@/components/ChoiceMenu";
import { ComposerFloatingPanel } from "@/components/ComposerFloatingPanel";
import { GitCommitDiffDialog, type GitCommitApproval } from "@/components/GitCommitDiffDialog";
import { PatchDiffDialog, type PatchApproval } from "@/components/PatchDiffDialog";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { pickDirectories } from "@/lib/desktopBridge";
import type { AssistantOverlay, AssistantOverlayPart } from "@/state/overlayStore";

type ComposerApproval = Extract<AssistantOverlayPart, { type: "approval" }>;
type ApprovalMenuAction = "approve-session" | "approve-turn" | "deny" | "review-git" | "review-patch";

export function ComposerApprovalBar({ approval, token }: { approval?: ComposerApproval; token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [pendingAction, setPendingAction] = useState<"turn" | "session" | "deny" | null>(null);
  const [selectedProjectDirs, setSelectedProjectDirs] = useState<string[]>([]);
  const [pickingProjectDir, setPickingProjectDir] = useState(false);
  const [viewingPatchApproval, setViewingPatchApproval] = useState<PatchApproval | null>(null);
  const [viewingGitCommit, setViewingGitCommit] = useState<GitCommitApproval | null>(null);
  useEffect(() => {
    setSelectedProjectDirs([]);
    setViewingPatchApproval(null);
    setViewingGitCommit(null);
  }, [approval?.approvalID]);
  if (!approval) {
    return null;
  }
  const current = approval;
  const isToolCallApproval = current.approvalKind === "tool_call";
  const targetMode = approvalTargetMode(current.payload);
  const title = approvalTitle(current, targetMode, t);
  const pending = pendingAction !== null;
  const isCodeApproval = targetMode === "code";
  const payloadProjectDirs = projectDirsFromPayload(current.payload);
  const hasPayloadProjectDirs = payloadProjectDirs.length > 0;
  const projectDirs = hasPayloadProjectDirs ? payloadProjectDirs : selectedProjectDirs;
  const suggestedDirName = suggestedProjectDirName(current.payload);
  const toolCallApproval = toolCallFromPayload(current.payload);
  const patchApproval = patchApprovalFromPayload(current.payload);
  const gitCommitApproval = gitCommitFromPayload(current.payload);
  const approvalReason = isToolCallApproval ? toolCallReason(toolCallApproval.operation, t) || current.reason : current.reason;

  async function approve(scope: "turn" | "session") {
    if (pending) {
      return;
    }
    setPendingAction(scope);
    try {
      await approveApproval(token, current.sessionID, current.approvalID, scope, isCodeApproval ? projectDirs : []);
      if (scope === "session") {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
          queryClient.invalidateQueries({ queryKey: queryKeys.projects() }),
        ]);
      }
      setViewingPatchApproval(null);
      setViewingGitCommit(null);
    } finally {
      setPendingAction(null);
    }
  }

  async function pickProjectDirs() {
    if (pending || pickingProjectDir || hasPayloadProjectDirs) {
      return;
    }
    setPickingProjectDir(true);
    try {
      const dirs = await pickProjectDirectories(t);
      if (dirs.length > 0) {
        setSelectedProjectDirs((prev) => dedupeStrings([...prev, ...dirs]));
      }
    } catch {
      toast.error(t("transcript.approvalProjectDirPickFailed"));
    } finally {
      setPickingProjectDir(false);
    }
  }

  function removeProjectDir(dir: string) {
    setSelectedProjectDirs((prev) => prev.filter((item) => item !== dir));
  }

  async function deny() {
    if (pending) {
      return;
    }
    setPendingAction("deny");
    try {
      await denyApproval(token, current.sessionID, current.approvalID);
      setViewingPatchApproval(null);
      setViewingGitCommit(null);
    } finally {
      setPendingAction(null);
    }
  }

  const approvalMenuItems: Array<ChoiceMenuItem<ApprovalMenuAction>> = [];
  if (isToolCallApproval) {
    approvalMenuItems.push({
      id: "approve-turn",
      label: approvalToolActionLabel(toolCallApproval.operation, Boolean(patchApproval), Boolean(gitCommitApproval), t),
      value: "approve-turn",
      render: () => (
        <ApprovalMenuOption
          description={t("transcript.approvalAllowToolCallDesc")}
          icon={Check}
          label={approvalToolActionLabel(toolCallApproval.operation, Boolean(patchApproval), Boolean(gitCommitApproval), t)}
          loading={pendingAction === "turn"}
        />
      ),
    });
    if (patchApproval) {
      approvalMenuItems.push({
        id: "review-patch",
        label: t("transcript.approvalPatchReview"),
        value: "review-patch",
        render: () => <ApprovalMenuOption description={t("transcript.approvalReviewDesc")} icon={FileText} label={t("transcript.approvalPatchReview")} />,
      });
    }
    if (gitCommitApproval) {
      approvalMenuItems.push({
        id: "review-git",
        label: t("transcript.approvalGitCommitReview"),
        value: "review-git",
        render: () => <ApprovalMenuOption description={t("transcript.approvalReviewDesc")} icon={FileText} label={t("transcript.approvalGitCommitReview")} />,
      });
    }
  } else {
    approvalMenuItems.push(
      {
        id: "approve-turn",
        label: t("transcript.approvalAllowTurn"),
        value: "approve-turn",
        render: () => (
          <ApprovalMenuOption
            description={t("transcript.approvalAllowTurnDesc")}
            icon={Check}
            label={t("transcript.approvalAllowTurn")}
            loading={pendingAction === "turn"}
          />
        ),
      },
      {
        id: "approve-session",
        label: t("transcript.approvalAllowSession"),
        value: "approve-session",
        render: () => (
          <ApprovalMenuOption
            description={t("transcript.approvalAllowSessionDesc")}
            icon={ShieldCheck}
            label={t("transcript.approvalAllowSession")}
            loading={pendingAction === "session"}
          />
        ),
      },
    );
  }
  approvalMenuItems.push({
    id: "deny",
    label: approvalDenyLabel(toolCallApproval.operation, Boolean(patchApproval), Boolean(gitCommitApproval), t),
    value: "deny",
    render: () => (
      <ApprovalMenuOption
        description={t("transcript.approvalDenyDesc")}
        icon={X}
        label={approvalDenyLabel(toolCallApproval.operation, Boolean(patchApproval), Boolean(gitCommitApproval), t)}
        loading={pendingAction === "deny"}
      />
    ),
  });

  function selectApprovalAction(action: ApprovalMenuAction) {
    switch (action) {
      case "approve-turn":
        void approve("turn");
        return;
      case "approve-session":
        void approve("session");
        return;
      case "deny":
        void deny();
        return;
      case "review-patch":
        if (patchApproval) {
          setViewingPatchApproval(patchApproval);
        }
        return;
      case "review-git":
        if (gitCommitApproval) {
          setViewingGitCommit(gitCommitApproval);
        }
        return;
    }
  }

  return (
    <ComposerFloatingPanel className="right-4 grid gap-1 overflow-y-auto px-3 py-2 text-xs sm:right-8">
      <div className="flex min-w-0 items-center gap-1.5">
        <ShieldCheck className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 truncate font-medium">{title}</span>
      </div>
      {approvalReason ? <div className="line-clamp-2 leading-5 text-muted-foreground">{approvalReason}</div> : null}
      {isToolCallApproval && toolCallApproval.command ? (
        <div className="max-h-28 overflow-auto rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
          <pre className="whitespace-pre-wrap break-words"><span className="select-none text-muted-foreground">$ </span>{toolCallApproval.command}</pre>
        </div>
      ) : null}
      {isToolCallApproval && toolCallApproval.paths.length > 0 && !patchApproval && !gitCommitApproval ? (
        <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
          {toolCallApproval.paths.map((path) => (
            <div key={path} className="truncate" >
              {path}
            </div>
          ))}
        </div>
      ) : null}
      {isToolCallApproval && patchApproval ? (
        <div className="flex min-w-0 items-center gap-3 text-[11px] text-muted-foreground">
          <span>{t("transcript.approvalPatchFiles").replace("{count}", String(patchApproval.fileCount))}</span>
          <span className="font-mono text-success">+{patchApproval.additions}</span>
          <span className="font-mono text-destructive">-{patchApproval.deletions}</span>
        </div>
      ) : null}
      {isToolCallApproval && gitCommitApproval ? (
        <div className="flex min-w-0 items-center gap-3 text-[11px] text-muted-foreground">
          <span>{t("transcript.approvalPatchFiles").replace("{count}", String(gitCommitApproval.fileCount))}</span>
          <span className="font-mono text-success">+{gitCommitApproval.additions}</span>
          <span className="font-mono text-destructive">-{gitCommitApproval.deletions}</span>
          <span className="min-w-0 truncate">{gitCommitApproval.commitMessage}</span>
        </div>
      ) : null}
      {isCodeApproval ? (
        <div className="grid gap-1">
          <div className="text-[11px] font-medium text-muted-foreground">
            {t("transcript.approvalProjectDirs")}
          </div>
          {projectDirs.length > 0 ? (
            <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
              {projectDirs.map((dir) => (
                <div key={dir} className="flex min-w-0 items-center gap-1" >
                  <span className="min-w-0 flex-1 truncate">{dir}</span>
                  {!hasPayloadProjectDirs ? (
                    <button
                      aria-label={t("common.delete")}
                      className="grid size-4 shrink-0 place-items-center rounded-full text-muted-foreground hover:text-foreground"
                      type="button"
                      onClick={() => removeProjectDir(dir)}
                    >
                      <X className="size-3" />
                    </button>
                  ) : null}
                </div>
              ))}
            </div>
          ) : null}
          {projectDirs.length === 0 ? (
            <div className="text-[11px] leading-4 text-muted-foreground">{t("transcript.approvalProjectDirsOptional")}</div>
          ) : null}
          {!hasPayloadProjectDirs ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <Button
                className="h-6 gap-1 rounded-full px-2 text-[11px]"
                disabled={pending || pickingProjectDir}
                size="sm"
                type="button"
                variant="secondary"
                onClick={() => void pickProjectDirs()}
              >
                {pickingProjectDir ? <Spinner className="size-3" /> : <FolderOpen className="size-3" />}
                {t("transcript.approvalProjectDirChoose")}
              </Button>
              {suggestedDirName ? <span className="text-[11px] text-muted-foreground">{t("transcript.approvalProjectDirsSuggested").replace("{name}", suggestedDirName)}</span> : null}
            </div>
          ) : null}
        </div>
      ) : null}
      <ChoiceMenu
        busy={pending}
        className="mt-0.5 border-t border-border/60 pt-1"
        focusMode="when-idle"
        items={approvalMenuItems}
        maxHeightClassName="max-h-44"
        onEscape={() => void deny()}
        onSelect={selectApprovalAction}
      />
      <PatchDiffDialog
        applying={pendingAction === "turn"}
        approval={viewingPatchApproval}
        rejecting={pendingAction === "deny"}
        onApply={() => void approve("turn")}
        onOpenChange={(open) => !open && setViewingPatchApproval(null)}
        onReject={() => void deny()}
      />
      <GitCommitDiffDialog
        approval={viewingGitCommit}
        committing={pendingAction === "turn"}
        rejecting={pendingAction === "deny"}
        onCommit={() => void approve("turn")}
        onOpenChange={(open) => !open && setViewingGitCommit(null)}
        onReject={() => void deny()}
      />
    </ComposerFloatingPanel>
  );
}

function ApprovalMenuOption({
  description,
  icon: Icon,
  label,
  loading = false,
}: {
  description: string;
  icon: LucideIcon;
  label: string;
  loading?: boolean;
}) {
  return (
    <div className="flex min-w-0 items-start gap-2 py-0.5">
      <span className="mt-0.5 grid size-4 shrink-0 place-items-center text-muted-foreground">
        {loading ? <Spinner className="size-3.5" /> : <Icon className="size-3.5" />}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-xs font-medium text-foreground">{label}</span>
        <span className="mt-0.5 block truncate text-[11px] leading-4 text-muted-foreground">{description}</span>
      </span>
    </div>
  );
}

function approvalToolActionLabel(operation: string, patchApproval: boolean, gitCommit: boolean, t: (key: string) => string) {
  if (patchApproval) {
    return t("transcript.approvalFilePatch");
  }
  if (gitCommit) {
    return t("transcript.approvalGitCommit");
  }
  switch (operation) {
    case "shell":
      return t("transcript.approvalRunCommand");
    case "process_start":
      return t("transcript.approvalStartProcess");
    default:
      return t("transcript.approvalAllowToolCall");
  }
}

function approvalDenyLabel(operation: string, patchApproval: boolean, gitCommit: boolean, t: (key: string) => string) {
  if (patchApproval) {
    return t("transcript.approvalDoNotApply");
  }
  if (gitCommit) {
    return t("transcript.approvalDoNotCommit");
  }
  if (operation === "shell" || operation === "process_start") {
    return t("transcript.approvalDoNotRun");
  }
  if (operation) {
    return t("transcript.approvalDoNotExecute");
  }
  return t("transcript.approvalDoNotAllow");
}

export function selectPendingApproval(assistants: Record<string, AssistantOverlay>, sessionID: string, runningTurnID?: string): ComposerApproval | undefined {
  if (runningTurnID) {
    const running = assistants[runningTurnID];
    const approval = firstPendingApproval(running);
    if (approval) {
      return approval;
    }
  }
  for (const overlay of Object.values(assistants)) {
    if (overlay.turnID === runningTurnID || overlay.sessionID !== sessionID) {
      continue;
    }
    const approval = firstPendingApproval(overlay);
    if (approval) {
      return approval;
    }
  }
  return undefined;
}

function firstPendingApproval(overlay: AssistantOverlay | undefined): ComposerApproval | undefined {
  if (!overlay) {
    return undefined;
  }
  const approval = overlay.parts.find(isPendingApprovalPart);
  if (!approval) {
    return undefined;
  }
  if (overlay.status === "streaming") {
    return approval;
  }
  return undefined;
}

function isPendingApprovalPart(part: AssistantOverlayPart): part is ComposerApproval {
  return part.type === "approval" && !part.status;
}

function approvalTargetMode(payload: unknown) {
  if (payload && typeof payload === "object" && "targetMode" in payload && typeof payload.targetMode === "string") {
    return payload.targetMode;
  }
  return "";
}

function projectDirsFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object") {
    return [];
  }
  const data = payload as Record<string, unknown>;
  const dirs = Array.isArray(data.projectDirs) ? data.projectDirs : Array.isArray(data.rootDirs) ? data.rootDirs : [];
  return dedupeStrings(dirs.filter((value): value is string => typeof value === "string").map((value) => value.trim()).filter(Boolean));
}

function suggestedProjectDirName(payload: unknown) {
  if (payload && typeof payload === "object" && "suggestedDirName" in payload && typeof payload.suggestedDirName === "string") {
    return payload.suggestedDirName.trim();
  }
  return "";
}

function toolCallFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object") {
    return { command: "", operation: "", paths: [] as string[] };
  }
  const data = payload as Record<string, unknown>;
  const operation = typeof data.operation === "string" ? data.operation.trim() : "";
  const command = typeof data.command === "string" ? data.command : "";
  const paths = Array.isArray(data.paths)
    ? data.paths.filter((value): value is string => typeof value === "string").map((value) => value.trim()).filter(Boolean)
    : [];
  return { command, operation, paths: dedupeStrings(paths) };
}

function patchApprovalFromPayload(payload: unknown): PatchApproval | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const data = payload as Record<string, unknown>;
  const projectRoot = typeof data.projectRoot === "string" ? data.projectRoot.trim() : "";
  const diff = typeof data.diff === "string" ? data.diff : "";
  if (!diff || !Array.isArray(data.files)) {
    return null;
  }
  const files = data.files.flatMap((value) => {
    if (!value || typeof value !== "object") {
      return [];
    }
    const file = value as Record<string, unknown>;
    const path = typeof file.path === "string" ? file.path.trim() : "";
    const operation: PatchApproval["files"][number]["operation"] | null =
      file.operation === "create" || file.operation === "update" || file.operation === "delete" ? file.operation : null;
    if (!path || !operation) {
      return [];
    }
    return [{
      additions: typeof file.additions === "number" ? file.additions : 0,
      deletions: typeof file.deletions === "number" ? file.deletions : 0,
      operation,
      path,
    }];
  });
  if (files.length === 0) {
    return null;
  }
  return {
    additions: typeof data.additions === "number" ? data.additions : 0,
    deletions: typeof data.deletions === "number" ? data.deletions : 0,
    diff,
    fileCount: typeof data.fileCount === "number" ? data.fileCount : files.length,
    files,
    projectRoot,
  };
}

function gitCommitFromPayload(payload: unknown): GitCommitApproval | null {
  if (!payload || typeof payload !== "object") {
    return null;
  }
  const data = payload as Record<string, unknown>;
  if (data.operation !== "git_commit" || typeof data.diff !== "string" || !Array.isArray(data.files)) {
    return null;
  }
  const files = data.files.flatMap((value) => {
    if (!value || typeof value !== "object") {
      return [];
    }
    const file = value as Record<string, unknown>;
    const path = typeof file.path === "string" ? file.path.trim() : "";
    if (!path) {
      return [];
    }
    return [{
      additions: typeof file.additions === "number" ? file.additions : 0,
      deletions: typeof file.deletions === "number" ? file.deletions : 0,
      path,
    }];
  });
  if (files.length === 0) {
    return null;
  }
  return {
    additions: typeof data.additions === "number" ? data.additions : 0,
    branch: typeof data.branch === "string" ? data.branch : "",
    commitMessage: typeof data.commitMessage === "string" ? data.commitMessage : "",
    deletions: typeof data.deletions === "number" ? data.deletions : 0,
    diff: data.diff,
    fileCount: typeof data.fileCount === "number" ? data.fileCount : files.length,
    files,
    repoRoot: typeof data.repoRoot === "string" ? data.repoRoot : "",
    truncated: data.truncated === true,
  };
}

function toolCallReason(operation: string, t: (key: string) => string) {
  switch (operation) {
    case "write":
    case "delete":
    case "move":
    case "copy":
    case "file_patch":
    case "git_stage":
    case "git_unstage":
    case "git_commit":
    case "app_save":
    case "shell":
    case "process_start":
      return t(`transcript.approvalToolCall.${operation}`);
    default:
      return "";
  }
}

function dedupeStrings(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index);
}

async function pickProjectDirectories(t: (key: string) => string) {
  const dirs = await pickDirectories({
    buttonLabel: t("transcript.approvalProjectDirChoose"),
    message: t("transcript.approvalProjectDirChooseMessage"),
    title: t("transcript.approvalProjectDirChooseTitle"),
  });
  return dedupeStrings(dirs.map((dir) => dir.trim()).filter(Boolean));
}

function approvalTitle(approval: ComposerApproval, targetMode: string, t: (key: string) => string) {
  if (approval.approvalKind === "tool_call") {
    return t("transcript.approvalToolCallTitle");
  }
  if (approval.approvalKind === "capability") {
    const mode = targetMode ? t(`mode.${targetMode}`) : "";
    if (mode) {
      return t("transcript.approvalCapabilityTitle").replace("{mode}", mode);
    }
  }
  return approval.title || t("transcript.approvalTitle");
}
