import { useSyncExternalStore } from "react";

// rail 折叠态:SessionRail(渲染形态)与 ChatPane(header 让位)共同消费。
// 有效折叠 = 用户偏好 || 左工作区宽度强制。
const KEY = "pudding.railCollapsed";

let pref = localStorage.getItem(KEY) === "1";
let layoutForcedCollapsed = false;
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

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
  return layoutForcedCollapsed;
}

export function useRailCollapsed() {
  return useSyncExternalStore(
    subscribe,
    () => pref || isForcedCollapsed(),
    () => pref || isForcedCollapsed(),
  );
}

// 布局宽度强制折叠:此时"展开"不可用,触发器只负责开合 popover
export function useRailForcedCollapsed() {
  return useSyncExternalStore(
    subscribe,
    isForcedCollapsed,
    isForcedCollapsed,
  );
}
