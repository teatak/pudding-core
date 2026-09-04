import { create } from "zustand";

import { browserWorkspaceTabKey, openWorkspaceTab, setWorkspaceOpen } from "@/state/workspaceStore";

type BrowserRevealState = {
  epochs: Record<string, number | undefined>;
  latest?: BrowserReveal;
  consumeReveal: (sessionID: string, epoch: number) => void;
  requestReveal: (sessionID: string, tabID?: string) => void;
};

type BrowserReveal = {
  epoch: number;
  serial: number;
  sessionID: string;
  tabID?: string;
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
  requestReveal: (sessionID, tabID) =>
    set((state) => {
      const epoch = (state.epochs[sessionID] || 0) + 1;
      revealSerial += 1;
      return {
        epochs: { ...state.epochs, [sessionID]: epoch },
        latest: { epoch, serial: revealSerial, sessionID, tabID },
      };
    }),
}));

export function requestBrowserReveal(sessionID: string, tabID?: string) {
  useBrowserRevealStore.getState().requestReveal(sessionID, tabID);
}

export function openBrowserReveal(sessionID: string, tabID?: string) {
  useBrowserRevealStore.getState().requestReveal(sessionID, tabID);
  if (tabID) {
    openWorkspaceTab(sessionID, browserWorkspaceTabKey(tabID));
  } else {
    setWorkspaceOpen(sessionID, true);
  }
}

export function consumeBrowserReveal(sessionID: string, epoch: number) {
  useBrowserRevealStore.getState().consumeReveal(sessionID, epoch);
}

export function useBrowserReveal(sessionID: string | undefined) {
  return useBrowserRevealStore((state) => {
    if (!sessionID) {
      return undefined;
    }
    const epoch = state.epochs[sessionID] || 0;
    if (!epoch) {
      return undefined;
    }
    return state.latest?.sessionID === sessionID && state.latest.epoch === epoch
      ? state.latest
      : { epoch, serial: 0, sessionID };
  });
}

export function useVisibleBrowserReveal(primarySessionID?: string, secondarySessionID?: string) {
  return useBrowserRevealStore((state) => {
    const reveal = state.latest;
    return reveal && (reveal.sessionID === primarySessionID || reveal.sessionID === secondarySessionID)
      ? reveal
      : undefined;
  });
}
