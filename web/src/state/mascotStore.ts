import { useSyncExternalStore } from "react";

const STORAGE_KEY = "pudding.mascotVisible";

let visible = localStorage.getItem(STORAGE_KEY) === "1";
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function setMascotVisible(next: boolean) {
  if (visible === next) {
    return;
  }
  visible = next;
  localStorage.setItem(STORAGE_KEY, next ? "1" : "0");
  listeners.forEach((listener) => listener());
}

export function useMascotVisible() {
  return useSyncExternalStore(subscribe, () => visible, () => visible);
}
