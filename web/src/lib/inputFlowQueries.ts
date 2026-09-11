import type {QueryClient} from "@tanstack/react-query";
import {queryKeys} from "@/api/queryKeys";

const INPUT_FLOW_CLIENT_MESSAGE_PREFIX = "input-flow-";

// 答复的幂等身份由后端固定为 input-flow-{requestID}:撤回、重答与 canonical
// 对账都依赖它,前端只在这一个地方解析。
export function inputFlowRequestID(clientMessageID: string | undefined) {
  if (!clientMessageID?.startsWith(INPUT_FLOW_CLIENT_MESSAGE_PREFIX)) return undefined;
  const requestID = clientMessageID.slice(INPUT_FLOW_CLIENT_MESSAGE_PREFIX.length);
  return requestID || undefined;
}

// Queue entries for question replies retain this identity through withdrawal
// and re-answering. Only the backend snapshot decides whether one is answered.
export function refreshInputRequestForQueuedInput(client: QueryClient, sessionID: string, clientMessageID: string) {
  const requestID = inputFlowRequestID(clientMessageID);
  if (!requestID) return;
  return client.invalidateQueries({queryKey: queryKeys.inputRequest(sessionID, requestID)});
}
