import type { ReactElement, ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

export function AppTooltip({ children, content, open }: { children: ReactElement; content: ReactNode; open?: boolean }) {
  return (
    <Tooltip open={open}>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="bottom" className="break-words">{content}</TooltipContent>
    </Tooltip>
  );
}
