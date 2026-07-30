import type { ComponentProps } from "react";

import { SelectContent } from "@/components/ui/select";
import { cn } from "@/lib/utils";

export function AppSelectContent({
  className,
  position = "popper",
  side = "bottom",
  align = "start",
  ...props
}: ComponentProps<typeof SelectContent>) {
  return (
    <SelectContent
      align={align}
      className={cn("no-drag-region", className)}
      position={position}
      side={side}
      {...props}
    />
  );
}
