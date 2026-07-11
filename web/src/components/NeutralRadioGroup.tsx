import type { ComponentProps, ReactNode } from "react";

import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Field, FieldContent, FieldLabel, FieldTitle } from "@/components/ui/field";
import { cn } from "@/lib/utils";

function NeutralRadioGroup({ className, ...props }: ComponentProps<typeof RadioGroup>) {
  return <RadioGroup className={cn("cursor-default", className)} {...props} />;
}

function NeutralRadioGroupItem({ className, ...props }: ComponentProps<typeof RadioGroupItem>) {
  return <RadioGroupItem className={cn("cursor-default", className)} {...props} />;
}

function NeutralRadioCard({
  className,
  id,
  selected,
  title,
  value,
}: {
  className?: string;
  id: string;
  selected: boolean;
  title: ReactNode;
  value: string;
}) {
  return (
    <FieldLabel className="cursor-default" htmlFor={id}>
      <Field
        className={cn(
          "min-h-14 items-center justify-start gap-3 rounded-xl px-4 py-3",
          selected ? "border-foreground/20 bg-accent" : "bg-transparent hover:bg-transparent",
          className,
        )}
        orientation="horizontal"
      >
        <NeutralRadioGroupItem id={id} value={value} />
        <FieldContent>
          <FieldTitle className="font-medium leading-5">{title}</FieldTitle>
        </FieldContent>
      </Field>
    </FieldLabel>
  );
}

export { NeutralRadioCard, NeutralRadioGroup, NeutralRadioGroupItem };
