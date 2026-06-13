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
        // 终结事件也刷 sessions:running 是 turns 表派生字段,overlay 清掉后
        // 若不刷新,stale 的 session.running=true 会让停止按钮/生成态卡住
        // (Composer / ChatPane 都读 overlayRunning || session.running)。
        // FinishTurn 与终结事件同事务,收到事件时库里已非 running,refetch 得 false。
        void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      }
      if (parsed.data.kind === "session.titled") {
        // 自动标题写回(provisional / LLM),刷新列表与 header
        void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      }
    };

    source.addEventListener("turn.started", handleMessage);
    source.addEventListener("turn.delta", handleMessage);
    source.addEventListener("turn.completed", handleMessage);
    source.addEventListener("turn.failed", handleMessage);
    source.addEventListener("turn.cancelled", handleMessage);
    source.addEventListener("session.titled", handleMessage);
    source.addEventListener("ping", handleMessage);

    source.onerror = () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.messages(sessionID) });
    };

    return () => source.close();
  }, [applyEvent, queryClient, sessionID, token]);
}
