import { useSyncExternalStore } from "react";

export type AgentConsoleMode = "floating" | "dock-left" | "dock-right" | "collapsed";
type ExpandedAgentConsoleMode = Exclude<AgentConsoleMode, "collapsed">;

const MODE_KEY = "pudding.agentConsoleMode";
const EXPANDED_MODE_KEY = "pudding.agentConsoleExpandedMode";

function isMode(value: string | null): value is AgentConsoleMode {
  return value === "floating" || value === "dock-left" || value === "dock-right" || value === "collapsed";
}

function isExpandedMode(value: string | null): value is ExpandedAgentConsoleMode {
  return value === "floating" || value === "dock-left" || value === "dock-right";
}

const savedMode = localStorage.getItem(MODE_KEY);
const savedExpandedMode = localStorage.getItem(EXPANDED_MODE_KEY);
let mode: AgentConsoleMode = isMode(savedMode) ? savedMode : "floating";
let expandedMode: ExpandedAgentConsoleMode = isExpandedMode(savedExpandedMode)
  ? savedExpandedMode
  : mode === "collapsed"
    ? "floating"
    : mode;
const listeners = new Set<() => void>();

function notify() {
  listeners.forEach((listener) => listener());
}

export function setAgentConsoleMode(next: AgentConsoleMode) {
  if (mode === next) {
    return;
  }
  mode = next;
  localStorage.setItem(MODE_KEY, next);
  if (next !== "collapsed") {
    expandedMode = next;
    localStorage.setItem(EXPANDED_MODE_KEY, next);
  }
  notify();
}

export function expandAgentConsole() {
  setAgentConsoleMode(expandedMode);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useAgentConsoleMode() {
  return useSyncExternalStore(subscribe, () => mode, () => mode);
}
