import { useMemo, useRef } from "react";

import type { ConversationTurn, Message } from "@/api/client";
import {
  isTurnPhaseActive,
  type AssistantOverlay,
  type CompactRun,
  type PendingUserMessage,
  type TurnPhaseState,
} from "@/state/overlayStore";

import {
  attachmentsFromContentParts,
  localFoldersFromContentParts,
  projectReferencesFromContentParts,
  textFromContentParts,
  transcriptPhaseKey,
  type TranscriptTurnVM,
  type TurnModelVM,
  type UserInputVM,
} from "./types";

type CanonicalTurnCacheEntry = {
  duration?: string;
  item: TranscriptTurnVM;
  phase?: TurnPhaseState;
  turn: ConversationTurn;
};

export function useTranscriptViewModel({
  assistantOverlays,
  compactRun,
  pendingUsers,
  sessionID,
  sessionRunning,
  turnDurationByID,
  turnPhase,
  turns,
}: {
  assistantOverlays: AssistantOverlay[];
  compactRun?: CompactRun;
  pendingUsers: PendingUserMessage[];
  sessionID: string;
  sessionRunning: boolean;
  turnDurationByID: Map<string, string>;
  turnPhase?: TurnPhaseState;
  turns: ConversationTurn[];
}) {
  const canonicalTurnCacheRef = useRef(new Map<string, CanonicalTurnCacheEntry>());
  const messages = useMemo(() => turns.flatMap((turn) => turn.messages), [turns]);
  const canonicalMessageIDs = useMemo(() => new Set(messages.map((message) => message.id)), [messages]);
  const displayPhase = useMemo<TurnPhaseState | undefined>(() => {
    if (isTurnPhaseActive(turnPhase)) {
      return turnPhase;
    }
    if (!sessionRunning || assistantOverlays.length > 0) {
      return undefined;
    }
    return { phase: "awaiting_model", sessionID, updatedAt: "" };
  }, [assistantOverlays.length, sessionID, sessionRunning, turnPhase]);

  const visibleAssistantOverlays = useMemo(
    () =>
      assistantOverlays.filter(
        (overlay) => !overlay.assistantMessageID || !canonicalMessageIDs.has(overlay.assistantMessageID) || !overlay.revealed,
      ),
    [assistantOverlays, canonicalMessageIDs],
  );

  const turnVMs = useMemo(() => {
    const canonicalTurnCache = canonicalTurnCacheRef.current;
    const liveByTurnID = new Map(visibleAssistantOverlays.map((overlay) => [overlay.turnID, overlay]));
    const pendingByClientID = new Map(pendingUsers.map((pending) => [pending.clientMessageID, pending]));
    const waitingGuidesByTurnID = new Map<string, PendingUserMessage[]>();
    const appliedGuidesByTurnID = new Map<string, PendingUserMessage[]>();
    for (const pending of pendingUsers) {
      if (!pending.turnID || (pending.status !== "steering" && pending.status !== "steered")) {
        continue;
      }
      const guidesByTurnID = pending.status === "steering" ? waitingGuidesByTurnID : appliedGuidesByTurnID;
      guidesByTurnID.set(pending.turnID, [
        ...(guidesByTurnID.get(pending.turnID) || []),
        pending,
      ]);
    }
    const usedPendingClientIDs = new Set<string>();
    const usedLiveTurnIDs = new Set<string>();
    const seenCanonicalTurnIDs = new Set<string>();
    const items: TranscriptTurnVM[] = [];

    for (const turn of turns) {
      const visibleMessages = turn.messages.filter((message) => {
        if (message.role !== "user" || !message.clientMessageID) {
          return true;
        }
        const pending = pendingByClientID.get(message.clientMessageID);
        return pending?.turnID !== turn.id || pending.status !== "steering";
      });
      const messageSegments = splitTurnMessages(visibleMessages);
      const canonicalClientIDs = new Set(
        messageSegments
          .map((segment) => segment.user.clientMessageID)
          .filter((clientMessageID): clientMessageID is string => Boolean(clientMessageID)),
      );
      const appliedGuides = (appliedGuidesByTurnID.get(turn.id) || []).filter(
        (pending) => !canonicalClientIDs.has(pending.clientMessageID),
      );
      const waitingGuides = waitingGuidesByTurnID.get(turn.id) || [];
      if (messageSegments.length > 1 || appliedGuides.length > 0 || waitingGuides.length > 0) {
        canonicalTurnCache.delete(turn.id);
        seenCanonicalTurnIDs.add(turn.id);
        for (const segment of messageSegments) {
          if (segment.user.clientMessageID) {
            usedPendingClientIDs.add(segment.user.clientMessageID);
          }
        }
        const phaseForTurn = displayPhase?.turnID === turn.id ? displayPhase : undefined;
        const duration = turnDurationByID.get(turn.id);
        const overlay = liveByTurnID.get(turn.id);
        const sequence: NonNullable<TranscriptTurnVM["sequence"]> = [];
        if (messageSegments.length === 0) {
          const outputs = visibleMessages.filter(isTurnOutputMessage);
          if (outputs.length > 0) {
            sequence.push({
              assistant: {
                duration,
                kind: "canonical",
                messages: outputs,
                model: modelFromTurn(turn),
              },
              key: `assistant:${turn.id}:initial`,
              kind: "assistant",
            });
          }
        }
        messageSegments.forEach((segment, index) => {
          if (index > 0) {
            sequence.push({
              key: `guide:${segment.user.id}`,
              kind: "guide",
              user: userFromMessage(segment.user),
            });
          }
          // The live overlay is the current assistant segment after the latest
          // guide. A turn refetch can already contain that segment's committed
          // parts, so rendering both briefly duplicates thought/tool rows.
          const currentLiveSegment = Boolean(overlay) && index === messageSegments.length - 1;
          if (segment.outputs.length > 0 && !currentLiveSegment) {
            sequence.push({
              assistant: {
                duration: index === messageSegments.length - 1 ? duration : undefined,
                kind: "canonical",
                messages: segment.outputs,
                model: modelFromTurn(turn),
              },
              key: `assistant:${segment.user.id}`,
              kind: "assistant",
            });
          }
        });
        appliedGuides.forEach((pending) => {
          usedPendingClientIDs.add(pending.clientMessageID);
          sequence.push({
            key: `guide:pending:${pending.clientMessageID}`,
            kind: "guide",
            user: userFromPending(pending, { pending: false }),
          });
        });
        if (overlay) {
          usedLiveTurnIDs.add(overlay.turnID);
          sequence.push({
            assistant: {
              canonicalReady: Boolean(overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID)),
              kind: "live",
              overlay,
              phase: phaseForTurn,
            },
            key: `assistant:${turn.id}:live`,
            kind: "assistant",
          });
        } else if (phaseForTurn) {
          sequence.push({
            assistant: { kind: "phase", phase: phaseForTurn },
            key: `assistant:${turn.id}:phase`,
            kind: "assistant",
          });
        }
        waitingGuides.forEach((pending) => {
          usedPendingClientIDs.add(pending.clientMessageID);
          sequence.push({
            key: `guide:waiting:${pending.clientMessageID}`,
            kind: "guide",
            user: userFromPending(pending, { pending: false }),
          });
        });
        items.push({
          anchorID: turn.id,
          fileChanges: turn.fileChanges,
          fileChangeState: turn.fileChangeState,
          key: `turn:${turn.id}`,
          kind: overlay ? "live" : phaseForTurn ? "phase" : "canonical",
          sequence,
          turnID: turn.id,
          user: messageSegments[0] ? userFromMessage(messageSegments[0].user) : undefined,
        });
        continue;
      }
      const user = userFromMessages(turn.messages);
      if (turn.clientMessageID) {
        usedPendingClientIDs.add(turn.clientMessageID);
      }
      const overlay = liveByTurnID.get(turn.id);
      if (overlay) {
        usedLiveTurnIDs.add(overlay.turnID);
        items.push({
          assistant: {
            canonicalReady: Boolean(overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID)),
            kind: "live",
            overlay,
            phase: displayPhase?.turnID === overlay.turnID ? displayPhase : undefined,
          },
          key: `turn:${turn.id}`,
          kind: "live",
          turnID: turn.id,
          user,
        });
        continue;
      }

      const phaseForTurn = displayPhase?.turnID === turn.id ? displayPhase : undefined;
      const duration = turnDurationByID.get(turn.id);
      const cached = canonicalTurnCache.get(turn.id);
      if (cached?.turn === turn && cached.duration === duration && cached.phase === phaseForTurn) {
        seenCanonicalTurnIDs.add(turn.id);
        items.push(cached.item);
        continue;
      }

      const outputMessages = turn.messages.filter(isTurnOutputMessage);
      const failed = turn.status === "failed" && Boolean(turn.error);
      const item: TranscriptTurnVM = {
        assistant:
          outputMessages.length > 0 || failed
            ? {
                duration,
                error: failed ? turn.error : undefined,
                kind: "canonical",
                messages: outputMessages,
                model: modelFromTurn(turn),
              }
            : phaseForTurn
              ? {
                  kind: "phase",
                  phase: phaseForTurn,
                }
            : undefined,
        key: `turn:${turn.id}`,
        kind: phaseForTurn && outputMessages.length === 0 ? "phase" : "canonical",
        turnID: turn.id,
        user,
        fileChanges: turn.fileChanges,
        fileChangeState: turn.fileChangeState,
      };
      seenCanonicalTurnIDs.add(turn.id);
      canonicalTurnCache.set(turn.id, { duration, item, phase: phaseForTurn, turn });
      items.push(item);
    }

    for (const overlay of visibleAssistantOverlays) {
      if (usedLiveTurnIDs.has(overlay.turnID)) {
        continue;
      }
      const pendingClientID =
        overlay.clientMessageID || (displayPhase?.turnID === overlay.turnID ? displayPhase.clientMessageID : undefined);
      const pending = pendingClientID ? pendingByClientID.get(pendingClientID) : undefined;
      if (pendingClientID) {
        usedPendingClientIDs.add(pendingClientID);
      }
      const appliedGuides = (appliedGuidesByTurnID.get(overlay.turnID) || []).filter(
        (guide) => guide.clientMessageID !== pendingClientID,
      );
      const waitingGuides = (waitingGuidesByTurnID.get(overlay.turnID) || []).filter(
        (guide) => guide.clientMessageID !== pendingClientID,
      );
      const assistant = {
        canonicalReady: Boolean(overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID)),
        kind: "live" as const,
        overlay,
        phase: displayPhase?.turnID === overlay.turnID ? displayPhase : undefined,
      };
      const sequence: NonNullable<TranscriptTurnVM["sequence"]> = [];
      appliedGuides.forEach((guide) => {
        usedPendingClientIDs.add(guide.clientMessageID);
        sequence.push({
          key: `guide:pending:${guide.clientMessageID}`,
          kind: "guide",
          user: userFromPending(guide, { pending: false }),
        });
      });
      if (appliedGuides.length > 0 || waitingGuides.length > 0) {
        sequence.push({
          assistant,
          key: `assistant:${overlay.turnID}:live`,
          kind: "assistant",
        });
      }
      waitingGuides.forEach((guide) => {
        usedPendingClientIDs.add(guide.clientMessageID);
        sequence.push({
          key: `guide:waiting:${guide.clientMessageID}`,
          kind: "guide",
          user: userFromPending(guide, { pending: false }),
        });
      });
      items.push({
        assistant: sequence.length > 0 ? undefined : assistant,
        clientMessageID: pendingClientID,
        key: `turn:${overlay.turnID}`,
        kind: "live",
        sequence: sequence.length > 0 ? sequence : undefined,
        turnID: overlay.turnID,
        user: pending ? userFromPending(pending, { pending: false }) : undefined,
      });
    }

    const phaseHasAssistant =
      displayPhase &&
      (displayPhase.turnID
        ? items.some(
            (item) =>
              item.turnID === displayPhase.turnID &&
              (item.assistant || item.sequence?.some((entry) => entry.kind === "assistant")),
          )
        : visibleAssistantOverlays.some((overlay) => overlay.status === "streaming"));
    if (displayPhase && !phaseHasAssistant) {
      const pending = displayPhase.clientMessageID ? pendingByClientID.get(displayPhase.clientMessageID) : undefined;
      if (displayPhase.clientMessageID) {
        usedPendingClientIDs.add(displayPhase.clientMessageID);
      }
      items.push({
        assistant: {
          kind: "phase",
          phase: displayPhase,
        },
        clientMessageID: displayPhase.clientMessageID,
        key: transcriptPhaseKey(displayPhase),
        kind: "phase",
        turnID: displayPhase.turnID,
        user: pending ? userFromPending(pending, { pending: displayPhase.phase === "submitting" && !displayPhase.turnID }) : undefined,
      });
    }

    for (const pending of pendingUsers) {
      if (usedPendingClientIDs.has(pending.clientMessageID)) {
        continue;
      }
      items.push({
        clientMessageID: pending.clientMessageID,
        key: `pending:${pending.clientMessageID}`,
        kind: "pending",
        user: userFromPending(pending),
      });
    }

    if (compactRun) {
      items.push({
        compact: compactRun,
        key: `compact:${sessionID}`,
        kind: "compact",
      });
    }

    for (const turnID of canonicalTurnCache.keys()) {
      if (!seenCanonicalTurnIDs.has(turnID)) {
        canonicalTurnCache.delete(turnID);
      }
    }

    return items.filter((item) => item.user || item.assistant || item.sequence?.length || item.compact);
  }, [canonicalMessageIDs, compactRun, displayPhase, pendingUsers, sessionID, turnDurationByID, turns, visibleAssistantOverlays]);

  const itemKeys = useMemo(() => turnVMs.map((item) => item.key), [turnVMs]);

  return {
    canonicalMessageIDs,
    displayPhase,
    hasItems: turnVMs.length > 0,
    itemKeys,
    turnVMs,
  };
}

function isTurnOutputMessage(message: Message) {
  return message.role !== "user" && message.role !== "system";
}

function modelFromTurn(turn: ConversationTurn): TurnModelVM | undefined {
  return turn.model ? { model: turn.model, provider: turn.provider } : undefined;
}

function userFromMessages(messages: Message[]): UserInputVM | undefined {
  const userMessage = messages.find((message) => message.role === "user");
  if (!userMessage) {
    return undefined;
  }
  return userFromMessage(userMessage);
}

function userFromMessage(userMessage: Message): UserInputVM {
  return {
    attachments: attachmentsFromContentParts(userMessage.parts),
    clientMessageID: userMessage.clientMessageID,
    createdAt: userMessage.createdAt,
    interrupted: userMessage.interrupted,
    localFolders: localFoldersFromContentParts(userMessage.parts),
    messageID: userMessage.id,
    projectReferences: projectReferencesFromContentParts(userMessage.parts),
    parts: userMessage.parts,
    text: textFromContentParts(userMessage.parts),
  };
}

function splitTurnMessages(messages: Message[]) {
  const segments: Array<{ outputs: Message[]; user: Message }> = [];
  for (const message of messages) {
    if (message.role === "user") {
      segments.push({ outputs: [], user: message });
      continue;
    }
    if (message.role === "system" || segments.length === 0) {
      continue;
    }
    segments[segments.length - 1].outputs.push(message);
  }
  return segments;
}

function userFromPending(message: PendingUserMessage, options: { pending?: boolean } = {}): UserInputVM {
  const parts = message.parts || [];
  return {
    attachments: attachmentsFromContentParts(parts),
    clientMessageID: message.clientMessageID,
    createdAt: message.createdAt,
    localFolders: localFoldersFromContentParts(parts),
    projectReferences: projectReferencesFromContentParts(parts),
    pending: options.pending ?? true,
    parts,
    status: options.pending === false ? undefined : message.status,
    text: message.text,
  };
}
