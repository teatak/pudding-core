import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getCommandApprovals, revokeCommandApprovals } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useRailOverlayHold } from "@/components/session-rail/overlayHold";
import { commandApprovalReasonKey } from "@/lib/commandApprovalReasons";
import { useI18n } from "@/i18n";

export function CommandApprovalsDialog({ token, sessionID, onClose }: { token: string; sessionID: string; onClose: () => void }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  useRailOverlayHold(true);
  const queryKey = queryKeys.commandApprovals(sessionID);
  const status = useQuery({ queryKey, queryFn: () => getCommandApprovals(token, sessionID), staleTime: 0 });
  const revoke = useMutation({
    mutationFn: () => revokeCommandApprovals(token, sessionID),
    onSuccess: (data) => queryClient.setQueryData(queryKey, data),
  });
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent data-command-approvals className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("commandApproval.title")}</DialogTitle>
          <DialogDescription>{t("commandApproval.description")}</DialogDescription>
        </DialogHeader>
        {status.isPending ? <Spinner /> : status.isError ? (
          <Button variant="outline" onClick={() => void status.refetch()}>{t("common.refresh")}</Button>
        ) : (
          <div className="space-y-3 text-sm">
            <p>{t("commandApproval.count").replace("{count}", String(status.data.grantCount)).replace("{reused}", String(status.data.reusedCount))}</p>
            <div className="space-y-1 text-muted-foreground">
              {Object.entries(status.data.approvalReasons).map(([reason, count]) => {
                const key = commandApprovalReasonKey(reason);
                return key ? <p key={reason} className="flex justify-between gap-3"><span>{t(key)}</span><span>{count}</span></p> : null;
              })}
            </div>
          </div>
        )}
        {revoke.isError ? <p role="alert" className="text-sm text-destructive">{t("commandApproval.revokeFailed")}</p> : null}
        <Button variant="outline" disabled={revoke.isPending || status.isPending || status.isError} onClick={() => revoke.mutate()}>
          {revoke.isPending ? <Spinner /> : null}{t("commandApproval.revoke")}
        </Button>
      </DialogContent>
    </Dialog>
  );
}
