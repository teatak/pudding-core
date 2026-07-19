import { ChevronDown, ChevronRight, Files } from "lucide-react";
import { useState } from "react";

import type { TurnFileChange } from "@/api/client";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { turnFileChangeFullPath, turnFileChangeLabel } from "@/lib/turnFileChanges";
import { openTurnFileChanges } from "@/state/filePreviewStore";

export function TurnFileChanges({ changes, sessionID, turnID }: {
  changes: TurnFileChange[];
  sessionID: string;
  turnID: string;
}) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const collapsible = changes.length > 6;
  const visible = collapsible && !expanded ? changes.slice(0, 5) : changes;
  const hiddenCount = changes.length - visible.length;
  const additions = changes.reduce((sum, change) => sum + change.additions, 0);
  const deletions = changes.reduce((sum, change) => sum + change.deletions, 0);
  const summary = t(changes.length === 1 ? "transcript.turnFilesSummaryOne" : "transcript.turnFilesSummary")
    .replace("{count}", String(changes.length));

  return (
    <section className="min-w-0 overflow-hidden rounded-lg border bg-muted/10 text-xs text-muted-foreground shadow-sm">
      <button
        className="group flex min-h-11 w-full items-center gap-2.5 bg-muted/35 px-3 py-2 text-left transition-colors hover:bg-muted/65 hover:text-foreground"
        type="button"
        onClick={() => openTurnFileChanges(sessionID, turnID, changes)}
      >
        <span className="grid size-8 shrink-0 place-items-center text-foreground/70 transition-colors group-hover:text-foreground">
          <Files className="size-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium text-foreground">{summary}</span>
          {additions > 0 || deletions > 0 ? (
            <span className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px]">
              {additions > 0 ? <span className="text-success">+{additions}</span> : null}
              {deletions > 0 ? <span className="text-destructive">−{deletions}</span> : null}
            </span>
          ) : null}
        </span>
        <ChevronRight className="size-3.5 shrink-0 opacity-60 transition-transform group-hover:translate-x-0.5" />
      </button>
      <div className="border-t px-1 py-1">
        {visible.map((change) => (
          <button
            key={change.id}
            className="group flex min-h-8 w-full min-w-0 items-center gap-2 rounded px-2 text-left transition-colors hover:bg-muted/60 hover:text-foreground"
            title={turnFileChangeFullPath(change)}
            type="button"
            onClick={() => openTurnFileChanges(sessionID, turnID, changes, change.id)}
          >
            <span className={cn("w-3 shrink-0 text-center font-mono text-[10px] font-semibold", statusClass(change.kind))}>
              {statusLetter(change.kind)}
            </span>
            <code className="min-w-0 flex-1 truncate font-mono text-[11px]">{turnFileChangeLabel(change, changes)}</code>
            {change.additions > 0 ? <span className="shrink-0 text-success">+{change.additions}</span> : null}
            {change.deletions > 0 ? <span className="shrink-0 text-destructive">−{change.deletions}</span> : null}
          </button>
        ))}
      </div>
      {collapsible ? (
        <button
          className="flex h-9 w-full items-center gap-1.5 border-t bg-muted/20 px-3 text-left font-medium text-foreground/85 transition-colors hover:bg-muted/50 hover:text-foreground"
          type="button"
          onClick={() => setExpanded((current) => !current)}
        >
          <span>{expanded ? t("transcript.turnFilesCollapse") : t("transcript.turnFilesMore").replace("{count}", String(hiddenCount))}</span>
          <ChevronDown className={cn("size-3.5 transition-transform", expanded && "rotate-180")} />
        </button>
      ) : null}
    </section>
  );
}

function statusLetter(kind: TurnFileChange["kind"]) {
  switch (kind) {
    case "added": return "A";
    case "deleted": return "D";
    case "renamed": return "R";
    default: return "M";
  }
}

function statusClass(kind: TurnFileChange["kind"]) {
  switch (kind) {
    case "added": return "text-success";
    case "deleted": return "text-destructive";
    case "renamed": return "text-info";
    default: return "text-warning";
  }
}
