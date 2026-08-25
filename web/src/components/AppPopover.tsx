import type { ComponentProps } from "react";

import { PopoverContent } from "@/components/ui/popover";

export const appPopoverItemStateClassName =
  "hover:bg-[var(--floating-hover)] focus-visible:bg-[var(--floating-hover)] active:bg-[var(--floating-active)]";

export const appPopoverControlItemStateClassName =
  "hover:bg-[var(--floating-hover)] focus-within:bg-[var(--floating-hover)] active:bg-[var(--floating-active)]";

export const appPopoverSelectedItemStateClassName =
  "bg-[var(--floating-active)] hover:bg-[var(--floating-active)] focus-within:bg-[var(--floating-active)]";

export function AppPopoverContent(props: ComponentProps<typeof PopoverContent>) {
  return <PopoverContent data-app-floating-content="" {...props} />;
}
