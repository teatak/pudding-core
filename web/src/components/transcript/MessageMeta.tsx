import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { BrandIcon, brandIconName } from "@/components/BrandIcons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { formatClock } from "@/lib/time";
import { cn } from "@/lib/utils";

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
}: {
  actions?: ReactNode;
  align?: "start" | "end";
  createdAt: string;
  duration?: string;
  model?: TurnModelVM;
  text: string;
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
      <span>{formatClock(createdAt)}</span>
      {duration ? <span className="text-muted-foreground/70">{t("transcript.turnDuration").replace("{duration}", duration)}</span> : null}
      {model ? <ModelPill model={model} /> : null}
    </div>
  );
}

function ModelPill({ model }: { model: TurnModelVM }) {
  const label = formatModelLabel(model.model);
  const brand = modelBrandName(model);
  const fallback = (label || model.model).trim().slice(0, 1).toUpperCase() || "M";

  return (
    <span
      className="inline-flex h-5 min-w-0 max-w-48 items-center gap-1 text-[11px] leading-none text-muted-foreground/75"
      title={model.model}
    >
      <span aria-hidden className="mx-0.5 size-1 shrink-0 rounded-full bg-muted-foreground/35" />
      <span className="relative grid size-4 shrink-0 place-items-center overflow-hidden rounded-full bg-muted-foreground/15 text-[9px] font-medium text-muted-foreground">
        <span>{fallback}</span>
        {brand ? <BrandIcon className="absolute inset-0 size-full" name={brand} shape="circle" /> : null}
      </span>
      <span className="truncate">{label}</span>
    </span>
  );
}

function modelBrandName(model: TurnModelVM) {
  const candidates = [
    model.provider,
    inferBrandFromText(model.provider),
    inferBrandFromText(model.model),
    model.model.split("/")[0],
  ];
  for (const candidate of candidates) {
    const brand = candidate ? brandIconName(candidate) : "";
    if (brand) {
      return brand;
    }
  }
  return "";
}

function inferBrandFromText(value?: string) {
  const text = value?.toLowerCase() || "";
  if (!text) {
    return "";
  }
  if (text.includes("deepseek")) return "deepseek";
  if (text.includes("gemini") || text.includes("google")) return "gemini";
  if (text.includes("openai") || text.includes("gpt")) return "openai";
  if (text.includes("qwen")) return "qwen";
  if (text.includes("mimo")) return "mimo";
  if (text.includes("moonshot") || text.includes("kimi")) return "moonshot";
  if (text.includes("zhipu") || text.includes("glm")) return "zhipu";
  if (text.includes("openrouter")) return "openrouter";
  if (text.includes("ollama")) return "ollama";
  if (text.includes("claude") || text.includes("anthropic")) return "claude";
  if (text.includes("grok")) return "grok";
  return "";
}
