import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, type CSSProperties } from "react";
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts";

import { getDailyUsage, type DailyUsageStat } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { SETTINGS_CONTENT_CLASS } from "./shared";

export function UsageSettings({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const usageQuery = useQuery({
    queryKey: queryKeys.usageDaily(365),
    queryFn: () => getDailyUsage(token, 365),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const days = usageQuery.data?.days || [];
  const summary = useMemo(() => summarizeDailyUsage(days), [days]);
  const hasUsage = summary.requestCount > 0 || summary.totalTokens > 0;

  return (
    <div className={cn(SETTINGS_CONTENT_CLASS, "gap-6 pt-2")}>
      <section className="grid min-w-0 gap-5">
        {usageQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription className="grid gap-2">
              <span>{t("settings.usage.loadFailed")}</span>
            </AlertDescription>
          </Alert>
        ) : null}
        {usageQuery.isLoading && !usageQuery.data ? <UsageSkeleton /> : null}
        {usageQuery.data && hasUsage ? (
          <>
            <div className="grid grid-cols-2 gap-y-4 border-b pb-5 sm:grid-cols-4 sm:divide-x sm:divide-border/70">
              <UsageMetric label={t("settings.usage.totalTokens")} value={formatUsageTokens(summary.totalTokens, locale)} />
              <UsageMetric label={t("settings.usage.requests")} value={formatNumber(summary.requestCount, locale)} />
              <UsageMetric label={t("settings.usage.activeDays")} value={formatNumber(summary.activeDays, locale)} />
              <UsageMetric
                detail={summary.peakDate ? formatUsageDateLabel(summary.peakDate, locale) : undefined}
                label={t("settings.usage.peakDay")}
                value={formatUsageTokens(summary.peakTokens, locale)}
              />
            </div>
            <UsageHeatmap days={days} locale={locale} t={t} />
            <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1.45fr)_minmax(18rem,0.85fr)]">
              <UsageTrend days={days} locale={locale} t={t} />
              <UsageBreakdown locale={locale} summary={summary} t={t} />
            </div>
            <UsageInsights locale={locale} summary={summary} t={t} />
          </>
        ) : null}
        {usageQuery.data && !hasUsage ? (
          <div className="rounded-xl border border-dashed px-4 py-12 text-center text-sm text-muted-foreground">
            {t("settings.usage.noData")}
          </div>
        ) : null}
      </section>
    </div>
  );
}

type Translate = (key: string) => string;

function UsageMetric({ detail, label, value }: { detail?: string; label: string; value: string }) {
  return (
    <div className="min-w-0 px-3 first:pl-0 sm:px-5">
      <div className="text-2xl font-semibold tabular-nums tracking-tight text-foreground">{value}</div>
      <div className="mt-0.5 text-xs text-muted-foreground">{label}</div>
      {detail ? <div className="mt-1 truncate text-[11px] text-muted-foreground/75">{detail}</div> : null}
    </div>
  );
}

function UsageHeatmap({
  days,
  locale,
  t,
}: {
  days: DailyUsageStat[];
  locale: string;
  t: Translate;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollLeft = el.scrollWidth;
    }
  }, [days.length]);

  if (days.length === 0) {
    return <div className="text-sm text-muted-foreground">{t("settings.usage.noData")}</div>;
  }
  const leading = parseUsageDate(days[0]?.date).getDay();
  const cells: Array<DailyUsageStat | null> = [...Array.from({ length: leading }, () => null), ...days];
  const weekCount = Math.ceil(cells.length / 7);
  const heatThresholds = usageHeatThresholds(days);
  const monthLabels = usageMonthLabels(days, leading, locale);
  const heatmapGapRem = Math.max(0, weekCount - 1) * 0.25;

  return (
    <div
      ref={scrollRef}
      className="grid min-w-0 gap-3 overflow-x-auto pb-1"
      style={
        {
          "--usage-heat-cell": `clamp(0.4rem, calc((100cqw - ${heatmapGapRem}rem) / ${weekCount}), 0.75rem)`,
        } as CSSProperties
      }
    >
      <div className="mx-auto w-max max-w-full">
        <div className="mb-3 text-sm font-medium text-muted-foreground">{t("settings.usage.last365Days")}</div>
        <div className="w-max max-w-full">
          <TooltipProvider>
            <div
              className="grid grid-flow-col grid-rows-7 gap-1"
              style={{ gridAutoColumns: "var(--usage-heat-cell)", gridTemplateRows: "repeat(7, var(--usage-heat-cell))" }}
            >
              {cells.map((day, index) =>
                day ? (
                  <UsageHeatmapCell
                    key={day.date}
                    day={day}
                    heatThresholds={heatThresholds}
                    locale={locale}
                    t={t}
                  />
                ) : (
                  <div key={`empty-${index}`} className="size-(--usage-heat-cell)" />
                ),
              )}
            </div>
          </TooltipProvider>
          <div
            className="mt-2.5 grid gap-1 text-xs text-muted-foreground"
            style={{ gridTemplateColumns: `repeat(${weekCount}, var(--usage-heat-cell))` }}
          >
            {monthLabels.map((label) => (
              <div key={`${label.month}-${label.column}`} className="whitespace-nowrap" style={{ gridColumn: `${label.column + 1} / span 4` }}>
                {label.month}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function UsageHeatmapCell({
  day,
  heatThresholds,
  locale,
  t,
}: {
  day: DailyUsageStat;
  heatThresholds: number[];
  locale: string;
  t: Translate;
}) {
  if (day.totalTokens <= 0 && day.requestCount <= 0) {
    return <div className={cn("size-(--usage-heat-cell) rounded-[2px]", usageHeatClass(day.totalTokens, heatThresholds, false))} />;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          aria-label={usageDayTitle(day, t, locale)}
          className={cn("size-(--usage-heat-cell) rounded-[2px] transition-colors", usageHeatClass(day.totalTokens, heatThresholds, true))}
          type="button"
        />
      </TooltipTrigger>
      <TooltipContent
        className="grid gap-1"
        side="top"
        sideOffset={8}
      >
        <div className="font-medium">{formatUsageDateLabel(day.date, locale)}</div>
        <div className="tabular-nums">{`${t("usage.totalTokens")} ${formatUsageTokens(day.totalTokens, locale)}`}</div>
        <div className="tabular-nums">{`${t("usage.requests")} ${formatNumber(day.requestCount, locale)}`}</div>
        <div className="tabular-nums">{`${t("usage.inputTotal")} ${formatUsageTokens(dailyInputTokens(day), locale)}`}</div>
        <div className="tabular-nums">{`${t("usage.outputTotal")} ${formatUsageTokens(dailyOutputTokens(day), locale)}`}</div>
      </TooltipContent>
    </Tooltip>
  );
}

function UsageTrend({ days, locale, t }: { days: DailyUsageStat[]; locale: string; t: Translate }) {
  const chartData = days.slice(-30).map((day) => ({
    date: day.date,
    input: dailyInputTokens(day),
    label: new Intl.DateTimeFormat(locale, { day: "numeric", month: "numeric" }).format(parseUsageDate(day.date)),
    output: dailyOutputTokens(day),
  }));
  const chartConfig = {
    input: { color: "var(--chart-2)", label: t("settings.usage.inputTokens") },
    output: { color: "var(--chart-1)", label: t("settings.usage.outputTokens") },
  } satisfies ChartConfig;

  return (
    <Card className="min-w-0" size="sm">
      <CardHeader>
        <CardTitle>{t("settings.usage.trend30Days")}</CardTitle>
        <CardDescription>{t("settings.usage.trend30DaysDesc")}</CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer className="h-52 w-full aspect-auto" config={chartConfig} initialDimension={{ height: 208, width: 560 }}>
          <AreaChart data={chartData} margin={{ bottom: 0, left: 0, right: 4, top: 8 }}>
            <CartesianGrid vertical={false} />
            <XAxis
              axisLine={false}
              dataKey="label"
              fontSize={10}
              interval={5}
              minTickGap={16}
              tickLine={false}
              tickMargin={10}
            />
            <ChartTooltip
              content={(
                <ChartTooltipContent
                  indicator="line"
                  labelFormatter={(_label, payload) => {
                    const date = payload[0]?.payload?.date;
                    return typeof date === "string" ? formatUsageDateLabel(date, locale) : "";
                  }}
                />
              )}
            />
            <Area
              dataKey="input"
              fill="var(--color-input)"
              fillOpacity={0.16}
              name="input"
              stroke="var(--color-input)"
              strokeWidth={2}
              type="monotone"
            />
            <Area
              dataKey="output"
              fill="var(--color-output)"
              fillOpacity={0.16}
              name="output"
              stroke="var(--color-output)"
              strokeWidth={2}
              type="monotone"
            />
          </AreaChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}

function UsageBreakdown({
  locale,
  summary,
  t,
}: {
  locale: string;
  summary: UsageSummary;
  t: Translate;
}) {
  const items = [
    { color: "bg-chart-2", label: t("settings.usage.inputUncached"), value: summary.inputUncachedTokens },
    { color: "bg-chart-3", label: t("settings.usage.inputCached"), value: summary.inputCachedTokens },
    { color: "bg-chart-4", label: t("settings.usage.cacheCreation"), value: summary.cacheCreationTokens },
    { color: "bg-chart-1", label: t("settings.usage.outputContent"), value: summary.outputContentTokens },
    { color: "bg-chart-5", label: t("settings.usage.outputReasoning"), value: summary.outputReasoningTokens },
  ];

  return (
    <Card className="min-w-0" size="sm">
      <CardHeader>
        <CardTitle>{t("settings.usage.tokenBreakdown")}</CardTitle>
        <CardDescription>{t("settings.usage.tokenBreakdownDesc")}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="flex h-2.5 overflow-hidden rounded-full bg-muted" role="img" aria-label={t("settings.usage.tokenBreakdown")}>
          {items.map((item) => (
            <div
              key={item.label}
              className={cn("h-full", item.color)}
              style={{ width: `${ratio(item.value, summary.totalTokens) * 100}%` }}
            />
          ))}
        </div>
        <div className="grid gap-3">
          {items.map((item) => (
            <div key={item.label} className="flex min-w-0 items-center gap-2.5">
              <span className={cn("size-2 shrink-0 rounded-[2px]", item.color)} />
              <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{item.label}</span>
              <span className="shrink-0 text-xs tabular-nums text-foreground">{formatUsageTokens(item.value, locale)}</span>
              <span className="w-10 shrink-0 text-right text-[11px] tabular-nums text-muted-foreground">
                {formatPercentage(ratio(item.value, summary.totalTokens), locale)}
              </span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

function UsageInsights({ locale, summary, t }: { locale: string; summary: UsageSummary; t: Translate }) {
  const inputTokens = summary.inputUncachedTokens + summary.inputCachedTokens + summary.cacheCreationTokens;
  const outputTokens = summary.outputContentTokens + summary.outputReasoningTokens;
  const insights = [
    {
      label: t("settings.usage.averagePerRequest"),
      value: formatUsageTokens(Math.round(ratio(summary.totalTokens, summary.requestCount)), locale),
    },
    {
      label: t("settings.usage.cachedInputShare"),
      value: formatPercentage(ratio(summary.inputCachedTokens, inputTokens), locale),
    },
    {
      label: t("settings.usage.reasoningOutputShare"),
      value: formatPercentage(ratio(summary.outputReasoningTokens, outputTokens), locale),
    },
    {
      label: t("settings.usage.longestStreak"),
      value: formatDays(summary.longestStreak, locale, t),
    },
  ];

  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>{t("settings.usage.insights")}</CardTitle>
      </CardHeader>
      <CardContent className="grid grid-cols-2 gap-x-4 gap-y-5 sm:grid-cols-4">
        {insights.map((insight) => (
          <div key={insight.label} className="min-w-0">
            <div className="truncate text-lg font-semibold tabular-nums tracking-tight">{insight.value}</div>
            <div className="mt-0.5 text-xs text-muted-foreground">{insight.label}</div>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function UsageSkeleton() {
  return (
    <div className="grid gap-4">
      <div className="grid gap-4 sm:grid-cols-4">
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
      </div>
      <Skeleton className="h-44" />
    </div>
  );
}

type UsageSummary = ReturnType<typeof summarizeDailyUsage>;

function summarizeDailyUsage(days: DailyUsageStat[]) {
  let currentStreak = 0;
  let longestStreak = 0;
  return days.reduce(
    (summary, day) => {
      summary.totalTokens += day.totalTokens;
      summary.requestCount += day.requestCount;
      summary.inputUncachedTokens += day.inputUncachedTokens;
      summary.inputCachedTokens += day.inputCachedTokens;
      summary.cacheCreationTokens += day.cacheCreationTokens;
      summary.outputContentTokens += day.outputContentTokens;
      summary.outputReasoningTokens += day.outputReasoningTokens;
      if (day.totalTokens > 0 || day.requestCount > 0) {
        summary.activeDays += 1;
        currentStreak += 1;
        longestStreak = Math.max(longestStreak, currentStreak);
      } else {
        currentStreak = 0;
      }
      if (day.totalTokens > summary.peakTokens) {
        summary.peakTokens = day.totalTokens;
        summary.peakDate = day.date;
      }
      summary.longestStreak = longestStreak;
      return summary;
    },
    {
      activeDays: 0,
      cacheCreationTokens: 0,
      inputCachedTokens: 0,
      inputUncachedTokens: 0,
      longestStreak: 0,
      outputContentTokens: 0,
      outputReasoningTokens: 0,
      peakDate: "",
      peakTokens: 0,
      requestCount: 0,
      totalTokens: 0,
    },
  );
}

function usageMonthLabels(days: DailyUsageStat[], leading: number, locale: string) {
  const labels: Array<{ column: number; month: string }> = [];
  let previousMonth = "";
  for (let index = 0; index < days.length; index += 1) {
    const date = parseUsageDate(days[index].date);
    const monthKey = `${date.getFullYear()}-${date.getMonth()}`;
    if (monthKey === previousMonth) {
      continue;
    }
    previousMonth = monthKey;
    labels.push({
      column: Math.floor((leading + index) / 7),
      month: new Intl.DateTimeFormat(locale, { month: "short" }).format(date),
    });
  }
  const first = labels[0];
  const second = labels[1];
  if (first && second && second.column - first.column < 5) {
    return labels.slice(1);
  }
  return labels;
}

function usageHeatThresholds(days: DailyUsageStat[]) {
  const values = days
    .map((day) => day.totalTokens)
    .filter((tokens) => tokens > 0)
    .sort((left, right) => left - right);
  if (values.length === 0) {
    return [];
  }
  return [0.2, 0.4, 0.6, 0.8].map((ratio) => values[Math.floor((values.length - 1) * ratio)]);
}

function usageHeatClass(tokens: number, thresholds: number[], active: boolean) {
  if (!active) {
    return "bg-slate-100 dark:bg-muted/60";
  }
  if (tokens <= 0 || thresholds.length === 0) {
    return "bg-[#dbeafe] dark:bg-[#263746]";
  }
  if (tokens >= thresholds[3]) {
    return "bg-[#2563eb] dark:bg-[#75a8d5]";
  }
  if (tokens >= thresholds[2]) {
    return "bg-[#3b82f6] dark:bg-[#5f8caf]";
  }
  if (tokens >= thresholds[1]) {
    return "bg-[#60a5fa] dark:bg-[#466b86]";
  }
  if (tokens >= thresholds[0]) {
    return "bg-[#93c5fd] dark:bg-[#30485b]";
  }
  return "bg-[#dbeafe] dark:bg-[#263746]";
}

function usageDayTitle(day: DailyUsageStat, t: Translate, locale: string) {
  return `${day.date} · ${t("usage.totalTokens")} ${formatUsageTokens(day.totalTokens, locale)} · ${t("usage.requests")} ${formatNumber(day.requestCount, locale)} · ${t("usage.inputTotal")} ${formatUsageTokens(dailyInputTokens(day), locale)} · ${t("usage.outputTotal")} ${formatUsageTokens(dailyOutputTokens(day), locale)}`;
}

function formatUsageDateLabel(date: string, locale: string) {
  return new Intl.DateTimeFormat(locale, { day: "numeric", month: "long" }).format(parseUsageDate(date));
}

function parseUsageDate(date: string) {
  return new Date(`${date}T00:00:00`);
}


function dailyInputTokens(day: DailyUsageStat) {
  return day.inputUncachedTokens + day.inputCachedTokens + day.cacheCreationTokens;
}

function dailyOutputTokens(day: DailyUsageStat) {
  return day.outputContentTokens + day.outputReasoningTokens;
}

function ratio(value: number, total: number) {
  return total > 0 ? value / total : 0;
}

function formatNumber(value: number, locale: string, maximumFractionDigits = 0) {
  return new Intl.NumberFormat(locale, { maximumFractionDigits }).format(value);
}

function formatPercentage(value: number, locale: string) {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 1, style: "percent" }).format(value);
}

function formatDays(value: number, locale: string, t: Translate) {
  const number = formatNumber(value, locale);
  if (locale === "en") {
    return `${number} ${value === 1 ? t("settings.usage.day") : t("settings.usage.days")}`;
  }
  return `${number} ${t("settings.usage.days")}`;
}

function formatUsageTokens(value: number, locale: string) {
  const abs = Math.abs(value);
  if (abs >= 1_000_000_000) {
    return `${formatNumber(value / 1_000_000_000, locale, 2)}B`;
  }
  if (abs >= 1_000_000) {
    return `${formatNumber(value / 1_000_000, locale, 2)}M`;
  }
  if (abs >= 1_000) {
    return `${formatNumber(value / 1_000, locale, value >= 10_000 ? 0 : 1)}K`;
  }
  return formatNumber(value, locale);
}
