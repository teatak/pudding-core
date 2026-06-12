import { useSyncExternalStore } from "react";

// rail 折叠态:SessionList(渲染形态)与 ChatPane(header 让位)共同消费。
// 有效折叠 = 用户偏好 || 窄屏强制;窄屏下展开按钮退化为开关 popover。
const KEY = "pudding.railCollapsed";

const media = window.matchMedia("(max-width: 920px)");
let pref = localStorage.getItem(KEY) === "1";
let small = media.matches;
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

media.addEventListener("change", (event) => {
  small = event.matches;
  notify();
});

export function setRailCollapsed(next: boolean) {
  pref = next;
  localStorage.setItem(KEY, next ? "1" : "0");
  notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useRailCollapsed() {
  return useSyncExternalStore(subscribe, () => pref || small, () => pref || small);
}

// 窄屏强制折叠:此时"展开"不可用,触发器只负责开合 popover
export function useRailForcedCollapsed() {
  return useSyncExternalStore(subscribe, () => small, () => small);
}
