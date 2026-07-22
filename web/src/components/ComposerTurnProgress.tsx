import { Check } from "lucide-react";

import { Spinner } from "@/components/Spinner";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import type { ActiveTurnPlan, TurnPlanStep } from "@/state/turnPlan";

export function ComposerTurnProgress({ progress }: { progress: ActiveTurnPlan }) {
  const { t } = useI18n();
  const current = progress.plan[progress.currentStep - 1];
  if (!current) {
    return null;
  }
  const label = `${progress.currentStep}/${progress.totalSteps} · ${current.step}`;

  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-full z-20 flex justify-center pb-2">
      <HoverCard openDelay={180} closeDelay={100}>
        <HoverCardTrigger asChild>
          <div
            aria-label={t("composer.planProgressAria").replace("{progress}", label)}
            className="pointer-events-auto flex h-9 max-w-[min(32rem,calc(100%-2rem))] items-center gap-2.5 rounded-lg border border-border/70 bg-popover/95 px-3 text-xs text-popover-foreground shadow-sm backdrop-blur outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            role="status"
            tabIndex={0}
          >
            <ProgressSegments steps={progress.plan} />
            <span className="shrink-0 font-medium tabular-nums text-muted-foreground">
              {progress.currentStep}/{progress.totalSteps}
            </span>
            <span aria-hidden="true" className="text-muted-foreground/60">·</span>
            <span className="min-w-0 truncate">{current.step}</span>
          </div>
        </HoverCardTrigger>
        <HoverCardContent
          align="center"
          side="top"
          sideOffset={8}
          className="w-80 max-w-[calc(100vw-2rem)] p-2"
        >
          <div className="grid gap-0.5">
            {progress.plan.map((step, index) => (
              <PlanStep key={`${index}:${step.step}`} active={index + 1 === progress.currentStep} index={index} step={step} />
            ))}
          </div>
        </HoverCardContent>
      </HoverCard>
    </div>
  );
}

function ProgressSegments({ steps }: { steps: TurnPlanStep[] }) {
  return (
    <span aria-hidden="true" className="flex w-16 shrink-0 gap-0.5">
      {steps.map((step, index) => (
        <span
          key={index}
          className={cn(
            "h-1.5 min-w-1 flex-1 rounded-[2px]",
            step.status === "completed" && "bg-primary",
            step.status === "in_progress" && "bg-primary/55",
            step.status === "pending" && "bg-muted-foreground/20",
          )}
        />
      ))}
    </span>
  );
}

function PlanStep({ active, index, step }: { active: boolean; index: number; step: TurnPlanStep }) {
  return (
    <div className={cn("flex min-h-8 items-center gap-2 rounded-md px-2 py-1.5", active && "bg-muted")}>
      <span className="grid size-4 shrink-0 place-items-center" aria-hidden="true">
        {step.status === "completed" ? (
          <Check className="size-3.5 text-primary" strokeWidth={2.2} />
        ) : step.status === "in_progress" ? (
          <Spinner className="size-3.5 text-primary" />
        ) : (
          <span className="size-2 rounded-[2px] bg-muted-foreground/25" />
        )}
      </span>
      <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{index + 1}</span>
      <span
        className={cn(
          "min-w-0 flex-1 break-words text-xs [overflow-wrap:anywhere]",
          !active && "text-muted-foreground",
        )}
      >
        {step.step}
      </span>
    </div>
  );
}
