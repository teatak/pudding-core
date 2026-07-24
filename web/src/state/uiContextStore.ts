import { useSyncExternalStore } from "react";

import type { ContentPart } from "@/api/client";

export type UIContextPart = Extract<ContentPart, { type: "ui_context" }>;

const ENABLED_KEY = "pudding.uiContextEnabled";

type VisibleUIContext = {
  context: UIContextPart;
  key: string;
  sessionID: string;
};

let visible: VisibleUIContext | undefined;
let enabled = localStorage.getItem(ENABLED_KEY) !== "0";
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function notify() {
  listeners.forEach((listener) => listener());
}

export function uiContextKey(context: UIContextPart) {
  return [
    context.surface,
    context.resource,
    context.id,
    context.name,
    context.path,
    context.url,
    context.kind,
    context.rootID,
    context.selectionText,
  ].join("\u0000");
}

export function setVisibleUIContext(sessionID: string, context?: UIContextPart) {
  if (!sessionID || !context) {
    clearVisibleUIContext(sessionID);
    return;
  }
  const key = uiContextKey(context);
  if (visible?.sessionID === sessionID && visible.key === key) {
    return;
  }
  visible = { context, key, sessionID };
  notify();
}

export function clearVisibleUIContext(sessionID?: string) {
  if (!visible || (sessionID && visible.sessionID !== sessionID)) {
    return;
  }
  visible = undefined;
  notify();
}

export function useVisibleUIContext(sessionID: string) {
  return useSyncExternalStore(
    subscribe,
    () => (visible?.sessionID === sessionID ? visible.context : undefined),
    () => undefined,
  );
}

export function setUIContextEnabled(next: boolean) {
  if (enabled === next) {
    return;
  }
  enabled = next;
  localStorage.setItem(ENABLED_KEY, next ? "1" : "0");
  notify();
}

export function useUIContextEnabled() {
  return useSyncExternalStore(subscribe, () => enabled, () => enabled);
}
