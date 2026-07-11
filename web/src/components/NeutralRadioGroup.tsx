import type { ComponentProps } from "react";

import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { cn } from "@/lib/utils";

function NeutralRadioGroup({ className, ...props }: ComponentProps<typeof RadioGroup>) {
  return <RadioGroup className={cn("cursor-default", className)} {...props} />;
}

function NeutralRadioGroupItem({ className, ...props }: ComponentProps<typeof RadioGroupItem>) {
  return (
    <RadioGroupItem
      className={cn(
        "cursor-default data-checked:border-foreground/70 data-checked:bg-foreground data-checked:text-background dark:data-checked:bg-foreground [&_[data-slot=radio-group-indicator]>span]:bg-background",
        className,
      )}
      {...props}
    />
  );
}

export { NeutralRadioGroup, NeutralRadioGroupItem };
