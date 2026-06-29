import { create } from "zustand";

import { newClientID } from "@/lib/id";

type SessionDraft = {
  clientMessageID: string;
  text: string;
};

type SessionDraftState = {
  drafts: Record<string, SessionDraft | undefined>;
  clear: (sessionID: string) => SessionDraft;
  ensure: (sessionID: string) => SessionDraft;
  setText: (sessionID: string, text: string) => void;
};

function newSessionDraft(text = ""): SessionDraft {
  return {
    clientMessageID: newClientID(),
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
