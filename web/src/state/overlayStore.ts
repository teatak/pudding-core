import { create } from "zustand";

import type { Message } from "@/api/client";
import type { SessionEvent } from "@/contracts/events";

export type PendingUserMessage = {
  clientMessageID: string;
  sessionID: string;
  text: string;
  createdAt: string;
};

export type AssistantOverlay = {
  turnID: string;
  sessionID: string;
  text: string;
  status: "streaming" | "completed" | "failed" | "cancelled";
  assistantMessageID?: string;
  interrupted?: boolean;
  error?: string;
};

export type TurnPhase =
  | "submitting"
  | "awaiting_model"
  | "streaming_text"
  | "thinking"
  | "streaming_tool_args"
  | "executing_tool"
  | "awaiting_followup"
  | "error"
  | "cancelled";

export type TurnPhaseState = {
  sessionID: string;
  phase: TurnPhase;
  updatedAt: string;
  turnID?: string;
  clientMessageID?: string;
  error?: string;
};

const activeTurnPhases = new Set<TurnPhase>([
  "submitting",
  "awaiting_model",
  "streaming_text",
  "thinking",
  "streaming_tool_args",
  "executing_tool",
  "awaiting_followup",
]);

type OverlayState = {
  pendingUsers: Record<string, PendingUserMessage[]>;
  assistants: Record<string, AssistantOverlay>;
  runningTurns: Record<string, string | undefined>;
  turnPhases: Record<string, TurnPhaseState | undefined>;
  lastEventSeqs: Record<string, number | undefined>;
  addPendingUser: (message: PendingUserMessage) => void;
  startSubmittingTurn: (sessionID: string, clientMessageID: string) => void;
  acceptSubmittingTurn: (sessionID: string, clientMessageID: string, turnID: string) => void;
  clearSubmittingTurn: (sessionID: string, clientMessageID: string) => void;
  removePendingUser: (sessionID: string, clientMessageID: string) => void;
  applyEvent: (event: SessionEvent) => void;
  reconcileMessages: (sessionID: string, messages: Message[]) => void;
  clearSession: (sessionID: string) => void;
};

function recordEventSeq(lastEventSeqs: Record<string, number | undefined>, event: SessionEvent) {
  if (!("seq" in event)) {
    return lastEventSeqs;
  }
  const previous = lastEventSeqs[event.sessionID] || 0;
  if (event.seq <= previous) {
    return lastEventSeqs;
  }
  return { ...lastEventSeqs, [event.sessionID]: event.seq };
}

export function isTurnPhaseActive(phase: TurnPhaseState | undefined) {
  return Boolean(phase && activeTurnPhases.has(phase.phase));
}

function makePhase(input: Omit<TurnPhaseState, "updatedAt">): TurnPhaseState {
  return { ...input, updatedAt: new Date().toISOString() };
}

export const useOverlayStore = create<OverlayState>((set) => ({
  pendingUsers: {},
  assistants: {},
  runningTurns: {},
  turnPhases: {},
  lastEventSeqs: {},
  addPendingUser: (message) =>
    set((state) => ({
      pendingUsers: {
        ...state.pendingUsers,
        [message.sessionID]: [
          ...(state.pendingUsers[message.sessionID] || []).filter(
            (item) => item.clientMessageID !== message.clientMessageID,
          ),
          message,
        ],
      },
    })),
  startSubmittingTurn: (sessionID, clientMessageID) =>
    set((state) => ({
      turnPhases: {
        ...state.turnPhases,
        [sessionID]: makePhase({ clientMessageID, phase: "submitting", sessionID }),
      },
    })),
  acceptSubmittingTurn: (sessionID, clientMessageID, turnID) =>
    set((state) => {
      const current = state.assistants[turnID] || {
        turnID,
        sessionID,
        text: "",
        status: "streaming" as const,
      };
      return {
        assistants: {
          ...state.assistants,
          [turnID]: current,
        },
        runningTurns: {
          ...state.runningTurns,
          [sessionID]: turnID,
        },
        turnPhases: {
          ...state.turnPhases,
          [sessionID]: makePhase({ clientMessageID, phase: "awaiting_model", sessionID, turnID }),
        },
      };
    }),
  clearSubmittingTurn: (sessionID, clientMessageID) =>
    set((state) => {
      const phase = state.turnPhases[sessionID];
      if (phase?.phase !== "submitting" || phase.clientMessageID !== clientMessageID) {
        return state;
      }
      const turnPhases = { ...state.turnPhases };
      delete turnPhases[sessionID];
      return { turnPhases };
    }),
  removePendingUser: (sessionID, clientMessageID) =>
    set((state) => ({
      pendingUsers: {
        ...state.pendingUsers,
        [sessionID]: (state.pendingUsers[sessionID] || []).filter(
          (item) => item.clientMessageID !== clientMessageID,
        ),
      },
    })),
  applyEvent: (event) =>
    set((state) => {
      if (event.kind === "ping" || event.kind === "session.titled") {
        return state; // titled 只驱动 sessions refetch,不进 overlay
      }
      if (event.kind === "turn.started") {
        // 新 turn 开始时清掉该 session 已终结的 overlay:
        // 无 canonical 产物的 failed 气泡(reconcile 清不到)在用户重试时让位
        const assistants = Object.fromEntries(
          Object.entries(state.assistants).filter(
            ([, overlay]) => overlay.sessionID !== event.sessionID || overlay.status === "streaming",
          ),
        );
        const current = assistants[event.turnID] || {
          turnID: event.turnID,
          sessionID: event.sessionID,
          text: "",
          status: "streaming" as const,
        };
        return {
          assistants: { ...assistants, [event.turnID]: current },
          lastEventSeqs: recordEventSeq(state.lastEventSeqs, event),
          runningTurns: { ...state.runningTurns, [event.sessionID]: event.turnID },
          turnPhases: {
            ...state.turnPhases,
            [event.sessionID]: makePhase({
              clientMessageID: event.clientMessageID,
              phase: "awaiting_model",
              sessionID: event.sessionID,
              turnID: event.turnID,
            }),
          },
        };
      }
      if (event.kind === "turn.delta") {
        const current = state.assistants[event.turnID] || {
          turnID: event.turnID,
          sessionID: event.sessionID,
          text: "",
          status: "streaming",
        };
        return {
          assistants: {
            ...state.assistants,
            [event.turnID]: {
              ...current,
              text: current.text + event.delta,
              status: "streaming",
            },
          },
          runningTurns: { ...state.runningTurns, [event.sessionID]: event.turnID },
          turnPhases: {
            ...state.turnPhases,
            [event.sessionID]: makePhase({
              phase: "streaming_text",
              sessionID: event.sessionID,
              turnID: event.turnID,
            }),
          },
        };
      }
      const status =
        event.kind === "turn.completed" ? "completed" : event.kind === "turn.failed" ? "failed" : "cancelled";
      const current = state.assistants[event.turnID] || {
        turnID: event.turnID,
        sessionID: event.sessionID,
        text: "",
        status,
      };
      return {
        assistants: {
          ...state.assistants,
          [event.turnID]: {
            ...current,
            status,
            assistantMessageID: "assistantMessageID" in event ? event.assistantMessageID : undefined,
            interrupted: "interrupted" in event ? event.interrupted : undefined,
            error: "error" in event ? event.error : undefined,
          },
        },
        lastEventSeqs: recordEventSeq(state.lastEventSeqs, event),
        runningTurns: { ...state.runningTurns, [event.sessionID]: undefined },
        turnPhases:
          event.kind === "turn.completed"
            ? { ...state.turnPhases, [event.sessionID]: undefined }
            : {
                ...state.turnPhases,
                [event.sessionID]: makePhase({
                  error: "error" in event ? event.error : undefined,
                  phase: event.kind === "turn.failed" ? "error" : "cancelled",
                  sessionID: event.sessionID,
                  turnID: event.turnID,
                }),
              },
      };
    }),
  reconcileMessages: (sessionID, messages) =>
    set((state) => {
      const canonicalClientIDs = new Set(messages.map((message) => message.clientMessageID).filter(Boolean));
      const canonicalMessageIDs = new Set(messages.map((message) => message.id));
      const currentPending = state.pendingUsers[sessionID] || [];
      const nextPending = currentPending.filter((message) => !canonicalClientIDs.has(message.clientMessageID));
      const pendingUsers = {
        ...state.pendingUsers,
        [sessionID]: nextPending,
      };
      const assistants = { ...state.assistants };
      let assistantsChanged = false;
      for (const overlay of Object.values(assistants)) {
        if (overlay.sessionID !== sessionID || overlay.status === "streaming") {
          continue;
        }
        if (overlay.assistantMessageID && canonicalMessageIDs.has(overlay.assistantMessageID)) {
          delete assistants[overlay.turnID];
          assistantsChanged = true;
        }
      }
      if (nextPending.length === currentPending.length && !assistantsChanged) {
        return state;
      }
      return { pendingUsers, assistants };
    }),
  clearSession: (sessionID) =>
    set((state) => {
      const pendingUsers = { ...state.pendingUsers };
      delete pendingUsers[sessionID];
      const runningTurns = { ...state.runningTurns };
      delete runningTurns[sessionID];
      const turnPhases = { ...state.turnPhases };
      delete turnPhases[sessionID];
      const lastEventSeqs = { ...state.lastEventSeqs };
      delete lastEventSeqs[sessionID];
      const assistants = Object.fromEntries(
        Object.entries(state.assistants).filter(([, overlay]) => overlay.sessionID !== sessionID),
      );
      return { pendingUsers, runningTurns, turnPhases, lastEventSeqs, assistants };
    }),
}));
