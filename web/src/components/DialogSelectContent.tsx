import type { ComponentProps } from "react";

import { SelectContent } from "@/components/ui/select";
import { markSelectOutsideInteraction } from "@/lib/layerGuards";

export function DialogSelectContent({ onPointerDownOutside, ...props }: ComponentProps<typeof SelectContent>) {
  return (
    <SelectContent
      onPointerDownOutside={(event) => {
        markSelectOutsideInteraction();
        onPointerDownOutside?.(event);
      }}
      {...props}
    />
  );
}
