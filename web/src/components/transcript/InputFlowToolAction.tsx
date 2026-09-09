import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {getUserInputRequest} from "@/api/client";
import {queryKeys} from "@/api/queryKeys";
import {Button} from "@/components/ui/button";
import {Spinner} from "@/components/Spinner";
import {useI18n} from "@/i18n";
import {showInputFlow, useInputFlowStore} from "@/state/inputFlowStore";

// Recover the question from canonical tool arguments, not from saved UI state.
export function InputFlowToolAction({token, sessionID, requestID, status}: {token: string; sessionID: string; requestID: string; status: string}) {
  const {t} = useI18n();
  const client = useQueryClient();
  const visible = useInputFlowStore((state) => state.requests.some((item) => item.sessionID === sessionID && item.id === requestID));
  const snapshot = useQuery({
    queryKey: queryKeys.inputRequest(sessionID, requestID),
    queryFn: () => getUserInputRequest(token, sessionID, requestID),
    enabled: status !== "answered",
    retry: false,
  });
  const reopen = useMutation({
    mutationFn: () => getUserInputRequest(token, sessionID, requestID),
    onSuccess: (request) => {
      client.setQueryData(queryKeys.inputRequest(sessionID, requestID), request);
      if (request.status !== "answered" && request.status !== "failed") showInputFlow(request);
    },
  });
  if (status === "answered" || snapshot.data?.status === "answered") return <span className="text-xs text-muted-foreground">{t("inputFlow.completed")}</span>;
  if (status === "failed" || visible) return null;
  return <span className="flex shrink-0 items-center gap-1">
    <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" disabled={reopen.isPending} onClick={() => reopen.mutate()}>
      {reopen.isPending ? <Spinner /> : null}{t("inputFlow.answerLater")}
    </Button>
    {reopen.isError ? <span className="text-xs text-destructive">{t("inputFlow.reopenFailed")}</span> : null}
  </span>;
}
