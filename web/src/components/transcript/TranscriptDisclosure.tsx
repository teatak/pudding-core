import { ChevronDown, ChevronRight } from "@/components/icons";
import { useState, type KeyboardEventHandler, type MouseEventHandler, type ReactNode, type ToggleEvent } from "react";

import { cn } from "@/lib/utils";

type TranscriptDisclosureProps = {
  children?: ReactNode;
  className?: string;
  contentClassName?: string;
  icon: ReactNode;
  iconClassName?: string;
  open?: boolean;
  summary?: ReactNode;
  summaryClassName?: string;
  title: ReactNode;
  onSummaryClick?: MouseEventHandler<HTMLElement>;
  onSummaryKeyDown?: KeyboardEventHandler<HTMLElement>;
  onToggle?: (event: ToggleEvent<HTMLDetailsElement>) => void;
};

export function TranscriptActivityIcon({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span className={cn("relative z-[1] inline-flex h-6 w-[18px] shrink-0 items-center justify-center text-muted-foreground/65 [&_svg]:size-3.5! [&_[data-slot=identity-icon]]:size-[18px]! [&_[data-slot=spinner]]:size-3!", className)}>
      {children}
    </span>
  );
}

export function TranscriptDisclosure({
  children,
  className,
  contentClassName,
  icon,
  iconClassName,
  open,
  summary,
  summaryClassName,
  title,
  onSummaryClick,
  onSummaryKeyDown,
  onToggle,
}: TranscriptDisclosureProps) {
  const [localOpen, setLocalOpen] = useState(false);
  const expandable = children != null;
  const resolvedOpen = open ?? localOpen;
  const row = (
    <>
      <TranscriptActivityIcon className={iconClassName}>{icon}</TranscriptActivityIcon>
      <span className="flex min-w-0 items-center gap-1.5">
        <span className="shrink-0 truncate">{title}</span>
        {summary != null ? (
          <span className={cn("min-w-0 truncate text-muted-foreground/50", summaryClassName)}>{summary}</span>
        ) : null}
        {expandable ? (
          <span className="shrink-0 text-muted-foreground/50">
            {resolvedOpen ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          </span>
        ) : null}
      </span>
    </>
  );

  if (!expandable) {
    return (
      <div className={cn("grid h-6 w-full grid-cols-[18px_minmax(0,1fr)] items-center gap-1 pr-1 text-[13px] leading-[1.5] text-muted-foreground", className)}>
        {row}
      </div>
    );
  }

  return (
    <details
      className={cn("min-w-0 max-w-full overflow-hidden text-[13px] leading-[1.5] text-muted-foreground", className)}
      open={resolvedOpen}
      onToggle={(event) => {
        if (open === undefined) {
          setLocalOpen(event.currentTarget.open);
        }
        onToggle?.(event);
      }}
    >
      <summary
        className="inline-grid h-6 max-w-full cursor-default list-none grid-cols-[18px_minmax(0,1fr)] items-center gap-1 pr-1 outline-none hover:text-foreground [&::-webkit-details-marker]:hidden"
        tabIndex={-1}
        onClick={onSummaryClick}
        onKeyDown={onSummaryKeyDown}
      >
        {row}
      </summary>
      <div className={cn("min-w-0 max-w-full py-1", contentClassName)}>{children}</div>
    </details>
  );
}
