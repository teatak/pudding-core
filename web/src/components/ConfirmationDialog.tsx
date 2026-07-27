import type { ComponentProps } from "react";

import { X } from "@/components/icons";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel as BaseAlertDialogCancel,
  AlertDialogContent as BaseAlertDialogContent,
  AlertDialogDescription as BaseAlertDialogDescription,
  AlertDialogFooter as BaseAlertDialogFooter,
  AlertDialogHeader as BaseAlertDialogHeader,
  AlertDialogTitle as BaseAlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { cn } from "@/lib/utils";

function AlertDialogContent({
  children,
  className,
  ...props
}: ComponentProps<typeof BaseAlertDialogContent>) {
  return (
    <BaseAlertDialogContent
      className={cn(
        "gap-5 p-6 data-[size=default]:max-w-[calc(100%-2rem)] data-[size=default]:sm:max-w-lg",
        className,
      )}
      {...props}
    >
      {children}
      <BaseAlertDialogCancel
        aria-label="Close"
        className="absolute top-3 right-3"
        size="icon-sm"
        variant="ghost"
      >
        <X />
      </BaseAlertDialogCancel>
    </BaseAlertDialogContent>
  );
}

function AlertDialogHeader({
  className,
  ...props
}: ComponentProps<typeof BaseAlertDialogHeader>) {
  return (
    <BaseAlertDialogHeader
      className={cn("flex flex-col items-start gap-2 pr-8 text-left", className)}
      {...props}
    />
  );
}

function AlertDialogTitle({
  className,
  ...props
}: ComponentProps<typeof BaseAlertDialogTitle>) {
  return (
    <BaseAlertDialogTitle
      className={cn("text-xl leading-tight font-semibold", className)}
      {...props}
    />
  );
}

function AlertDialogDescription({
  className,
  ...props
}: ComponentProps<typeof BaseAlertDialogDescription>) {
  return (
    <BaseAlertDialogDescription
      className={cn("text-base leading-relaxed", className)}
      {...props}
    />
  );
}

function AlertDialogFooter({
  className,
  ...props
}: ComponentProps<typeof BaseAlertDialogFooter>) {
  return (
    <BaseAlertDialogFooter
      className={cn(
        "mx-0 mb-0 flex-row items-center justify-end gap-2 rounded-none border-0 bg-transparent p-0",
        className,
      )}
      {...props}
    />
  );
}

function AlertDialogCancel({
  className,
  variant = "ghost",
  ...props
}: ComponentProps<typeof BaseAlertDialogCancel>) {
  return (
    <BaseAlertDialogCancel
      className={cn(
        "border-0 hover:!bg-accent dark:hover:!bg-accent",
        className,
      )}
      variant={variant}
      {...props}
    />
  );
}

export {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
};
