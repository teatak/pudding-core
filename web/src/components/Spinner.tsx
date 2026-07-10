import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Spinner({
  className,
  role,
  "aria-label": ariaLabel,
  "aria-labelledby": ariaLabelledBy,
  ...props
}: ComponentProps<"span">) {
  const labelled = Boolean(ariaLabel || ariaLabelledBy);

  return (
    <span
      {...props}
      aria-hidden={labelled ? undefined : true}
      aria-label={ariaLabel}
      aria-labelledby={ariaLabelledBy}
      className={cn(
        "inline-block size-4 shrink-0 rounded-full border-[1.5px] border-current/25 border-t-current animate-spin",
        className,
      )}
      data-slot="spinner"
      role={role || (labelled ? "status" : undefined)}
    />
  );
}
