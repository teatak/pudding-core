import { forwardRef, type ComponentProps } from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type ShellActionButtonProps = Omit<ComponentProps<typeof Button>, "variant">;

export const ShellActionButton = forwardRef<HTMLButtonElement, ShellActionButtonProps>(function ShellActionButton(
  { className, type = "button", ...props },
  ref,
) {
  return (
    <Button
      ref={ref}
      className={cn("pudding-shell-action bg-transparent", className)}
      type={type}
      variant="ghost"
      {...props}
    />
  );
});
