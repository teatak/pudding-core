type PanelLayout = Record<string, number>

type PanelLayoutOptions = {
  minPercent?: number
  maxPercent?: number
}

function parsePanelLayoutValue(raw: string | null): unknown {
  try {
    return JSON.parse(raw || "")
  } catch {
    return undefined
  }
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max)
}

function normalizePanelLayout(
  candidate: unknown,
  fallback: PanelLayout,
  options: PanelLayoutOptions = {},
): PanelLayout {
  const keys = Object.keys(fallback)
  const min = options.minPercent ?? 1
  const max = options.maxPercent ?? 99
  const values = keys.map((key) => {
    const value =
      candidate && typeof candidate === "object" && !Array.isArray(candidate)
        ? (candidate as PanelLayout)[key]
        : undefined
    const next = typeof value === "number" && Number.isFinite(value) && value > 0 ? value : fallback[key]
    return clamp(next, min, max)
  })
  const total = values.reduce((sum, value) => sum + value, 0)
  if (!Number.isFinite(total) || total <= 0) {
    return fallback
  }
  return Object.fromEntries(keys.map((key, index) => [key, (values[index] / total) * 100]))
}

export function readPanelPixelSize(key: string, fallback: number, min: number, max: number) {
  const saved = Number(localStorage.getItem(key))
  return Number.isFinite(saved) && saved >= min && saved <= max ? saved : fallback
}

// 导航 rail 按"像素宽"持久化:它是导航面板,不是核心工作区比例的一部分。
// 仅在值变化时写,避免 onResize 高频回调期间无谓的 localStorage 写入。
export function savePanelPixelSize(key: string, px: number) {
  if (!Number.isFinite(px)) {
    return
  }
  const next = String(Math.round(px))
  if (localStorage.getItem(key) !== next) {
    localStorage.setItem(key, next)
  }
}

export function readOptionalPanelLayout(key: string) {
  const parsed = parsePanelLayoutValue(localStorage.getItem(key))
  return parsed && typeof parsed === "object" && !Array.isArray(parsed)
    ? Object.fromEntries(
        Object.entries(parsed).filter((entry): entry is [string, number] => {
          return typeof entry[1] === "number" && Number.isFinite(entry[1]) && entry[1] > 0
        }),
      )
    : undefined
}

export function readPanelLayout(
  key: string,
  fallback: PanelLayout,
  options: PanelLayoutOptions = {},
): PanelLayout {
  const parsed = parsePanelLayoutValue(localStorage.getItem(key))
  if (typeof parsed === "number" && Number.isFinite(parsed) && parsed > 0 && parsed < 1) {
    const keys = Object.keys(fallback)
    if (keys.length === 2) {
      return normalizePanelLayout({ [keys[0]]: parsed * 100, [keys[1]]: (1 - parsed) * 100 }, fallback, options)
    }
  }
  return normalizePanelLayout(parsed, fallback, options)
}

export function savePanelLayout(key: string, layout: PanelLayout) {
  const next = JSON.stringify(layout)
  if (localStorage.getItem(key) !== next) {
    localStorage.setItem(key, next)
  }
}
