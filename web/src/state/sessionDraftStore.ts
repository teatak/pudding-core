import { create } from "zustand";

import type { Attachment, ProjectReference } from "@/api/client";
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
        reference.rootID === input.rootID && reference.path === input.path && reference.kind === input.kind,
    )
  ) {
    return false;
  }
  const reference: ProjectReference = { ...input, id: newClientID() };
  state.setProjectReferences(sessionID, (current) => [...current, reference]);
  state.setPartOrder(sessionID, (current) => [...current, { type: "project_reference", id: reference.id }]);
  return true;
}
