import { useSyncExternalStore } from "react";

// 工作区开合是纯 UI 偏好，仅在前端持久化。
const KEY = "pudding.workspaceOpen";
const LEGACY_KEY = "pudding.canvasOpen";

let open = (localStorage.getItem(KEY) ?? localStorage.getItem(LEGACY_KEY)) === "1";
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

export function setWorkspaceOpen(next: boolean) {
  open = next;
  localStorage.setItem(KEY, next ? "1" : "0");
  localStorage.removeItem(LEGACY_KEY);
  notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useWorkspaceOpen() {
  return useSyncExternalStore(subscribe, () => open, () => open);
}
