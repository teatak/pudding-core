import { useSyncExternalStore } from "react";

// canvas 栏开合(docs/design.md 2.4):纯 UI 偏好,localStorage 持久。
// 本版只有布局插槽与空态,内容(小组件/产物/工具大块输出)后续解封。
const KEY = "pudding.canvasOpen";

let open = localStorage.getItem(KEY) === "1";
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

export function setCanvasOpen(next: boolean) {
  open = next;
  localStorage.setItem(KEY, next ? "1" : "0");
  notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useCanvasOpen() {
  return useSyncExternalStore(subscribe, () => open, () => open);
}
