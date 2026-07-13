import type { HTMLAttributes } from "react";

import { cn } from "@/lib/utils";

export function ComposerFloatingPanel({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "absolute bottom-full left-4 z-20 max-h-[min(20rem,calc(100vh-12rem))] rounded-t-lg border border-border/70 bg-popover/95 text-popover-foreground shadow-sm backdrop-blur sm:left-16",
        className,
      )}
      {...props}
    />
  );
}
