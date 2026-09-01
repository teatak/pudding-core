export const BODY_SHELL_ASSET_ID = "body-shell" as const;
export const BODY_HIGHLIGHT_ASSET_ID = "body-highlight" as const;
export const LEFT_ARM_ASSET_ID = "left-arm" as const;
export const RIGHT_ARM_ASSET_ID = "right-arm" as const;
export const ANTENNA_STEM_ASSET_ID = "antenna-stem" as const;
export const ANTENNA_BASE_ASSET_ID = "antenna-base" as const;
export const FACE_FRAME_ASSET_ID = "face-frame" as const;
export const FACE_GLASS_ASSET_ID = "face-glass" as const;
export const FACE_EYES_ASSET_ID = "face-eyes" as const;
export const FACE_THINKING_EYES_ASSET_ID = "face-thinking-eyes" as const;
export const FACE_MOUTH_ASSET_ID = "face-mouth" as const;
export const FACE_BLUSH_ASSET_ID = "face-blush" as const;
export const FACE_REFLECTION_ASSET_ID = "face-reflection" as const;
export const FACE_ERROR_EYES_ASSET_ID = "face-error-eyes" as const;
export const FACE_ERROR_MOUTH_ASSET_ID = "face-error-mouth" as const;

export type SceneAssetID =
  | typeof BODY_SHELL_ASSET_ID
  | typeof BODY_HIGHLIGHT_ASSET_ID
  | typeof LEFT_ARM_ASSET_ID
  | typeof RIGHT_ARM_ASSET_ID
  | typeof ANTENNA_STEM_ASSET_ID
  | typeof ANTENNA_BASE_ASSET_ID
  | typeof FACE_FRAME_ASSET_ID
  | typeof FACE_GLASS_ASSET_ID
  | typeof FACE_EYES_ASSET_ID
  | typeof FACE_THINKING_EYES_ASSET_ID
  | typeof FACE_MOUTH_ASSET_ID
  | typeof FACE_BLUSH_ASSET_ID
  | typeof FACE_REFLECTION_ASSET_ID
  | typeof FACE_ERROR_EYES_ASSET_ID
  | typeof FACE_ERROR_MOUTH_ASSET_ID;

export const SCENE_ASSET_OPTIONS: ReadonlyArray<{ id: SceneAssetID; label: string }> = [
  { id: BODY_SHELL_ASSET_ID, label: "主体后壳" },
  { id: BODY_HIGHLIGHT_ASSET_ID, label: "中层高光" },
  { id: LEFT_ARM_ASSET_ID, label: "左手臂" },
  { id: RIGHT_ARM_ASSET_ID, label: "右手臂" },
  { id: ANTENNA_STEM_ASSET_ID, label: "天线杆" },
  { id: ANTENNA_BASE_ASSET_ID, label: "天线底座" },
  { id: FACE_FRAME_ASSET_ID, label: "前部面框" },
  { id: FACE_GLASS_ASSET_ID, label: "内面罩玻璃" },
  { id: FACE_EYES_ASSET_ID, label: "眼睛" },
  { id: FACE_THINKING_EYES_ASSET_ID, label: "思考眼睛" },
  { id: FACE_MOUTH_ASSET_ID, label: "嘴部" },
  { id: FACE_BLUSH_ASSET_ID, label: "腮红" },
  { id: FACE_REFLECTION_ASSET_ID, label: "面罩反光" },
  { id: FACE_ERROR_EYES_ASSET_ID, label: "错误眼睛" },
  { id: FACE_ERROR_MOUTH_ASSET_ID, label: "错误嘴型" },
];

export const BODY_SHELL_VIEW_BOX = "18 32 92 80";
export const BODY_SHELL_PATH = "M52 34 C56 34 72 34 76 34 C90 34 102 42 106 56 C108 63 107.5 76 108 84 C109 99 99 108 84 109 C71 111 57 111 44 109 C29 108 19 99 20 84 C20.5 76 20 63 22 56 C26 42 38 34 52 34 Z";
export const BODY_METAL_HIGHLIGHT_PATH = "M24.5 62 C25.5 51 32.5 42 43 38.5";
export const LEFT_ARM_VIEW_BOX = "7 86 20 29";
export const LEFT_ARM_PATH = "M22 88 C17.5 91.5 11 98.5 8.5 104 C7 108 9 112 12.5 113 C16 114 19 111 19.5 107 C20 103 21.5 100 24.5 97";
export const RIGHT_ARM_VIEW_BOX = "101 86 20 29";
export const RIGHT_ARM_PATH = "M106 88 C110.5 91.5 117 98.5 119.5 104 C121 108 119 112 115.5 113 C112 114 109 111 108.5 107 C108 103 106.5 100 103.5 97";
export const ANTENNA_STEM_VIEW_BOX = "52 10 24 26";
export const ANTENNA_STEM_PATH = "M64 30.5 C64.5 27 63.5 23 60 19";
export const ANTENNA_TIP_CENTER = { x: 60, y: 19 } as const;
export const ANTENNA_BASE_VIEW_BOX = "54 28 20 10";
export const ANTENNA_BASE_PATH = "M59 35 L60.5 30 H67.5 L69 35 Z";
export const FACE_FRAME_VIEW_BOX = "28 52 72 49";
export const FACE_SCREEN_PATH = "M40 53 H88 C96 53 100 58 100 66 V88 C100 96 95 100 87 100 H41 C33 100 28 96 28 88 V66 C28 58 32 53 40 53 Z";
export const FACE_SCREEN_GROOVE_TRANSFORM = "translate(64 77) scale(0.89) translate(-64 -77)";
export const FACE_SCREEN_INSET_TRANSFORM = "translate(64 77) scale(0.85) translate(-64 -77)";
export const MOUTH_PATH = "M59 88.5 Q64 91 69 88.5";
