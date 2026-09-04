import type { HTMLAttributes } from "react";

import { composerMenuShadowClassName } from "@/components/composerControlStyles";
import { cn } from "@/lib/utils";

export function ComposerFloatingPanel({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "pudding-composer-floating-panel absolute left-1/2 z-20 max-h-[min(20rem,calc(100vh-12rem))] w-[min(34rem,calc(100%-2rem))] -translate-x-1/2 rounded-lg border border-border/70 bg-popover/95 p-3 text-popover-foreground backdrop-blur",
        composerMenuShadowClassName,
        className,
      )}
      {...props}
    />
  );
}
