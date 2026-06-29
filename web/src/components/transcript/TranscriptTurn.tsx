import { memo } from "react";

import { AssistantOutput, CompactPendingMarker } from "./AssistantOutput";
import type { TranscriptDisplaySettings, TranscriptTurnVM, TurnDisclosureState } from "./types";
import { UserInput } from "./UserInput";

function TranscriptTurnView({
  disclosure,
  displaySettings,
  onAssistantContentGrow,
  onAssistantRevealComplete,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  turn,
}: {
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  onAssistantContentGrow?: () => void;
  onAssistantRevealComplete?: (turnID: string) => void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  turn: TranscriptTurnVM;
}) {
  const anchorTurnID = turn.turnID || turn.key;
  return (
    <div className="grid min-w-0 gap-4" data-transcript-turn-id={anchorTurnID}>
      {turn.user ? (
        <div>
          <UserInput
            user={turn.user}
            onQueuedCancel={onQueuedCancel}
            onQueuedEditStart={onQueuedEditStart}
            onQueuedSave={onQueuedSave}
          />
        </div>
      ) : null}
      {turn.assistant ? (
        <div className="min-w-0" data-transcript-ai-anchor={anchorTurnID}>
          <AssistantOutput
            assistant={turn.assistant}
            disclosure={disclosure}
            displaySettings={displaySettings}
            turnID={anchorTurnID}
            onContentGrow={onAssistantContentGrow}
            onRevealComplete={onAssistantRevealComplete}
          />
        </div>
      ) : null}
      {turn.compact ? (
        <div className="min-w-0" data-transcript-ai-anchor={anchorTurnID}>
          <CompactPendingMarker />
        </div>
      ) : null}
    </div>
  );
}

export const TranscriptTurn = memo(TranscriptTurnView, (previous, next) => {
  return (
    previous.disclosure === next.disclosure &&
    previous.displaySettings === next.displaySettings &&
    transcriptTurnEqual(previous.turn, next.turn)
  );
});

function transcriptTurnEqual(previous: TranscriptTurnVM, next: TranscriptTurnVM) {
  return (
    previous.key === next.key &&
    previous.kind === next.kind &&
    previous.turnID === next.turnID &&
    previous.clientMessageID === next.clientMessageID &&
    compactEqual(previous.compact, next.compact) &&
    userEqual(previous.user, next.user) &&
    assistantEqual(previous.assistant, next.assistant)
  );
}

function compactEqual(previous: TranscriptTurnVM["compact"], next: TranscriptTurnVM["compact"]) {
  if (previous === next) {
    return true;
  }
  if (!previous || !next) {
    return false;
  }
  return previous.sessionID === next.sessionID && previous.startedAt === next.startedAt;
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
