// 桌面壳运行模式识别(docs/design.md 2.3):壳加载 URL 附 ?shell=mac,
// 这里写入 <html data-shell> 并持久化;浏览器访问无此参数,零 inset。
// 壳内行为(全屏检测、双击缩放)只依赖 Wails 注入的 window.wails 全局,
// 页面不 import wails runtime 包;浏览器模式下该全局不存在,全部静默跳过。
const SHELL_KEY = "pudding.shell";

type WailsWindowAPI = {
  IsFullscreen: () => Promise<boolean>;
  ToggleMaximise: () => Promise<void>;
};

function wailsWindow(): WailsWindowAPI | undefined {
  return (window as { wails?: { Window?: WailsWindowAPI } }).wails?.Window;
}

export function initShellMode() {
  const url = new URL(window.location.href);
  const fromURL = url.searchParams.get("shell");
  if (fromURL) {
    sessionStorage.setItem(SHELL_KEY, fromURL);
    url.searchParams.delete("shell");
    window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  }
  const shell = fromURL || sessionStorage.getItem(SHELL_KEY);
  if (!shell) {
    return;
  }
  document.documentElement.dataset.shell = shell;
  initFullscreenTracking();
  initToolbarDoubleClick();
}

// 全屏时红绿灯隐藏,inset 让位要归零:进出全屏必然触发 resize,
// 在 resize 上向壳查询真实全屏态,写回 <html data-fullscreen>
function initFullscreenTracking() {
  let pending = false;
  const sync = () => {
    const api = wailsWindow();
    if (!api || pending) {
      return;
    }
    pending = true;
    void api
      .IsFullscreen()
      .then((fullscreen) => {
        document.documentElement.toggleAttribute("data-fullscreen", fullscreen);
      })
      .catch(() => undefined)
      .finally(() => {
        pending = false;
      });
  };
  window.addEventListener("resize", sync);
  sync();
}

// macOS 习惯:双击标题栏 = 缩放窗口(等同 option+绿灯,非全屏)。
// 命中拖拽区空白处才触发,交互控件上的双击不算。
function initToolbarDoubleClick() {
  window.addEventListener("dblclick", (event) => {
    const target = event.target as HTMLElement | null;
    if (!target?.closest(".drag-region")) {
      return;
    }
    if (target.closest("button, a, input, textarea, select, [role='button']")) {
      return;
    }
    void wailsWindow()?.ToggleMaximise();
  });
}
