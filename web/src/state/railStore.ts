import { useSyncExternalStore } from "react";

import { useIsMobile } from "@/hooks/use-mobile";

// 用户偏好持久化；窄窗口仅临时强制折叠，不覆盖该偏好。
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
  const narrowWindow = useIsMobile();
  return preferredCollapsed || forcedCollapsed || narrowWindow;
}

export function useRailResponsiveCollapsed() {
  return useSyncExternalStore(
    subscribe,
    () => responsiveCollapsed,
    () => responsiveCollapsed,
  );
}
