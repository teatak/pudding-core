import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  XAxis,
  YAxis,
  type TooltipContentProps,
  type TooltipValueType,
} from "recharts";

import { getDailyUsage, type DailyUsageStat } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  type ChartConfig,
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { SETTINGS_CONTENT_CLASS } from "./shared";

const USAGE_QUERY_DAYS = 370;
const USAGE_SUMMARY_DAYS = 365;

export function UsageSettings({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const usageQuery = useQuery({
    queryKey: queryKeys.usageDaily(USAGE_QUERY_DAYS),
    queryFn: () => getDailyUsage(token, USAGE_QUERY_DAYS),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const days = usageQuery.data?.days || [];
  const summary = useMemo(() => summarizeDailyUsage(days.slice(-USAGE_SUMMARY_DAYS)), [days]);
  const dailyChartData = useMemo(() => dailyUsageChartData(days, locale), [days, locale]);
  const monthlyChartData = useMemo(() => monthlyUsageChartData(days, locale), [days, locale]);
  const hasUsage = summary.requestCount > 0 || summary.totalTokens > 0;

  return (
    <div className={cn(SETTINGS_CONTENT_CLASS, "gap-6")}>
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
            <UsageOverview locale={locale} summary={summary} t={t} />
            <UsageBarChartCard
              dailyData={dailyChartData}
              locale={locale}
              monthlyData={monthlyChartData}
              t={t}
            />
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
type UsageMetricData = { detail?: string; label: string; value: string };

function UsageOverview({ locale, summary, t }: { locale: string; summary: UsageSummary; t: Translate }) {
  const inputTokens = summary.inputUncachedTokens + summary.inputCachedTokens + summary.cacheCreationTokens;
  const outputTokens = summary.outputContentTokens + summary.outputReasoningTokens;
  const primaryMetrics: UsageMetricData[] = [
    { label: t("settings.usage.totalTokens"), value: formatUsageTokens(summary.totalTokens, locale) },
    { label: t("settings.usage.requests"), value: formatNumber(summary.requestCount, locale) },
    { label: t("settings.usage.activeDays"), value: formatNumber(summary.activeDays, locale) },
    {
      detail: summary.peakDate ? formatUsageDateLabel(summary.peakDate, locale) : undefined,
      label: t("settings.usage.peakDay"),
      value: formatUsageTokens(summary.peakTokens, locale),
    },
  ];
  const insightMetrics: UsageMetricData[] = [
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
  const metrics = [...primaryMetrics, ...insightMetrics];

  return (
    <section className="grid gap-3">
      <h3 className="text-sm font-medium">{t("settings.usage.overview")}</h3>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {metrics.map((metric, index) => (
          <Card key={metric.label} className="min-w-0 gap-0 py-3" size="sm">
            <CardContent>
              <div
                className={cn(
                  "truncate font-semibold tabular-nums tracking-tight text-foreground",
                  index < primaryMetrics.length ? "text-2xl" : "text-lg",
                )}
              >
                {metric.value}
              </div>
              <div className="mt-0.5 text-xs text-muted-foreground">{metric.label}</div>
              {metric.detail ? <div className="mt-1 truncate text-[11px] text-muted-foreground/75">{metric.detail}</div> : null}
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}

type UsageChartDatum = {
  cacheHitTokens: number;
  cacheMissTokens: number;
  contentOutputTokens: number;
  key: string;
  label: string;
  reasoningOutputTokens: number;
  requestCount: number;
  tooltipLabel: string;
  totalTokens: number;
};

type UsageSeriesKey = "cacheHitTokens" | "cacheMissTokens" | "contentOutputTokens" | "reasoningOutputTokens";

function usageChartSeries(t: Translate): Array<{ color: string; key: UsageSeriesKey; label: string }> {
  return [
    { color: "var(--chart-1)", key: "cacheHitTokens", label: t("settings.usage.cacheHit") },
    { color: "var(--chart-2)", key: "cacheMissTokens", label: t("settings.usage.cacheMiss") },
    { color: "var(--chart-4)", key: "contentOutputTokens", label: t("settings.usage.outputContent") },
    { color: "var(--chart-5)", key: "reasoningOutputTokens", label: t("settings.usage.outputReasoning") },
  ];
}

function UsageBarChartCard({
  dailyData,
  locale,
  monthlyData,
  t,
}: {
  dailyData: UsageChartDatum[];
  locale: string;
  monthlyData: UsageChartDatum[];
  t: Translate;
}) {
  const [period, setPeriod] = useState<"months" | "days">("days");
  const data = period === "months" ? monthlyData : dailyData;
  const description = t(period === "months" ? "settings.usage.trend12MonthsDesc" : "settings.usage.trend30DaysDesc");
  const xAxisInterval = period === "months" ? 0 : 4;
  const series = usageChartSeries(t);
  const stackedSeries = [...series].reverse();
  const chartConfig = Object.fromEntries(
    series.map((item) => [item.key, { color: item.color, label: item.label }]),
  ) as ChartConfig;
  const hasPeriodUsage = data.some((item) => item.totalTokens > 0 || item.requestCount > 0);

  return (
    <Tabs value={period} onValueChange={(value) => setPeriod(value === "days" ? "days" : "months")}>
      <Card className="min-w-0" size="sm">
        <CardHeader>
          <CardTitle>{t("settings.usage.tokenTrend")}</CardTitle>
          <CardDescription>{description}</CardDescription>
          <CardAction>
            <TabsList aria-label={t("settings.usage.period")}>
              <TabsTrigger className="px-3" value="months">{t("settings.usage.period12Months")}</TabsTrigger>
              <TabsTrigger className="px-3" value="days">{t("settings.usage.period30Days")}</TabsTrigger>
            </TabsList>
          </CardAction>
        </CardHeader>
        <CardContent className="grid gap-3">
          <div className="flex flex-wrap gap-x-4 gap-y-2 text-xs text-muted-foreground">
            {series.map((item) => (
              <span key={item.key} className="inline-flex items-center gap-1.5">
                <span className="size-2 rounded-[2px]" style={{ backgroundColor: item.color }} />
                {item.label}
              </span>
            ))}
          </div>
          {hasPeriodUsage ? (
            <ChartContainer
              aria-label={`${t("settings.usage.tokenTrend")} · ${description}`}
              className="h-60 w-full aspect-auto [&_.recharts-surface]:outline-none [&_.recharts-wrapper]:outline-none"
              config={chartConfig}
              initialDimension={{ height: 240, width: 760 }}
              role="img"
              onFocusCapture={(event) => {
                const target = event.target;
                const blur = "blur" in target ? target.blur : undefined;
                if (target !== event.currentTarget && typeof blur === "function") {
                  blur.call(target);
                }
              }}
              onMouseDownCapture={(event) => event.preventDefault()}
            >
              <BarChart accessibilityLayer={false} barCategoryGap="20%" data={data} margin={{ bottom: 0, left: 0, right: 4, top: 8 }}>
                <CartesianGrid vertical={false} />
                <XAxis
                  axisLine={false}
                  dataKey="label"
                  fontSize={10}
                  interval={xAxisInterval}
                  minTickGap={12}
                  tickLine={false}
                  tickMargin={10}
                />
                <YAxis
                  axisLine={false}
                  fontSize={10}
                  tickFormatter={(value: number) => formatUsageTokens(value, locale)}
                  tickLine={false}
                  width={48}
                />
                <ChartTooltip
                  content={<UsageChartTooltip locale={locale} t={t} />}
                  cursor={false}
                  isAnimationActive={false}
                />
                {stackedSeries.map((item) => (
                  <Bar
                    key={item.key}
                    activeBar={false}
                    dataKey={item.key}
                    fill={`var(--color-${item.key})`}
                    focusable="false"
                    isAnimationActive={false}
                    maxBarSize={48}
                    name={item.label}
                    radius={item.key === "cacheHitTokens" ? [4, 4, 0, 0] : 0}
                    stackId="tokens"
                  />
                ))}
              </BarChart>
            </ChartContainer>
          ) : (
            <div className="grid h-60 place-items-center text-sm text-muted-foreground">{t("settings.usage.noPeriodData")}</div>
          )}
        </CardContent>
      </Card>
    </Tabs>
  );
}

type UsageChartTooltipProps = Partial<TooltipContentProps<TooltipValueType, string>> & {
  locale: string;
  t: Translate;
};

function UsageChartTooltip({ active, locale, payload, t }: UsageChartTooltipProps) {
  if (!active || !payload?.length) {
    return null;
  }
  const datum = payload[0]?.payload as UsageChartDatum | undefined;
  if (!datum) {
    return null;
  }
  const series = usageChartSeries(t);

  return (
    <div className="grid min-w-56 gap-2 rounded-xl border border-border/60 bg-popover p-3 text-xs text-popover-foreground shadow-xl">
      <div className="text-sm font-semibold">{datum.tooltipLabel}</div>
      <div className="h-px bg-border/70" />
      <div className="grid gap-2">
        {series.map((item) => (
          <div key={item.key} className="flex items-center gap-2">
            <span className="size-2.5 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
            <span className="min-w-0 flex-1 text-muted-foreground">{item.label}</span>
            <span className="shrink-0 font-mono font-medium tabular-nums">
              {formatUsageTokens(datum[item.key], locale)}
            </span>
          </div>
        ))}
      </div>
      <div className="flex items-center justify-between gap-4 border-t border-border/70 pt-2">
        <span className="text-muted-foreground">{t("settings.usage.totalTokens")}</span>
        <span className="font-mono font-semibold tabular-nums">{formatUsageTokens(datum.totalTokens, locale)}</span>
      </div>
    </div>
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

function dailyUsageChartData(days: DailyUsageStat[], locale: string): UsageChartDatum[] {
  const labelFormatter = new Intl.DateTimeFormat(locale, { day: "numeric", month: "numeric" });
  const tooltipFormatter = new Intl.DateTimeFormat(locale, { day: "2-digit", month: "2-digit", year: "numeric" });
  return days.slice(-30).map((day) => {
    const date = parseUsageDate(day.date);
    return usageChartDatum(day, day.date, labelFormatter.format(date), tooltipFormatter.format(date));
  });
}

function monthlyUsageChartData(days: DailyUsageStat[], locale: string): UsageChartDatum[] {
  const latestDateValue = days[days.length - 1]?.date;
  if (!latestDateValue) {
    return [];
  }
  const latestDate = parseUsageDate(latestDateValue);
  const labelFormatter = new Intl.DateTimeFormat(locale, { month: "short" });
  const tooltipFormatter = new Intl.DateTimeFormat(locale, { month: "long", year: "numeric" });
  const monthly = Array.from({ length: 12 }, (_, index) => {
    const date = new Date(latestDate.getFullYear(), latestDate.getMonth() - (11 - index), 1);
    return {
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      contentOutputTokens: 0,
      key: usageMonthKey(date),
      label: labelFormatter.format(date),
      reasoningOutputTokens: 0,
      requestCount: 0,
      tooltipLabel: tooltipFormatter.format(date),
      totalTokens: 0,
    } satisfies UsageChartDatum;
  });
  const byMonth = new Map(monthly.map((month) => [month.key, month]));
  for (const day of days) {
    const month = byMonth.get(day.date.slice(0, 7));
    if (!month) {
      continue;
    }
    month.cacheHitTokens += day.inputCachedTokens;
    month.cacheMissTokens += day.inputUncachedTokens + day.cacheCreationTokens;
    month.contentOutputTokens += day.outputContentTokens;
    month.reasoningOutputTokens += day.outputReasoningTokens;
    month.requestCount += day.requestCount;
    month.totalTokens += day.totalTokens;
  }
  return monthly;
}

function usageChartDatum(day: DailyUsageStat, key: string, label: string, tooltipLabel: string): UsageChartDatum {
  return {
    cacheHitTokens: day.inputCachedTokens,
    cacheMissTokens: day.inputUncachedTokens + day.cacheCreationTokens,
    contentOutputTokens: day.outputContentTokens,
    key,
    label,
    reasoningOutputTokens: day.outputReasoningTokens,
    requestCount: day.requestCount,
    tooltipLabel,
    totalTokens: day.totalTokens,
  };
}

function usageMonthKey(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}`;
}

function formatUsageDateLabel(date: string, locale: string) {
  return new Intl.DateTimeFormat(locale, { day: "numeric", month: "long" }).format(parseUsageDate(date));
}

function parseUsageDate(date: string) {
  return new Date(`${date}T00:00:00`);
}

function ratio(value: number, total: number) {
  return total > 0 ? value / total : 0;
}

function formatNumber(value: number, locale: string, maximumFractionDigits = 0) {
  return new Intl.NumberFormat(locale, {
    maximumFractionDigits,
    useGrouping: !locale.startsWith("zh"),
  }).format(value);
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
  const units = usageTokenUnits(locale);
  let unitIndex = units.findIndex((unit) => abs >= unit.divisor);
  if (unitIndex < 0) {
    return formatNumber(value, locale);
  }
  while (unitIndex >= 0) {
    const unit = units[unitIndex];
    const scaled = value / unit.divisor;
    const scaledAbs = Math.abs(scaled);
    const maximumFractionDigits = scaledAbs >= 100 ? 0 : scaledAbs >= 10 ? 1 : 2;
    const rounded = Number(scaled.toFixed(maximumFractionDigits));
    const promotionThreshold = unitIndex > 0
      ? units[unitIndex - 1].divisor / unit.divisor
      : Number.POSITIVE_INFINITY;
    if (Math.abs(rounded) >= promotionThreshold) {
      unitIndex -= 1;
      continue;
    }
    return `${formatNumber(scaled, locale, maximumFractionDigits)}${unit.suffix}`;
  }
  return formatNumber(value, locale);
}

function usageTokenUnits(locale: string) {
  if (locale.startsWith("zh")) {
    const traditional = locale === "zh-TW";
    return [
      { divisor: 100_000_000, suffix: traditional ? "億" : "亿" },
      { divisor: 10_000, suffix: traditional ? "萬" : "万" },
    ];
  }
  return [
    { divisor: 1_000_000_000, suffix: "B" },
    { divisor: 1_000_000, suffix: "M" },
    { divisor: 1_000, suffix: "K" },
  ];
}
