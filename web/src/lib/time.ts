// Session 列表要极短:中文 1分/1时/1天/1周,英文 now/1m/1h/1d/1w。
export function formatRelative(iso: string, locale: string) {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return "";
  }

  const elapsedSec = Math.max(0, Math.floor((Date.now() - then) / 1000));
  const units = getShortRelativeUnits(locale);
  if (elapsedSec < 60) {
    return units.now;
  }
  if (elapsedSec < 3600) {
    return `${Math.max(1, Math.floor(elapsedSec / 60))}${units.minute}`;
  }
  if (elapsedSec < 86400) {
    return `${Math.floor(elapsedSec / 3600)}${units.hour}`;
  }
  if (elapsedSec < 86400 * 7) {
    return `${Math.floor(elapsedSec / 86400)}${units.day}`;
  }
  if (elapsedSec < 86400 * 28) {
    return `${Math.floor(elapsedSec / (86400 * 7))}${units.week}`;
  }
  return new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(then);
}

function getShortRelativeUnits(locale: string) {
  if (locale.startsWith("zh-TW")) {
    return { now: "現在", minute: "分", hour: "時", day: "天", week: "週" };
  }
  if (locale.startsWith("zh")) {
    return { now: "现在", minute: "分", hour: "时", day: "天", week: "周" };
  }
  return { now: "now", minute: "m", hour: "h", day: "d", week: "w" };
}

// 时钟时间(HH:mm),消息 meta 用
export function formatClock(iso: string) {
  const date = new Date(iso);
  if (Number.isNaN(date.valueOf())) {
    return "";
  }
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(date);
}
