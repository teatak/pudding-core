type PanelLayout = Record<string, number>

function parsePanelLayout(raw: string | null): PanelLayout | undefined {
  try {
    const parsed = JSON.parse(raw || "")
    if (!parsed || typeof parsed !== "object") {
      return undefined
    }
    const entries = Object.entries(parsed).filter((entry): entry is [string, number] => {
      return typeof entry[1] === "number" && Number.isFinite(entry[1]) && entry[1] > 0
    })
    return entries.length > 0 ? Object.fromEntries(entries) : undefined
  } catch {
    return undefined
  }
}

export function readPanelPixelSize(key: string, fallback: number, min: number, max: number) {
  const saved = Number(localStorage.getItem(key))
  return Number.isFinite(saved) && saved >= min && saved <= max ? saved : fallback
}

// 侧栏(rail / canvas)按"像素宽"持久化:会话是弹性 remainder,两侧定宽,所以只存
// 两侧各自的像素宽,换窗口宽 / 换组合时都不会像百分比那样还原成错的宽度。
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
  return parsePanelLayout(localStorage.getItem(key))
}

export function readPanelLayout(key: string, fallback: PanelLayout): PanelLayout {
  try {
    const parsed = JSON.parse(localStorage.getItem(key) || "")
    if (typeof parsed === "number" && Number.isFinite(parsed) && parsed > 0 && parsed < 1) {
      const keys = Object.keys(fallback)
      if (keys.length === 2) {
        return { [keys[0]]: parsed * 100, [keys[1]]: (1 - parsed) * 100 }
      }
    }
    return parsePanelLayout(localStorage.getItem(key)) || fallback
  } catch {
    return fallback
  }
}

export function savePanelLayout(key: string, layout: PanelLayout) {
  localStorage.setItem(key, JSON.stringify(layout))
}
