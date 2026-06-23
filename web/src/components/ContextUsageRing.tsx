import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useState, type ReactNode } from "react";

import { getSessionUsage, type SessionUsage } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
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
  const currentTokens = usage ? usage.lastPromptTokens || usage.contextEstimatedTokens : 0;
  const estimatedTokens = usage?.contextEstimatedTokens || 0;
  const currentPercent = usage && usage.contextWindow > 0 ? Math.min(100, (currentTokens / usage.contextWindow) * 100) : 0;
  const estimatedPercent = usage && usage.contextWindow > 0 ? Math.min(100, (estimatedTokens / usage.contextWindow) * 100) : 0;
  const tone = Math.max(currentPercent, estimatedPercent) >= 95 ? "danger" : Math.max(currentPercent, estimatedPercent) >= 80 ? "warning" : "ok";

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          aria-label={t("usage.contextWindow")}
          className="relative h-5 w-5 shrink-0 rounded-full border-0 bg-transparent p-0 text-muted-foreground hover:bg-transparent hover:text-foreground"
          type="button"
          variant="ghost"
        >
          <Ring current={usageQuery.isLoading ? 0 : currentPercent} estimate={usageQuery.isLoading ? 0 : estimatedPercent} tone={tone} />
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
            currentPercent={currentPercent}
            currentTokens={currentTokens}
            estimatedPercent={estimatedPercent}
            estimatedTokens={estimatedTokens}
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
  currentPercent,
  currentTokens,
  estimatedPercent,
  estimatedTokens,
  tone,
  usage,
}: {
  currentPercent: number;
  currentTokens: number;
  estimatedPercent: number;
  estimatedTokens: number;
  tone: "ok" | "warning" | "danger";
  usage: SessionUsage;
}) {
  const { t } = useI18n();
  const compactPercent =
    usage.contextWindow > 0 && usage.autoCompactThresholdTokens > 0
      ? Math.min(100, (usage.autoCompactThresholdTokens / usage.contextWindow) * 100)
      : 0;
  return (
    <div>
      <div className="space-y-2 px-3 py-2.5">
        <SectionHeader>{t("usage.contextWindow")}</SectionHeader>
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-3">
          <div className="truncate text-base font-semibold text-foreground">
            {formatTokens(currentTokens)} / {usage.contextWindow > 0 ? formatTokens(usage.contextWindow) : "--"}
          </div>
          <div className="text-right text-xs font-medium tabular-nums text-muted-foreground">{Math.round(currentPercent)}%</div>
        </div>
        <div className="relative h-1.5 overflow-hidden rounded-full bg-muted">
          <div
            className={cn("absolute inset-y-0 left-0 rounded-full opacity-45", progressClass(tone))}
            style={{ width: `${Math.min(100, estimatedPercent)}%` }}
          />
          <div
            className={cn("absolute inset-y-0 left-0 rounded-full", progressClass(tone))}
            style={{ width: `${Math.min(100, currentPercent)}%` }}
          />
          {compactPercent > 0 ? (
            <div
              aria-label={`${t("usage.autoCompactThreshold")} ${formatTokens(usage.autoCompactThresholdTokens)}`}
              className="absolute inset-y-[-2px] w-px bg-foreground/45"
              style={{ left: `${compactPercent}%` }}
              title={`${t("usage.autoCompactThreshold")} ${formatTokens(usage.autoCompactThresholdTokens)}`}
            />
          ) : null}
        </div>
        <div className="space-y-0.5">
          <CollapsibleUsageGroup label={t("usage.nextEstimate")} value={estimatedTokens}>
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
  return (
    <div className={cn("grid min-h-6 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 text-sm font-semibold text-foreground", className)}>
      <div>{children}</div>
      {typeof value === "number" ? <div className="text-right tabular-nums">{formatTokens(value)}</div> : null}
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
          {raw ? formatNumber(value) : formatTokens(value)}
        </div>
      </button>
      {open ? <div className="space-y-0.5">{children}</div> : null}
    </div>
  );
}

function UsageRow({
  icon,
  indent = false,
  label,
  labelMuted = false,
  muted = false,
  raw = false,
  subtle = false,
  value,
}: {
  icon?: ReactNode;
  indent?: boolean;
  label: string;
  labelMuted?: boolean;
  muted?: boolean;
  raw?: boolean;
  subtle?: boolean;
  value: number;
}) {
  return (
    <div className={cn("grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 leading-[18px]", subtle && "text-foreground/70", muted && "text-muted-foreground")}>
      <div className={cn("flex min-w-0 items-center gap-1.5 truncate", indent && "pl-3", labelMuted && "text-muted-foreground")}>
        {icon}
        <span className="truncate">{label}</span>
      </div>
      <div className={cn("text-right font-medium tabular-nums", muted ? "text-muted-foreground" : subtle ? "text-foreground/70" : "text-foreground/90")}>
        {raw ? formatNumber(value) : formatTokens(value)}
      </div>
    </div>
  );
}

function Ring({ current, estimate, tone }: { current: number; estimate: number; tone: "ok" | "warning" | "danger" }) {
  const radius = 8;
  const circumference = 2 * Math.PI * radius;
  const currentOffset = circumference - (Math.min(100, current) / 100) * circumference;
  const estimateOffset = circumference - (Math.min(100, estimate) / 100) * circumference;
  const showEstimate = estimate > current + 0.3;
  return (
    <svg aria-hidden="true" className="size-[18px] -rotate-90" viewBox="0 0 24 24">
      <circle className="stroke-muted-foreground/30" cx="12" cy="12" fill="none" r={radius} strokeWidth="2.4" />
      {showEstimate ? (
        <circle
          className={cn("opacity-45 transition-[stroke-dashoffset]", strokeClass(tone))}
          cx="12"
          cy="12"
          fill="none"
          r={radius}
          strokeDasharray={circumference}
          strokeDashoffset={estimateOffset}
          strokeLinecap="round"
          strokeWidth="2.4"
        />
      ) : null}
      <circle
        className={cn("transition-[stroke-dashoffset]", strokeClass(tone))}
        cx="12"
        cy="12"
        fill="none"
        r={radius}
        strokeDasharray={circumference}
        strokeDashoffset={currentOffset}
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
  return "stroke-emerald-500";
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

function formatNumber(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

function formatTokens(value: number) {
  const abs = Math.abs(value);
  if (abs >= 1_000_000_000) {
    return `${formatFixedUnit(value / 1_000_000_000)}B`;
  }
  if (abs >= 1_000_000) {
    return `${formatFixedUnit(value / 1_000_000)}M`;
  }
  if (abs >= 1_000) {
    return `${formatKUnit(value / 1_000)}K`;
  }
  return formatNumber(value);
}

function formatFixedUnit(value: number) {
  return value.toFixed(2);
}

function formatKUnit(value: number) {
  return value.toFixed(value >= 10 ? 0 : 1).replace(/\.0$/, "");
}
