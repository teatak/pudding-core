export type CenteredLayoutConstraints = {
  dockedMinimumWidth: number;
  railChatMinimumWidth: number;
  thirdColumnMinimumWidth: number;
};

export type CenteredLayoutPresentation = {
  railResponsiveCollapsed: boolean;
  workspaceOverlay: boolean;
};

export function resolveCenteredLayoutPresentation({
  focused,
  constraints,
  layoutWidth,
  leftGroupRatio,
  workspaceDockRequested,
}: {
  focused: boolean;
  constraints: CenteredLayoutConstraints;
  layoutWidth: number;
  leftGroupRatio: number;
  workspaceDockRequested: boolean;
}): CenteredLayoutPresentation {
  if (focused) {
    return { railResponsiveCollapsed: true, workspaceOverlay: false };
  }

  // The first measurement is delivered from ResizeObserver after mount. Keep the
  // preferred desktop presentation until then instead of flashing both overlays.
  if (!Number.isFinite(layoutWidth) || layoutWidth <= 0) {
    return {
      railResponsiveCollapsed: false,
      workspaceOverlay: false,
    };
  }

  const {
    dockedMinimumWidth,
    railChatMinimumWidth,
    thirdColumnMinimumWidth,
  } = constraints;

  if (!workspaceDockRequested) {
    return {
      railResponsiveCollapsed: layoutWidth < railChatMinimumWidth,
      workspaceOverlay: false,
    };
  }

  if (layoutWidth < dockedMinimumWidth) {
    return {
      railResponsiveCollapsed: layoutWidth < railChatMinimumWidth,
      workspaceOverlay: true,
    };
  }

  // The divider separates the rail and conversation from the workspace.
  const leftGroupWidth = layoutWidth * leftGroupRatio;
  const expandedRailFits =
    layoutWidth >= railChatMinimumWidth + thirdColumnMinimumWidth &&
    leftGroupWidth >= railChatMinimumWidth;
  return {
    railResponsiveCollapsed: !expandedRailFits,
    workspaceOverlay: false,
  };
}
