import { useQueryClient } from "@tanstack/react-query";
import { Check, FileText, FolderOpen, ShieldCheck, X, type LucideIcon } from "@/components/icons";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { approveApproval, denyApproval } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppIcon } from "@/components/AppIcon";
import { ChoiceMenu, type ChoiceMenuItem } from "@/components/ChoiceMenu";
import { ComposerFloatingPanel } from "@/components/ComposerFloatingPanel";
import { GitCommitDiffDialog, type GitCommitApproval } from "@/components/GitCommitDiffDialog";
import { PatchDiffDialog, type PatchApproval } from "@/components/PatchDiffDialog";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useDesktopApplicationIdentity } from "@/hooks/useDesktopApplicationIdentity";
import { useI18n } from "@/i18n";
import { pickDirectories } from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";
import { syncSessionProjectState } from "@/lib/sessionProjectState";
import type { AssistantOverlay, AssistantOverlayPart } from "@/state/overlayStore";

type ComposerApproval = Extract<AssistantOverlayPart, { type: "approval" }>;
type ApprovalMenuAction = "approve-session" | "approve-turn" | "deny" | "review-git" | "review-patch";

export function ComposerApprovalBar({
  approval,
  preview = false,
  token,
}: {
  approval?: ComposerApproval;
  preview?: boolean;
  token: string;
}) {
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
  const isComputerAppApproval = isToolCallApproval && toolCallApproval.scope === "computer" && Boolean(toolCallApproval.appID);
  const approvalReason = isToolCallApproval
    ? toolCallReason(toolCallApproval.operation, toolCallApproval.execution, toolCallApproval.hostAccessReason, t) || current.reason
    : current.reason;

  async function approve(scope: "turn" | "session") {
    if (pending || preview) {
      return;
    }
    setPendingAction(scope);
    try {
      const response = await approveApproval(token, current.sessionID, current.approvalID, scope, isCodeApproval ? projectDirs : []);
      if (scope === "session") {
        await syncSessionProjectState(queryClient, token, current.sessionID, response.session);
      }
      setViewingPatchApproval(null);
      setViewingGitCommit(null);
    } finally {
      setPendingAction(null);
    }
  }

  async function pickProjectDirs() {
    if (pending || pickingProjectDir || hasPayloadProjectDirs || preview) {
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
    if (pending || preview) {
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
  if (isToolCallApproval && !isComputerAppApproval) {
    const approveAction: ApprovalMenuAction = "approve-turn";
    const approveLabel = approvalToolActionLabel(toolCallApproval.operation, Boolean(patchApproval), Boolean(gitCommitApproval), t);
    approvalMenuItems.push({
      id: approveAction,
      label: approveLabel,
      value: approveAction,
      render: () => (
        <ApprovalMenuOption
          description={t("transcript.approvalAllowToolCallDesc")}
          icon={Check}
          label={approveLabel}
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
  } else if (!isToolCallApproval) {
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
  if (!isComputerAppApproval) {
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
  }

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
    <ComposerFloatingPanel
      className={cn(
        "overflow-y-auto text-xs",
        !isComputerAppApproval && "grid gap-1",
      )}
      onKeyDown={(event) => {
        if (isComputerAppApproval && event.key === "Escape") {
          event.preventDefault();
          void deny();
        }
      }}
    >
      {isComputerAppApproval ? (
        <ComputerApprovalTarget
          appID={toolCallApproval.appID}
          approveDescription={t("transcript.approvalAllowComputerAppDesc")}
          approveLabel={t("transcript.approvalAllowComputerApp")}
          denyDescription={t("transcript.approvalDenyDesc")}
          denyLabel={t("transcript.approvalRejectComputerApp")}
          pendingAction={pendingAction}
          onApprove={() => void approve("session")}
          onDeny={() => void deny()}
        />
      ) : (
        <>
          <div className="flex min-w-0 items-center gap-1.5">
            <ShieldCheck className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 truncate font-medium">{title}</span>
          </div>
          {approvalReason ? <div className="line-clamp-2 leading-5 text-muted-foreground">{approvalReason}</div> : null}
        </>
      )}
      {isToolCallApproval && toolCallApproval.command ? (
        <div className="max-h-28 overflow-auto rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
          <pre className="whitespace-pre-wrap break-words"><span className="select-none text-muted-foreground">$ </span>{toolCallApproval.command}</pre>
        </div>
      ) : null}
      {isToolCallApproval && toolCallApproval.paths.length > 0 && !isComputerAppApproval && !patchApproval && !gitCommitApproval ? (
        <div className="grid gap-1 rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
          {toolCallApproval.paths.map((path) => (
            <div key={path} className="truncate" >
              {path}
            </div>
          ))}
        </div>
      ) : null}
      {isToolCallApproval && !isComputerAppApproval && toolCallApproval.valuePreview !== undefined ? (
        <div className="max-h-20 overflow-auto rounded-md border border-border/70 bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-4">
          <pre className="whitespace-pre-wrap break-words">{toolCallApproval.valuePreview}</pre>
        </div>
      ) : null}
      {isToolCallApproval && patchApproval ? (
        <div className="flex min-w-0 items-center gap-3 text-[11px] text-muted-foreground">
          <span>{t("transcript.approvalPatchFiles").replace("{count}", String(patchApproval.fileCount))}</span>
          <span className="font-mono text-git-added">+{patchApproval.additions}</span>
          <span className="font-mono text-git-deleted">-{patchApproval.deletions}</span>
        </div>
      ) : null}
      {isToolCallApproval && gitCommitApproval ? (
        <div className="flex min-w-0 items-center gap-3 text-[11px] text-muted-foreground">
          <span>{t("transcript.approvalPatchFiles").replace("{count}", String(gitCommitApproval.fileCount))}</span>
          <span className="font-mono text-git-added">+{gitCommitApproval.additions}</span>
          <span className="font-mono text-git-deleted">-{gitCommitApproval.deletions}</span>
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
      {!isComputerAppApproval ? (
        <ChoiceMenu
          busy={pending}
          className="mt-0.5 border-t border-border/60 pt-1"
          focusMode="when-idle"
          items={approvalMenuItems}
          maxHeightClassName="max-h-44"
          onEscape={() => void deny()}
          onSelect={selectApprovalAction}
        />
      ) : null}
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

function ComputerApprovalTarget({
  appID,
  approveDescription,
  approveLabel,
  denyDescription,
  denyLabel,
  pendingAction,
  onApprove,
  onDeny,
}: {
  appID: string;
  approveDescription: string;
  approveLabel: string;
  denyDescription: string;
  denyLabel: string;
  pendingAction: "turn" | "session" | "deny" | null;
  onApprove: () => void;
  onDeny: () => void;
}) {
  const identity = useDesktopApplicationIdentity(appID);

  return (
    <div className="flex min-w-0 items-center gap-2">
        {identity?.iconURL ? (
          <AppIcon className="shrink-0" size="md" src={identity.iconURL} />
        ) : (
          <span aria-hidden="true" className="size-8 shrink-0" />
        )}
        <div className="min-w-0 flex-1 truncate text-sm font-medium" title={appID}>
          {identity?.name || appID}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button
            aria-label={approveLabel}
            disabled={pendingAction !== null}
            size="sm"
            title={approveDescription}
            type="button"
            variant="secondary"
            onClick={onApprove}
          >
            {pendingAction === "session" ? <Spinner className="size-3.5" /> : <Check className="size-4" />}
            {approveLabel}
          </Button>
          <Button
            aria-label={denyLabel}
            disabled={pendingAction !== null}
            size="sm"
            title={denyDescription}
            type="button"
            variant="ghost"
            onClick={onDeny}
          >
            {pendingAction === "deny" ? <Spinner className="size-3.5" /> : <X className="size-4" />}
            {denyLabel}
          </Button>
        </div>
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
    return { appID: "", command: "", execution: "", hostAccessReason: "", operation: "", paths: [] as string[], scope: "", valuePreview: undefined as string | undefined };
  }
  const data = payload as Record<string, unknown>;
  const appID = typeof data.appID === "string" ? data.appID.trim() : "";
  const operation = typeof data.operation === "string" ? data.operation.trim() : "";
  const scope = typeof data.scope === "string" ? data.scope.trim() : "";
  const command = typeof data.command === "string" ? data.command : "";
  const execution = data.execution === "host" || data.execution === "sandbox" ? data.execution : "";
  const hostAccessReason = typeof data.hostAccessReason === "string" ? data.hostAccessReason.trim() : "";
  const paths = Array.isArray(data.paths)
    ? data.paths.filter((value): value is string => typeof value === "string").map((value) => value.trim()).filter(Boolean)
    : [];
  const valuePreview = typeof data.valuePreview === "string" ? data.valuePreview : undefined;
  return { appID, command, execution, hostAccessReason, operation, paths: dedupeStrings(paths), scope, valuePreview };
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

function toolCallReason(operation: string, execution: string, hostAccessReason: string, t: (key: string) => string) {
  if (execution === "host") {
    const summary = operation === "process_start"
      ? t("transcript.approvalToolCall.process_startFullAccess")
      : t("transcript.approvalToolCall.commandFullAccess");
    return hostAccessReason ? `${summary} ${hostAccessReason}` : summary;
  }
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
    case "computer_observe":
    case "computer_use_app":
    case "computer_quit_app":
    case "computer_press":
    case "computer_set_value":
    case "computer_select":
    case "computer_submit":
    case "computer_click":
    case "computer_drag":
    case "computer_scroll":
    case "computer_actions":
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
