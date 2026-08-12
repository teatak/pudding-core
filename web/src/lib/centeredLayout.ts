export type CenteredLayoutConstraints = {
  dockedMinimumWidth: number;
  railChatMinimumWidth: number;
  railWorkspaceMinimumWidth: number;
  thirdColumnMinimumWidth: number;
};

export type CenteredLayoutDockSide = "left" | "right";

export type CenteredLayoutPresentation = {
  railResponsiveCollapsed: boolean;
  workspaceOverlay: boolean;
};

export function resolveCenteredLayoutPresentation({
  chatDockSide,
  constraints,
  layoutWidth,
  leftGroupRatio,
  workspaceDockRequested,
}: {
  chatDockSide: CenteredLayoutDockSide;
  constraints: CenteredLayoutConstraints;
  layoutWidth: number;
  leftGroupRatio: number;
  workspaceDockRequested: boolean;
}): CenteredLayoutPresentation {
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
    railWorkspaceMinimumWidth,
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

  // The divider splits [rail + left content] from the third column. Keeping
  // that boundary as the single source prevents it from jumping when the rail
  // yields. Which content joins the rail depends on the chat dock side.
  const leftGroupWidth = layoutWidth * leftGroupRatio;
  const leftGroupMinimumWidth = chatDockSide === "left"
    ? railChatMinimumWidth
    : railWorkspaceMinimumWidth;
  const expandedRailFits =
    layoutWidth >= leftGroupMinimumWidth + thirdColumnMinimumWidth &&
    leftGroupWidth >= leftGroupMinimumWidth;
  return {
    railResponsiveCollapsed: !expandedRailFits,
    workspaceOverlay: false,
  };
}
