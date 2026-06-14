import { useSyncExternalStore } from "react";

export type Theme = "light" | "dark" | "system";
type ResolvedTheme = "light" | "dark";
type ThemeSnapshot = `${Theme}:${ResolvedTheme}`;

const STORAGE_KEY = "pudding.theme";
const listeners = new Set<() => void>();
let themeSyncStarted = false;

export function readStoredTheme(): Theme {
  if (typeof window === "undefined") {
    return "system";
  }
  const value = window.localStorage.getItem(STORAGE_KEY);
  return value === "light" || value === "dark" || value === "system" ? value : "system";
}

export function resolveTheme(theme: Theme): ResolvedTheme {
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

function notifyThemeChange() {
  applyTheme(readStoredTheme());
  listeners.forEach((listener) => listener());
}

export function startThemeSync() {
  if (typeof window === "undefined" || themeSyncStarted) {
    return;
  }
  themeSyncStarted = true;
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  media.addEventListener("change", notifyThemeChange);
  window.addEventListener("storage", notifyThemeChange);
}

export function setTheme(theme: Theme) {
  window.localStorage.setItem(STORAGE_KEY, theme);
  notifyThemeChange();
}

function subscribe(listener: () => void) {
  startThemeSync();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function snapshot(): ThemeSnapshot {
  const theme = readStoredTheme();
  return `${theme}:${resolveTheme(theme)}`;
}

export function useTheme() {
  const value = useSyncExternalStore(subscribe, snapshot, snapshot);
  const [theme, resolved] = value.split(":") as [Theme, ResolvedTheme];
  return { theme, resolved };
}
