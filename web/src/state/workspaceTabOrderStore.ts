import { useSyncExternalStore } from "react";

const STORAGE_KEY = "pudding.workspaceTabOrder.v1";
const LEGACY_STORAGE_KEY = "pudding.canvasTabOrder.v1";
const EMPTY_ORDER: string[] = [];

let orders = readOrders();
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useWorkspaceTabOrder(scope: string) {
  return useSyncExternalStore(
    subscribe,
    () => orders[scope] || EMPTY_ORDER,
    () => EMPTY_ORDER,
  );
}

export function reconcileWorkspaceTabOrder(scope: string, availableIDs: string[]) {
  const next = mergeWorkspaceTabOrder(orders[scope], availableIDs);
  if (sameOrder(orders[scope], next)) {
    return;
  }
  writeScope(scope, next);
}

export function setWorkspaceTabOrder(scope: string, next: string[]) {
  if (sameOrder(orders[scope], next)) {
    return;
  }
  writeScope(scope, next);
}

export function mergeWorkspaceTabOrder(saved: string[] | undefined, availableIDs: string[]) {
  const available = new Set(availableIDs);
  const seen = new Set<string>();
  const next: string[] = [];
  (saved || []).forEach((id) => {
    if (available.has(id) && !seen.has(id)) {
      seen.add(id);
      next.push(id);
    }
  });
  availableIDs.forEach((id) => {
    if (!seen.has(id)) {
      seen.add(id);
      next.push(id);
    }
  });
  return next;
}

function writeScope(scope: string, order: string[]) {
  orders = { ...orders, [scope]: order };
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(orders));
    localStorage.removeItem(LEGACY_STORAGE_KEY);
  } catch {
    // Best-effort UI preference.
  }
  notify();
}

function sameOrder(current: string[] | undefined, next: string[]) {
  return Boolean(current && current.length === next.length && current.every((id, index) => id === next[index]));
}

function readOrders(): Record<string, string[]> {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || localStorage.getItem(LEGACY_STORAGE_KEY) || "{}") as Record<string, unknown>;
    return Object.fromEntries(
      Object.entries(parsed).flatMap(([scope, value]) => {
        if (!Array.isArray(value)) {
          return [];
        }
        const order = value.filter((id): id is string => typeof id === "string" && id.length > 0);
        return [[scope, order]];
      }),
    );
  } catch {
    return {};
  }
}
