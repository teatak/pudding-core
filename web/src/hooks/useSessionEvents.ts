import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { queryKeys } from "@/api/queryKeys";
import { sessionEvent } from "@/contracts/events";
import { useOverlayStore } from "@/state/overlayStore";

export function useSessionEvents(sessionID: string | undefined, token: string) {
  const queryClient = useQueryClient();
  const applyEvent = useOverlayStore((state) => state.applyEvent);

  useEffect(() => {
    if (!sessionID || !token) {
      return;
    }
    const params = new URLSearchParams({ token });
    const source = new EventSource(`/sessions/${encodeURIComponent(sessionID)}/events?${params.toString()}`);

    source.onopen = () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.messages(sessionID) });
    };

    const handleMessage = (event: MessageEvent<string>) => {
      let payload: unknown;
      try {
        payload = JSON.parse(event.data);
      } catch {
        console.warn("malformed session event payload", event.data);
        return;
      }
      const parsed = sessionEvent.safeParse(payload);
      if (!parsed.success) {
        console.warn("invalid session event", parsed.error);
        return;
      }
      applyEvent(parsed.data);
      if (
        parsed.data.kind === "turn.completed" ||
        parsed.data.kind === "turn.failed" ||
        parsed.data.kind === "turn.cancelled"
      ) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.messages(sessionID) });
      }
    };

    source.addEventListener("turn.started", handleMessage);
    source.addEventListener("turn.delta", handleMessage);
    source.addEventListener("turn.completed", handleMessage);
    source.addEventListener("turn.failed", handleMessage);
    source.addEventListener("turn.cancelled", handleMessage);
    source.addEventListener("ping", handleMessage);

    source.onerror = () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.messages(sessionID) });
    };

    return () => source.close();
  }, [applyEvent, queryClient, sessionID, token]);
}
