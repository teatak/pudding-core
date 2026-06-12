// 桌面壳运行模式识别(docs/design.md 2.3):壳加载 URL 附 ?shell=mac,
// 这里写入 <html data-shell> 并持久化;浏览器访问无此参数,零 inset。
//
// **不要依赖 window.wails / ExecJS**(旧项目踩坑结论):Wails 不向跨
// origin(loopback http)页面注入 runtime.js,ExecJS 全部滞留 pendingJS
// 永不 flush,window.wails 全局也不存在。壳内只有两条可靠通路:
// native cgo(壳侧)和纯 web 启发式(页面侧)。
const SHELL_KEY = "pudding.shell";

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
}

// 全屏时红绿灯隐藏,inset 让位要归零。检测走视口启发式:macOS native
// fullscreen 下 webview 视口恰好铺满整块屏幕(zoom 最大化只填 visibleFrame,
// 不会同时命中宽高)。旧项目同款(useDesktopTitlebarInset),是全屏让位的
// source of truth;进出全屏必触发 resize,无需任何壳侧通知。
function initFullscreenTracking() {
  const sync = () => {
    const fullscreen =
      Math.abs(window.innerHeight - window.screen.height) <= 1 &&
      Math.abs(window.innerWidth - window.screen.width) <= 1;
    document.documentElement.toggleAttribute("data-fullscreen", fullscreen);
  };
  window.addEventListener("resize", sync);
  sync();
}
