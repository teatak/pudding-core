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
      className={cn(
        "bg-transparent hover:bg-control-hover focus-visible:bg-control-hover aria-expanded:bg-control-hover aria-pressed:bg-control-hover data-[state=open]:bg-control-hover active:bg-control-active dark:hover:bg-control-hover",
        className,
      )}
      data-shell-action=""
      type={type}
      variant="ghost"
      {...props}
    />
  );
});
