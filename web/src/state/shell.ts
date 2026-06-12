// 桌面壳运行模式识别(docs/design.md 2.3):壳加载 URL 附 ?shell=mac,
// 这里写入 <html data-shell> 并持久化;浏览器访问无此参数,零 inset。
// 全屏态(data-fullscreen)由壳经 ExecJS 切换,页面不引入 wails runtime。
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
  if (shell) {
    document.documentElement.dataset.shell = shell;
  }
}
