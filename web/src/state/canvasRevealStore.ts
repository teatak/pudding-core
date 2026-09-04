import { create } from "zustand";

import { canvasWorkspaceTabKey, openWorkspaceTab } from "@/state/workspaceStore";

type CanvasReveal = {
  itemID: string;
  serial: number;
  sessionID: string;
};

type CanvasRevealState = {
  latest?: CanvasReveal;
  consume: (serial: number) => void;
  request: (sessionID: string, itemID: string) => void;
};

let revealSerial = 0;

const useCanvasRevealStore = create<CanvasRevealState>((set) => ({
  consume: (serial) =>
    set((state) => (state.latest?.serial === serial ? { latest: undefined } : state)),
  request: (sessionID, itemID) =>
    set(() => {
      revealSerial += 1;
      return { latest: { itemID, serial: revealSerial, sessionID } };
    }),
}));

export function requestCanvasReveal(sessionID: string, itemID: string) {
  useCanvasRevealStore.getState().request(sessionID, itemID);
}

export function openCanvasReveal(sessionID: string, itemID: string) {
  useCanvasRevealStore.getState().request(sessionID, itemID);
  openWorkspaceTab(sessionID, canvasWorkspaceTabKey(itemID));
}

export function consumeCanvasReveal(serial: number) {
  useCanvasRevealStore.getState().consume(serial);
}

export function useVisibleCanvasReveal(primarySessionID?: string, secondarySessionID?: string) {
  return useCanvasRevealStore((state) => {
    const reveal = state.latest;
    return reveal && (reveal.sessionID === primarySessionID || reveal.sessionID === secondarySessionID)
      ? reveal
      : undefined;
  });
}
