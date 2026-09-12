import type { SessionEvent } from "@/contracts/events";

// Only transient text deltas are coalesced. Every other event is an ordering
// boundary, especially tool calls, steering, cancellation and canonical commit.
export function createSessionEventBatcher(deliver: (event: SessionEvent) => void) {
  let pending: Extract<SessionEvent, { kind: "turn.delta" }> | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function flush() {
    clearTimeout(timer);
    timer = undefined;
    const event = pending;
    pending = undefined;
    if (event) deliver(event);
  }

  function push(event: SessionEvent) {
    if (event.kind !== "turn.delta") {
      flush();
      deliver(event);
      return;
    }
    if (pending && (pending.sessionID !== event.sessionID || pending.turnID !== event.turnID || pending.part !== event.part)) {
      flush();
    }
    pending = pending ? { ...pending, delta: pending.delta + event.delta } : event;
    timer ??= setTimeout(flush, 50);
  }

  return { push, flush };
}
