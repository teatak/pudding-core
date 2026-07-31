import { create } from "zustand";

export type TranscriptTurnReveal = {
  messageRole?: "assistant" | "user";
  serial: number;
  sessionID: string;
  turnID: string;
};

type TranscriptRevealState = {
  pending: Record<string, TranscriptTurnReveal | undefined>;
  consume: (sessionID: string, serial: number) => void;
  request: (sessionID: string, turnID: string, messageRole?: "assistant" | "user") => void;
};

let revealSerial = 0;

const useTranscriptRevealStore = create<TranscriptRevealState>((set) => ({
  pending: {},
  consume: (sessionID, serial) =>
    set((state) => {
      if (state.pending[sessionID]?.serial !== serial) {
        return state;
      }
      const { [sessionID]: _consumed, ...pending } = state.pending;
      return { pending };
    }),
  request: (sessionID, turnID, messageRole) =>
    set((state) => {
      revealSerial += 1;
      return {
        pending: {
          ...state.pending,
          [sessionID]: { messageRole, serial: revealSerial, sessionID, turnID },
        },
      };
    }),
}));

export function requestTranscriptTurnReveal(
  sessionID: string,
  turnID: string,
  messageRole?: "assistant" | "user",
) {
  useTranscriptRevealStore.getState().request(sessionID, turnID, messageRole);
}

export function consumeTranscriptTurnReveal(sessionID: string, serial: number) {
  useTranscriptRevealStore.getState().consume(sessionID, serial);
}

export function useTranscriptTurnReveal(sessionID: string) {
  return useTranscriptRevealStore((state) => state.pending[sessionID]);
}
