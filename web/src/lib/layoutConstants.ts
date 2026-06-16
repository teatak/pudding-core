export const layoutStorageKeys = {
  splitRatio: "pudding.splitRatio",
  workspaceRatio: "pudding.workspaceRatio",
} as const;

export const workspaceLayout = {
  fallback: { chat: 70, canvas: 30 },
  closed: { chat: 100, canvas: 0 },
  minPercent: 1,
  maxPercent: 99,
  minChatPx: 360,
  minCanvasPx: 240,
} as const;

export const resizeTargetMinimumSize = {
  coarse: 20,
  fine: 10,
} as const;

export const splitLayout = {
  fallback: { primary: 50, split: 50 },
  closed: { primary: 100, split: 0 },
  minPercent: 20,
  maxPercent: 80,
  minPanePx: 280,
} as const;
