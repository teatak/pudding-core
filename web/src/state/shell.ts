import { consumeLaunchParam } from "@/state/launchParams";

// 桌面壳运行模式识别:Electron 壳加载 URL 附 ?shell=electron / electron-mac,
// 这里写入 <html data-shell> 并持久化;浏览器访问无此参数,零 inset。
const SHELL_KEY = "pudding.shell";

type ElectronShellBridge = {
  isFullscreen: () => Promise<boolean>;
  onFullscreenChanged: (listener: (fullscreen: boolean) => void) => () => void;
};

declare global {
  interface Window {
    puddingElectronShell?: ElectronShellBridge;
  }
}

export function initShellMode() {
  const fromURL = consumeLaunchParam("shell");
  if (fromURL) {
    sessionStorage.setItem(SHELL_KEY, fromURL);
  }
  const shell = normalizeShell(fromURL || sessionStorage.getItem(SHELL_KEY));
  if (!shell) {
    sessionStorage.removeItem(SHELL_KEY);
    return;
  }
  sessionStorage.setItem(SHELL_KEY, shell);
  document.documentElement.dataset.shell = shell;
  if (shell === "electron-mac") {
    void initElectronFullscreenTracking();
  }
}

export function isElectronShell() {
  return normalizeShell(document.documentElement.dataset.shell || null) !== "";
}

async function initElectronFullscreenTracking() {
  const bridge = typeof window === "undefined" ? undefined : window.puddingElectronShell;
  if (!bridge) {
    return;
  }
  const apply = (fullscreen: boolean) => {
    document.documentElement.toggleAttribute("data-fullscreen", fullscreen);
  };
  const off = bridge.onFullscreenChanged(apply);
  window.addEventListener("beforeunload", off, { once: true });
  try {
    apply(await bridge.isFullscreen());
  } catch {
    apply(false);
  }
}

function normalizeShell(shell: string | null) {
  return shell === "electron" || shell === "electron-mac" ? shell : "";
}
