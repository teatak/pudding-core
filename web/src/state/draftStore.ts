import { create } from "zustand";

export type DraftModelValue = { provider?: string; model?: string };

type DraftState = {
  text: string;
  model: DraftModelValue;
  setText: (text: string) => void;
  setModel: (model: DraftModelValue) => void;
  clear: () => void;
};

const LAST_MODEL_STORAGE_KEY = "pudding.lastModel";

function readLastModel(): DraftModelValue {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(LAST_MODEL_STORAGE_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw) as DraftModelValue;
    if (typeof parsed.provider === "string" && typeof parsed.model === "string" && parsed.provider && parsed.model) {
      return { provider: parsed.provider, model: parsed.model };
    }
  } catch {
    // localStorage 可能被禁用;draft 继续使用内存态。
  }
  return {};
}

function writeLastModel(model: DraftModelValue) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    if (model.provider && model.model) {
      window.localStorage.setItem(LAST_MODEL_STORAGE_KEY, JSON.stringify({ provider: model.provider, model: model.model }));
      return;
    }
    window.localStorage.removeItem(LAST_MODEL_STORAGE_KEY);
  } catch {
    // 忽略持久化失败。
  }
}

// 新会话未落库前的本地草稿。只放内存,切换会话不丢,真正提交成功后清空。
export const useDraftStore = create<DraftState>((set) => ({
  text: "",
  model: readLastModel(),
  setText: (text) => set({ text }),
  setModel: (model) => {
    writeLastModel(model);
    set({ model });
  },
  clear: () => set({ text: "" }),
}));
