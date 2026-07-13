import { create } from "zustand";

import { setCanvasOpen } from "@/state/canvasStore";

type BrowserRevealState = {
  epochs: Record<string, number | undefined>;
  consumeReveal: (sessionID: string, epoch: number) => void;
  requestReveal: (sessionID: string) => void;
};

export const useBrowserRevealStore = create<BrowserRevealState>((set) => ({
  epochs: {},
  consumeReveal: (sessionID, epoch) =>
    set((state) => {
      if ((state.epochs[sessionID] || 0) !== epoch) {
        return state;
      }
      const { [sessionID]: _consumed, ...epochs } = state.epochs;
      return { epochs };
    }),
  requestReveal: (sessionID) =>
    set((state) => ({
      epochs: {
        ...state.epochs,
        [sessionID]: (state.epochs[sessionID] || 0) + 1,
      },
    })),
}));

export function requestBrowserReveal(sessionID: string) {
  useBrowserRevealStore.getState().requestReveal(sessionID);
  setCanvasOpen(true);
}

export function consumeBrowserReveal(sessionID: string, epoch: number) {
  useBrowserRevealStore.getState().consumeReveal(sessionID, epoch);
}

export function useBrowserRevealEpoch(sessionID: string | undefined) {
  return useBrowserRevealStore((state) => (sessionID ? state.epochs[sessionID] || 0 : 0));
}
