import type { Attachment, ContentPart, LocalFolder, Message } from "@/api/client";
import type { TranscriptDisplaySettings } from "@/lib/appSettings";
import type { AssistantOverlay, CompactRun, TurnPhaseState } from "@/state/overlayStore";

export type { TranscriptDisplaySettings };

export type UserInputVM = {
  clientMessageID?: string;
  createdAt?: string;
  interrupted?: boolean;
  pending?: boolean;
  status?: "submitting" | "queued" | "editing";
  text: string;
  attachments?: Attachment[];
  localFolders?: LocalFolder[];
  parts?: ContentPart[];
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
  compact?: CompactRun;
  key: string;
  kind: "canonical" | "compact" | "live" | "pending" | "phase";
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
  | { attachment: Attachment; key?: string; type: "attachment" }
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

export function attachmentsFromContentParts(parts: ContentPart[]): Attachment[] {
  return parts
    .filter((part) => part.type === "attachment")
    .map((part) => ({
      id: part.id,
      name: part.name,
      attachmentKey: part.attachmentKey,
      url: part.url,
      mime: part.mime,
      size: part.size,
      origin: part.origin,
      sourcePath: part.sourcePath,
      createdAt: part.createdAt,
      audioTranscript: part.audioTranscript,
    }));
}

export function localFoldersFromContentParts(parts: ContentPart[]): LocalFolder[] {
  return parts
    .filter((part) => part.type === "local_folder")
    .map((part) => ({
      id: part.id,
      name: part.name,
      path: part.path,
      origin: part.origin,
    }));
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
