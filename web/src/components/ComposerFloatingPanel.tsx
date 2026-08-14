import type { HTMLAttributes } from "react";

import { composerMenuShadowClassName } from "@/components/composerControlStyles";
import { cn } from "@/lib/utils";

export function ComposerFloatingPanel({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "pudding-composer-floating-panel absolute bottom-full left-4 z-20 max-h-[min(20rem,calc(100vh-12rem))] rounded-t-lg border border-border/70 bg-popover/95 text-popover-foreground backdrop-blur sm:left-16",
        composerMenuShadowClassName,
        className,
      )}
      {...props}
    />
  );
}
