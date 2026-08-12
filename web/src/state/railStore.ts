import { useSyncExternalStore } from "react";

// 用户偏好持久化；响应式折叠由 App 根据「左栏 + Chat」区域宽度派生。
const KEY = "pudding.railCollapsed";

let pref = localStorage.getItem(KEY) === "1";
let responsiveCollapsed = false;
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

export function setRailCollapsed(next: boolean) {
  pref = next;
  localStorage.setItem(KEY, next ? "1" : "0");
  notify();
}

export function getRailCollapsedPreference() {
  return pref;
}

export function setRailResponsiveCollapsed(next: boolean) {
  if (responsiveCollapsed === next) {
    return;
  }
  responsiveCollapsed = next;
  notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useRailCollapsed() {
  const preferredCollapsed = useSyncExternalStore(
    subscribe,
    () => pref,
    () => pref,
  );
  const forcedCollapsed = useSyncExternalStore(
    subscribe,
    () => responsiveCollapsed,
    () => responsiveCollapsed,
  );
  return preferredCollapsed || forcedCollapsed;
}

export function useRailResponsiveCollapsed() {
  return useSyncExternalStore(
    subscribe,
    () => responsiveCollapsed,
    () => responsiveCollapsed,
  );
}
