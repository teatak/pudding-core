import { useSyncExternalStore } from "react";

// rail 折叠态:SessionList(渲染形态)与 ChatPane(header 让位)共同消费,
// localStorage 持久化(UI 偏好)。
const KEY = "pudding.railCollapsed";

let collapsed = localStorage.getItem(KEY) === "1";
const listeners = new Set<() => void>();

export function setRailCollapsed(next: boolean) {
  collapsed = next;
  localStorage.setItem(KEY, next ? "1" : "0");
  listeners.forEach((listener) => listener());
}

export function useRailCollapsed() {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => collapsed,
    () => collapsed,
  );
}
