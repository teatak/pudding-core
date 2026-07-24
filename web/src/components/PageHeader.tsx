import type { ReactNode } from "react";

import { cn } from "@/lib/utils";
import { useRailCollapsed } from "@/state/railStore";

export function PageHeader({
  actions,
  className,
  icon,
  title,
}: {
  actions?: ReactNode;
  className?: string;
  icon?: ReactNode;
  title: ReactNode;
}) {
  const railCollapsed = useRailCollapsed();
  const headerStyle = railCollapsed
    ? { paddingLeft: "calc(var(--traffic-inset) + var(--rail-toggle-left) + var(--toolbar-icon-button-size) + var(--rail-title-gap))" }
    : undefined;

  return (
    <header
      className={cn("pointer-events-none relative z-30 flex h-(--toolbar-h) shrink-0 items-center justify-between gap-3 px-(--toolbar-edge-inset)", className)}
      style={headerStyle}
    >
      <div className="flex min-w-0 items-center gap-1">
        {icon ? (
          <div className="pudding-chrome-icon no-drag-region pointer-events-auto flex h-(--toolbar-icon-button-size) w-(--toolbar-icon-button-size) shrink-0 items-center justify-center">
            {icon}
          </div>
        ) : null}
        <h1 className="truncate text-sm font-medium">{title}</h1>
      </div>
      {actions ? <div className="no-drag-region pointer-events-auto flex shrink-0 items-center gap-2">{actions}</div> : null}
    </header>
  );
}
