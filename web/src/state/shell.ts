import { consumeLaunchParam } from "@/state/launchParams";

// 桌面壳运行模式识别(docs/design.md 2.3):壳加载 URL 附 ?shell=mac,
// 这里写入 <html data-shell> 并持久化;浏览器访问无此参数,零 inset。
const SHELL_KEY = "pudding.shell";

type WailsEvent = { data?: unknown };
type WailsEvents = {
  On?: (name: string, callback: (event: WailsEvent) => void) => void | (() => void);
  Emit?: (name: string, data?: unknown) => void | Promise<void>;
};

declare global {
  interface Window {
    wails?: {
      Events?: WailsEvents;
    };
  }
}

export function initShellMode() {
  const fromURL = consumeLaunchParam("shell");
  if (fromURL) {
    sessionStorage.setItem(SHELL_KEY, fromURL);
  }
  const shell = fromURL || sessionStorage.getItem(SHELL_KEY);
  if (!shell) {
    return;
  }
  document.documentElement.dataset.shell = shell;
  if (shell === "mac") {
    void initMacFullscreenTracking();
  }
}

// 全屏时红绿灯隐藏,inset 让位要归零。只认 Wails native fullscreen 事件,
// 不再保留视口尺寸启发式。
async function initMacFullscreenTracking() {
  const runtimeURL = "/wails/runtime.js";
  try {
    await import(/* @vite-ignore */ runtimeURL);
  } catch {
    return;
  }

  const events = window.wails?.Events;
  if (!events?.On) {
    return;
  }

  const apply = (value: unknown) => {
    if (typeof value === "boolean") {
      document.documentElement.toggleAttribute("data-fullscreen", value);
    }
  };

  events.On("desktop:fullscreen", (event) => apply(event.data));
  await events.Emit?.("desktop:fullscreen:request");
}
