import { ChevronDown, ChevronRight } from "@/components/icons";
import {
  useContext,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEventHandler,
  type MouseEventHandler,
  type ReactNode,
  type ToggleEvent,
} from "react";

import { cn } from "@/lib/utils";

import { TranscriptItemMeasureContext } from "./TranscriptItemMeasureContext";

type TranscriptDisclosureProps = {
  action?: ReactNode;
  children?: ReactNode;
  className?: string;
  contentClassName?: string;
  icon: ReactNode;
  iconClassName?: string;
  open?: boolean;
  summary?: ReactNode;
  title: ReactNode;
  onSummaryClick?: MouseEventHandler<HTMLElement>;
  onSummaryKeyDown?: KeyboardEventHandler<HTMLElement>;
  onToggle?: (event: ToggleEvent<HTMLDetailsElement>) => void;
};

export function TranscriptActivityIcon({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span className={cn("relative z-[1] inline-flex h-6 w-[18px] shrink-0 items-center justify-center [&_svg]:size-3.5! [&_[data-slot=identity-icon]]:size-4! [&_[data-slot=spinner]]:size-3!", className)}>
      {children}
    </span>
  );
}

export function TranscriptDisclosure({
  action,
  children,
  className,
  contentClassName,
  icon,
  iconClassName,
  open,
  summary,
  title,
  onSummaryClick,
  onSummaryKeyDown,
  onToggle,
}: TranscriptDisclosureProps) {
  const [localOpen, setLocalOpen] = useState(false);
  const expandable = children != null;
  const resolvedOpen = open ?? localOpen;
  const measureItem = useContext(TranscriptItemMeasureContext);
  const previousLayoutRef = useRef({ expandable, open: resolvedOpen });

  useLayoutEffect(() => {
    const previous = previousLayoutRef.current;
    previousLayoutRef.current = { expandable, open: resolvedOpen };
    if (
      !measureItem ||
      (previous.expandable === expandable && previous.open === resolvedOpen)
    ) {
      return;
    }
    queueMicrotask(measureItem);
  }, [expandable, measureItem, resolvedOpen]);

  const row = (
    <>
      <TranscriptActivityIcon className={iconClassName}>{icon}</TranscriptActivityIcon>
      <span className="flex min-w-0 items-center gap-1.5">
        <span className="shrink-0 truncate">{title}</span>
        {summary != null ? (
          <span className="min-w-0 truncate text-muted-foreground">{summary}</span>
        ) : null}
        {expandable ? (
          <span className="shrink-0">
            {resolvedOpen ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          </span>
        ) : null}
        {action != null ? (
          <span className="inline-flex shrink-0 items-center" data-transcript-header-action onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
            {action}
          </span>
        ) : null}
      </span>
    </>
  );

  if (!expandable) {
    return (
      <div className={cn("grid h-6 w-full grid-cols-[18px_minmax(0,1fr)] items-center gap-1 pr-1 text-[13px] leading-[1.5] text-foreground/70", className)}>
        {row}
      </div>
    );
  }

  return (
    <details
      className={cn("min-w-0 max-w-full overflow-hidden text-[13px] leading-[1.5] text-foreground/70", className)}
      open={resolvedOpen}
      onToggle={onToggle}
    >
      <summary
        className="grid h-6 w-fit max-w-full cursor-default list-none grid-cols-[18px_minmax(0,1fr)] items-center gap-1 pr-1 outline-none hover:text-foreground [&::-webkit-details-marker]:hidden"
        tabIndex={-1}
        onClick={(event) => {
          onSummaryClick?.(event);
          if (open === undefined && !event.defaultPrevented) {
            // Native <details> toggles before its asynchronous toggle event.
            // Commit local state first so layout measurement runs before paint.
            event.preventDefault();
            setLocalOpen((current) => !current);
          }
        }}
        onKeyDown={(event) => {
          onSummaryKeyDown?.(event);
          if (open === undefined && !event.defaultPrevented && (event.key === "Enter" || event.key === " ")) {
            event.preventDefault();
            setLocalOpen((current) => !current);
          }
        }}
      >
        {row}
      </summary>
      <div className={cn("min-w-0 max-w-full py-1", contentClassName)}>{children}</div>
    </details>
  );
}
