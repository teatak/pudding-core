import { create } from "zustand";

import type { FilePreviewInput } from "@/state/filePreviewStore";
import { setWorkspaceOpen } from "@/state/workspaceStore";

export type ProjectFileRevealInput = {
  absolutePath?: string;
  column?: number;
  fallback?: FilePreviewInput;
  line?: number;
  relativePath?: string;
  rootPath?: string;
  sessionID: string;
};

export type ProjectFileReveal = ProjectFileRevealInput & {
  serial: number;
};

type ProjectRevealState = {
  pending: Record<string, ProjectFileReveal | undefined>;
  consume: (sessionID: string, serial: number) => void;
  request: (input: ProjectFileRevealInput) => void;
};

let revealSerial = 0;

const useProjectRevealStore = create<ProjectRevealState>((set) => ({
  pending: {},
  consume: (sessionID, serial) =>
    set((state) => {
      if (state.pending[sessionID]?.serial !== serial) {
        return state;
      }
      const { [sessionID]: _consumed, ...pending } = state.pending;
      return { pending };
    }),
  request: (input) =>
    set((state) => {
      revealSerial += 1;
      return {
        pending: {
          ...state.pending,
          [input.sessionID]: { ...input, serial: revealSerial },
        },
      };
    }),
}));

export function requestProjectFileReveal(input: ProjectFileRevealInput) {
  useProjectRevealStore.getState().request(input);
  setWorkspaceOpen(true);
}

export function consumeProjectFileReveal(sessionID: string, serial: number) {
  useProjectRevealStore.getState().consume(sessionID, serial);
}

export function useProjectFileReveal(sessionID?: string) {
  return useProjectRevealStore((state) => (sessionID ? state.pending[sessionID] : undefined));
}

export function useVisibleProjectFileReveal(primarySessionID?: string, secondarySessionID?: string) {
  return useProjectRevealStore((state) => {
    const primary = primarySessionID ? state.pending[primarySessionID] : undefined;
    const secondary = secondarySessionID ? state.pending[secondarySessionID] : undefined;
    if (!primary) return secondary;
    if (!secondary) return primary;
    return primary.serial > secondary.serial ? primary : secondary;
  });
}
