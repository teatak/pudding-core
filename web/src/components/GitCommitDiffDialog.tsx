import { Check, X } from "lucide-react";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useI18n } from "@/i18n";

export type GitCommitApproval = {
  additions: number;
  branch: string;
  commitMessage: string;
  deletions: number;
  diff: string;
  fileCount: number;
  files: Array<{
    additions: number;
    deletions: number;
    path: string;
  }>;
  repoRoot: string;
  truncated: boolean;
};

export function GitCommitDiffDialog({
  approval,
  committing,
  onCommit,
  onOpenChange,
  onReject,
  rejecting,
}: {
  approval: GitCommitApproval | null;
  committing?: boolean;
  onCommit: () => void;
  onOpenChange: (open: boolean) => void;
  onReject: () => void;
  rejecting?: boolean;
}) {
  const { t } = useI18n();
  const busy = committing || rejecting;
  return (
    <Dialog open={Boolean(approval)} onOpenChange={onOpenChange}>
      <DialogContent className="grid max-h-[min(800px,calc(100svh-2rem))] w-[min(1040px,calc(100vw-2rem))] max-w-none grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="px-4 pt-4 pr-12">
          <DialogTitle>{t("transcript.approvalGitCommitReviewTitle")}</DialogTitle>
          <DialogDescription className="grid gap-0.5">
            <span className="truncate text-foreground/90">{approval?.commitMessage}</span>
            <span className="truncate font-mono text-[11px]">{approval?.repoRoot}</span>
          </DialogDescription>
        </DialogHeader>
        {approval ? (
          <div className="flex flex-wrap gap-x-4 gap-y-1 border-y border-border/60 px-4 py-2 text-[11px] text-muted-foreground">
            {approval.branch ? <code className="font-mono">{approval.branch}</code> : null}
            <span>{t("transcript.approvalPatchFiles").replace("{count}", String(approval.fileCount))}</span>
            <span className="font-mono text-success">+{approval.additions}</span>
            <span className="font-mono text-destructive">-{approval.deletions}</span>
            {approval.truncated ? <span className="text-warning">{t("transcript.codeTruncated")}</span> : null}
          </div>
        ) : null}
        <div className="min-h-0 min-w-0 overflow-y-auto px-4 py-3">
          {approval ? (
            <div className="grid min-w-0 gap-3">
              <div className="grid gap-0.5">
                {approval.files.map((file) => (
                  <div key={file.path} className="grid min-h-7 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-b border-border/40 text-[11px] last:border-b-0">
                    <code className="min-w-0 truncate font-mono text-foreground/90">{file.path}</code>
                    <span className="shrink-0 font-mono">
                      <span className="text-success">+{file.additions}</span>{" "}
                      <span className="text-destructive">-{file.deletions}</span>
                    </span>
                  </div>
                ))}
              </div>
              <pre className="block max-h-[520px] w-full min-w-0 max-w-full overflow-auto whitespace-pre border-t border-border/60 pt-3 font-mono text-[11px] leading-4 text-foreground/85">{approval.diff}</pre>
            </div>
          ) : null}
        </div>
        <DialogFooter className="mx-0 mb-0 flex-row items-center justify-end gap-2 rounded-none rounded-b-xl px-4 py-3">
          <Button className="h-8 min-w-20 gap-1.5 px-3 leading-none [&_svg]:size-4" disabled={busy} type="button" variant="ghost" onClick={onReject}>
            {rejecting ? <Spinner /> : <X />}
            {t("transcript.approvalDeny")}
          </Button>
          <Button className="h-8 min-w-20 gap-1.5 px-3 leading-none [&_svg]:size-4" disabled={!approval?.diff || busy} type="button" onClick={onCommit}>
            {committing ? <Spinner /> : <Check />}
            {t("transcript.approvalGitCommit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
