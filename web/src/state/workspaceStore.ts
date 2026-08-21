import { useSyncExternalStore } from "react";

// 工作区开合是 session-scoped UI 偏好，仅在前端持久化。
const KEY = "pudding.workspaceOpenSessions";
const LEGACY_KEYS = ["pudding.workspaceOpen", "pudding.canvasOpen"];

let openSessionIDs = readOpenSessionIDs();
let activeSessionID = "";
const listeners = new Set<() => void>();

for (const key of LEGACY_KEYS) {
  localStorage.removeItem(key);
}

function notify() {
  listeners.forEach((listener) => listener());
}

export function setWorkspaceOpen(sessionID: string, next: boolean) {
  if (!sessionID) return;

  const activeChanged = activeSessionID !== sessionID;
  const openChanged = openSessionIDs.has(sessionID) !== next;
  activeSessionID = sessionID;
  if (openChanged) {
    const updated = new Set(openSessionIDs);
    if (next) {
      updated.add(sessionID);
    } else {
      updated.delete(sessionID);
    }
    openSessionIDs = updated;
    localStorage.setItem(KEY, JSON.stringify([...updated]));
  }
  if (activeChanged || openChanged) notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useWorkspaceOpen(sessionID: string | undefined) {
  return useSyncExternalStore(
    subscribe,
    () => Boolean(sessionID && openSessionIDs.has(sessionID)),
    () => false,
  );
}

export function useActiveWorkspaceSessionID(
  primarySessionID: string | undefined,
  secondarySessionID?: string,
) {
  return useSyncExternalStore(
    subscribe,
    () => visibleWorkspaceSessionID(primarySessionID, secondarySessionID),
    () => primarySessionID || secondarySessionID || "",
  );
}

function visibleWorkspaceSessionID(primarySessionID: string | undefined, secondarySessionID?: string) {
  if (activeSessionID && (activeSessionID === primarySessionID || activeSessionID === secondarySessionID)) {
    return activeSessionID;
  }
  return primarySessionID || secondarySessionID || "";
}

function readOpenSessionIDs() {
  try {
    const value = JSON.parse(localStorage.getItem(KEY) || "[]") as unknown;
    return new Set(Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []);
  } catch {
    return new Set<string>();
  }
}
