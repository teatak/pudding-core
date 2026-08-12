import type { Session } from "@/api/client";
import { isTurnPhaseActive, useOverlayStore } from "@/state/overlayStore";

export type SessionGroupActivity = "running" | "completed" | undefined;

export function isSessionTurnRunning(
  session: Session,
  runningTurns: Record<string, string | undefined>,
  turnPhases: ReturnType<typeof useOverlayStore.getState>["turnPhases"],
) {
  const phase = turnPhases[session.id];
  if (phase && !isTurnPhaseActive(phase)) {
    return false;
  }
  return session.running || Boolean(runningTurns[session.id]) || isTurnPhaseActive(phase);
}

export function sessionGroupActivity(
  sessions: Session[],
  runningTurns: Record<string, string | undefined>,
  turnPhases: ReturnType<typeof useOverlayStore.getState>["turnPhases"],
  completedSessions: Record<string, boolean | undefined>,
): SessionGroupActivity {
  if (sessions.some((session) => isSessionTurnRunning(session, runningTurns, turnPhases))) {
    return "running";
  }
  return sessions.some((session) => completedSessions[session.id]) ? "completed" : undefined;
}
