import { useSyncExternalStore } from "react";

import { consumeLaunchParam } from "@/state/launchParams";

export type Theme = "light" | "dark" | "system";
type ResolvedTheme = "light" | "dark";
type ThemeSnapshot = `${Theme}:${ResolvedTheme}`;

const STORAGE_KEY = "pudding.theme";
const FALLBACK_STYLE_ID = "pudding-theme-fallback";
const listeners = new Set<() => void>();
let themeSyncStarted = false;
let nativeTheme: Theme | null = null;
let nativeResolvedTheme: ResolvedTheme | null = null;

export function readStoredTheme(): Theme {
  if (typeof window === "undefined") {
    return "system";
  }
  const value = window.localStorage.getItem(STORAGE_KEY);
  return value === "light" || value === "dark" || value === "system" ? value : "system";
}

declare global {
  interface Window {
    __puddingSetThemeState?: (state: unknown) => void;
  }
}

if (typeof window !== "undefined") {
  window.__puddingSetThemeState = (state: unknown) => {
    const next = normalizeThemeState(state);
    if (!next) return;
    applyNativeThemeState(next.theme, next.resolved);
  };
}

export function initThemeFromLaunch() {
  if (typeof window === "undefined") {
    return;
  }
  const theme = consumeLaunchParam("theme");
  if (isTheme(theme)) {
    applyNativeThemeState(theme, theme === "system" ? browserResolvedTheme() : theme);
  }
}

export function resolveTheme(theme: Theme): ResolvedTheme {
  if (theme !== "system") {
    return theme;
  }
  if (nativeResolvedTheme) {
    return nativeResolvedTheme;
  }
  return browserResolvedTheme();
}

export function applyTheme(theme: Theme) {
  if (typeof document === "undefined") {
    return;
  }
  const resolved = resolveTheme(theme);
  applyDocumentTheme(resolved);
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
  if (isDesktopThemeControlled()) {
    return;
  }
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  media.addEventListener("change", notifyThemeChange);
  window.addEventListener("storage", notifyThemeChange);
}

export function setTheme(theme: Theme) {
  if (isDesktopThemeControlled()) {
    import("@wailsio/runtime")
      .then(({ Events }) => Events.Emit("desktop:theme-set", theme))
      .catch(() => setLocalTheme(theme));
    return;
  }
  setLocalTheme(theme);
}

function subscribe(listener: () => void) {
  startThemeSync();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function snapshot(): ThemeSnapshot {
  const theme = nativeTheme ?? readStoredTheme();
  return `${theme}:${resolveTheme(theme)}`;
}

export function useTheme() {
  const value = useSyncExternalStore(subscribe, snapshot, snapshot);
  const [theme, resolved] = value.split(":") as [Theme, ResolvedTheme];
  return { theme, resolved };
}

function setLocalTheme(theme: Theme) {
  window.localStorage.setItem(STORAGE_KEY, theme);
  notifyThemeChange();
}

function applyNativeThemeState(theme: Theme, resolved: ResolvedTheme) {
  const oldSnapshot = snapshot();
  nativeTheme = theme;
  nativeResolvedTheme = resolved;
  window.localStorage.setItem(STORAGE_KEY, theme);
  applyDocumentTheme(resolved);
  if (snapshot() !== oldSnapshot) {
    listeners.forEach((listener) => listener());
  }
}

function normalizeThemeState(state: unknown): { theme: Theme; resolved: ResolvedTheme } | null {
  if (!state || typeof state !== "object") {
    return null;
  }
  const record = state as Record<string, unknown>;
  if (!isTheme(record.theme) || !isResolvedTheme(record.resolved)) {
    return null;
  }
  return { theme: record.theme, resolved: record.resolved };
}

function isTheme(value: unknown): value is Theme {
  return value === "light" || value === "dark" || value === "system";
}

function isResolvedTheme(value: unknown): value is ResolvedTheme {
  return value === "light" || value === "dark";
}

function browserResolvedTheme(): ResolvedTheme {
  if (typeof window === "undefined") {
    return "light";
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function applyDocumentTheme(resolved: ResolvedTheme) {
  if (typeof document === "undefined") {
    return;
  }
  const { bg, fg } = themeFallbackColors(resolved);
  document.documentElement.classList.toggle("dark", resolved === "dark");
  document.documentElement.style.colorScheme = resolved;
  document.documentElement.style.backgroundColor = bg;
  document.documentElement.style.color = fg;
  let style = document.getElementById(FALLBACK_STYLE_ID);
  if (!style) {
    style = document.createElement("style");
    style.id = FALLBACK_STYLE_ID;
    document.head.appendChild(style);
  }
  style.textContent = `html,body,#root{background:${bg};color:${fg};}`;
}

function themeFallbackColors(resolved: ResolvedTheme) {
  return resolved === "dark"
    ? { bg: "oklch(0.205 0 0)", fg: "oklch(0.95 0 0)" }
    : { bg: "oklch(1 0 0)", fg: "oklch(0.18 0 0)" };
}

function isDesktopThemeControlled() {
  return typeof document !== "undefined" && Boolean(document.documentElement.dataset.shell);
}
