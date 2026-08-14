import { useSyncExternalStore } from "react";

import { consumeLaunchParam } from "@/state/launchParams";

export type Theme = "light" | "dark" | "system";
export const ACCENT_THEMES = ["blue", "indigo", "red", "emerald", "orange", "neutral"] as const;
export type AccentTheme = (typeof ACCENT_THEMES)[number];
type ResolvedTheme = "light" | "dark";
type ThemeSnapshot = `${Theme}:${ResolvedTheme}`;
type NativeThemeState = { theme: Theme; resolved: ResolvedTheme };

const STORAGE_KEY = "pudding.theme";
const ACCENT_STORAGE_KEY = "pudding.accent-theme";
const FALLBACK_STYLE_ID = "pudding-theme-fallback";
const listeners = new Set<() => void>();
const accentListeners = new Set<() => void>();
let themeSyncStarted = false;
let accentThemeSyncStarted = false;
let nativeTheme: Theme | null = null;
let nativeResolvedTheme: ResolvedTheme | null = null;

export function readStoredTheme(): Theme {
  if (typeof window === "undefined") {
    return "system";
  }
  const value = window.localStorage.getItem(STORAGE_KEY);
  return value === "light" || value === "dark" || value === "system" ? value : "system";
}

export function readStoredAccentTheme(): AccentTheme {
  if (typeof window === "undefined") {
    return "blue";
  }
  const value = window.localStorage.getItem(ACCENT_STORAGE_KEY);
  if (value === "cyan" || value === "green") {
    window.localStorage.setItem(ACCENT_STORAGE_KEY, "blue");
    return "blue";
  }
  if (value === "violet") {
    window.localStorage.setItem(ACCENT_STORAGE_KEY, "blue");
    return "blue";
  }
  if (value === "amber" || value === "yellow") {
    window.localStorage.setItem(ACCENT_STORAGE_KEY, "red");
    return "red";
  }
  if (value === "fuchsia") {
    window.localStorage.setItem(ACCENT_STORAGE_KEY, "orange");
    return "orange";
  }
  return isAccentTheme(value) ? value : "blue";
}

declare global {
  interface Window {
    __puddingSetThemeState?: (state: unknown) => void;
    puddingElectronTheme?: {
      getState: () => Promise<unknown>;
      setTheme: (theme: Theme) => Promise<unknown>;
      onUpdated: (listener: (state: unknown) => void) => () => void;
    };
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
  const resolvedTheme = consumeLaunchParam("resolvedTheme");
  if (isTheme(theme)) {
    const resolved = isResolvedTheme(resolvedTheme) ? resolvedTheme : theme === "system" ? browserResolvedTheme() : theme;
    applyNativeThemeState(theme, resolved);
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
  const electronTheme = electronThemeBridge();
  if (electronTheme) {
    electronTheme.onUpdated((state) => window.__puddingSetThemeState?.(state));
    void electronTheme.getState().then((state) => window.__puddingSetThemeState?.(state));
    return;
  }
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  media.addEventListener("change", notifyThemeChange);
  window.addEventListener("storage", notifyThemeChange);
}

export function setTheme(theme: Theme) {
  const electronTheme = electronThemeBridge();
  if (electronTheme) {
    electronTheme
      .setTheme(theme)
      .then((state) => window.__puddingSetThemeState?.(state))
      .catch(() => setLocalTheme(theme));
    return;
  }
  setLocalTheme(theme);
}

export function applyAccentTheme(accent: AccentTheme) {
  if (typeof document === "undefined") {
    return;
  }
  document.documentElement.dataset.accent = accent;
}

export function setAccentTheme(accent: AccentTheme) {
  window.localStorage.setItem(ACCENT_STORAGE_KEY, accent);
  applyAccentTheme(accent);
  accentListeners.forEach((listener) => listener());
}

export function startAccentThemeSync() {
  if (typeof window === "undefined" || accentThemeSyncStarted) {
    return;
  }
  accentThemeSyncStarted = true;
  window.addEventListener("storage", (event) => {
    if (event.key !== ACCENT_STORAGE_KEY) {
      return;
    }
    applyAccentTheme(readStoredAccentTheme());
    accentListeners.forEach((listener) => listener());
  });
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

export function useAccentTheme() {
  return useSyncExternalStore(subscribeAccentTheme, readStoredAccentTheme, readStoredAccentTheme);
}

function subscribeAccentTheme(listener: () => void) {
  startAccentThemeSync();
  accentListeners.add(listener);
  return () => {
    accentListeners.delete(listener);
  };
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

function normalizeThemeState(state: unknown): NativeThemeState | null {
  if (!state || typeof state !== "object") {
    return null;
  }
  const record = state as Record<string, unknown>;
  if (!isTheme(record.theme) || !isResolvedTheme(record.resolved)) {
    return null;
  }
  return { theme: record.theme, resolved: record.resolved };
}

function electronThemeBridge() {
  return typeof window === "undefined" ? undefined : window.puddingElectronTheme;
}

function isTheme(value: unknown): value is Theme {
  return value === "light" || value === "dark" || value === "system";
}

function isAccentTheme(value: unknown): value is AccentTheme {
  return ACCENT_THEMES.some((accent) => accent === value);
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
    ? { bg: "#171717", fg: "oklch(0.95 0 0)" }
    : { bg: "oklch(1 0 0)", fg: "oklch(0.18 0 0)" };
}
