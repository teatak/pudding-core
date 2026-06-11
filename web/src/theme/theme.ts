import { useSyncExternalStore } from "react";

export type Theme = "light" | "dark" | "system";

const STORAGE_KEY = "pudding.theme";
const listeners = new Set<() => void>();

export function readStoredTheme(): Theme {
  if (typeof window === "undefined") {
    return "system";
  }
  const value = window.localStorage.getItem(STORAGE_KEY);
  return value === "light" || value === "dark" || value === "system" ? value : "system";
}

export function resolveTheme(theme: Theme): "light" | "dark" {
  if (theme !== "system") {
    return theme;
  }
  if (typeof window === "undefined") {
    return "light";
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function applyTheme(theme: Theme) {
  if (typeof document === "undefined") {
    return;
  }
  const resolved = resolveTheme(theme);
  document.documentElement.classList.toggle("dark", resolved === "dark");
  document.documentElement.style.colorScheme = resolved;
}

export function setTheme(theme: Theme) {
  window.localStorage.setItem(STORAGE_KEY, theme);
  applyTheme(theme);
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  const notify = () => {
    applyTheme(readStoredTheme());
    listener();
  };
  media.addEventListener("change", notify);
  window.addEventListener("storage", notify);
  return () => {
    listeners.delete(listener);
    media.removeEventListener("change", notify);
    window.removeEventListener("storage", notify);
  };
}

function snapshot() {
  return readStoredTheme();
}

export function useTheme() {
  const theme = useSyncExternalStore(subscribe, snapshot, snapshot);
  return { theme, resolved: resolveTheme(theme) };
}
