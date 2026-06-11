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

type OverlayState = {
  pendingUsers: Record<string, PendingUserMessage[]>;
  assistants: Record<string, AssistantOverlay>;
  runningTurns: Record<string, string | undefined>;
  addPendingUser: (message: PendingUserMessage) => void;
  removePendingUser: (sessionID: string, clientMessageID: string) => void;
  applyEvent: (event: SessionEvent) => void;
  reconcileMessages: (sessionID: string, messages: Message[]) => void;
  clearSession: (sessionID: string) => void;
};

export const useOverlayStore = create<OverlayState>((set) => ({
  pendingUsers: {},
  assistants: {},
  runningTurns: {},
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
      if (event.kind === "ping") {
        return state;
      }
      if (event.kind === "turn.started") {
        // 新 turn 开始时清掉该 session 已终结的 overlay:
        // 无 canonical 产物的 failed 气泡(reconcile 清不到)在用户重试时让位
        const assistants = Object.fromEntries(
          Object.entries(state.assistants).filter(
            ([, overlay]) => overlay.sessionID !== event.sessionID || overlay.status === "streaming",
          ),
        );
        return {
          assistants,
          runningTurns: { ...state.runningTurns, [event.sessionID]: event.turnID },
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
        runningTurns: { ...state.runningTurns, [event.sessionID]: undefined },
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
      const assistants = Object.fromEntries(
        Object.entries(state.assistants).filter(([, overlay]) => overlay.sessionID !== sessionID),
      );
      return { pendingUsers, runningTurns, assistants };
    }),
}));
