import type { VirtualItem } from "@tanstack/react-virtual";
import { create } from "zustand";

type TranscriptViewport = {
  atLatest: boolean;
  measurements: VirtualItem[];
  scrollOffset: number;
};

// Renderer-only reading state; canonical messages remain in the query cache.
// Primary and split panes can read the same session at different positions.
export const useTranscriptViewportStore = create<{
  viewports: Record<string, TranscriptViewport | undefined>;
  save: (key: string, viewport: TranscriptViewport) => void;
}>((set) => ({
  viewports: {},
  save: (key, viewport) => set((state) => ({
    viewports: { ...state.viewports, [key]: viewport },
  })),
}));
