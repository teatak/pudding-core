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
  const headerStyle = railCollapsed ? { paddingLeft: "calc(var(--traffic-inset) + 3.25rem)" } : undefined;

  return (
    <header
      className={cn("drag-region relative z-30 flex h-(--toolbar-h) shrink-0 items-center justify-between gap-3 px-6", className)}
      style={headerStyle}
    >
      <div className="flex min-w-0 items-center gap-2">
        {icon ? <div className="no-drag-region flex shrink-0 items-center text-muted-foreground">{icon}</div> : null}
        <h1 className="truncate text-sm font-medium">{title}</h1>
      </div>
      {actions ? <div className="no-drag-region flex shrink-0 items-center gap-2">{actions}</div> : null}
    </header>
  );
}
