import { create } from "zustand";

import type { Attachment, ProjectReference, QueuedInput } from "@/api/client";
import { newClientID } from "@/lib/id";
import type { LocalFolderPath } from "@/lib/localFolders";
import type { DraftPartOrderItem } from "@/lib/submitParts";

export type SessionDraftAttachment = {
  id: string;
  name: string;
  previewURL?: string;
  size: number;
  status: "uploading" | "uploaded" | "error";
  attachment?: Attachment;
};

type SessionDraft = {
  attachments: SessionDraftAttachment[];
  clientMessageID: string;
  localFolders: LocalFolderPath[];
  partOrder: DraftPartOrderItem[];
  projectReferences: ProjectReference[];
  text: string;
};

type DraftListUpdate<T> = T[] | ((current: T[]) => T[]);

type SessionDraftState = {
  queueEdits: Record<string, string | undefined>;
  beginQueueEdit: (sessionID: string, input: QueuedInput) => void;
  endQueueEdit: (sessionID: string) => void;
  drafts: Record<string, SessionDraft | undefined>;
  clear: (sessionID: string) => SessionDraft;
  ensure: (sessionID: string) => SessionDraft;
  setAttachments: (sessionID: string, update: DraftListUpdate<SessionDraftAttachment>) => void;
  setLocalFolders: (sessionID: string, update: DraftListUpdate<LocalFolderPath>) => void;
  setPartOrder: (sessionID: string, update: DraftListUpdate<DraftPartOrderItem>) => void;
  setProjectReferences: (sessionID: string, update: DraftListUpdate<ProjectReference>) => void;
  setText: (sessionID: string, text: string) => void;
};

function newSessionDraft(text = ""): SessionDraft {
  return {
    attachments: [],
    clientMessageID: newClientID(),
    localFolders: [],
    partOrder: [],
    projectReferences: [],
    text,
  };
}

export const useSessionDraftStore = create<SessionDraftState>((set, get) => ({
  queueEdits: {},
  beginQueueEdit: (sessionID, input) => {
    const key = queuedDraftKey(sessionID, input.clientMessageID);
    const draft = newSessionDraft(input.text);
    draft.clientMessageID = input.clientMessageID;
    for (const part of input.parts || []) {
      if (part.type === "attachment") {
        draft.attachments.push({ id: part.id, name: part.name, size: part.size, status: "uploaded", attachment: part });
        draft.partOrder.push({ type: "attachment", id: part.id });
      } else if (part.type === "local_folder") {
        draft.localFolders.push(part);
        draft.partOrder.push({ type: "local_folder", id: part.id });
      } else if (part.type === "project_reference") {
        draft.projectReferences.push(part);
        draft.partOrder.push({ type: "project_reference", id: part.id });
      }
    }
    set((state) => ({ drafts: { ...state.drafts, [key]: draft }, queueEdits: { ...state.queueEdits, [sessionID]: input.clientMessageID } }));
  },
  endQueueEdit: (sessionID) => set((state) => {
    const id = state.queueEdits[sessionID];
    const drafts = { ...state.drafts };
    if (id) {
      const key = queuedDraftKey(sessionID, id);
      for (const item of drafts[key]?.attachments || []) if (item.previewURL) URL.revokeObjectURL(item.previewURL);
      delete drafts[key];
    }
    const queueEdits = { ...state.queueEdits };
    delete queueEdits[sessionID];
    return { drafts, queueEdits };
  }),
  drafts: {},
  clear: (sessionID) => {
    const draft = newSessionDraft();
    set((state) => ({
      drafts: {
        ...state.drafts,
        [sessionID]: draft,
      },
    }));
    return draft;
  },
  ensure: (sessionID) => {
    const existing = get().drafts[sessionID];
    if (existing) {
      return existing;
    }
    const draft = newSessionDraft();
    set((state) => ({
      drafts: {
        ...state.drafts,
        [sessionID]: draft,
      },
    }));
    return draft;
  },
  setAttachments: (sessionID, update) => {
    set((state) => {
      const current = state.drafts[sessionID] ?? newSessionDraft();
      return {
        drafts: {
          ...state.drafts,
          [sessionID]: {
            ...current,
            attachments: applyDraftListUpdate(current.attachments, update),
          },
        },
      };
    });
  },
  setLocalFolders: (sessionID, update) => {
    set((state) => {
      const current = state.drafts[sessionID] ?? newSessionDraft();
      return {
        drafts: {
          ...state.drafts,
          [sessionID]: {
            ...current,
            localFolders: applyDraftListUpdate(current.localFolders, update),
          },
        },
      };
    });
  },
  setPartOrder: (sessionID, update) => {
    set((state) => {
      const current = state.drafts[sessionID] ?? newSessionDraft();
      return {
        drafts: {
          ...state.drafts,
          [sessionID]: {
            ...current,
            partOrder: applyDraftListUpdate(current.partOrder, update),
          },
        },
      };
    });
  },
  setProjectReferences: (sessionID, update) => {
    set((state) => {
      const current = state.drafts[sessionID] ?? newSessionDraft();
      return {
        drafts: {
          ...state.drafts,
          [sessionID]: {
            ...current,
            projectReferences: applyDraftListUpdate(current.projectReferences ?? [], update),
          },
        },
      };
    });
  },
  setText: (sessionID, text) => {
    set((state) => {
      const current = state.drafts[sessionID] ?? newSessionDraft();
      return {
        drafts: {
          ...state.drafts,
          [sessionID]: {
            ...current,
            text,
          },
        },
      };
    });
  },
}));

// Local draft key only; it must never be passed as a business sessionID.
export function queuedDraftKey(sessionID: string, clientMessageID: string) {
  return JSON.stringify([sessionID, clientMessageID]);
}

function applyDraftListUpdate<T>(current: T[], update: DraftListUpdate<T>) {
  return typeof update === "function" ? update(current) : update;
}

export function addProjectReferenceToSessionDraft(
  sessionID: string,
  input: Omit<ProjectReference, "id">,
) {
  const state = useSessionDraftStore.getState();
  const draft = state.ensure(sessionID);
  if (
    (draft.projectReferences ?? []).some(
      (reference) =>
        reference.rootID === input.rootID &&
        reference.path === input.path &&
        reference.kind === input.kind &&
        reference.startLine === input.startLine &&
        reference.startColumn === input.startColumn &&
        reference.endLine === input.endLine &&
        reference.endColumn === input.endColumn,
    )
  ) {
    return false;
  }
  const reference: ProjectReference = { ...input, id: newClientID() };
  state.setProjectReferences(sessionID, (current) => [...current, reference]);
  state.setPartOrder(sessionID, (current) => [...current, { type: "project_reference", id: reference.id }]);
  return true;
}
