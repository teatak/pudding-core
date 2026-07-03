import { useSyncExternalStore } from "react";

// rail 折叠态:SessionRail(渲染形态)与 ChatPane(header 让位)共同消费。
// 有效折叠 = 用户偏好 || 窄屏强制 || 左工作区宽度强制。
const KEY = "pudding.railCollapsed";

// 断点 720 < 壳窗口 MinWidth 760:保留媒体查询兜底,主要桌面折叠由
// App 根据左工作区实际宽度驱动。920 时代踩过坑:WKWebView 跨屏/HiDPI 下 CSS viewport 与窗口
// 逻辑宽有缩放偏差,1200pt 窗口可折算到 920 以下,失焦/移屏瞬间 rail
// 被强制收起,观感像"窗口出现大片黑背景"。
const media = window.matchMedia("(max-width: 720px)");
let pref = localStorage.getItem(KEY) === "1";
let layoutForcedCollapsed = false;
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

// 窄屏与否在读取时实时取 media.matches,不做事件驱动的增量缓存:
// WKWebView 在窗口失焦/挂起期间 change 事件不可靠(挂起瞬间误报变窄、
// 恢复后的回正事件被吞,缓存值卡死导致 rail 折叠不自愈)。
// 事件只负责触发重渲染;focus / pageshow 兜底,确保恢复时重读一次。
media.addEventListener("change", notify);
window.addEventListener("focus", notify);
window.addEventListener("pageshow", notify);

export function setRailCollapsed(next: boolean) {
  pref = next;
  localStorage.setItem(KEY, next ? "1" : "0");
  notify();
}

export function setRailLayoutForcedCollapsed(next: boolean) {
  if (layoutForcedCollapsed === next) {
    return;
  }
  layoutForcedCollapsed = next;
  notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function isForcedCollapsed() {
  return media.matches || layoutForcedCollapsed;
}

export function useRailCollapsed() {
  return useSyncExternalStore(
    subscribe,
    () => pref || isForcedCollapsed(),
    () => pref || isForcedCollapsed(),
  );
}

// 窄屏强制折叠:此时"展开"不可用,触发器只负责开合 popover
export function useRailForcedCollapsed() {
  return useSyncExternalStore(
    subscribe,
    isForcedCollapsed,
    isForcedCollapsed,
  );
}
