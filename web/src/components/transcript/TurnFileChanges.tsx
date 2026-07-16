import { ChevronRight, Files } from "lucide-react";

import type { TurnFileChange } from "@/api/client";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { openTurnFileChanges } from "@/state/filePreviewStore";

export function TurnFileChanges({ changes, sessionID, turnID }: {
  changes: TurnFileChange[];
  sessionID: string;
  turnID: string;
}) {
  const { t } = useI18n();
  const visible = changes.length <= 6 ? changes : changes.slice(0, 5);
  const hiddenCount = changes.length - visible.length;

  return (
    <section className="min-w-0 text-xs text-muted-foreground">
      <button
        className="group flex h-7 max-w-full items-center gap-1.5 rounded px-1.5 transition-colors hover:bg-muted/60 hover:text-foreground"
        type="button"
        onClick={() => openTurnFileChanges(sessionID, turnID, changes)}
      >
        <Files className="size-3.5 shrink-0" />
        <span>{t("transcript.turnFilesTitle")}</span>
        <span>{changes.length}</span>
        <ChevronRight className="size-3.5 shrink-0 opacity-60 transition-transform group-hover:translate-x-0.5" />
      </button>
      <div className="ml-1.5 border-l pl-2">
        {visible.map((change) => (
          <button
            key={change.id}
            className="group flex h-7 w-full min-w-0 items-center gap-2 rounded px-1.5 text-left transition-colors hover:bg-muted/60 hover:text-foreground"
            title={`${change.rootPath}/${change.path}`}
            type="button"
            onClick={() => openTurnFileChanges(sessionID, turnID, changes, change.id)}
          >
            <span className={cn("w-3 shrink-0 text-center font-mono text-[10px] font-semibold", statusClass(change.kind))}>
              {statusLetter(change.kind)}
            </span>
            <code className="min-w-0 flex-1 truncate font-mono text-[11px]">{change.path}</code>
            {change.additions > 0 ? <span className="shrink-0 text-success">+{change.additions}</span> : null}
            {change.deletions > 0 ? <span className="shrink-0 text-destructive">−{change.deletions}</span> : null}
          </button>
        ))}
        {hiddenCount > 0 ? (
          <button
            className="h-7 rounded px-1.5 text-left transition-colors hover:bg-muted/60 hover:text-foreground"
            type="button"
            onClick={() => openTurnFileChanges(sessionID, turnID, changes)}
          >
            {t("transcript.turnFilesMore").replace("{count}", String(hiddenCount))}
          </button>
        ) : null}
      </div>
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
