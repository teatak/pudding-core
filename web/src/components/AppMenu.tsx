import type { ComponentProps } from "react";

import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

const contentClassName = "min-w-40 w-max rounded-xl border-0 bg-popover/95 p-1.5 font-sans shadow-[0_10px_28px_rgb(0_0_0/0.18)] ring-1 ring-foreground/10 backdrop-blur-xl data-open:animate-none data-closed:animate-none dark:shadow-[0_10px_28px_rgb(0_0_0/0.45)]";
const itemClassName = "min-h-[26px] gap-2 rounded-md px-2 py-0.5 text-[13px] leading-5 font-normal whitespace-nowrap focus:bg-control-hover focus:text-foreground [&_svg:not([class*='size-'])]:size-3.5";
const radioItemClassName = "min-h-[26px] gap-2 rounded-md py-0.5 pr-8 pl-2 text-[13px] leading-5 font-normal whitespace-nowrap focus:bg-control-hover focus:text-foreground [&_svg:not([class*='size-'])]:size-3.5";
const labelClassName = "min-h-[26px] gap-2 px-2 py-0.5 text-[13px] leading-5 font-normal text-muted-foreground";
const separatorClassName = "mx-1 my-1 bg-foreground/10";

export function AppDropdownMenuContent({ className, ...props }: ComponentProps<typeof DropdownMenuContent>) {
  return <DropdownMenuContent collisionPadding={8} className={cn(contentClassName, className)} data-app-floating-content="" {...props} />;
}

export function AppDropdownMenuItem({ className, ...props }: ComponentProps<typeof DropdownMenuItem>) {
  return <DropdownMenuItem className={cn(itemClassName, className)} {...props} />;
}

export function AppDropdownMenuRadioItem({ className, ...props }: ComponentProps<typeof DropdownMenuRadioItem>) {
  return <DropdownMenuRadioItem className={cn(radioItemClassName, className)} {...props} />;
}

export function AppDropdownMenuLabel({ className, ...props }: ComponentProps<typeof DropdownMenuLabel>) {
  return <DropdownMenuLabel className={cn(labelClassName, className)} {...props} />;
}

export function AppDropdownMenuSeparator({ className, ...props }: ComponentProps<typeof DropdownMenuSeparator>) {
  return <DropdownMenuSeparator className={cn(separatorClassName, className)} {...props} />;
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

export function AppContextMenuLabel({ className, ...props }: ComponentProps<typeof ContextMenuLabel>) {
  return <ContextMenuLabel className={cn(labelClassName, className)} {...props} />;
}

export function AppContextMenuSeparator({ className, ...props }: ComponentProps<typeof ContextMenuSeparator>) {
  return <ContextMenuSeparator className={cn(separatorClassName, className)} {...props} />;
}

export function AppContextMenuSubContent({ className, ...props }: ComponentProps<typeof ContextMenuSubContent>) {
  return <ContextMenuSubContent className={cn(contentClassName, className)} data-app-floating-content="" {...props} />;
}

export function AppContextMenuSubTrigger({ className, ...props }: ComponentProps<typeof ContextMenuSubTrigger>) {
  return <ContextMenuSubTrigger className={cn(itemClassName, className)} {...props} />;
}
