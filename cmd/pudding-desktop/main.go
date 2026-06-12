// pudding-desktop 是 Pudding 的桌面壳:daemon 同进程内嵌(单二进制),
// 窗口加载带 token 的本地 URL(内存直传,无手贴),tray 常驻。
// 边界:壳只做启动与系统集成,不碰 session runtime
// (docs/technology-decisions.md 第 4 节)。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/teatak/pudding-core/internal/daemon"
)

func main() {
	// AutoPort:默认端口被 CLI 版 daemon 等占用时回落随机端口,壳知道实际地址
	d, err := daemon.Start(daemon.Options{AutoPort: true, DefaultModel: "mock-model"})
	if err != nil {
		slog.Error("pudding-desktop: start daemon", "err", err)
		os.Exit(1)
	}

	app := application.New(application.Options{
		Name: "Pudding",
	})

	// 窗口 chrome(docs/design.md 2.3):macOS 用 HiddenInset 隐藏标题栏,
	// 红绿灯由 NSToolbar inset rule 定位(绝不 cgo setFrame,旧项目踩坑);
	// URL 附 ?shell=mac 让页面启用 --traffic-inset / --toolbar-h 让位。
	//
	// 与页面的三处同值约定:InvisibleTitleBarHeight = web 的 --toolbar-h
	// = cgo 的 kPuddingToolbarHeight = 54(同一条"工具条带"语义:原生可
	// 拖区 / 视觉工具条 / 双击 zoom 检测区)。
	//
	// **页面与壳之间没有 ExecJS / window.wails 通路**(Wails 不向跨 origin
	// 页面注入 runtime,旧项目结论):全屏 inset 归零由页面视口启发式自理
	// (state/shell.ts),壳只负责 native 侧 chrome(toolbar / 红绿灯 / zoom)。
	installZoomSwizzle()
	windowOpts := application.WebviewWindowOptions{
		Title:     "Pudding",
		URL:       d.OpenURL(),
		Width:     1200,
		Height:    800,
		MinWidth:  760,
		MinHeight: 520,
	}
	if runtime.GOOS == "darwin" {
		windowOpts.URL += "&shell=mac"
		windowOpts.Mac = application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 54,
		}
	}
	window := app.Window.NewWithOptions(windowOpts)

	var hideAfterFullscreenExit atomic.Bool
	if runtime.GOOS == "darwin" {
		// NSWindow 在 NewWithOptions 后异步创建,这里直接调 NativeWindow()
		// 还是 nil;WindowFocus 首次 fire 时已就绪,attach 内部去重。
		window.RegisterHook(events.Common.WindowFocus, func(*application.WindowEvent) {
			attachDoubleClickToZoom(window)
		})
		// 监听 Mac native 事件而非 Common alias:Common 走 setupEventMapping
		// 的异步 forwarding,fullscreen transition 期间会 race(旧项目结论)
		window.OnWindowEvent(events.Mac.WindowDidEnterFullScreen, func(*application.WindowEvent) {
			setFullscreenChrome(window, true)
		})
		window.OnWindowEvent(events.Mac.WindowWillExitFullScreen, func(*application.WindowEvent) {
			// 先藏红绿灯再切 styleMask:layout 重算的过渡帧不可见,
			// DidExit 后按钮以正确 inset 位置一步到位出现
			setTrafficLightsHidden(window, true)
			setFullscreenChrome(window, false)
		})
		window.OnWindowEvent(events.Mac.WindowDidExitFullScreen, func(*application.WindowEvent) {
			setTrafficLightsHidden(window, false)
			if hideAfterFullscreenExit.Swap(false) {
				window.Hide()
			}
		})
	}

	// 关窗 = 隐藏:daemon 常驻后台,tray 可唤回;退出只走 tray 菜单。
	// 全屏中直接 Hide 会留下黑屏 Space,先退全屏再藏。
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		if runtime.GOOS == "darwin" && window.IsFullscreen() {
			hideAfterFullscreenExit.Store(true)
			window.UnFullscreen()
			return
		}
		window.Hide()
	})

	tray := app.SystemTray.New()
	tray.SetLabel("Pudding")
	menu := app.NewMenu()
	menu.Add("显示 Pudding").OnClick(func(*application.Context) {
		window.Show()
		window.Focus()
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)

	// daemon serve 异常退出时带走壳,避免空窗口假活
	go func() {
		if err := <-d.ServeErr(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("pudding-desktop: daemon serve", "err", err)
			app.Quit()
		}
	}()

	if err := app.Run(); err != nil {
		slog.Error("pudding-desktop: run", "err", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Shutdown(shutdownCtx); err != nil {
		slog.Error("pudding-desktop: shutdown daemon", "err", err)
	}
}
