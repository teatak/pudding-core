import { create } from "zustand";

import { setCanvasOpen } from "@/state/canvasStore";

export type FilePreview = {
  callID?: string;
  content: string;
  id: string;
  lineStart: number;
  lineStep: 1 | -1;
  openedAt: number;
  path: string;
  sessionID: string;
  source: "diagnostic" | "read" | "search" | "slice" | "write";
  truncated: boolean;
  turnID?: string;
};

export type FilePreviewInput = Omit<FilePreview, "id" | "openedAt">;

type FilePreviewReveal = {
  previewID: string;
  serial: number;
  sessionID: string;
};

type FilePreviewState = {
  pendingReveal?: FilePreviewReveal;
  previews: Record<string, FilePreview[] | undefined>;
  clearPreviews: () => void;
  closePreview: (sessionID: string, previewID: string) => void;
  consumeReveal: (serial: number) => void;
  openPreview: (preview: FilePreviewInput) => string;
};

let revealSerial = 0;
let previewSerial = 0;
const EMPTY_PREVIEWS: FilePreview[] = [];

const useFilePreviewStore = create<FilePreviewState>((set) => ({
  previews: {},
  clearPreviews: () => set({ pendingReveal: undefined, previews: {} }),
  closePreview: (sessionID, previewID) =>
    set((state) => {
      const remaining = (state.previews[sessionID] || []).filter((preview) => preview.id !== previewID);
      if (remaining.length > 0) {
        return {
          previews: { ...state.previews, [sessionID]: remaining },
          pendingReveal: state.pendingReveal?.previewID === previewID ? undefined : state.pendingReveal,
        };
      }
      const { [sessionID]: _closed, ...previews } = state.previews;
      return {
        previews,
        pendingReveal: state.pendingReveal?.previewID === previewID ? undefined : state.pendingReveal,
      };
    }),
  consumeReveal: (serial) =>
    set((state) => (state.pendingReveal?.serial === serial ? { pendingReveal: undefined } : state)),
  openPreview: (preview) => {
    let openedPreviewID = "";
    set((state) => {
      const openedAt = Date.now();
      const sessionPreviews = state.previews[preview.sessionID] || [];
      const existing = sessionPreviews.find((entry) => entry.path === preview.path);
      if (!existing) {
        previewSerial += 1;
      }
      openedPreviewID = existing?.id || `file-preview-${previewSerial}`;
      revealSerial += 1;
      const openedPreview: FilePreview = { ...preview, id: openedPreviewID, openedAt };
      return {
        pendingReveal: { previewID: openedPreviewID, serial: revealSerial, sessionID: preview.sessionID },
        previews: {
          ...state.previews,
          [preview.sessionID]: existing
            ? sessionPreviews.map((entry) => (entry.id === existing.id ? openedPreview : entry))
            : [...sessionPreviews, openedPreview],
        },
      };
    });
    return openedPreviewID;
  },
}));

export function openFilePreview(preview: FilePreviewInput) {
  const previewID = useFilePreviewStore.getState().openPreview(preview);
  setCanvasOpen(true);
  window.requestAnimationFrame(() => {
    if (useFilePreviewStore.getState().previews[preview.sessionID]?.some((entry) => entry.id === previewID)) {
      setCanvasOpen(true);
    }
  });
}

export function closeFilePreview(sessionID: string, previewID: string) {
  useFilePreviewStore.getState().closePreview(sessionID, previewID);
}

export function clearFilePreviews() {
  useFilePreviewStore.getState().clearPreviews();
}

export function consumeFilePreviewReveal(serial: number) {
  useFilePreviewStore.getState().consumeReveal(serial);
}

export function useFilePreviews(sessionID?: string) {
  return useFilePreviewStore((state) => (sessionID ? state.previews[sessionID] || EMPTY_PREVIEWS : EMPTY_PREVIEWS));
}

export function useFilePreviewReveal(primarySessionID?: string, secondarySessionID?: string) {
  return useFilePreviewStore((state) => {
    const reveal = state.pendingReveal;
    return reveal && (reveal.sessionID === primarySessionID || reveal.sessionID === secondarySessionID)
      ? reveal
      : undefined;
  });
}
