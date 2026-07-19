export const layoutStorageKeys = {
  projectBrowserRatio: "pudding.projectBrowserRatio",
  projectSidebarRatio: "pudding.projectSidebarRatio",
  splitRatio: "pudding.splitRatio",
  workspaceRatio: "pudding.workspaceRatio",
} as const;

export const sessionRailLayout = {
  expandedWidthPx: 268,
} as const;

export const chatLayout = {
  minimumContentWidthPx: 380,
  horizontalGutterPx: 40,
} as const;

export const workspaceLayout = {
  fallback: { chat: 30, workspace: 70 },
  closed: { chat: 100, workspace: 0 },
  minPercent: 1,
  maxPercent: 99,
  minChatPx: chatLayout.minimumContentWidthPx + chatLayout.horizontalGutterPx,
  maxChatPx: 920,
  minWorkspacePx: 320,
  drawerBreakpointPx:
    chatLayout.minimumContentWidthPx + chatLayout.horizontalGutterPx + 320,
  drawerWidthPx: 560,
  railAutoCollapsePx:
    chatLayout.minimumContentWidthPx + chatLayout.horizontalGutterPx + sessionRailLayout.expandedWidthPx,
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
