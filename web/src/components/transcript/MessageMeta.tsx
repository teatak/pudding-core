import { Check, Copy, SquareMousePointer } from "@/components/icons";
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
  return <Badge variant="outline">{t("transcript.interrupted")}</Badge>;
}

export function MessageMeta({
  actions,
  align = "start",
  createdAt,
  duration,
  hideStandardDetails = false,
  hoverGroup = "message",
  model,
  persistentStatus,
  text,
  trailingActions,
  uiContext,
}: {
  actions?: ReactNode;
  align?: "start" | "end";
  createdAt: string;
  duration?: string;
  hideStandardDetails?: boolean;
  hoverGroup?: "assistant-turn" | "message";
  model?: TurnModelVM;
  persistentStatus?: ReactNode;
  text: string;
  trailingActions?: ReactNode;
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
        "flex h-6 w-full items-center gap-1 text-xs text-muted-foreground",
        !persistentStatus && "opacity-0 transition-opacity",
        !persistentStatus && hoverGroup === "message" && "group-hover:opacity-100 group-focus-within:opacity-100",
        !persistentStatus && hoverGroup === "assistant-turn" && "group-hover/assistant-turn:opacity-100 group-focus-within/assistant-turn:opacity-100",
        align === "end" && "justify-end",
      )}
    >
      <div className="flex items-center gap-1">
        <div className={cn("flex items-center", align === "start" && !actions && !trailingActions && "w-4 justify-center")}>
          {actions}
          {hideStandardDetails ? null : (
            <>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    aria-label={t(copied ? "common.copied" : "common.copy")}
                    className="size-6 bg-transparent active:translate-y-0"
                    size="icon-xs"
                    tabIndex={-1}
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
                    {copied ? <Check className="size-3 text-success" /> : <Copy className="size-3" />}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t(copied ? "common.copied" : "common.copy")}</TooltipContent>
              </Tooltip>
              {trailingActions}
              {uiContext ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span className="grid size-6 place-items-center text-muted-foreground">
                      <SquareMousePointer className="size-3" />
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{uiContextLabel(uiContext, t)}</TooltipContent>
                </Tooltip>
              ) : null}
            </>
          )}
        </div>
        {hideStandardDetails ? null : (
          <div className="flex items-center gap-2">
            <span>{formatClock(createdAt)}</span>
            {duration ? <span className="text-muted-foreground/70">{t("transcript.turnDuration").replace("{duration}", duration)}</span> : null}
            {model ? (
              <>
                <span aria-hidden className="size-1 shrink-0 rounded-full bg-muted-foreground/35" />
                <ModelPill model={model} />
              </>
            ) : null}
          </div>
        )}
      </div>
      {persistentStatus}
    </div>
  );
}

function ModelPill({ model }: { model: TurnModelVM }) {
  const label = formatModelLabel(model.model);

  return (
    <span
      className="inline-flex h-5 min-w-0 max-w-48 items-center text-[11px] leading-none text-muted-foreground/60"

    >
      <span className="truncate">{label}</span>
    </span>
  );
}
