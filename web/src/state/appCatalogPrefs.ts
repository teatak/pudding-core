import { useSyncExternalStore } from "react";

const SHOW_PREVIEW_KEY = "pudding.apps.showPreviewVersions";

let showPreviewAppVersions = localStorage.getItem(SHOW_PREVIEW_KEY) === "1";
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

export function setShowPreviewAppVersions(next: boolean) {
  showPreviewAppVersions = next;
  localStorage.setItem(SHOW_PREVIEW_KEY, next ? "1" : "0");
  notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useShowPreviewAppVersions() {
  return useSyncExternalStore(
    subscribe,
    () => showPreviewAppVersions,
    () => showPreviewAppVersions,
  );
}
