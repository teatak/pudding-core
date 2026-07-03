import { create } from "zustand";

import type { Attachment, LocalFolder, Message } from "@/api/client";
import type { SessionEvent } from "@/contracts/events";

export type PendingUserMessage = {
  clientMessageID: string;
  sessionID: string;
  status?: "submitting" | "queued" | "editing";
  text: string;
  attachments?: Attachment[];
  localFolders?: LocalFolder[];
  createdAt: string;
};

export type CompactRun = {
  sessionID: string;
  startedAt: string;
};

export type AssistantOverlay = {
  turnID: string;
  sessionID: string;
  clientMessageID?: string;
  text: string;
  parts: AssistantOverlayPart[];
  status: "streaming" | "completed" | "failed" | "cancelled";
  assistantMessageID?: string;
  interrupted?: boolean;
  error?: string;
  revealed?: boolean;
};

export type AssistantOverlayPart =
  | { type: "text"; text: string }
  | { type: "thought"; text: string }
  | {
      type: "approval";
      approvalID: string;
      approvalKind: string;
      callID?: string;
      payload?: unknown;
      reason?: string;
      risk?: string;
      sessionID: string;
      status?: "approved" | "denied" | "cancelled" | "expired";
      title?: string;
    }
  | {
      type: "tool";
      callID: string;
      name?: string;
      argsText: string;
      phase?: "streaming_args" | "running" | "ok" | "error";
      summary?: string;
      summaryKind?: string;
      summaryCount?: number;
    };

export type TurnPhase =
  | "submitting"
  | "awaiting_model"
  | "streaming_text"
  | "thinking"
  | "streaming_tool_args"
  | "executing_tool"
  | "awaiting_approval"
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
  "awaiting_approval",
  "awaiting_followup",
]);

type OverlayState = {
  pendingUsers: Record<string, PendingUserMessage[]>;
  assistants: Record<string, AssistantOverlay>;
  compactRuns: Record<string, CompactRun | undefined>;
  runningTurns: Record<string, string | undefined>;
  turnPhases: Record<string, TurnPhaseState | undefined>;
  lastEventSeqs: Record<string, number | undefined>;
  addPendingUser: (message: PendingUserMessage) => void;
  startCompactRun: (sessionID: string) => void;
  finishCompactRun: (sessionID: string) => void;
  startSubmittingTurn: (sessionID: string, clientMessageID: string) => void;
  acceptSubmittingTurn: (sessionID: string, clientMessageID: string, turnID: string) => void;
  clearSubmittingTurn: (sessionID: string, clientMessageID: string) => void;
  removePendingUser: (sessionID: string, clientMessageID: string) => void;
  applyEvent: (event: SessionEvent) => void;
  markAssistantRevealed: (turnID: string) => void;
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

function phaseForTurn(
  current: TurnPhaseState | undefined,
  input: Omit<TurnPhaseState, "updatedAt">,
): TurnPhaseState {
  if (
    current?.sessionID === input.sessionID &&
    current.phase === input.phase &&
    current.turnID === input.turnID &&
    current.clientMessageID === input.clientMessageID &&
    current.error === input.error
  ) {
    return current;
  }
  return makePhase(input);
}

function emptyAssistantOverlay(
  turnID: string,
  sessionID: string,
  status: AssistantOverlay["status"] = "streaming",
): AssistantOverlay {
  return {
    turnID,
    sessionID,
    text: "",
    parts: [],
    status,
    revealed: false,
  };
}

function overlayWithDefaults(overlay: AssistantOverlay | undefined, turnID: string, sessionID: string): AssistantOverlay {
  if (!overlay) {
    return emptyAssistantOverlay(turnID, sessionID);
  }
  return { ...overlay, revealed: overlay.revealed || false };
}

function upsertPendingUser(
  pendingUsers: Record<string, PendingUserMessage[]>,
  message: PendingUserMessage,
): Record<string, PendingUserMessage[]> {
  return {
    ...pendingUsers,
    [message.sessionID]: [
      ...(pendingUsers[message.sessionID] || []).filter((item) => item.clientMessageID !== message.clientMessageID),
      message,
    ],
  };
}

function appendThoughtPart(parts: AssistantOverlayPart[], delta: string): AssistantOverlayPart[] {
  if (!delta) {
    return parts;
  }
  const last = parts.length - 1;
  if (last >= 0 && parts[last].type === "thought") {
    return parts.map((part, index) =>
      index === last && part.type === "thought" ? { ...part, text: part.text + delta } : part,
    );
  }
  return [...parts, { type: "thought", text: delta }];
}

function appendTextPart(parts: AssistantOverlayPart[], delta: string): AssistantOverlayPart[] {
  if (!delta) {
    return parts;
  }
  const last = parts.length - 1;
  if (last >= 0 && parts[last].type === "text") {
    return parts.map((part, index) =>
      index === last && part.type === "text" ? { ...part, text: part.text + delta } : part,
    );
  }
  return [...parts, { type: "text", text: delta }];
}

function upsertToolPart(
  parts: AssistantOverlayPart[],
  event: Extract<SessionEvent, { kind: "turn.tool" }>,
  callID: string,
) {
  const index = parts.findIndex((part) => part.type === "tool" && part.callID === callID);
  const current = index >= 0 && parts[index].type === "tool" ? parts[index] : { type: "tool" as const, callID, argsText: "" };
  const next: AssistantOverlayPart = {
    ...current,
    callID,
    name: event.name || current.name,
    phase: event.phase || current.phase,
    summary: event.summary || current.summary,
    summaryKind: event.summaryKind || current.summaryKind,
    summaryCount: event.summaryCount ?? current.summaryCount,
    argsText: current.argsText + (event.argsDelta || ""),
  };
  if (index < 0) {
    return [...parts, next];
  }
  return parts.map((part, i) => (i === index ? next : part));
}

function upsertApprovalPart(
  parts: AssistantOverlayPart[],
  event: Extract<SessionEvent, { kind: "approval.requested" }>,
) {
  const index = parts.findIndex((part) => part.type === "approval" && part.approvalID === event.approvalID);
  const next: AssistantOverlayPart = {
    type: "approval",
    approvalID: event.approvalID,
    approvalKind: event.approvalKind,
    callID: event.callID,
    payload: event.payload,
    reason: event.reason,
    risk: event.risk,
    sessionID: event.sessionID,
    title: event.title,
  };
  if (index < 0) {
    return [...parts, next];
  }
  return parts.map((part, i) => (i === index ? next : part));
}

function resolveApprovalPart(
  parts: AssistantOverlayPart[],
  event: Extract<SessionEvent, { kind: "approval.resolved" }>,
) {
  return parts.map((part) =>
    part.type === "approval" && part.approvalID === event.approvalID
      ? { ...part, status: event.status }
      : part,
  );
}

function hasPendingSkillDraftApproval(overlay: AssistantOverlay | undefined) {
  return Boolean(
    overlay?.parts.some((part) => part.type === "approval" && part.approvalKind === "skill_draft" && !part.status),
  );
}

export const useOverlayStore = create<OverlayState>((set) => ({
  pendingUsers: {},
  assistants: {},
  compactRuns: {},
  runningTurns: {},
  turnPhases: {},
  lastEventSeqs: {},
  addPendingUser: (message) =>
    set((state) => ({
      pendingUsers: upsertPendingUser(state.pendingUsers, message),
    })),
  startCompactRun: (sessionID) =>
    set((state) => ({
      compactRuns: {
        ...state.compactRuns,
        [sessionID]: { sessionID, startedAt: new Date().toISOString() },
      },
    })),
  finishCompactRun: (sessionID) =>
    set((state) => {
      if (!state.compactRuns[sessionID]) {
        return state;
      }
      const compactRuns = { ...state.compactRuns };
      delete compactRuns[sessionID];
      return { compactRuns };
    }),
  startSubmittingTurn: (sessionID, clientMessageID) =>
    set((state) => ({
      turnPhases: {
        ...state.turnPhases,
        [sessionID]: makePhase({ clientMessageID, phase: "submitting", sessionID }),
      },
    })),
  acceptSubmittingTurn: (sessionID, clientMessageID, turnID) =>
    set((state) => {
      const current = overlayWithDefaults(state.assistants[turnID], turnID, sessionID);
      return {
        assistants: {
          ...state.assistants,
          [turnID]: { ...current, clientMessageID },
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
      if (event.kind === "input.queued" || event.kind === "input.updated") {
        const lastEventSeqs = recordEventSeq(state.lastEventSeqs, event);
        if (event.status === "cancelled" || event.status === "promoted") {
          return {
            lastEventSeqs,
            pendingUsers: {
              ...state.pendingUsers,
              [event.sessionID]: (state.pendingUsers[event.sessionID] || []).filter(
                (item) => item.clientMessageID !== event.clientMessageID,
              ),
            },
          };
        }
        return {
          lastEventSeqs,
          pendingUsers: upsertPendingUser(state.pendingUsers, {
            clientMessageID: event.clientMessageID,
            createdAt: new Date().toISOString(),
            sessionID: event.sessionID,
            status: event.status,
            text: event.text,
          }),
        };
      }
      if (event.kind === "turn.started") {
        // 新 turn 开始时清掉该 session 已终结的 overlay:
        // 无 canonical 产物的 failed 气泡(reconcile 清不到)在用户重试时让位
        const assistants = Object.fromEntries(
          Object.entries(state.assistants).filter(
            ([, overlay]) =>
              overlay.sessionID !== event.sessionID || overlay.status === "streaming" || hasPendingSkillDraftApproval(overlay),
          ),
        );
        const current = overlayWithDefaults(assistants[event.turnID], event.turnID, event.sessionID);
        return {
          assistants: { ...assistants, [event.turnID]: { ...current, clientMessageID: event.clientMessageID } },
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
        const current = overlayWithDefaults(state.assistants[event.turnID], event.turnID, event.sessionID);
        const phase = state.turnPhases[event.sessionID];
        const part = event.part;
        const parts = part === "thought" ? appendThoughtPart(current.parts, event.delta) : appendTextPart(current.parts, event.delta);
        const nextPhase = part === "thought" ? "thinking" : "streaming_text";
        const nextRunningTurns =
          state.runningTurns[event.sessionID] === event.turnID
            ? state.runningTurns
            : { ...state.runningTurns, [event.sessionID]: event.turnID };
        const nextTurnPhase = phaseForTurn(phase, {
          phase: nextPhase,
          sessionID: event.sessionID,
          turnID: event.turnID,
        });
        const nextTurnPhases =
          nextTurnPhase === phase ? state.turnPhases : { ...state.turnPhases, [event.sessionID]: nextTurnPhase };
        return {
          assistants: {
            ...state.assistants,
            [event.turnID]: {
              ...current,
              text: part === "text" ? current.text + event.delta : current.text,
              parts,
              status: "streaming",
              clientMessageID:
                current.clientMessageID || (phase?.turnID === event.turnID ? phase.clientMessageID : undefined),
              revealed: false,
            },
          },
          runningTurns: nextRunningTurns,
          turnPhases: nextTurnPhases,
        };
      }
      if (event.kind === "turn.tool") {
        const current = overlayWithDefaults(state.assistants[event.turnID], event.turnID, event.sessionID);
        const currentPhase = state.turnPhases[event.sessionID];
        const callID = event.callID;
        let phase: TurnPhaseState["phase"] = "awaiting_followup";
        if (event.phase === "streaming_args") {
          phase = "streaming_tool_args";
        } else if (event.phase === "running") {
          phase = "executing_tool";
        } else if (event.phase === "error") {
          phase = "error";
        }
        const nextRunningTurns =
          state.runningTurns[event.sessionID] === event.turnID
            ? state.runningTurns
            : { ...state.runningTurns, [event.sessionID]: event.turnID };
        const nextTurnPhase = phaseForTurn(currentPhase, {
          phase,
          sessionID: event.sessionID,
          turnID: event.turnID,
        });
        const nextTurnPhases =
          nextTurnPhase === currentPhase ? state.turnPhases : { ...state.turnPhases, [event.sessionID]: nextTurnPhase };
        return {
          assistants: {
            ...state.assistants,
            [event.turnID]: {
              ...current,
              parts: upsertToolPart(current.parts, event, callID),
              status: "streaming",
              clientMessageID:
                current.clientMessageID || (currentPhase?.turnID === event.turnID ? currentPhase.clientMessageID : undefined),
              revealed: false,
            },
          },
          runningTurns: nextRunningTurns,
          turnPhases: nextTurnPhases,
        };
      }
      if (event.kind === "approval.requested") {
        const current = overlayWithDefaults(state.assistants[event.turnID], event.turnID, event.sessionID);
        const currentPhase = state.turnPhases[event.sessionID];
        if (event.approvalKind === "skill_draft") {
          return {
            assistants: {
              ...state.assistants,
              [event.turnID]: {
                ...current,
                parts: upsertApprovalPart(current.parts, event),
                clientMessageID:
                  current.clientMessageID || (currentPhase?.turnID === event.turnID ? currentPhase.clientMessageID : undefined),
                revealed: false,
              },
            },
          };
        }
        return {
          assistants: {
            ...state.assistants,
            [event.turnID]: {
              ...current,
              parts: upsertApprovalPart(current.parts, event),
              status: "streaming",
              clientMessageID:
                current.clientMessageID || (currentPhase?.turnID === event.turnID ? currentPhase.clientMessageID : undefined),
              revealed: false,
            },
          },
          runningTurns: { ...state.runningTurns, [event.sessionID]: event.turnID },
          turnPhases: {
            ...state.turnPhases,
            [event.sessionID]: makePhase({
              phase: "awaiting_approval",
              sessionID: event.sessionID,
              turnID: event.turnID,
            }),
          },
        };
      }
      if (event.kind === "approval.resolved") {
        const current = overlayWithDefaults(state.assistants[event.turnID], event.turnID, event.sessionID);
        if (event.approvalKind === "skill_draft") {
          return {
            assistants: {
              ...state.assistants,
              [event.turnID]: {
                ...current,
                parts: resolveApprovalPart(current.parts, event),
                revealed: false,
              },
            },
          };
        }
        return {
          assistants: {
            ...state.assistants,
            [event.turnID]: {
              ...current,
              parts: resolveApprovalPart(current.parts, event),
              status: "streaming",
              revealed: false,
            },
          },
          turnPhases: {
            ...state.turnPhases,
            [event.sessionID]: makePhase({
              phase: event.status === "approved" ? "awaiting_followup" : "error",
              sessionID: event.sessionID,
              turnID: event.turnID,
            }),
          },
        };
      }
      const status =
        event.kind === "turn.completed" ? "completed" : event.kind === "turn.failed" ? "failed" : "cancelled";
      const current = overlayWithDefaults(state.assistants[event.turnID], event.turnID, event.sessionID);
      return {
        assistants: {
          ...state.assistants,
          [event.turnID]: {
            ...current,
            status,
            assistantMessageID: "assistantMessageID" in event ? event.assistantMessageID : undefined,
            interrupted: "interrupted" in event ? event.interrupted : undefined,
            error: "error" in event ? event.error : undefined,
            revealed: false,
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
  markAssistantRevealed: (turnID) =>
    set((state) => {
      const overlay = state.assistants[turnID];
      if (!overlay || overlay.revealed) {
        return state;
      }
      if (overlay.assistantMessageID) {
        if (hasPendingSkillDraftApproval(overlay)) {
          return {
            assistants: {
              ...state.assistants,
              [turnID]: { ...overlay, revealed: true },
            },
          };
        }
        const assistants = { ...state.assistants };
        delete assistants[turnID];
        return { assistants };
      }
      return {
        assistants: {
          ...state.assistants,
          [turnID]: { ...overlay, revealed: true },
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
          if (hasPendingSkillDraftApproval(overlay)) {
            continue;
          }
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
      const compactRuns = { ...state.compactRuns };
      delete compactRuns[sessionID];
      const lastEventSeqs = { ...state.lastEventSeqs };
      delete lastEventSeqs[sessionID];
      const assistants = Object.fromEntries(
        Object.entries(state.assistants).filter(([, overlay]) => overlay.sessionID !== sessionID),
      );
      return { pendingUsers, runningTurns, turnPhases, compactRuns, lastEventSeqs, assistants };
    }),
}));
