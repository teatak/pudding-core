import { ChevronDown, ChevronRight, Files } from "lucide-react";
import { useState } from "react";

import type { TurnFileChange } from "@/api/client";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { turnFileChangeLabel } from "@/lib/turnFileChanges";
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
    <section className="min-w-0 overflow-hidden rounded-lg border border-border/70 bg-background text-sm text-muted-foreground shadow-none">
      <button
        className="group/change-header relative isolate flex min-h-11 w-full items-center gap-2 overflow-hidden bg-muted/35 px-3 py-2 text-left hover:text-foreground"
        type="button"
        onClick={() => openTurnFileChanges(sessionID, turnID, changes)}
      >
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 bg-muted/[46%] opacity-0 group-hover/change-header:opacity-100"
        />
        <span className="relative z-[1] shrink-0 text-foreground/70 group-hover/change-header:text-foreground">
          <Files className="size-3.5" />
        </span>
        <span className="relative z-[1] flex min-w-0 flex-1 items-center gap-2">
          <span className="truncate font-medium text-foreground">{summary}</span>
          {additions > 0 || deletions > 0 ? (
            <span className="flex shrink-0 items-center gap-1.5 font-mono text-xs">
              {additions > 0 ? <span className="text-success">+{additions}</span> : null}
              {deletions > 0 ? <span className="text-destructive">−{deletions}</span> : null}
            </span>
          ) : null}
        </span>
        <ChevronRight className="relative z-[1] size-3.5 shrink-0 opacity-60" />
      </button>
      <div className="border-t border-border/70 bg-background px-1 py-1">
        {visible.map((change) => (
          <button
            key={change.id}
            className="group/change-row relative isolate flex min-h-8 w-full min-w-0 items-center gap-2 overflow-hidden rounded px-2 text-left hover:text-foreground"
            type="button"
            onClick={() => openTurnFileChanges(sessionID, turnID, changes, change.id)}
          >
            <span
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 bg-muted/60 opacity-0 group-hover/change-row:opacity-100"
            />
            <span className={cn("relative z-[1] w-3 shrink-0 text-center font-mono text-[10px] font-semibold", statusClass(change.kind))}>
              {statusLetter(change.kind)}
            </span>
            <code className="relative z-[1] min-w-0 flex-1 truncate font-mono text-xs">{turnFileChangeLabel(change, changes)}</code>
            {change.additions > 0 ? <span className="relative z-[1] shrink-0 text-xs text-success">+{change.additions}</span> : null}
            {change.deletions > 0 ? <span className="relative z-[1] shrink-0 text-xs text-destructive">−{change.deletions}</span> : null}
          </button>
        ))}
      </div>
      {collapsible ? (
        <button
          className="group/change-footer relative isolate flex h-9 w-full items-center gap-1.5 overflow-hidden border-t border-border/70 bg-muted/20 px-3 text-left text-xs font-medium text-foreground/85 hover:text-foreground"
          type="button"
          onClick={() => setExpanded((current) => !current)}
        >
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-muted/[38%] opacity-0 group-hover/change-footer:opacity-100"
          />
          <span className="relative z-[1]">{expanded ? t("transcript.turnFilesCollapse") : t("transcript.turnFilesMore").replace("{count}", String(hiddenCount))}</span>
          <ChevronDown className={cn("relative z-[1] size-3.5 transition-transform", expanded && "rotate-180")} />
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
