import type { Attachment, ContentPart, LocalFolder, Message, ProjectReference, TurnFileChange } from "@/api/client";
import type { TranscriptDisplaySettings } from "@/lib/appSettings";
import type { AssistantOverlay, CompactRun, TurnPhaseState } from "@/state/overlayStore";
import type { UIContextPart } from "@/state/uiContextStore";

export type { TranscriptDisplaySettings };

export type UserInputVM = {
  clientMessageID?: string;
  createdAt?: string;
  interrupted?: boolean;
  messageID?: string;
  pending?: boolean;
  status?: "submitting" | "queued" | "editing" | "steering" | "steered";
  text: string;
  attachments?: Attachment[];
  localFolders?: LocalFolder[];
  projectReferences?: ProjectReference[];
  parts?: ContentPart[];
};

export type AssistantOutputVM =
  | {
      duration?: string;
      error?: string;
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

export type TranscriptTurnSequenceItem =
  | {
      assistant: AssistantOutputVM;
      key: string;
      kind: "assistant";
    }
  | {
      key: string;
      kind: "guide";
      user: UserInputVM;
    };

export type TranscriptTurnVM = {
  anchorID?: string;
  assistant?: AssistantOutputVM;
  clientMessageID?: string;
  compact?: CompactRun;
  key: string;
  kind: "canonical" | "compact" | "live" | "pending" | "phase";
  turnID?: string;
  user?: UserInputVM;
  fileChanges?: TurnFileChange[];
  sequence?: TranscriptTurnSequenceItem[];
};

export type TurnModelVM = {
  model: string;
  provider?: string;
};

export type TurnDisclosureState = {
  hasState: (key: string) => boolean;
  isOpen: (key: string) => boolean;
  setOpen: (key: string, open: boolean) => void;
};

export type TranscriptSearchTarget = {
  messageID: string;
  occurrenceIndex: number;
  role: "assistant" | "user";
  turnID: string;
};

export type TranscriptSearchState = {
  target?: TranscriptSearchTarget;
  terms: string[];
};

export type TurnPartVM =
  | { key?: string; messageID?: string; type: "text"; text: string }
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
      attachments?: Attachment[];
      args?: unknown;
      argsText?: string;
      dotPhase?: TurnPhaseState["phase"];
      id?: string;
      key?: string;
      liveStderr?: string;
      liveStdout?: string;
      name?: string;
      phase?: "streaming_args" | "running" | "ok" | "error";
      phaseUpdatedAt?: string;
      resultContent?: string;
      resultName?: string;
      resultOk?: boolean;
      summaryCount?: number;
      summaryKind?: string;
      type: "tool_use";
    }
  | {
      attachments?: Attachment[];
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

export function projectReferencesFromContentParts(parts: ContentPart[]): ProjectReference[] {
  return parts
    .filter((part) => part.type === "project_reference")
    .map((part) => ({
      id: part.id,
      name: part.name,
      path: part.path,
      sourcePath: part.sourcePath,
      rootID: part.rootID,
      kind: part.kind,
      startLine: part.startLine,
      startColumn: part.startColumn,
      endLine: part.endLine,
      endColumn: part.endColumn,
    }));
}

export function uiContextFromContentParts(parts: ContentPart[] | undefined): UIContextPart | undefined {
  return parts?.find((part): part is UIContextPart => part.type === "ui_context");
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
