import { forwardRef, type ComponentProps } from "react";

import { cn } from "@/lib/utils";

export const RailIconAction = forwardRef<HTMLButtonElement, ComponentProps<"button">>(function RailIconAction(
  { className, type = "button", ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      className={cn(
        "flex size-6 shrink-0 items-center justify-center rounded-[min(var(--radius-md),10px)] border border-transparent bg-clip-padding text-sidebar-foreground/60 opacity-0 outline-none transition-all hover:bg-interactive-hover hover:text-foreground active:bg-interactive-pressed aria-expanded:bg-interactive-selected aria-expanded:text-foreground data-[state=open]:bg-interactive-selected focus-visible:border-ring focus-visible:opacity-100 focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className,
      )}
      type={type}
      {...props}
    />
  );
});
