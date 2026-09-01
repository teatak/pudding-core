export const MASCOT_MOOD_OPTIONS = [
  { id: "idle", label: "Idle" },
  { id: "thinking", label: "Thinking" },
  { id: "error", label: "Error" },
] as const;

export type MascotMood = (typeof MASCOT_MOOD_OPTIONS)[number]["id"];
