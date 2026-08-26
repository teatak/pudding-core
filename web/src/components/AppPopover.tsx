import type { ComponentProps } from "react";

import { PopoverContent } from "@/components/ui/popover";

export const appPopoverItemStateClassName =
  "hover:bg-interactive-hover focus-visible:bg-interactive-hover active:bg-interactive-pressed";

export const appPopoverControlItemStateClassName =
  "hover:bg-interactive-hover focus-within:bg-interactive-hover active:bg-interactive-pressed";

export const appPopoverSelectedItemStateClassName =
  "bg-interactive-selected hover:bg-interactive-selected focus-within:bg-interactive-selected active:bg-interactive-pressed";

export function AppPopoverContent(props: ComponentProps<typeof PopoverContent>) {
  return <PopoverContent data-app-floating-content="" {...props} />;
}
