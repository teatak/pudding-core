import { useSyncExternalStore } from "react";

import { initialToken, saveToken } from "@/state/token";

let token = initialToken();

const listeners = new Set<() => void>();

window.addEventListener("pudding-token-change", (event) => {
  token = String((event as CustomEvent<string>).detail || "");
  listeners.forEach((listener) => listener());
});

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function snapshot() {
  return token;
}

export function useToken() {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}

export function setToken(next: string) {
  saveToken(next);
}
