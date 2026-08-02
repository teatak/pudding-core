import { create } from "zustand";

const STORAGE_KEY = "pudding.reasoningEffortByModel";

type ReasoningEffortPreferenceState = {
  byModel: Record<string, string>;
  setForModel: (modelKey: string, effort: string) => void;
};

function readPreferences() {
  if (typeof window === "undefined") {
    return {};
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(STORAGE_KEY) || "{}") as Record<string, unknown>;
    return Object.fromEntries(
      Object.entries(parsed).filter(([key, value]) => key && typeof value === "string" && value),
    ) as Record<string, string>;
  } catch {
    return {};
  }
}

function writePreferences(preferences: Record<string, string>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    if (Object.keys(preferences).length > 0) {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(preferences));
    } else {
      window.localStorage.removeItem(STORAGE_KEY);
    }
  } catch {
    // localStorage 可能被禁用;偏好继续使用内存态。
  }
}

export const useReasoningEffortPreferenceStore = create<ReasoningEffortPreferenceState>((set) => ({
  byModel: readPreferences(),
  setForModel: (modelKey, effort) => {
    const normalizedKey = modelKey.trim();
    const normalizedEffort = effort.trim();
    if (!normalizedKey) {
      return;
    }
    set((state) => {
      const byModel = { ...state.byModel };
      if (normalizedEffort) {
        byModel[normalizedKey] = normalizedEffort;
      } else {
        delete byModel[normalizedKey];
      }
      writePreferences(byModel);
      return { byModel };
    });
  },
}));
