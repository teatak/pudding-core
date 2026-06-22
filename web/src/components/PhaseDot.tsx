import type { TurnPhase } from "@/state/overlayStore";
import { cn } from "@/lib/utils";

type PhaseDotProps = {
  phase?: TurnPhase;
  active?: boolean;
  className?: string;
  size?: "sm" | "md";
};

const phaseColors: Record<TurnPhase, string> = {
  submitting: "var(--phase-submitting)",
  awaiting_model: "var(--phase-awaiting-model)",
  streaming_text: "var(--phase-streaming-text)",
  awaiting_followup: "var(--phase-awaiting-followup)",
  thinking: "var(--phase-thinking)",
  streaming_tool_args: "var(--phase-streaming-tool-args)",
  executing_tool: "var(--phase-executing-tool)",
  error: "var(--phase-error)",
  cancelled: "var(--phase-cancelled)",
};

export function PhaseDot({ active = true, className, phase = "awaiting_model", size = "md" }: PhaseDotProps) {
  const color = phaseColors[phase];
  const dotSize = size === "sm" ? "size-1.5" : "size-2.5";
  const haloSize = size === "sm" ? "size-1.5" : "size-3";
  const shellSize = size === "sm" ? "size-1.5" : "size-3";

  return (
    <span aria-hidden="true" className={cn("relative inline-flex shrink-0 items-center justify-center", shellSize, className)}>
      {active ? (
        <span className={cn("phase-dot-breathe absolute rounded-full opacity-20", haloSize)} style={{ backgroundColor: color }} />
      ) : null}
      <span className={cn("relative rounded-full", dotSize)} style={{ backgroundColor: color }} />
    </span>
  );
}
