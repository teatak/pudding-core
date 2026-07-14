import { create } from "zustand";

import { setWorkspaceOpen } from "@/state/workspaceStore";

type BrowserRevealState = {
  epochs: Record<string, number | undefined>;
  latest?: BrowserReveal;
  consumeReveal: (sessionID: string, epoch: number) => void;
  requestReveal: (sessionID: string) => void;
};

type BrowserReveal = {
  epoch: number;
  serial: number;
  sessionID: string;
};

let revealSerial = 0;

export const useBrowserRevealStore = create<BrowserRevealState>((set) => ({
  epochs: {},
  consumeReveal: (sessionID, epoch) =>
    set((state) => {
      if ((state.epochs[sessionID] || 0) !== epoch) {
        return state;
      }
      const { [sessionID]: _consumed, ...epochs } = state.epochs;
      return {
        epochs,
        latest: state.latest?.sessionID === sessionID && state.latest.epoch === epoch
          ? undefined
          : state.latest,
      };
    }),
  requestReveal: (sessionID) =>
    set((state) => {
      const epoch = (state.epochs[sessionID] || 0) + 1;
      revealSerial += 1;
      return {
        epochs: { ...state.epochs, [sessionID]: epoch },
        latest: { epoch, serial: revealSerial, sessionID },
      };
    }),
}));

export function requestBrowserReveal(sessionID: string) {
  useBrowserRevealStore.getState().requestReveal(sessionID);
  setWorkspaceOpen(true);
}

export function consumeBrowserReveal(sessionID: string, epoch: number) {
  useBrowserRevealStore.getState().consumeReveal(sessionID, epoch);
}

export function useBrowserRevealEpoch(sessionID: string | undefined) {
  return useBrowserRevealStore((state) => (sessionID ? state.epochs[sessionID] || 0 : 0));
}

export function useVisibleBrowserReveal(primarySessionID?: string, secondarySessionID?: string) {
  return useBrowserRevealStore((state) => {
    const reveal = state.latest;
    return reveal && (reveal.sessionID === primarySessionID || reveal.sessionID === secondarySessionID)
      ? reveal
      : undefined;
  });
}
