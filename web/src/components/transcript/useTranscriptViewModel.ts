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
    const usedPendingClientIDs = new Set<string>();
    const usedLiveTurnIDs = new Set<string>();
    const seenCanonicalTurnIDs = new Set<string>();
    const items: TranscriptTurnVM[] = [];

    for (const turn of turns) {
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
      const item: TranscriptTurnVM = {
        assistant:
          outputMessages.length > 0
            ? {
                duration,
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
      items.push({
        assistant: {
          canonicalReady: Boolean(overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID)),
          kind: "live",
          overlay,
          phase: displayPhase?.turnID === overlay.turnID ? displayPhase : undefined,
        },
        clientMessageID: pendingClientID,
        key: `turn:${overlay.turnID}`,
        kind: "live",
        turnID: overlay.turnID,
        user: pending ? userFromPending(pending, { pending: false }) : undefined,
      });
    }

    const phaseHasAssistant =
      displayPhase &&
      (displayPhase.turnID
        ? items.some((item) => item.turnID === displayPhase.turnID && item.assistant)
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

    return items.filter((item) => item.user || item.assistant || item.compact);
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
  return {
    attachments: attachmentsFromContentParts(userMessage.parts),
    createdAt: userMessage.createdAt,
    interrupted: userMessage.interrupted,
    localFolders: localFoldersFromContentParts(userMessage.parts),
    text: textFromContentParts(userMessage.parts),
  };
}

function userFromPending(message: PendingUserMessage, options: { pending?: boolean } = {}): UserInputVM {
  return {
    attachments: message.attachments,
    clientMessageID: message.clientMessageID,
    createdAt: message.createdAt,
    localFolders: message.localFolders,
    pending: options.pending ?? true,
    status: options.pending === false ? undefined : message.status,
    text: message.text,
  };
}
