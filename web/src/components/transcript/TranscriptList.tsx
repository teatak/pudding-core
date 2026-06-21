import { memo } from "react";

import { TranscriptTurn } from "./TranscriptTurn";
import type { TranscriptTurnVM, TurnDisclosureState } from "./types";

export const TranscriptList = memo(function TranscriptList({
  disclosure,
  onAssistantContentGrow,
  onAssistantRevealComplete,
  onQueuedCancel,
  onQueuedEditStart,
  onQueuedSave,
  turns,
}: {
  disclosure?: TurnDisclosureState;
  onAssistantContentGrow?: () => void;
  onAssistantRevealComplete?: (turnID: string) => void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  turns: TranscriptTurnVM[];
}) {
  return (
    <>
      {turns.map((turn) => (
        <TranscriptTurn
          key={turn.key}
          disclosure={disclosure}
          onAssistantContentGrow={onAssistantContentGrow}
          onAssistantRevealComplete={onAssistantRevealComplete}
          onQueuedCancel={onQueuedCancel}
          onQueuedEditStart={onQueuedEditStart}
          onQueuedSave={onQueuedSave}
          turn={turn}
        />
      ))}
    </>
  );
});
