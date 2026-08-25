import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Eye, FileDiff, FileImage, FilePlus, FileText, Redo2, Undo2 } from "@/components/icons";
import { useState } from "react";
import { toast } from "sonner";

import { redoTurnFileChanges, undoTurnFileChanges, type TurnFileChange } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import type { TurnsInfiniteData } from "@/components/transcript/useTranscriptTurns";
import { useI18n } from "@/i18n";
import { turnFileChangeLabel, turnFileDiffChanges } from "@/lib/turnFileChanges";
import { cn } from "@/lib/utils";
import { openTurnFileChanges } from "@/state/filePreviewStore";
import { requestProjectFileReveal } from "@/state/projectRevealStore";

export function TurnFileChanges({ changes, fileChangeState, sessionID, token, turnID }: {
  changes: TurnFileChange[];
  fileChangeState?: "applied" | "undone";
  sessionID: string;
  token: string;
  turnID: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const collapsible = changes.length > 6;
  const visible = collapsible && !expanded ? changes.slice(0, 5) : changes;
  const hiddenCount = changes.length - visible.length;
  const additions = changes.reduce((sum, change) => sum + change.additions, 0);
  const deletions = changes.reduce((sum, change) => sum + change.deletions, 0);
  const singleChange = changes.length === 1 ? changes[0] : undefined;
  const summary = singleChange
    ? turnFileChangeLabel(singleChange, changes)
    : t("transcript.turnFilesSummary").replace("{count}", String(changes.length));
  const state = fileChangeState;
  const displayState = state || "applied";
  const reversible = Boolean(state) && changes.every((change) => change.reversible);
  const diffChanges = turnFileDiffChanges(changes);
  const previewable = changes.find((change) => resourcePath(change, displayState));
  const reviewable = diffChanges.length > 0;

  const actionMutation = useMutation({
    mutationFn: (action: "undo" | "redo") =>
      action === "undo"
        ? undoTurnFileChanges(token, sessionID, turnID)
        : redoTurnFileChanges(token, sessionID, turnID),
    onSuccess: ({ state: nextState }) => {
      queryClient.setQueryData<TurnsInfiniteData>(queryKeys.turns(sessionID), (previous) =>
        previous ? {
          ...previous,
          pages: previous.pages.map((page) => ({
            ...page,
            turns: page.turns.map((turn) => turn.id === turnID ? { ...turn, fileChangeState: nextState } : turn),
          })),
        } : previous,
      );
    },
    onError: () => toast.error(t("transcript.turnFilesActionFailed")),
  });

  const openReview = () => {
    if (diffChanges.length > 0) openTurnFileChanges(sessionID, turnID, changes, diffChanges[0].id);
  };

  const openPrimary = () => {
    if (reviewable) {
      openReview();
      return;
    }
    if (singleChange && previewable) {
      revealResource(sessionID, previewable, displayState);
    }
  };

  const primaryActionAvailable = reviewable || Boolean(singleChange && previewable);

  return (
    <section className="min-w-0 overflow-hidden rounded-xl border border-border/70 bg-card text-sm text-muted-foreground shadow-none dark:bg-background">
      <div className="flex min-h-12 items-center gap-2 bg-background px-2 py-1.5 transition-colors hover:bg-muted/35 dark:bg-muted/20 dark:hover:bg-muted/35">
        <button className="flex min-w-0 flex-1 items-center gap-2.5 text-left disabled:pointer-events-none" disabled={!primaryActionAvailable} type="button" onClick={openPrimary}>
          <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted/60 text-foreground/65">
            {singleChange?.kind === "added" ? <FilePlus className="size-4" /> : <FileDiff className="size-4" />}
          </span>
          <span className="flex min-w-0 flex-1 items-center gap-2">
            <span className="flex min-w-0 items-center gap-1.5 text-foreground">
              {singleChange ? <span className="shrink-0 font-medium">{t(statusLabelKey(singleChange.kind))}</span> : null}
              <span className={cn("truncate", singleChange ? "font-mono" : "font-medium")}>{summary}</span>
            </span>
            {additions > 0 || deletions > 0 ? (
              <span className="flex shrink-0 items-center gap-1.5 font-mono text-xs opacity-85">
                {additions > 0 ? <span className="text-git-added">+{additions}</span> : null}
                {deletions > 0 ? <span className="text-git-deleted">−{deletions}</span> : null}
              </span>
            ) : null}
          </span>
        </button>
        <div className="flex shrink-0 items-center gap-1.5">
          <button
            className="inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 font-medium text-foreground hover:bg-muted/60 disabled:cursor-not-allowed disabled:opacity-40"
            disabled={!reversible || actionMutation.isPending}
            title={!reversible ? t("transcript.turnFilesActionUnavailable") : undefined}
            type="button"
            onClick={() => actionMutation.mutate(state === "undone" ? "redo" : "undo")}
          >
            {actionMutation.isPending ? <Spinner className="size-3.5" /> : state === "undone" ? <Redo2 className="size-3.5" /> : <Undo2 className="size-3.5" />}
            {state === "undone" ? t("transcript.turnFilesRedo") : t("transcript.turnFilesUndo")}
          </button>
          {changes.length === 1 ? <button
            className="inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 font-medium text-foreground hover:bg-muted/60 disabled:opacity-40"
            disabled={!previewable}
            type="button"
            onClick={() => previewable && revealResource(sessionID, previewable, displayState)}
          >
            <Eye className="size-3.5" />
            {t("transcript.turnFilesPreview")}
          </button> : null}
          {reviewable ? <button
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border/70 bg-transparent px-3 font-medium text-foreground hover:bg-muted/60"
            type="button"
            onClick={openReview}
          >
            <FileText className="size-3.5" />
            {t("transcript.turnFilesReview")}
          </button> : null}
        </div>
      </div>
      {changes.length > 1 ? (
        <div className="border-t border-border/50">
          {visible.map((change) => {
            const relativePath = resourcePath(change, displayState);
            const diffable = diffChanges.some((candidate) => candidate.id === change.id);
            const label = turnFileChangeLabel(change, changes);
            return (
              <div key={change.id} className="group/change-row flex min-h-[38px] w-full min-w-0 items-center text-foreground/70 transition-colors hover:bg-muted/35 hover:text-foreground/90">
                <button
                  className="flex min-h-[38px] min-w-0 flex-1 items-center gap-2 overflow-hidden px-3 text-left disabled:pointer-events-none"
                  disabled={!diffable}
                  type="button"
                  onClick={() => openTurnFileChanges(sessionID, turnID, changes, change.id)}
                >
                  {change.binary ? <FileImage className="size-3.5 shrink-0" /> : null}
                  <code className="min-w-0 flex-1 truncate font-mono text-xs">{label}</code>
                </button>
                <div className="flex shrink-0 items-center gap-2 pr-3">
                  {relativePath ? (
                    <button
                      aria-label={`${t("transcript.turnFilesPreview")} ${label}`}
                      className="inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground/75 opacity-0 hover:bg-muted/70 hover:text-foreground group-hover/change-row:opacity-100 focus-visible:opacity-100"
                      type="button"
                      onClick={() => revealResource(sessionID, change, displayState)}
                    >
                      <Eye className="size-3.5" />
                    </button>
                  ) : null}
                  {change.additions > 0 ? <span className="shrink-0 text-xs text-git-added opacity-65 group-hover/change-row:opacity-85">+{change.additions}</span> : null}
                  {change.deletions > 0 ? <span className="shrink-0 text-xs text-git-deleted opacity-65 group-hover/change-row:opacity-85">−{change.deletions}</span> : null}
                </div>
              </div>
            );
          })}
        </div>
      ) : null}
      {collapsible ? (
        <button
          className="group/change-footer relative isolate flex h-9 w-full items-center gap-1.5 overflow-hidden border-t border-border/50 bg-transparent px-3 text-left text-xs font-medium text-foreground/85 hover:text-foreground"
          type="button"
          onClick={() => setExpanded((current) => !current)}
        >
          <span aria-hidden="true" className="pointer-events-none absolute inset-0 bg-muted/[38%] opacity-0 group-hover/change-footer:opacity-100" />
          <span className="relative z-[1]">{expanded ? t("transcript.turnFilesCollapse") : t("transcript.turnFilesMore").replace("{count}", String(hiddenCount))}</span>
          <ChevronDown className={cn("relative z-[1] size-3.5 transition-transform", expanded && "rotate-180")} />
        </button>
      ) : null}
    </section>
  );
}

function resourcePath(change: TurnFileChange, state: "applied" | "undone") {
  if (state === "applied") {
    return change.kind === "deleted" ? undefined : change.path;
  }
  if (change.kind === "added") {
    return undefined;
  }
  return change.kind === "renamed" ? change.originalPath : change.path;
}

function revealResource(sessionID: string, change: TurnFileChange, state: "applied" | "undone") {
  const relativePath = resourcePath(change, state);
  if (!relativePath) {
    return;
  }
  requestProjectFileReveal({
    absolutePath: `${change.rootPath.replace(/[\\/]+$/, "")}/${relativePath}`,
    relativePath,
    rootPath: change.rootPath,
    sessionID,
  });
}

function statusLabelKey(kind: TurnFileChange["kind"]) {
  switch (kind) {
    case "added": return "turnFiles.added";
    case "deleted": return "turnFiles.deleted";
    case "renamed": return "turnFiles.renamed";
    default: return "turnFiles.modified";
  }
}
