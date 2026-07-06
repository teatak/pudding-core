import { create } from "zustand";

import type { Attachment } from "@/api/client";
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
  setText: (sessionID: string, text: string) => void;
};

function newSessionDraft(text = ""): SessionDraft {
  return {
    attachments: [],
    clientMessageID: newClientID(),
    localFolders: [],
    partOrder: [],
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
