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

let goResolvedSystemTheme: ResolvedTheme | null = null;

// Go 通过 window.ExecJS 直接调用此全局函数，绕过 Wails 事件系统
// 彻底避免事件监听器注册时机和 HMR 重复注册的问题
declare global {
  interface Window {
    __puddingSetResolvedTheme?: (resolved: string) => void;
  }
}
if (typeof window !== "undefined") {
  window.__puddingSetResolvedTheme = (resolved: string) => {
    if (resolved !== "dark" && resolved !== "light") return;
    const old = goResolvedSystemTheme;
    goResolvedSystemTheme = resolved as ResolvedTheme;
    if (readStoredTheme() === "system" && old !== goResolvedSystemTheme) {
      // 直接操作 DOM，绝不调 applyTheme（避免发事件回 Go）
      document.documentElement.classList.toggle("dark", resolved === "dark");
      document.documentElement.style.colorScheme = resolved;
      listeners.forEach((l) => l());
    }
  };
}

export function resolveTheme(theme: Theme): ResolvedTheme {
  if (theme !== "system") {
    return theme;
  }
  if (goResolvedSystemTheme) {
    return goResolvedSystemTheme;
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
  
  import("@wailsio/runtime").then(({ Events }) => {
    Events.Emit("desktop:theme-changed", theme).catch(() => {});
  }).catch(() => {});
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
