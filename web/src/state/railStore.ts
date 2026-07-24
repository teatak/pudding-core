import { useSyncExternalStore } from "react";

// rail 折叠态由用户偏好控制。右侧工作区内的布局变化不得影响 SessionRail。
const KEY = "pudding.railCollapsed";

let pref = localStorage.getItem(KEY) === "1";
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

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
  return useSyncExternalStore(
    subscribe,
    () => pref,
    () => pref,
  );
}
