import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight } from "@/components/icons";
import { useState, type ReactNode } from "react";

import { getSessionUsage, type SessionUsage } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { Button } from "@/components/ui/button";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

type ContextUsageRingProps = {
  token: string;
  sessionID: string;
};

export function ContextUsageRing({ token, sessionID }: ContextUsageRingProps) {
  const { t } = useI18n();
  const usageQuery = useQuery({
    queryKey: queryKeys.sessionUsage(sessionID),
    queryFn: () => getSessionUsage(token, sessionID),
    enabled: Boolean(token && sessionID),
    staleTime: 5_000,
  });
  const usage = usageQuery.data;
  const contextTokens = usage?.contextEstimatedTokens || 0;
  const contextPercent = usage && usage.contextWindow > 0 ? Math.min(100, (contextTokens / usage.contextWindow) * 100) : 0;
  const tone = contextPercent >= 95 ? "danger" : contextPercent >= 80 ? "warning" : "ok";

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          aria-label={t("usage.contextWindow")}
          className="relative rounded-full text-muted-foreground"
          size="icon"
          type="button"
          variant="ghost"
        >
          <Ring percent={usageQuery.isLoading ? 0 : contextPercent} tone={tone} />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className="w-68 gap-0 p-0 text-xs"
        side="top"
        sideOffset={10}
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        {usage ? (
          <UsagePanel
            contextPercent={contextPercent}
            contextTokens={contextTokens}
            tone={tone}
            usage={usage}
          />
        ) : (
          <EmptyPanel loading={usageQuery.isLoading} />
        )}
      </PopoverContent>
    </Popover>
  );
}

function UsagePanel({
  contextPercent,
  contextTokens,
  tone,
  usage,
}: {
  contextPercent: number;
  contextTokens: number;
  tone: "ok" | "warning" | "danger";
  usage: SessionUsage;
}) {
  const { locale, t } = useI18n();
  const compactPercent =
    usage.contextWindow > 0 && usage.autoCompactThresholdTokens > 0
      ? Math.min(100, (usage.autoCompactThresholdTokens / usage.contextWindow) * 100)
      : 0;
  return (
    <div>
      <div className="space-y-2 px-3 py-2.5">
        <SectionHeader>{t("usage.contextWindow")}</SectionHeader>
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-3">
          <div className="flex min-w-0 items-baseline gap-1.5">
            <span className="shrink-0 text-xs font-medium text-muted-foreground">{t("usage.currentInput")}</span>
            <span className="truncate text-base font-semibold text-foreground">
              {formatTokens(contextTokens, locale)} / {usage.contextWindow > 0 ? formatTokens(usage.contextWindow, locale) : "--"}
            </span>
          </div>
          <div className="text-right text-xs font-medium tabular-nums text-muted-foreground">{Math.round(contextPercent)}%</div>
        </div>
        <div className="relative h-1.5 overflow-hidden rounded-full bg-foreground/10">
          <div
            className={cn("absolute inset-y-0 left-0 rounded-full", progressClass(tone))}
            style={{ width: `${Math.min(100, contextPercent)}%` }}
          />
          {compactPercent > 0 ? (
            <div
              aria-label={`${t("usage.autoCompactThreshold")} ${formatTokens(usage.autoCompactThresholdTokens, locale)}`}
              className="absolute inset-y-[-2px] w-px bg-foreground/45"
              style={{ left: `${compactPercent}%` }}

            />
          ) : null}
        </div>
        <div className="space-y-0.5">
          <CollapsibleUsageGroup label={t("usage.nextEstimate")} value={contextTokens}>
            {usage.inputCalibrationSamples > 0 ? (
              <>
                <UsageRow label={t("usage.rawEstimate")} value={usage.contextRawEstimatedTokens} muted indent />
                <UsageRow
                  formattedValue={`${usage.inputCalibrationFactor.toFixed(2)}×`}
                  label={t("usage.calibrationFactor")}
                  value={usage.inputCalibrationFactor}
                  muted
                  indent
                />
              </>
            ) : null}
            <UsageRow label={t("usage.conversationHistory")} value={usage.messageEstimatedTokens} muted indent />
            <UsageRow label={t("usage.systemPrompt")} value={usage.systemPromptEstimatedTokens} muted indent />
            <UsageRow label={t("usage.toolsSchema")} value={usage.toolsSchemaEstimatedTokens} muted indent />
          </CollapsibleUsageGroup>
          {usage.autoCompactThresholdTokens > 0 ? (
            <UsageRow label={t("usage.autoCompactThreshold")} value={usage.autoCompactThresholdTokens} />
          ) : null}
        </div>
      </div>
      <div className="border-t px-3 py-2.5">
        <SectionHeader className="mb-1.5" value={usage.lastPromptTokens + usage.lastOutputTokens}>
          {t("usage.lastRequest")}
        </SectionHeader>
        <div className="space-y-0.5">
          <CollapsibleUsageGroup label={t("usage.inputTotal")} value={usage.lastPromptTokens} subtle>
            <UsageRow label={t("usage.cacheMiss")} value={usage.lastInputUncachedTokens} muted indent />
            {usage.lastCacheCreationTokens > 0 ? (
              <UsageRow label={t("usage.cacheWrite")} value={usage.lastCacheCreationTokens} muted indent />
            ) : null}
            <UsageRow label={t("usage.cacheHit")} value={usage.lastInputCachedTokens} muted indent />
          </CollapsibleUsageGroup>
          <CollapsibleUsageGroup label={t("usage.outputTotal")} value={usage.lastOutputTokens} subtle>
            <UsageRow label={t("usage.outputContent")} value={usage.lastOutputContentTokens} muted indent />
            <UsageRow label={t("usage.outputReasoning")} value={usage.lastOutputReasoningTokens} muted indent />
          </CollapsibleUsageGroup>
        </div>
      </div>
      <div className="border-t px-3 py-2.5">
        <SectionHeader className="mb-1.5" value={usage.cumulativeTotalTokens}>
          {t("usage.sessionCumulative")}
        </SectionHeader>
        <div className="space-y-0.5">
          <UsageRow label={t("usage.requests")} value={usage.requestCount} raw subtle />
          <CollapsibleUsageGroup label={t("usage.inputTotal")} value={usage.cumulativeInputTokens} subtle>
            <UsageRow label={t("usage.cacheMiss")} value={usage.cumulativeInputUncachedTokens} muted indent />
            {usage.cumulativeCacheCreationTokens > 0 ? (
              <UsageRow label={t("usage.cacheWrite")} value={usage.cumulativeCacheCreationTokens} muted indent />
            ) : null}
            <UsageRow label={t("usage.cacheHit")} value={usage.cumulativeInputCachedTokens} muted indent />
          </CollapsibleUsageGroup>
          <CollapsibleUsageGroup label={t("usage.outputTotal")} value={usage.cumulativeOutputTokens} subtle>
            <UsageRow label={t("usage.outputContent")} value={usage.cumulativeOutputContentTokens} muted indent />
            <UsageRow label={t("usage.outputReasoning")} value={usage.cumulativeOutputReasoningTokens} muted indent />
          </CollapsibleUsageGroup>
        </div>
      </div>
    </div>
  );
}

function EmptyPanel({ loading }: { loading: boolean }) {
  const { t } = useI18n();
  return <div className="p-3 text-xs text-muted-foreground">{loading ? t("common.loading") : t("usage.unavailable")}</div>;
}

function SectionHeader({ children, className, value }: { children: ReactNode; className?: string; value?: number }) {
  const { locale } = useI18n();
  return (
    <div className={cn("grid min-h-6 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 text-sm font-semibold text-foreground", className)}>
      <div>{children}</div>
      {typeof value === "number" ? <div className="text-right tabular-nums">{formatTokens(value, locale)}</div> : null}
    </div>
  );
}

function CollapsibleUsageGroup({
  children,
  icon,
  label,
  raw = false,
  subtle = false,
  value,
}: {
  children: ReactNode;
  icon?: ReactNode;
  label: string;
  raw?: boolean;
  subtle?: boolean;
  value: number;
}) {
  const { locale } = useI18n();
  const [open, setOpen] = useState(false);
  const Chevron = open ? ChevronDown : ChevronRight;

  return (
    <div className="space-y-0.5">
      <button
        aria-expanded={open}
        className={cn(
          "grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-sm text-left leading-[18px] hover:text-foreground",
          subtle && "text-foreground/70",
        )}
        type="button"
        onClick={() => setOpen((value) => !value)}
      >
        <div className="flex min-w-0 items-center gap-1.5 truncate">
          {icon}
          <span className="truncate">{label}</span>
          <Chevron className="ml-1 size-3 shrink-0 text-muted-foreground" />
        </div>
        <div className={cn("text-right font-medium tabular-nums", subtle ? "text-foreground/70" : "text-foreground/90")}>
          {raw ? formatNumber(value, locale) : formatTokens(value, locale)}
        </div>
      </button>
      {open ? <div className="space-y-0.5">{children}</div> : null}
    </div>
  );
}

function UsageRow({
  formattedValue,
  icon,
  indent = false,
  label,
  labelMuted = false,
  muted = false,
  raw = false,
  subtle = false,
  value,
}: {
  formattedValue?: string;
  icon?: ReactNode;
  indent?: boolean;
  label: string;
  labelMuted?: boolean;
  muted?: boolean;
  raw?: boolean;
  subtle?: boolean;
  value: number;
}) {
  const { locale } = useI18n();
  return (
    <div className={cn("grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 leading-[18px]", subtle && "text-foreground/70", muted && "text-muted-foreground")}>
      <div className={cn("flex min-w-0 items-center gap-1.5 truncate", indent && "pl-3", labelMuted && "text-muted-foreground")}>
        {icon}
        <span className="truncate">{label}</span>
      </div>
      <div className={cn("text-right font-medium tabular-nums", muted ? "text-muted-foreground" : subtle ? "text-foreground/70" : "text-foreground/90")}>
        {formattedValue ?? (raw ? formatNumber(value, locale) : formatTokens(value, locale))}
      </div>
    </div>
  );
}

function Ring({ percent, tone }: { percent: number; tone: "ok" | "warning" | "danger" }) {
  const radius = 8;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - (Math.min(100, percent) / 100) * circumference;
  return (
    <svg aria-hidden="true" className="size-4 -rotate-90" viewBox="0 0 24 24">
      <circle className="stroke-muted-foreground/30" cx="12" cy="12" fill="none" r={radius} strokeWidth="2.4" />
      <circle
        className={cn("transition-[stroke-dashoffset]", strokeClass(tone))}
        cx="12"
        cy="12"
        fill="none"
        r={radius}
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        strokeWidth="2.4"
      />
    </svg>
  );
}

function strokeClass(tone: "ok" | "warning" | "danger") {
  if (tone === "danger") {
    return "stroke-destructive";
  }
  if (tone === "warning") {
    return "stroke-amber-500";
  }
  return "stroke-muted-foreground";
}

function progressClass(tone: "ok" | "warning" | "danger") {
  if (tone === "danger") {
    return "bg-destructive";
  }
  if (tone === "warning") {
    return "bg-amber-500";
  }
  return "bg-emerald-500";
}

function formatNumber(value: number, locale: string, maximumFractionDigits = 0) {
  return new Intl.NumberFormat(locale, {
    maximumFractionDigits,
    useGrouping: !locale.startsWith("zh"),
  }).format(value);
}

function formatTokens(value: number, locale: string) {
  const abs = Math.abs(value);
  const units = [
    { divisor: 1_000_000_000, suffix: "B" },
    { divisor: 1_000_000, suffix: "M" },
    { divisor: 1_000, suffix: "K" },
  ];
  for (const unit of units) {
    if (abs >= unit.divisor) {
      const scaled = value / unit.divisor;
      const scaledAbs = Math.abs(scaled);
      const maximumFractionDigits = scaledAbs >= 100 ? 0 : scaledAbs >= 10 ? 1 : 2;
      return `${formatNumber(scaled, locale, maximumFractionDigits)}${unit.suffix}`;
    }
  }
  return formatNumber(value, locale);
}
