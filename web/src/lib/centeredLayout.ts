export type CenteredLayoutConstraints = {
  chatDockMaximumWidth: number;
  chatDockMinimumWidth: number;
  leftPairMinimumWidth: number;
  railWidth: number;
  rightPairMinimumWidth: number;
  workspaceDockMinimumWidth: number;
};

export type CenteredLayoutPresentation = {
  railResponsiveCollapsed: boolean;
  workspaceOverlay: boolean;
};

function clamp(value: number, minimum: number, maximum: number) {
  return Math.min(Math.max(value, minimum), maximum);
}

function dockedChatWidth({
  constraints,
  dockRatio,
  stageWidth,
}: {
  constraints: CenteredLayoutConstraints;
  dockRatio: number;
  stageWidth: number;
}) {
  const chatMinimumWidth = Math.min(
    constraints.chatDockMinimumWidth,
    stageWidth / 2,
  );
  const workspaceMinimumWidth = Math.min(
    constraints.workspaceDockMinimumWidth,
    stageWidth / 2,
  );
  const chatMaximumWidth = Math.max(
    chatMinimumWidth,
    Math.min(
      constraints.chatDockMaximumWidth,
      stageWidth - workspaceMinimumWidth,
    ),
  );
  return clamp(
    stageWidth * clamp(dockRatio, 0.2, 0.8),
    chatMinimumWidth,
    chatMaximumWidth,
  );
}

export function resolveCenteredLayoutPresentation({
  constraints,
  dockRatio,
  layoutWidth,
  workspaceDockRequested,
}: {
  constraints: CenteredLayoutConstraints;
  dockRatio: number;
  layoutWidth: number;
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
    leftPairMinimumWidth,
    railWidth,
    rightPairMinimumWidth,
  } = constraints;

  if (!workspaceDockRequested) {
    return {
      railResponsiveCollapsed: layoutWidth < leftPairMinimumWidth,
      workspaceOverlay: false,
    };
  }

  // Chat is shared by two overlapping constraint regions:
  //
  //   [ rail + chat ] and [ chat + workspace ]
  //
  // Try the four stable presentations in product-priority order. The rail yields
  // before a docked workspace; once the workspace becomes an overlay it no longer
  // consumes width, so the rail may expand again when its own pair fits.
  const expandedRailStageWidth = Math.max(0, layoutWidth - railWidth);
  const expandedRailChatWidth = dockedChatWidth({
    constraints,
    dockRatio,
    stageWidth: expandedRailStageWidth,
  });
  const leftPairWidth = railWidth + expandedRailChatWidth;
  const rightPairWidth = expandedRailStageWidth;
  if (
    leftPairWidth >= leftPairMinimumWidth &&
    rightPairWidth >= rightPairMinimumWidth
  ) {
    return {
      railResponsiveCollapsed: false,
      workspaceOverlay: false,
    };
  }
  if (layoutWidth >= rightPairMinimumWidth) {
    return {
      railResponsiveCollapsed: true,
      workspaceOverlay: false,
    };
  }
  return {
    railResponsiveCollapsed: layoutWidth < leftPairMinimumWidth,
    workspaceOverlay: true,
  };
}
