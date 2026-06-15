import { consumeLaunchParam } from "@/state/launchParams";

// 桌面壳运行模式识别(docs/design.md 2.3):壳加载 URL 附 ?shell=mac,
// 这里写入 <html data-shell> 并持久化;浏览器访问无此参数,零 inset。
const SHELL_KEY = "pudding.shell";

type WailsRuntime = typeof import("@wailsio/runtime");

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
    void initNoZoomRectsTracking();
  }
}

// 全屏时红绿灯由系统隐藏,inset 让位归零。退出全屏用 will 事件提前
// 建立红绿灯安全区,did 事件只兜底最终状态。
async function initMacFullscreenTracking() {
  let runtime: WailsRuntime;
  try {
    runtime = await import("@wailsio/runtime");
  } catch {
    return;
  }

  const { Events, Window: WailsWindow } = runtime;

  const apply = (fullscreen: boolean) => {
    document.documentElement.toggleAttribute("data-fullscreen", fullscreen);
  };

  Events.On(Events.Types.Common.WindowFullscreen, () => apply(true));
  Events.On(Events.Types.Mac.WindowWillExitFullScreen, () => apply(false));
  Events.On(Events.Types.Common.WindowUnFullscreen, () => apply(false));

  try {
    apply(await WailsWindow.IsFullscreen());
  } catch {
    // Runtime may be unavailable in pure browser mode; shell layout still works
    // from subsequent Wails window events.
  }
}

let noZoomRectsTrackingStarted = false;

type NoZoomRect = {
  x: number;
  y: number;
  w: number;
  h: number;
};

async function initNoZoomRectsTracking() {
  if (noZoomRectsTrackingStarted) {
    return;
  }
  noZoomRectsTrackingStarted = true;

  let runtime: WailsRuntime;
  try {
    runtime = await import("@wailsio/runtime");
  } catch {
    return;
  }

  const { Events } = runtime;
  let lastPayload = "";
  let timer: number | undefined;
  let observed = new Set<Element>();
  const resizeObserver = new ResizeObserver(() => schedule());

  const readRects = (): NoZoomRect[] =>
    Array.from(document.querySelectorAll<HTMLElement>(".no-drag-region"))
      .map((element) => {
        const rect = element.getBoundingClientRect();
        return {
          x: rect.left,
          y: rect.top,
          w: rect.width,
          h: rect.height,
        };
      })
      .filter((rect) => rect.w > 0 && rect.h > 0);

  const syncObservedElements = () => {
    const next = new Set<Element>(document.querySelectorAll(".no-drag-region"));
    if (next.size === observed.size && Array.from(next).every((element) => observed.has(element))) {
      return;
    }
    resizeObserver.disconnect();
    observed = next;
    observed.forEach((element) => resizeObserver.observe(element));
  };

  const emit = () => {
    syncObservedElements();
    const rects = readRects();
    const payload = JSON.stringify(rects);
    if (payload === lastPayload) {
      return;
    }
    lastPayload = payload;
    void Events.Emit("desktop:no-zoom-rects", rects).catch(() => {});
  };

  function schedule() {
    if (timer !== undefined) {
      window.clearTimeout(timer);
    }
    timer = window.setTimeout(emit, 80);
  }

  new MutationObserver(() => {
    syncObservedElements();
    schedule();
  }).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class", "style", "hidden"],
    childList: true,
    subtree: true,
  });
  window.addEventListener("resize", schedule);
  window.addEventListener("scroll", schedule, true);
  window.visualViewport?.addEventListener("resize", schedule);
  window.visualViewport?.addEventListener("scroll", schedule);
  syncObservedElements();
  schedule();
}
