import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, FileImage, FileText, Files, Redo2, Undo2 } from "@/components/icons";
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
  const previewable = changes.find((change) => change.binary && resourcePath(change, displayState));

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
    if (diffChanges.length > 0) {
      openTurnFileChanges(sessionID, turnID, changes, diffChanges[0].id);
    } else if (previewable) {
      revealResource(sessionID, previewable, displayState);
    }
  };

  return (
    <section className="min-w-0 overflow-hidden rounded-xl border border-border/70 bg-background text-sm text-muted-foreground shadow-none">
      <div className="flex min-h-14 items-center gap-3 bg-transparent px-3 py-2.5 hover:bg-muted/25">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-foreground/70">
          <Files className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5 text-foreground">
            {singleChange ? <span className="shrink-0 font-medium">{t(statusLabelKey(singleChange.kind))}</span> : null}
            <span className={cn("truncate", singleChange ? "font-mono" : "font-medium")}>{summary}</span>
          </div>
          {additions > 0 || deletions > 0 ? (
            <div className="mt-0.5 flex items-center gap-1.5 font-mono text-xs">
              {additions > 0 ? <span className="text-git-added">+{additions}</span> : null}
              {deletions > 0 ? <span className="text-git-deleted">−{deletions}</span> : null}
            </div>
          ) : null}
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <button
            className="inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 font-medium text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-40"
            disabled={!reversible || actionMutation.isPending}
            title={!reversible ? t("transcript.turnFilesActionUnavailable") : undefined}
            type="button"
            onClick={() => actionMutation.mutate(state === "undone" ? "redo" : "undo")}
          >
            {actionMutation.isPending ? <Spinner className="size-3.5" /> : state === "undone" ? <Redo2 className="size-3.5" /> : <Undo2 className="size-3.5" />}
            {state === "undone" ? t("transcript.turnFilesRedo") : t("transcript.turnFilesUndo")}
          </button>
          <button
            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-background px-3 font-medium text-foreground hover:bg-muted disabled:opacity-40"
            disabled={diffChanges.length === 0 && !previewable}
            type="button"
            onClick={openReview}
          >
            <FileText className="size-3.5" />
            {t("transcript.turnFilesReview")}
          </button>
        </div>
      </div>
      {changes.length > 1 ? (
        <div className="border-t border-border/70 bg-background">
          {visible.map((change) => {
            const canOpen = !change.binary || Boolean(resourcePath(change, displayState));
            return (
              <button
                key={change.id}
                className="group/change-row relative isolate flex min-h-8 w-full min-w-0 items-center gap-2 overflow-hidden px-3 text-left hover:text-foreground disabled:pointer-events-none"
                disabled={!canOpen}
                type="button"
                onClick={() => change.binary ? revealResource(sessionID, change, displayState) : openTurnFileChanges(sessionID, turnID, changes, change.id)}
              >
                <span aria-hidden="true" className="pointer-events-none absolute inset-0 bg-muted/60 opacity-0 group-hover/change-row:opacity-100" />
                {change.binary ? <FileImage className="relative z-[1] size-3.5 shrink-0" /> : null}
                <code className="relative z-[1] min-w-0 flex-1 truncate font-mono text-xs">{turnFileChangeLabel(change, changes)}</code>
                {change.additions > 0 ? <span className="relative z-[1] shrink-0 text-xs text-git-added">+{change.additions}</span> : null}
                {change.deletions > 0 ? <span className="relative z-[1] shrink-0 text-xs text-git-deleted">−{change.deletions}</span> : null}
              </button>
            );
          })}
        </div>
      ) : null}
      {collapsible ? (
        <button
          className="group/change-footer relative isolate flex h-9 w-full items-center gap-1.5 overflow-hidden border-t border-border/70 bg-muted/20 px-3 text-left text-xs font-medium text-foreground/85 hover:text-foreground"
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
