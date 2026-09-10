import type {QueryClient} from "@tanstack/react-query";
import {queryKeys} from "@/api/queryKeys";

// Queue entries for question replies retain this identity through withdrawal
// and re-answering. Only the backend snapshot decides whether one is answered.
export function refreshInputRequestForQueuedInput(client: QueryClient, sessionID: string, clientMessageID: string) {
  const prefix = "input-flow-";
  if (!clientMessageID.startsWith(prefix) || clientMessageID.length === prefix.length) return;
  return client.invalidateQueries({queryKey: queryKeys.inputRequest(sessionID, clientMessageID.slice(prefix.length))});
}
