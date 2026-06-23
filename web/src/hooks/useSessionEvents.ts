import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { getTurn, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { upsertTurnIntoPages, type TurnsInfiniteData } from "@/components/transcript/useTranscriptTurns";
import { sessionEvent, type SessionEvent } from "@/contracts/events";
import { apiURL } from "@/state/apiBase";
import { useOverlayStore } from "@/state/overlayStore";

export function useSessionEvents(sessionID: string | undefined, token: string) {
  useVisibleSessionEvents(sessionID ? [sessionID] : [], token);
}

export function useVisibleSessionEvents(sessionIDs: string[], token: string) {
  useSessionEventSources(sessionIDs, token, true);
}

export function useBackgroundSessionEvents(sessionIDs: string[], token: string) {
  useSessionEventSources(sessionIDs, token, false);
}

function useSessionEventSources(sessionIDs: string[], token: string, syncMessages: boolean) {
  const queryClient = useQueryClient();
  const applyEvent = useOverlayStore((state) => state.applyEvent);
  const sessionIDsKey = normalizeSessionIDs(sessionIDs).join("\n");

  useEffect(() => {
    if (!token || !sessionIDsKey) {
      return;
    }
    const sources = sessionIDsKey.split("\n").map((sessionID) =>
      openSessionEventSource({
        applyEvent,
        queryClient,
        sessionID,
        syncMessages,
        token,
      }),
    );
    return () => {
      for (const source of sources) {
        source.close();
      }
    };
  }, [applyEvent, queryClient, sessionIDsKey, syncMessages, token]);
}

function normalizeSessionIDs(sessionIDs: string[]) {
  return Array.from(new Set(sessionIDs.filter(Boolean))).sort();
}

function openSessionEventSource({
  applyEvent,
  queryClient,
  sessionID,
  syncMessages,
  token,
}: {
  applyEvent: (event: SessionEvent) => void;
  queryClient: QueryClient;
  sessionID: string;
  syncMessages: boolean;
  token: string;
}) {
  const params = new URLSearchParams({ token });
  const after = useOverlayStore.getState().lastEventSeqs[sessionID];
  if (after) {
    params.set("after", String(after));
  }
  const source = new EventSource(apiURL(`/sessions/${encodeURIComponent(sessionID)}/events?${params.toString()}`));
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
    syncSessionListFromEvent(queryClient, parsed.data);
    if (parsed.data.kind === "turn.started" || isTurnTerminalEvent(parsed.data)) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessionUsage(sessionID) });
    }
    if (isTurnTerminalEvent(parsed.data) && syncMessages) {
      syncTerminalTurn(queryClient, token, sessionID, parsed.data.turnID);
    }
    if (syncMessages && (isInputEvent(parsed.data) || parsed.data.kind === "turn.started")) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
    }
  };

  source.addEventListener("turn.started", handleMessage);
  source.addEventListener("turn.delta", handleMessage);
  source.addEventListener("turn.tool", handleMessage);
  source.addEventListener("turn.completed", handleMessage);
  source.addEventListener("turn.failed", handleMessage);
  source.addEventListener("turn.cancelled", handleMessage);
  source.addEventListener("input.queued", handleMessage);
  source.addEventListener("input.updated", handleMessage);
  source.addEventListener("session.titled", handleMessage);
  source.addEventListener("ping", handleMessage);
  return {
    close: () => {
      source.close();
    },
  };
}

function syncTerminalTurn(queryClient: QueryClient, token: string, sessionID: string, turnID: string) {
  void getTurn(token, sessionID, turnID)
    .then((turn) => {
      queryClient.setQueryData<TurnsInfiniteData>(queryKeys.turns(sessionID), (previous) => upsertTurnIntoPages(previous, turn));
    })
    .catch((error) => {
      console.warn("failed to sync terminal turn", error);
    });
}

function syncSessionListFromEvent(queryClient: QueryClient, event: SessionEvent) {
  if (event.kind === "turn.started") {
    patchSessionInList(queryClient, event.sessionID, { lastActivityAt: new Date().toISOString(), running: true });
    return;
  }
  if (isTurnTerminalEvent(event)) {
    patchSessionInList(queryClient, event.sessionID, { lastActivityAt: new Date().toISOString(), running: false });
    return;
  }
  if (event.kind === "input.queued") {
    patchSessionInList(queryClient, event.sessionID, { lastActivityAt: new Date().toISOString() });
    return;
  }
  if (event.kind === "session.titled") {
    patchSessionInList(queryClient, event.sessionID, { title: event.title });
  }
}

function isTurnTerminalEvent(
  event: SessionEvent,
): event is Extract<SessionEvent, { kind: "turn.completed" | "turn.failed" | "turn.cancelled" }> {
  return event.kind === "turn.completed" || event.kind === "turn.failed" || event.kind === "turn.cancelled";
}

function isInputEvent(event: SessionEvent) {
  return event.kind === "input.queued" || event.kind === "input.updated";
}

function patchSessionInList(queryClient: QueryClient, sessionID: string, patch: Partial<Session>) {
  queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
    if (!previous) {
      return previous;
    }
    let matched = false;
    const sessions = previous.sessions.map((session) => {
      if (session.id !== sessionID) {
        return session;
      }
      matched = true;
      return { ...session, ...patch };
    });
    return matched ? { sessions: sortSessionsByActivity(sessions) } : previous;
  });
}

function sortSessionsByActivity(sessions: Session[]) {
  return [...sessions].sort((a, b) => {
    const activityDiff = Date.parse(b.lastActivityAt) - Date.parse(a.lastActivityAt);
    if (activityDiff !== 0) {
      return activityDiff;
    }
    return Date.parse(b.createdAt) - Date.parse(a.createdAt);
  });
}
