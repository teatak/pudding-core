import { useSyncExternalStore } from "react";

const STORAGE_KEY = "pudding.workspace.closedProjectTabs.v1";

let closedProjectTabs = readClosedProjectTabs();
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useProjectTabClosed(sessionID: string) {
  return useSyncExternalStore(
    subscribe,
    () => Boolean(sessionID && closedProjectTabs[sessionID]),
    () => false,
  );
}

export function setProjectTabClosed(sessionID: string, closed: boolean) {
  if (!sessionID || Boolean(closedProjectTabs[sessionID]) === closed) {
    return;
  }
  const next = { ...closedProjectTabs };
  if (closed) {
    next[sessionID] = true;
  } else {
    delete next[sessionID];
  }
  closedProjectTabs = next;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(closedProjectTabs));
  } catch {
    // Best-effort UI preference.
  }
  listeners.forEach((listener) => listener());
}

function readClosedProjectTabs(): Record<string, true> {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}") as Record<string, unknown>;
    return Object.fromEntries(
      Object.entries(parsed).filter(([sessionID, closed]) => Boolean(sessionID) && closed === true),
    ) as Record<string, true>;
  } catch {
    return {};
  }
}
