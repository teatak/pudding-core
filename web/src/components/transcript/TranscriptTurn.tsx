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
  token,
  turn,
}: {
  disclosure?: TurnDisclosureState;
  displaySettings?: TranscriptDisplaySettings;
  onAssistantContentGrow?: () => void;
  onAssistantRevealComplete?: (turnID: string) => void;
  onQueuedCancel?: (clientMessageID: string) => Promise<unknown>;
  onQueuedEditStart?: (clientMessageID: string) => Promise<unknown>;
  onQueuedSave?: (clientMessageID: string, text: string) => Promise<unknown>;
  token: string;
  turn: TranscriptTurnVM;
}) {
  const anchorTurnID = turn.turnID || turn.key;
  return (
    <div className="grid min-w-0 gap-4" data-transcript-turn-id={anchorTurnID}>
      {turn.user ? (
        <div className="min-w-0">
          <UserInput
            token={token}
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
            token={token}
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
    previous.token === next.token &&
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
    previous.text === next.text &&
    partsEqual(previous.parts, next.parts) &&
    attachmentsEqual(previous.attachments, next.attachments) &&
    localFoldersEqual(previous.localFolders, next.localFolders)
  );
}

function partsEqual(previous: NonNullable<TranscriptTurnVM["user"]>["parts"], next: NonNullable<TranscriptTurnVM["user"]>["parts"]) {
  if (previous === next) {
    return true;
  }
  if (!previous || !next) {
    return false;
  }
  if (previous.length !== next.length) {
    return false;
  }
  return previous.every((part, index) => {
    const other = next[index];
    if (part.type !== other.type) {
      return false;
    }
    if (part.type === "attachment" && other.type === "attachment") {
      return part.id === other.id && part.attachmentKey === other.attachmentKey;
    }
    if (part.type === "local_folder" && other.type === "local_folder") {
      return part.id === other.id && part.path === other.path;
    }
    if (part.type === "text" && other.type === "text") {
      return part.text === other.text;
    }
    return true;
  });
}

function attachmentsEqual(
  previous: NonNullable<TranscriptTurnVM["user"]>["attachments"],
  next: NonNullable<TranscriptTurnVM["user"]>["attachments"],
) {
  if (previous === next) {
    return true;
  }
  if (!previous || !next) {
    return false;
  }
  if (previous.length !== next.length) {
    return false;
  }
  return previous.every((item, index) => {
    const other = next[index];
    return item.id === other.id && item.attachmentKey === other.attachmentKey && item.name === other.name && item.size === other.size;
  });
}

function localFoldersEqual(
  previous: NonNullable<TranscriptTurnVM["user"]>["localFolders"],
  next: NonNullable<TranscriptTurnVM["user"]>["localFolders"],
) {
  if (previous === next) {
    return true;
  }
  if (!previous || !next) {
    return false;
  }
  if (previous.length !== next.length) {
    return false;
  }
  return previous.every((item, index) => {
    const other = next[index];
    return item.id === other.id && item.name === other.name && item.path === other.path;
  });
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
