import { memo } from "react";

import { AssistantOutput } from "./AssistantOutput";
import type { TranscriptTurnVM, TurnDisclosureState } from "./types";
import { UserInput } from "./UserInput";

export const TranscriptTurn = memo(function TranscriptTurn({
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
    <div
      className="grid min-w-0 gap-4"
      data-transcript-item-id={turn.key}
      data-transcript-item-role="turn"
      data-transcript-turn-id={turn.turnID}
    >
      {turn.user ? (
        <UserInput
          user={turn.user}
          onQueuedCancel={onQueuedCancel}
          onQueuedEditStart={onQueuedEditStart}
          onQueuedSave={onQueuedSave}
        />
      ) : null}
      {turn.assistant ? (
        <div
          className="min-w-0"
          data-transcript-anchor-id={turn.assistant.anchorID}
          data-transcript-anchor-role="assistant"
        >
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
});
