import { create } from "zustand";

export type DraftModelValue = { provider?: string; model?: string };

type DraftState = {
  text: string;
  model: DraftModelValue;
  setText: (text: string) => void;
  setModel: (model: DraftModelValue) => void;
  clear: () => void;
};

// 新会话未落库前的本地草稿。只放内存,切换会话不丢,真正提交成功后清空。
export const useDraftStore = create<DraftState>((set) => ({
  text: "",
  model: {},
  setText: (text) => set({ text }),
  setModel: (model) => set({ model }),
  clear: () => set({ text: "", model: {} }),
}));
