export const SETTINGS_KEYS = {
  compactAutoThresholdPercent: "compact_auto_threshold_percent",
  compactTailInputTurns: "compact_tail_input_turns",
  showCompactSummary: "show_compact_summary",
  showReasoning: "show_reasoning",
  showRawToolInfo: "show_raw_tool_info",
  showAppPreviewVersions: "show_app_preview_versions",
  editorFontFamily: "editor_font_family",
  editorFontSize: "editor_font_size",
  editorLineHeight: "editor_line_height",
} as const;

export const DEFAULT_EDITOR_FONT_FAMILY = "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace";
export const DEFAULT_EDITOR_FONT_SIZE = 12;
export const DEFAULT_EDITOR_LINE_HEIGHT = 20;

export const SETTINGS_DEFAULTS: Record<string, string> = {
  [SETTINGS_KEYS.compactAutoThresholdPercent]: "80",
  [SETTINGS_KEYS.compactTailInputTurns]: "2",
  [SETTINGS_KEYS.showCompactSummary]: "true",
  [SETTINGS_KEYS.showReasoning]: "true",
  [SETTINGS_KEYS.showRawToolInfo]: "true",
  [SETTINGS_KEYS.showAppPreviewVersions]: "false",
  [SETTINGS_KEYS.editorFontFamily]: DEFAULT_EDITOR_FONT_FAMILY,
  [SETTINGS_KEYS.editorFontSize]: String(DEFAULT_EDITOR_FONT_SIZE),
  [SETTINGS_KEYS.editorLineHeight]: String(DEFAULT_EDITOR_LINE_HEIGHT),
};

export type TranscriptDisplaySettings = {
  showCompactSummary: boolean;
  showReasoning: boolean;
  showRawToolInfo: boolean;
};

export type EditorTypography = {
  fontFamily: string;
  fontSize: number;
  lineHeight: number;
  resolvedLineHeight: number;
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

export function editorTypographySettings(settings?: Record<string, string>): EditorTypography {
  const values = settingsWithDefaults(settings);
  const fontSize = integerSetting(values[SETTINGS_KEYS.editorFontSize], 10, 24, DEFAULT_EDITOR_FONT_SIZE);
  const lineHeight = editorLineHeightSetting(values[SETTINGS_KEYS.editorLineHeight]);
  return {
    fontFamily: values[SETTINGS_KEYS.editorFontFamily]?.trim() || DEFAULT_EDITOR_FONT_FAMILY,
    fontSize,
    lineHeight,
    resolvedLineHeight: lineHeight || automaticEditorLineHeight(fontSize),
  };
}

function editorLineHeightSetting(value: string | undefined) {
  const parsed = Number(value);
  if (Number.isInteger(parsed) && (parsed === 0 || (parsed >= 12 && parsed <= 40))) {
    return parsed;
  }
  return DEFAULT_EDITOR_LINE_HEIGHT;
}

function integerSetting(value: string | undefined, min: number, max: number, fallback: number) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= min && parsed <= max ? parsed : fallback;
}

function automaticEditorLineHeight(fontSize: number) {
  const isMacintosh = typeof navigator !== "undefined" && /Macintosh|Mac OS X/.test(navigator.userAgent);
  return Math.max(8, Math.round(fontSize * (isMacintosh ? 1.5 : 1.35)));
}
