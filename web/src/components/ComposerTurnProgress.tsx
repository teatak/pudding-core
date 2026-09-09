import { ChevronUp, Circle, CircleCheckBig } from "@/components/icons";

import { AppPopoverContent } from "@/components/AppPopover";
import { Spinner } from "@/components/Spinner";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import type { ActiveTurnPlan, TurnPlanStep } from "@/state/turnPlan";

export function ComposerTurnProgress({ progress, paused = false }: { progress: ActiveTurnPlan; paused?: boolean }) {
  const { t } = useI18n();
  const current = progress.plan[progress.currentStep - 1];
  if (!current) {
    return null;
  }
  const stepLabel = t("composer.planProgressStep")
    .replace("{current}", String(progress.currentStep))
    .replace("{total}", String(progress.totalSteps));
  const completedLabel = t("composer.planProgressCompleted")
    .replace("{completed}", String(progress.plan.filter((step) => step.status === "completed").length))
    .replace("{total}", String(progress.totalSteps));
  const label = `${stepLabel} · ${current.step}`;

  return (
    <div className="pointer-events-none flex min-w-0 flex-1 justify-center">
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            aria-label={t("composer.planProgressAria").replace("{progress}", label)}
            className="group pointer-events-auto flex h-9 w-fit max-w-[min(28rem,calc(100%-2rem))] cursor-pointer items-center gap-2 rounded-lg border border-border/60 bg-popover/95 px-3 text-xs text-popover-foreground shadow-xs backdrop-blur outline-none hover:bg-interactive-hover data-[state=open]:bg-interactive-hover focus-visible:ring-1 focus-visible:ring-ring/30"
          >
            <ProgressSegments steps={progress.plan} paused={paused} />
            <span className="shrink-0 font-medium tabular-nums text-muted-foreground">
              {stepLabel}
            </span>
            <span aria-hidden="true" className="text-muted-foreground/60">·</span>
            <span className="min-w-0 truncate">{current.step}</span>
            <ChevronUp aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground group-data-[state=open]:rotate-180" />
          </button>
        </PopoverTrigger>
        <AppPopoverContent
          aria-label={t("composer.planProgressTitle")}
          align="center"
          side="top"
          sideOffset={8}
          className="max-h-[min(24rem,var(--radix-popover-content-available-height))] w-80 max-w-[calc(100vw-2rem)] overflow-y-auto p-2"
        >
          <div className="flex items-center justify-between gap-3 px-2 pt-1 text-xs">
            <span className="font-medium">{t("composer.planProgressTitle")}</span>
            <span className="tabular-nums text-muted-foreground">{completedLabel}</span>
          </div>
          <ol className="grid gap-0.5">
            {progress.plan.map((step, index) => (
              <PlanStep key={index} active={index + 1 === progress.currentStep} index={index} step={step} paused={paused} />
            ))}
          </ol>
        </AppPopoverContent>
      </Popover>
    </div>
  );
}

function ProgressSegments({ steps, paused }: { steps: TurnPlanStep[]; paused: boolean }) {
  return (
    <span aria-hidden="true" className="flex w-16 shrink-0 gap-0.5">
      {steps.map((step, index) => (
        <span
          key={index}
          className={cn(
            "h-1.5 min-w-0 flex-1 rounded-[2px]",
            step.status === "completed" && "bg-success",
            step.status === "in_progress" && "bg-success",
            step.status === "in_progress" && !paused && "pudding-plan-step-breathe",
            step.status === "pending" && "bg-muted-foreground/20",
          )}
        />
      ))}
    </span>
  );
}

function PlanStep({ active, index, step, paused }: { active: boolean; index: number; step: TurnPlanStep; paused: boolean }) {
  return (
    <li aria-current={active ? "step" : undefined} className={cn("flex min-h-8 items-center gap-2 rounded-md px-2 py-1.5", active && "bg-interactive-selected")}>
      <span className="grid size-4 shrink-0 place-items-center" aria-hidden="true">
        {step.status === "completed" ? (
          <CircleCheckBig className="size-4 text-muted-foreground" data-icon-weight="subtle" />
        ) : step.status === "in_progress" && !paused ? (
          <Spinner className="size-3.5 text-muted-foreground motion-reduce:animate-none" />
        ) : (
          <Circle className="size-3.5 text-muted-foreground" data-icon-weight="subtle" />
        )}
      </span>
      <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{index + 1}</span>
      <span
        className={cn(
          "min-w-0 flex-1 break-words text-xs [overflow-wrap:anywhere]",
          active ? "text-foreground" : "text-muted-foreground",
        )}
      >
        {step.step}
      </span>
    </li>
  );
}
