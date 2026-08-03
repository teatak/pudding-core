import { useSyncExternalStore } from "react";

export type AgentConsoleMode = "floating" | "dock-left" | "dock-right";

const MODE_KEY = "pudding.agentConsoleMode";

function isMode(value: string | null): value is AgentConsoleMode {
  return value === "floating" || value === "dock-left" || value === "dock-right";
}

const savedMode = localStorage.getItem(MODE_KEY);
let mode: AgentConsoleMode = isMode(savedMode) ? savedMode : "dock-left";
let floatingExpanded = false;
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

export function setAgentConsoleMode(next: AgentConsoleMode) {
  if (mode === next) {
    return;
  }
  if (next === "floating") {
    floatingExpanded = false;
  }
  mode = next;
  localStorage.setItem(MODE_KEY, next);
  notify();
}

export function setFloatingConsoleExpanded(next: boolean) {
  if (floatingExpanded === next) {
    return;
  }
  floatingExpanded = next;
  notify();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useAgentConsoleMode() {
  return useSyncExternalStore(subscribe, () => mode, () => mode);
}

export function useFloatingConsoleExpanded() {
  return useSyncExternalStore(subscribe, () => floatingExpanded, () => floatingExpanded);
}
