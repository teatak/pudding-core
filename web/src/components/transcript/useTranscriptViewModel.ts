import { useMemo } from "react";

import type { ConversationTurn, Message } from "@/api/client";
import {
  isTurnPhaseActive,
  type AssistantOverlay,
  type PendingUserMessage,
  type TurnPhaseState,
} from "@/state/overlayStore";

import { textFromContentParts, transcriptPhaseKey, type TranscriptTurnVM, type UserInputVM } from "./types";

export function useTranscriptViewModel({
  assistantOverlays,
  pendingUsers,
  sessionID,
  sessionRunning,
  turnDurationByID,
  turnPhase,
  turns,
}: {
  assistantOverlays: AssistantOverlay[];
  pendingUsers: PendingUserMessage[];
  sessionID: string;
  sessionRunning: boolean;
  turnDurationByID: Map<string, string>;
  turnPhase?: TurnPhaseState;
  turns: ConversationTurn[];
}) {
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
    const liveByTurnID = new Map(visibleAssistantOverlays.map((overlay) => [overlay.turnID, overlay]));
    const pendingByClientID = new Map(pendingUsers.map((pending) => [pending.clientMessageID, pending]));
    const usedPendingClientIDs = new Set<string>();
    const usedLiveTurnIDs = new Set<string>();
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
            anchorID: assistantAnchorID(turn.id),
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

      const outputMessages = turn.messages.filter(isTurnOutputMessage);
      const phaseForTurn = displayPhase?.turnID === turn.id ? displayPhase : undefined;
      items.push({
        assistant:
          outputMessages.length > 0
            ? {
                anchorID: assistantAnchorID(turn.id),
                duration: turnDurationByID.get(turn.id),
                kind: "canonical",
                messages: outputMessages,
              }
            : phaseForTurn
              ? {
                  anchorID: assistantAnchorID(turn.id),
                  kind: "phase",
                  phase: phaseForTurn,
                }
            : undefined,
        key: `turn:${turn.id}`,
        kind: phaseForTurn && outputMessages.length === 0 ? "phase" : "canonical",
        turnID: turn.id,
        user,
      });
    }

    for (const overlay of visibleAssistantOverlays) {
      if (usedLiveTurnIDs.has(overlay.turnID)) {
        continue;
      }
      const pendingClientID = displayPhase?.turnID === overlay.turnID ? displayPhase.clientMessageID : undefined;
      const pending = pendingClientID ? pendingByClientID.get(pendingClientID) : undefined;
      if (pendingClientID) {
        usedPendingClientIDs.add(pendingClientID);
      }
      items.push({
        assistant: {
          anchorID: assistantAnchorID(overlay.turnID),
          canonicalReady: Boolean(overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID)),
          kind: "live",
          overlay,
          phase: displayPhase?.turnID === overlay.turnID ? displayPhase : undefined,
        },
        clientMessageID: pendingClientID,
        key: `turn:${overlay.turnID}`,
        kind: "live",
        turnID: overlay.turnID,
        user: pending ? userFromPending(pending) : undefined,
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
          anchorID: displayPhase.turnID ? assistantAnchorID(displayPhase.turnID) : transcriptPhaseKey(displayPhase),
          kind: "phase",
          phase: displayPhase,
        },
        clientMessageID: displayPhase.clientMessageID,
        key: transcriptPhaseKey(displayPhase),
        kind: "phase",
        turnID: displayPhase.turnID,
        user: pending ? userFromPending(pending) : undefined,
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

    return items.filter((item) => item.user || item.assistant);
  }, [canonicalMessageIDs, displayPhase, pendingUsers, turnDurationByID, turns, visibleAssistantOverlays]);

  const itemKeys = useMemo(() => turnVMs.map((item) => item.key), [turnVMs]);

  return {
    canonicalMessageIDs,
    displayPhase,
    hasItems: turnVMs.length > 0,
    itemKeys,
    turnVMs,
  };
}

function assistantAnchorID(turnID: string) {
  return `assistant:${turnID}`;
}

function isTurnOutputMessage(message: Message) {
  return message.role !== "user";
}

function userFromMessages(messages: Message[]): UserInputVM | undefined {
  const userMessage = messages.find((message) => message.role === "user");
  if (!userMessage) {
    return undefined;
  }
  return {
    createdAt: userMessage.createdAt,
    interrupted: userMessage.interrupted,
    text: textFromContentParts(userMessage.parts),
  };
}

function userFromPending(message: PendingUserMessage): UserInputVM {
  return {
    clientMessageID: message.clientMessageID,
    createdAt: message.createdAt,
    pending: true,
    status: message.status,
    text: message.text,
  };
}
