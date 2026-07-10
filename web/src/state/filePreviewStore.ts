import { create } from "zustand";

import { setCanvasOpen } from "@/state/canvasStore";

export type FilePreview = {
  callID?: string;
  content: string;
  lineStart: number;
  lineStep: 1 | -1;
  openedAt: number;
  path: string;
  sessionID: string;
  source: "read" | "slice";
  truncated: boolean;
  turnID?: string;
};

export type FilePreviewInput = Omit<FilePreview, "openedAt">;

type FilePreviewReveal = {
  serial: number;
  sessionID: string;
};

type FilePreviewState = {
  pendingReveal?: FilePreviewReveal;
  previews: Record<string, FilePreview | undefined>;
  clearPreviews: () => void;
  closePreview: (sessionID: string) => void;
  consumeReveal: (serial: number) => void;
  openPreview: (preview: FilePreviewInput) => void;
};

let revealSerial = 0;

const useFilePreviewStore = create<FilePreviewState>((set) => ({
  previews: {},
  clearPreviews: () => set({ pendingReveal: undefined, previews: {} }),
  closePreview: (sessionID) =>
    set((state) => {
      const { [sessionID]: _closed, ...previews } = state.previews;
      return {
        previews,
        pendingReveal: state.pendingReveal?.sessionID === sessionID ? undefined : state.pendingReveal,
      };
    }),
  consumeReveal: (serial) =>
    set((state) => (state.pendingReveal?.serial === serial ? { pendingReveal: undefined } : state)),
  openPreview: (preview) =>
    set((state) => {
      const openedAt = Date.now();
      revealSerial += 1;
      return {
        pendingReveal: { serial: revealSerial, sessionID: preview.sessionID },
        previews: {
          ...state.previews,
          [preview.sessionID]: { ...preview, openedAt },
        },
      };
    }),
}));

export function openFilePreview(preview: FilePreviewInput) {
  useFilePreviewStore.getState().openPreview(preview);
  setCanvasOpen(true);
  window.requestAnimationFrame(() => {
    if (useFilePreviewStore.getState().previews[preview.sessionID]) {
      setCanvasOpen(true);
    }
  });
}

export function closeFilePreview(sessionID: string) {
  useFilePreviewStore.getState().closePreview(sessionID);
}

export function clearFilePreviews() {
  useFilePreviewStore.getState().clearPreviews();
}

export function consumeFilePreviewReveal(serial: number) {
  useFilePreviewStore.getState().consumeReveal(serial);
}

export function useFilePreview(sessionID?: string) {
  return useFilePreviewStore((state) => (sessionID ? state.previews[sessionID] : undefined));
}

export function useFilePreviewReveal(primarySessionID?: string, secondarySessionID?: string) {
  return useFilePreviewStore((state) => {
    const reveal = state.pendingReveal;
    return reveal && (reveal.sessionID === primarySessionID || reveal.sessionID === secondarySessionID)
      ? reveal
      : undefined;
  });
}
