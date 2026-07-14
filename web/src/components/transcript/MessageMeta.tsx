import { Check, Copy, SquareMousePointer } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { uiContextLabel } from "@/components/UIContextControl";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { formatClock } from "@/lib/time";
import { cn } from "@/lib/utils";
import type { UIContextPart } from "@/state/uiContextStore";

import type { TurnModelVM } from "./types";

export function InterruptedBadge() {
  const { t } = useI18n();
  return (
    <div className="mt-2">
      <Badge variant="outline">{t("transcript.interrupted")}</Badge>
    </div>
  );
}

export function MessageMeta({
  actions,
  align = "start",
  createdAt,
  duration,
  model,
  text,
  uiContext,
}: {
  actions?: ReactNode;
  align?: "start" | "end";
  createdAt: string;
  duration?: string;
  model?: TurnModelVM;
  text: string;
  uiContext?: UIContextPart;
}) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (resetTimer.current) {
        window.clearTimeout(resetTimer.current);
      }
    };
  }, []);
  return (
    <div
      className={cn(
        "flex h-6 w-full items-center gap-2 text-xs text-muted-foreground opacity-0 group-hover:opacity-100 group-focus-within:opacity-100",
        align === "end" && "justify-end",
      )}
    >
      {actions}
      <Button
        aria-label={t("common.copy")}
        className={cn(
          "size-6 bg-transparent transition-colors hover:bg-muted dark:hover:bg-muted/50 active:translate-y-0",
          align === "start" && "-ml-1",
        )}
        size="icon-xs"
        type="button"
        variant="ghost"
        onClick={() => {
          void navigator.clipboard.writeText(text).then(() => {
            setCopied(true);
            if (resetTimer.current) {
              window.clearTimeout(resetTimer.current);
            }
            resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
          });
        }}
      >
        {copied ? <Check className="text-success" /> : <Copy />}
      </Button>
      {uiContext ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="grid size-5 place-items-center text-muted-foreground/60">
              <SquareMousePointer className="size-3.5" />
            </span>
          </TooltipTrigger>
          <TooltipContent>{uiContextLabel(uiContext, t)}</TooltipContent>
        </Tooltip>
      ) : null}
      <span>{formatClock(createdAt)}</span>
      {duration ? <span className="text-muted-foreground/70">{t("transcript.turnDuration").replace("{duration}", duration)}</span> : null}
      {model ? (
        <>
          <span aria-hidden className="size-1 shrink-0 rounded-full bg-muted-foreground/35" />
          <ModelPill model={model} />
        </>
      ) : null}
    </div>
  );
}

function ModelPill({ model }: { model: TurnModelVM }) {
  const label = formatModelLabel(model.model);

  return (
    <span
      className="inline-flex h-5 min-w-0 max-w-48 items-center text-[11px] leading-none text-muted-foreground/60"
      title={model.model}
    >
      <span className="truncate">{label}</span>
    </span>
  );
}
