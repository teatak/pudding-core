import type { ComponentProps } from "react";

import { PopoverContent } from "@/components/ui/popover";

export function AppPopoverContent(props: ComponentProps<typeof PopoverContent>) {
  return <PopoverContent data-app-floating-content="" {...props} />;
}
