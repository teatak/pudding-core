import { useQuery } from "@tanstack/react-query";
import { ChevronDown, FileDiff } from "lucide-react";
import type { ReactNode } from "react";

import { getTurnFileChange, type TurnFileChange } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
} from "@/components/AppMenu";
import { TextDiffViewer } from "@/components/diff/TextDiffViewer";
import { Spinner } from "@/components/Spinner";
import { DropdownMenu, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useI18n } from "@/i18n";
import { turnFileChangeFullPath, turnFileChangeLabel, turnFileDiffChanges } from "@/lib/turnFileChanges";
import { cn } from "@/lib/utils";
import { selectTurnFileChange, type FilePreview } from "@/state/filePreviewStore";

export function TurnFileDiffSurface({ active, preview, token }: { active: boolean; preview: FilePreview; token: string }) {
  const { t } = useI18n();
  const changes = turnFileDiffChanges(preview.fileChanges || []);
  const selected = changes.find((change) => change.id === preview.selectedFileChangeID) || changes[0];
  const detailQuery = useQuery({
    enabled: active && Boolean(token && preview.turnID && selected?.id),
    queryKey: queryKeys.turnFileChange(preview.sessionID, preview.turnID || "", selected?.id || ""),
    queryFn: () => getTurnFileChange(token, preview.sessionID, preview.turnID || "", selected!.id),
    staleTime: Infinity,
  });
  const detail = detailQuery.data;

  return (
    <div
      aria-hidden={!active}
      className={cn(
        "absolute inset-0 z-10 flex min-h-0 flex-col overflow-hidden bg-[var(--workspace-background)] text-card-foreground",
        !active && "pointer-events-none invisible opacity-0",
      )}
    >
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] px-3">
        <FileDiff className="size-4 shrink-0 text-muted-foreground" />
        {selected ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button className="flex min-w-0 max-w-full flex-1 items-center gap-1.5 rounded px-1.5 py-1 text-left hover:bg-muted" type="button">
                <ChangeStatus change={selected} />
                <code className="min-w-0 flex-1 truncate font-mono text-xs" >{turnFileChangeLabel(selected, changes)}</code>
                <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-80 w-[min(32rem,calc(100vw-2rem))] overflow-y-auto">
              {changes.map((change) => (
                <DropdownMenuItem key={change.id} onSelect={() => selectTurnFileChange(preview.sessionID, preview.id, change.id)}>
                  <ChangeStatus change={change} />
                  <code className="min-w-0 flex-1 truncate font-mono text-xs">{turnFileChangeLabel(change, changes)}</code>
                  <ChangeStats change={change} />
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <span className="text-xs text-muted-foreground">{t("turnFiles.empty")}</span>
        )}
        <span className="shrink-0 text-[11px] text-muted-foreground">{changes.length}</span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-background">
        {detailQuery.isLoading ? (
          <DiffStatus><Spinner className="size-6" />{t("common.loading")}</DiffStatus>
        ) : detailQuery.isError ? (
          <DiffStatus>{t("turnFiles.loadFailed")}</DiffStatus>
        ) : detail?.binary ? (
          <DiffStatus>{t("turnFiles.binary")}</DiffStatus>
        ) : detail?.tooLarge ? (
          <DiffStatus>{t("turnFiles.tooLarge")}</DiffStatus>
        ) : detail ? (
          <TextDiffViewer oldValue={detail.oldContent || ""} newValue={detail.newContent || ""} />
        ) : null}
      </div>
      {selected ? (
        <div className="flex h-7 shrink-0 items-center gap-2 border-t border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] px-3 text-[11px] text-muted-foreground">
          <span>{t(kindLabelKey(selected.kind))}</span>
          {selected.originalPath ? <><span aria-hidden="true">·</span><code className="truncate font-mono">{selected.originalPath} → {selected.path}</code></> : null}
          <span className="flex-1" />
          <ChangeStats change={selected} />
        </div>
      ) : null}
    </div>
  );
}

function ChangeStatus({ change }: { change: TurnFileChange }) {
  return <span className={cn("w-3 shrink-0 text-center font-mono text-[10px] font-semibold", statusClass(change.kind))}>{statusLetter(change.kind)}</span>;
}

function ChangeStats({ change }: { change: TurnFileChange }) {
  return (
    <span className="flex shrink-0 items-center gap-1.5 text-[10px]">
      {change.additions > 0 ? <span className="text-git-added">+{change.additions}</span> : null}
      {change.deletions > 0 ? <span className="text-git-deleted">−{change.deletions}</span> : null}
    </span>
  );
}

function DiffStatus({ children }: { children: ReactNode }) {
  return <div className="flex h-full min-h-64 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-muted-foreground">{children}</div>;
}

function statusLetter(kind: TurnFileChange["kind"]) {
  if (kind === "added") return "A";
  if (kind === "deleted") return "D";
  if (kind === "renamed") return "R";
  return "M";
}

function statusClass(kind: TurnFileChange["kind"]) {
  if (kind === "added") return "text-git-added";
  if (kind === "deleted") return "text-git-deleted";
  if (kind === "renamed") return "text-git-renamed";
  return "text-git-modified";
}

function kindLabelKey(kind: TurnFileChange["kind"]) {
  if (kind === "added") return "turnFiles.added" as const;
  if (kind === "deleted") return "turnFiles.deleted" as const;
  if (kind === "renamed") return "turnFiles.renamed" as const;
  return "turnFiles.modified" as const;
}
