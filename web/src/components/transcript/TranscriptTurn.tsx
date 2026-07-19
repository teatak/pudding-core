import { memo } from "react";

import { AssistantOutput, AssistantOutputMeta, CompactPendingMarker } from "./AssistantOutput";
import { TurnFileChanges } from "./TurnFileChanges";
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
  sessionID,
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
  sessionID: string;
  token: string;
  turn: TranscriptTurnVM;
}) {
  const anchorTurnID = turn.turnID || turn.key;
  return (
    <div className="grid min-w-0 gap-3" data-transcript-turn-id={anchorTurnID}>
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
        <div className="group min-w-0" data-transcript-ai-anchor={anchorTurnID}>
          <AssistantOutput
            assistant={turn.assistant}
            disclosure={disclosure}
            displaySettings={displaySettings}
            sessionID={sessionID}
            token={token}
            turnID={anchorTurnID}
            onContentGrow={onAssistantContentGrow}
            onRevealComplete={onAssistantRevealComplete}
          />
          {turn.turnID && turn.fileChanges?.length ? (
            <div className="mt-3">
              <TurnFileChanges changes={turn.fileChanges} sessionID={sessionID} turnID={turn.turnID} />
            </div>
          ) : null}
          <AssistantOutputMeta assistant={turn.assistant} />
        </div>
      ) : null}
      {!turn.assistant && turn.turnID && turn.fileChanges?.length ? (
        <TurnFileChanges changes={turn.fileChanges} sessionID={sessionID} turnID={turn.turnID} />
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
    previous.sessionID === next.sessionID &&
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
    previous.fileChanges === next.fileChanges &&
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
    localFoldersEqual(previous.localFolders, next.localFolders) &&
    projectReferencesEqual(previous.projectReferences, next.projectReferences)
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
    if (part.type === "project_reference" && other.type === "project_reference") {
      return (
        part.id === other.id &&
        part.rootID === other.rootID &&
        part.path === other.path &&
        part.sourcePath === other.sourcePath &&
        part.kind === other.kind &&
        part.startLine === other.startLine &&
        part.startColumn === other.startColumn &&
        part.endLine === other.endLine &&
        part.endColumn === other.endColumn
      );
    }
    if (part.type === "text" && other.type === "text") {
      return part.text === other.text;
    }
    if (part.type === "ui_context" && other.type === "ui_context") {
      return (
        part.surface === other.surface &&
        part.resource === other.resource &&
        part.id === other.id &&
        part.name === other.name &&
        part.path === other.path &&
        part.url === other.url &&
        part.kind === other.kind &&
        part.rootID === other.rootID
      );
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

function projectReferencesEqual(
  previous: NonNullable<TranscriptTurnVM["user"]>["projectReferences"],
  next: NonNullable<TranscriptTurnVM["user"]>["projectReferences"],
) {
  if (previous === next) {
    return true;
  }
  if (!previous || !next || previous.length !== next.length) {
    return false;
  }
  return previous.every((item, index) => {
    const other = next[index];
    return (
      item.id === other.id &&
      item.name === other.name &&
      item.path === other.path &&
      item.sourcePath === other.sourcePath &&
      item.rootID === other.rootID &&
      item.kind === other.kind &&
      item.startLine === other.startLine &&
      item.startColumn === other.startColumn &&
      item.endLine === other.endLine &&
      item.endColumn === other.endColumn
    );
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
