import { Check, Loader2, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useI18n } from "@/i18n";

export type PatchProposalApproval = {
  additions: number;
  deletions: number;
  diff: string;
  fileCount: number;
  files: Array<{
    additions: number;
    deletions: number;
    operation: "create" | "update" | "delete";
    path: string;
  }>;
  projectRoot: string;
  proposalID: string;
};

export function PatchProposalDiffDialog({
  applying,
  onApply,
  onOpenChange,
  onReject,
  proposal,
  rejecting,
}: {
  applying?: boolean;
  onApply: () => void;
  onOpenChange: (open: boolean) => void;
  onReject: () => void;
  proposal: PatchProposalApproval | null;
  rejecting?: boolean;
}) {
  const { t } = useI18n();
  const busy = applying || rejecting;
  return (
    <Dialog open={Boolean(proposal)} onOpenChange={onOpenChange}>
      <DialogContent className="grid max-h-[min(800px,calc(100svh-2rem))] w-[min(1040px,calc(100vw-2rem))] max-w-none grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="px-4 pt-4 pr-12">
          <DialogTitle>{t("transcript.approvalPatchReviewTitle")}</DialogTitle>
          <DialogDescription className="truncate font-mono text-[11px]">{proposal?.projectRoot}</DialogDescription>
        </DialogHeader>
        {proposal ? (
          <div className="flex flex-wrap gap-x-4 gap-y-1 border-y border-border/60 px-4 py-2 text-[11px] text-muted-foreground">
            <span>{t("transcript.approvalPatchFiles").replace("{count}", String(proposal.fileCount))}</span>
            <span className="font-mono text-success">+{proposal.additions}</span>
            <span className="font-mono text-destructive">-{proposal.deletions}</span>
            <code className="min-w-0 truncate font-mono">{proposal.proposalID}</code>
          </div>
        ) : null}
        <div className="min-h-0 overflow-y-auto px-4 py-3">
          {proposal ? (
            <div className="grid gap-3">
              <div className="grid gap-0.5">
                {proposal.files.map((file) => (
                  <div key={file.path} className="grid min-h-7 grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-3 border-b border-border/40 text-[11px] last:border-b-0">
                    <code className="min-w-0 truncate font-mono text-foreground/90">{file.path}</code>
                    <span className="text-muted-foreground">{t(`transcript.codePatchOperation.${file.operation}`)}</span>
                    <span className="shrink-0 font-mono">
                      <span className="text-success">+{file.additions}</span>{" "}
                      <span className="text-destructive">-{file.deletions}</span>
                    </span>
                  </div>
                ))}
              </div>
              <pre className="max-h-[520px] overflow-auto whitespace-pre border-t border-border/60 pt-3 font-mono text-[11px] leading-4 text-foreground/85">{proposal.diff}</pre>
            </div>
          ) : null}
        </div>
        <DialogFooter className="mx-0 mb-0 flex-row items-center justify-end gap-2 rounded-none rounded-b-xl px-4 py-3">
          <Button className="h-8 min-w-20 gap-1.5 px-3 leading-none [&_svg]:size-4" disabled={busy} type="button" variant="ghost" onClick={onReject}>
            {rejecting ? <Loader2 className="animate-spin" /> : <X />}
            {t("transcript.approvalDeny")}
          </Button>
          <Button className="h-8 min-w-20 gap-1.5 px-3 leading-none [&_svg]:size-4" disabled={!proposal?.diff || busy} type="button" onClick={onApply}>
            {applying ? <Loader2 className="animate-spin" /> : <Check />}
            {t("transcript.approvalPatchApply")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
