import { memo } from "react";

import { AssistantOutput } from "./AssistantOutput";
import type { TranscriptTurnVM, TurnDisclosureState } from "./types";
import { UserInput } from "./UserInput";

function TranscriptTurnView({
  disclosure,
  onAssistantContentGrow,
  onAssistantRevealComplete,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  turn,
}: {
  disclosure?: TurnDisclosureState;
  onAssistantContentGrow?: () => void;
  onAssistantRevealComplete?: (turnID: string) => void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  turn: TranscriptTurnVM;
}) {
  return (
    <div className="grid min-w-0 gap-4" data-transcript-turn-id={turn.turnID}>
      {turn.user ? (
        <UserInput
          user={turn.user}
          onQueuedCancel={onQueuedCancel}
          onQueuedEditStart={onQueuedEditStart}
          onQueuedSave={onQueuedSave}
        />
      ) : null}
      {turn.assistant ? (
        <div className="min-w-0">
          <AssistantOutput
            assistant={turn.assistant}
            disclosure={disclosure}
            turnID={turn.turnID || turn.key}
            onContentGrow={onAssistantContentGrow}
            onRevealComplete={onAssistantRevealComplete}
          />
        </div>
      ) : null}
    </div>
  );
}

export const TranscriptTurn = memo(TranscriptTurnView, (previous, next) => {
  return previous.disclosure === next.disclosure && transcriptTurnEqual(previous.turn, next.turn);
});

function transcriptTurnEqual(previous: TranscriptTurnVM, next: TranscriptTurnVM) {
  return (
    previous.key === next.key &&
    previous.kind === next.kind &&
    previous.turnID === next.turnID &&
    previous.clientMessageID === next.clientMessageID &&
    userEqual(previous.user, next.user) &&
    assistantEqual(previous.assistant, next.assistant)
  );
}

function userEqual(previous: TranscriptTurnVM["user"], next: TranscriptTurnVM["user"]) {
  if (previous === next) {
    return true;
  }
  if (!previous || !next) {
    return false;
  }
  return (
    previous.clientMessageID === next.clientMessageID &&
    previous.createdAt === next.createdAt &&
    previous.interrupted === next.interrupted &&
    previous.pending === next.pending &&
    previous.status === next.status &&
    previous.text === next.text
  );
}

function assistantEqual(previous: TranscriptTurnVM["assistant"], next: TranscriptTurnVM["assistant"]) {
  if (previous === next) {
    return true;
  }
  if (!previous || !next || previous.kind !== next.kind) {
    return false;
  }
  if (previous.kind === "canonical" && next.kind === "canonical") {
    return previous.duration === next.duration && previous.messages === next.messages;
  }
  if (previous.kind === "live" && next.kind === "live") {
    return (
      previous.canonicalReady === next.canonicalReady &&
      previous.overlay === next.overlay &&
      previous.phase === next.phase
    );
  }
  if (previous.kind === "phase" && next.kind === "phase") {
    return previous.phase === next.phase;
  }
  return false;
}
