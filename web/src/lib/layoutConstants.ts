export const layoutStorageKeys = {
  agentConsoleDockSplitRatio: "pudding.agentConsoleDockSplitRatio",
  projectBrowserRatio: "pudding.projectBrowserRatio",
  projectSidebarRatio: "pudding.projectSidebarRatio",
  splitRatio: "pudding.splitRatio",
} as const;

export const sessionRailLayout = {
  expandedWidthPx: 268,
} as const;

export const chatLayout = {
  minimumContentWidthPx: 380,
  horizontalGutterPx: 40,
} as const;

export const workspaceLayout = {
  defaultLeftGroupRatio: 0.55,
  minChatPx: chatLayout.minimumContentWidthPx + chatLayout.horizontalGutterPx,
  minWorkspacePx: 380,
  drawerBreakpointPx:
    chatLayout.minimumContentWidthPx + chatLayout.horizontalGutterPx + 380,
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
