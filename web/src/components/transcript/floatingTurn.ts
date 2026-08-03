import type { TranscriptTurnVM } from "./types";

export function selectFloatingTurn(
  turns: TranscriptTurnVM[],
  runningTurnID?: string,
): TranscriptTurnVM | undefined {
  if (runningTurnID) {
    const running = findLast(turns, (turn) => turn.turnID === runningTurnID);
    if (running) {
      return running;
    }
  }

  const submitting = findLast(
    turns,
    (turn) =>
      turn.kind === "pending" &&
      turn.user?.status === "submitting" &&
      Boolean(turn.clientMessageID || turn.user.clientMessageID),
  );
  if (submitting) {
    return submitting;
  }

  return findLast(turns, (turn) => Boolean(turn.turnID));
}

export function selectFloatingQueuedTurns(turns: TranscriptTurnVM[]) {
  return turns.filter(
    (turn) =>
      turn.kind === "pending" &&
      Boolean(turn.user) &&
      (turn.user?.status === "queued" || turn.user?.status === "editing"),
  );
}

function findLast<T>(items: T[], predicate: (item: T) => boolean) {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (predicate(items[index])) {
      return items[index];
    }
  }
  return undefined;
}
