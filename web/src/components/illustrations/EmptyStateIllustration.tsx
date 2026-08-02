import type { ReactNode, SVGProps } from "react";

import { cn } from "@/lib/utils";

export const emptyStateIllustrationStrokeOpacity = 0.45;
export const emptyStateIllustrationDetailStrokeOpacity = 0.3;
export const emptyStateIllustrationStrokeWidth = 2;

type EmptyStateIllustrationProps = Omit<
  SVGProps<SVGSVGElement>,
  "aria-hidden" | "children" | "focusable" | "role" | "viewBox"
> & {
  children: ReactNode;
};

export function EmptyStateIllustration({
  children,
  className,
  ...props
}: EmptyStateIllustrationProps) {
  return (
    <svg
      aria-hidden="true"
      className={cn("size-36 overflow-visible text-foreground", className)}
      fill="none"
      focusable="false"
      viewBox="0 0 144 112"
      {...props}
    >
      {children}
    </svg>
  );
}
