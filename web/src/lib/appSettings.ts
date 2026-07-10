export const SETTINGS_KEYS = {
  compactAutoThresholdPercent: "compact_auto_threshold_percent",
  compactTailInputTurns: "compact_tail_input_turns",
  showCompactSummary: "show_compact_summary",
  showReasoning: "show_reasoning",
  showRawToolInfo: "show_raw_tool_info",
} as const;

export const SETTINGS_DEFAULTS: Record<string, string> = {
  [SETTINGS_KEYS.compactAutoThresholdPercent]: "80",
  [SETTINGS_KEYS.compactTailInputTurns]: "2",
  [SETTINGS_KEYS.showCompactSummary]: "true",
  [SETTINGS_KEYS.showReasoning]: "true",
  [SETTINGS_KEYS.showRawToolInfo]: "true",
};

export type TranscriptDisplaySettings = {
  showCompactSummary: boolean;
  showReasoning: boolean;
  showRawToolInfo: boolean;
};

export function settingsWithDefaults(settings?: Record<string, string>) {
  return { ...SETTINGS_DEFAULTS, ...(settings || {}) };
}

export function boolSetting(settings: Record<string, string> | undefined, key: string, fallback = true) {
  const value = settingsWithDefaults(settings)[key];
  if (value === "true") {
    return true;
  }
  if (value === "false") {
    return false;
  }
  return fallback;
}

export function transcriptDisplaySettings(settings?: Record<string, string>): TranscriptDisplaySettings {
  return {
    showCompactSummary: boolSetting(settings, SETTINGS_KEYS.showCompactSummary),
    showReasoning: boolSetting(settings, SETTINGS_KEYS.showReasoning),
    showRawToolInfo: boolSetting(settings, SETTINGS_KEYS.showRawToolInfo),
  };
}
