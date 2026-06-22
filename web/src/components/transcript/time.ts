import { useEffect, useState } from "react";

import type { Locale } from "@/i18n";

export function useElapsedDuration(startedAt: string | undefined, locale: Locale) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!startedAt) {
      return;
    }
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [startedAt]);
  if (!startedAt) {
    return "";
  }
  const start = Date.parse(startedAt);
  if (!Number.isFinite(start)) {
    return "";
  }
  return formatDurationMs(Math.max(0, now - start), locale);
}

export function formatDurationBetween(startedAt: string, endedAt: string, locale: Locale) {
  const start = Date.parse(startedAt);
  const end = Date.parse(endedAt);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) {
    return "";
  }
  return formatDurationMs(end - start, locale);
}

function formatDurationMs(ms: number, locale: Locale) {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  if (totalSeconds <= 0) {
    return locale === "en" ? "less than 1 sec" : "不到1秒";
  }
  const seconds = totalSeconds % 60;
  const totalMinutes = Math.floor(totalSeconds / 60);
  const minutes = totalMinutes % 60;
  const hours = Math.floor(totalSeconds / 3600);
  if (locale === "en") {
    const parts = [];
    if (hours > 0) {
      parts.push(`${hours} hr`);
    }
    if (totalMinutes > 0) {
      parts.push(`${hours > 0 ? minutes : totalMinutes} min`);
    }
    if (seconds > 0 || parts.length === 0) {
      parts.push(`${seconds} sec`);
    }
    return parts.join(" ");
  }
  const hourUnit = locale === "zh-TW" ? "小時" : "小时";
  if (hours > 0) {
    return `${hours}${hourUnit}${minutes ? `${minutes}分` : ""}${seconds ? `${seconds}秒` : ""}`;
  }
  if (totalMinutes > 0) {
    return `${totalMinutes}分${seconds ? `${seconds}秒` : ""}`;
  }
  return `${seconds}秒`;
}
