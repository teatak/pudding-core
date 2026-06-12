// 相对时间:Intl.RelativeTimeFormat 原生承担多语言,组件不维护文案。
export function formatRelative(iso: string, locale: string) {
  const then = new Date(iso).getTime();
  const diffSec = Math.round((then - Date.now()) / 1000);
  const abs = Math.abs(diffSec);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (abs < 60) {
    return rtf.format(0, "second"); // "现在 / now"
  }
  if (abs < 3600) {
    return rtf.format(Math.trunc(diffSec / 60), "minute");
  }
  if (abs < 86400) {
    return rtf.format(Math.trunc(diffSec / 3600), "hour");
  }
  if (abs < 86400 * 7) {
    return rtf.format(Math.trunc(diffSec / 86400), "day");
  }
  return new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(then);
}

// 时钟时间(HH:mm),消息 meta 用
export function formatClock(iso: string) {
  const date = new Date(iso);
  if (Number.isNaN(date.valueOf())) {
    return "";
  }
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(date);
}
