import type { ContentPart, Message } from "@/api/client";
import type { AssistantOverlay, TurnPhaseState } from "@/state/overlayStore";

export type UserInputVM = {
  clientMessageID?: string;
  createdAt?: string;
  interrupted?: boolean;
  pending?: boolean;
  status?: "submitting" | "queued" | "editing";
  text: string;
};

export type AssistantOutputVM =
  | {
      duration?: string;
      kind: "canonical";
      messages: Message[];
      model?: TurnModelVM;
    }
  | {
      canonicalReady: boolean;
      kind: "live";
      overlay: AssistantOverlay;
      phase?: TurnPhaseState;
    }
  | {
      kind: "phase";
      phase: TurnPhaseState;
    };

export type TranscriptTurnVM = {
  assistant?: AssistantOutputVM;
  clientMessageID?: string;
  key: string;
  kind: "canonical" | "live" | "pending" | "phase";
  turnID?: string;
  user?: UserInputVM;
};

export type TurnModelVM = {
  model: string;
  provider?: string;
};

export type TurnDisclosureState = {
  isOpen: (key: string) => boolean;
  setOpen: (key: string, open: boolean) => void;
};

export type TurnPartVM =
  | { key?: string; type: "text"; text: string }
  | { active?: boolean; key?: string; text: string; type: "thought" }
  | {
      active?: boolean;
      approvalID: string;
      approvalKind: string;
      key?: string;
      payload?: unknown;
      reason?: string;
      risk?: string;
      sessionID: string;
      status?: "approved" | "denied" | "cancelled" | "expired";
      title?: string;
      type: "approval";
    }
  | {
      active?: boolean;
      args?: unknown;
      argsText?: string;
      dotPhase?: TurnPhaseState["phase"];
      id?: string;
      key?: string;
      name?: string;
      phase?: "streaming_args" | "running" | "ok" | "error";
      phaseUpdatedAt?: string;
      resultContent?: string;
      resultName?: string;
      resultOk?: boolean;
      summary?: string;
      summaryCount?: number;
      summaryKind?: string;
      type: "tool_use";
    }
  | {
      content?: string;
      id?: string;
      key?: string;
      name?: string;
      ok?: boolean;
      summaryCount?: number;
      summaryKind?: string;
      type: "tool_result";
    };

export function textFromContentParts(parts: ContentPart[]) {
  return parts
    .filter((part) => part.type === "text")
    .map((part) => part.text)
    .join("");
}

export function transcriptPhaseKey(phase: TurnPhaseState) {
  if (phase.turnID) {
    return `assistant:${phase.turnID}`;
  }
  if (phase.clientMessageID) {
    return `assistant:pending:${phase.clientMessageID}`;
  }
  return `assistant:phase:${phase.sessionID}`;
}
