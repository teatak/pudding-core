import type { ComponentProps } from "react";

import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
} from "@/components/ui/context-menu";
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

const contentClassName = "min-w-36 w-max font-sans";
const itemClassName = "min-h-7 whitespace-nowrap text-[13px] leading-5 font-normal [&_svg:not([class*='size-'])]:size-3.5";

export function AppDropdownMenuContent({ className, ...props }: ComponentProps<typeof DropdownMenuContent>) {
  return <DropdownMenuContent collisionPadding={8} className={cn(contentClassName, className)} data-app-floating-content="" {...props} />;
}

export function AppDropdownMenuItem({ className, ...props }: ComponentProps<typeof DropdownMenuItem>) {
  return <DropdownMenuItem className={cn(itemClassName, className)} {...props} />;
}

export function AppDropdownMenuRadioItem({ className, ...props }: ComponentProps<typeof DropdownMenuRadioItem>) {
  return <DropdownMenuRadioItem className={cn(itemClassName, className)} {...props} />;
}

export function AppDropdownMenuSeparator(props: ComponentProps<typeof DropdownMenuSeparator>) {
  return <DropdownMenuSeparator {...props} />;
}

export function AppDropdownMenuSubContent({ className, ...props }: ComponentProps<typeof DropdownMenuSubContent>) {
  return <DropdownMenuSubContent className={cn(contentClassName, className)} data-app-floating-content="" {...props} />;
}

export function AppDropdownMenuSubTrigger({ className, ...props }: ComponentProps<typeof DropdownMenuSubTrigger>) {
  return <DropdownMenuSubTrigger className={cn(itemClassName, className)} {...props} />;
}

export function AppContextMenuContent({ className, ...props }: ComponentProps<typeof ContextMenuContent>) {
  return <ContextMenuContent collisionPadding={8} className={cn(contentClassName, className)} data-app-floating-content="" {...props} />;
}

export function AppContextMenuItem({ className, ...props }: ComponentProps<typeof ContextMenuItem>) {
  return <ContextMenuItem className={cn(itemClassName, className)} {...props} />;
}

export function AppContextMenuSeparator(props: ComponentProps<typeof ContextMenuSeparator>) {
  return <ContextMenuSeparator {...props} />;
}
