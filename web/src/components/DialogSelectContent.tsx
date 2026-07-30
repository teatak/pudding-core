import type { ComponentProps } from "react";

import { AppSelectContent } from "@/components/AppSelectContent";
import { SelectGroup } from "@/components/ui/select";
import { markSelectOutsideInteraction } from "@/lib/layerGuards";

export function DialogSelectContent({ children, onPointerDownOutside, ...props }: ComponentProps<typeof AppSelectContent>) {
  return (
    <AppSelectContent
      onPointerDownOutside={(event) => {
        markSelectOutsideInteraction();
        onPointerDownOutside?.(event);
      }}
      {...props}
    >
      <SelectGroup>{children}</SelectGroup>
    </AppSelectContent>
  );
}
