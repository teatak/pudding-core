// pudding-desktop 是 Pudding 的桌面壳:daemon 同进程内嵌(单二进制),
// 窗口加载带 token 的本地 URL(内存直传,无手贴),tray 常驻。
// 通道单端口:端口被占即报错退出——不 attach 既有实例,因为 attach 会让壳加载
// 旧实例 serve 的旧 web,改了代码却看不到,极易踩坑。
// 边界:壳只做启动与系统集成,不碰 session runtime
// (docs/technology-decisions.md 第 4 节)。
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/teatak/pudding-core/internal/daemon"
	"github.com/teatak/pudding-core/internal/home"
)

//go:embed tray_icon.png
var trayIcon []byte // macOS menubar template icon(黑 + alpha 剪影)

func main() {
	addr := home.DefaultAddr()
	d, err := daemon.Start(daemon.Options{Addr: addr})
	if err != nil {
		// 通道单端口:端口被占说明已有实例在跑。不再 attach(壳会加载旧实例 serve
		// 的旧 web,改了代码却看不到),直接报错退出,让用户先停掉旧实例
		// (或用 make desktop-dev / web-dev,它们会先停旧实例再起)。
		if errors.Is(err, syscall.EADDRINUSE) {
			slog.Error("pudding-desktop: channel port already in use — another instance is running; quit it first (or use `make desktop-dev`)",
				"addr", addr)
		} else {
			slog.Error("pudding-desktop: start daemon failed", "addr", addr, "err", err)
		}
		os.Exit(1)
	}
	openURL := d.OpenURL()

	// 开发态热更:PUDDING_DEV_URL 指向 Vite dev server(:5174,HMR),壳窗口从那
	// 加载,改 web/src 即时生效、免 make desktop 重建;daemon 仍在本进程跑,Vite
	// 反代 API 回本 daemon(token 内存直传)。
	if dev := strings.TrimSpace(os.Getenv("PUDDING_DEV_URL")); dev != "" {
		openURL = fmt.Sprintf("%s/?token=%s", strings.TrimRight(dev, "/"), d.Token())
		slog.Info("pudding-desktop: dev url override", "dev", dev)
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
		URL:       openURL,
		Width:     1200,
		Height:    800,
		MinWidth:  760,
		MinHeight: 520,
		// 不透明深色窗口底:WKWebView 在窗口失焦/遮挡时合成路径降级,
		// 默认透明底会把桌面透出来(失焦时内容区出现暗色伪影)。
		// 取值贴近暗色主题 --background(#212121);浅色主题下 webview
		// 不透明绘制时底色不可见,无影响。
		BackgroundColour: application.NewRGB(33, 33, 33),
	}
	if runtime.GOOS == "darwin" {
		windowOpts.URL += "&shell=mac"
		// 启动白闪修复:先隐藏窗口,等 web 首帧导航完成再显示(见下方 darwin hook)。
		// 内容就绪前 WKWebView 是白底,window 深色底兜不住那一帧——只能不显示。
		windowOpts.Hidden = true
		windowOpts.Mac = application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 54,
		}
	}
	window := app.Window.NewWithOptions(windowOpts)

	// 窗口底色跟随系统外观(深 #212121 / 浅 #f9fafb,即两套主题的
	// --background 等值):失焦合成降级透底时与页面同色,不反差。
	// options 里的初值只覆盖首帧,这里在运行期持续校正。
	applyWindowBase := func() {
		if app.Env.IsDarkMode() {
			window.SetBackgroundColour(application.NewRGB(33, 33, 33))
		} else {
			window.SetBackgroundColour(application.NewRGB(249, 250, 251))
		}
	}
	app.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) {
		applyWindowBase()
	})

	var hideAfterFullscreenExit atomic.Bool
	if runtime.GOOS == "darwin" {
		// 启动白闪修复:窗口 Hidden 创建,web 首次导航完成(内容已加载)再显示。
		// 只显示一次——用户之后手动隐藏,dev 的 HMR reload 再触发导航也不弹回。
		var windowShown atomic.Bool
		showOnce := func() {
			if !windowShown.Swap(true) {
				window.Show()
				window.Focus()
			}
		}
		// 启动白闪缓解(非根治):WKWebView 内容在独立 web 进程渲染,didFinishNavigation
		// (加载完成)≠ web 进程首次合成帧;直接 Show 会露合成前的默认白底。等一拍让首帧
		// 画出(已深色,因 index.html inline 脚本 + CSS)再 Show。固定延迟是赛跑,偶尔首帧
		// 更慢仍会闪——根治需前端"已绘制"信号回推 native(经 daemon HTTP,见讨论)。
		window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
			time.AfterFunc(200*time.Millisecond, showOnce)
		})
		// 兜底:导航事件万一不来(加载失败等),也别让窗口永久隐藏
		time.AfterFunc(3*time.Second, showOnce)
		// NSWindow 在 NewWithOptions 后异步创建,这里直接调 NativeWindow()
		// 还是 nil;WindowFocus 首次 fire 时已就绪,attach 内部去重。
		window.RegisterHook(events.Common.WindowFocus, func(*application.WindowEvent) {
			attachDoubleClickToZoom(window)
			applyWindowBase()
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

	// 关窗隐藏后点 Dock 图标要能唤回(wails 内建 reopen-show 在该 alpha
	// 下不可靠,自己兜底);tray 菜单仍是另一入口
	if runtime.GOOS == "darwin" {
		app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
			window.Show()
			window.Focus()
		})
	}

	tray := app.SystemTray.New()
	// macOS 用 template icon(单色,自动适配明暗菜单栏);其它平台用普通 icon
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayIcon)
	} else {
		tray.SetIcon(trayIcon)
	}
	tray.SetTooltip("Pudding")
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
