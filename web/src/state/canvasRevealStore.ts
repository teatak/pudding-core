import { create } from "zustand";

import { recordWorkspaceActivity } from "@/state/workspaceActivityStore";
import { setWorkspaceOpen } from "@/state/workspaceStore";

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

export function requestCanvasReveal(
  sessionID: string,
  itemID: string,
  activity?: { resourceKind: string; title?: string },
) {
  useCanvasRevealStore.getState().request(sessionID, itemID);
  if (activity) {
    recordWorkspaceActivity({
      kind: "canvas",
      resourceID: itemID,
      resourceKind: activity.resourceKind,
      sessionID,
      title: activity.title,
    });
  }
}

export function openCanvasReveal(sessionID: string, itemID: string) {
  useCanvasRevealStore.getState().request(sessionID, itemID);
  setWorkspaceOpen(true);
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
