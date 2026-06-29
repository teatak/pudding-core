import * as React from "react"

import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"

function Field({
  className,
  orientation = "vertical",
  ...props
}: React.ComponentProps<"div"> & {
  orientation?: "vertical" | "horizontal" | "responsive"
}) {
  return (
    <div
      data-orientation={orientation}
      data-slot="field"
      className={cn(
        "flex flex-col gap-1.5 rounded-lg border bg-background px-3 py-2.5 transition-colors",
        orientation === "horizontal" && "flex-row items-center justify-between gap-3",
        orientation === "responsive" && "sm:flex-row sm:items-center sm:justify-between sm:gap-3",
        className
      )}
      {...props}
    />
  )
}

function FieldLabel({
  className,
  ...props
}: React.ComponentProps<typeof Label>) {
  return (
    <Label
      data-slot="field-label"
      className={cn("block w-full cursor-pointer text-left", className)}
      {...props}
    />
  )
}

function FieldContent({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="field-content"
      className={cn("grid min-w-0 gap-1", className)}
      {...props}
    />
  )
}

function FieldTitle({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="field-title"
      className={cn("text-sm font-normal leading-none text-foreground", className)}
      {...props}
    />
  )
}

function FieldDescription({
  className,
  ...props
}: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="field-description"
      className={cn("text-xs leading-relaxed text-muted-foreground", className)}
      {...props}
    />
  )
}

export { Field, FieldContent, FieldDescription, FieldLabel, FieldTitle }
