// pudding-desktop 是 Pudding 的桌面壳:daemon 同进程内嵌(单二进制),
// Wails 托管前端(runtime 可用),业务 API 直连内嵌 daemon HTTP,tray 常驻。
// 通道单端口:端口被占即报错退出——不 attach 既有实例,因为 attach 会让壳加载
// 旧实例 serve 的旧 web,改了代码却看不到,极易踩坑。
// 边界:壳只做启动与系统集成,不碰 session runtime
// (docs/technology-decisions.md 第 4 节)。
package main

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
	"github.com/teatak/pudding-core/internal/webui"
)

//go:embed tray_icon.png
var trayIcon []byte // macOS menubar template icon(黑 + alpha 剪影)

func main() {
	addr := home.DefaultAddr()
	d, err := daemon.Start(daemon.Options{Addr: addr})
	if err != nil {
		// 通道单端口:端口被占说明已有实例在跑。不再 attach(壳会加载旧实例 serve
		// 的旧 web,改了代码却看不到),直接报错退出,让用户先停掉旧实例
		// (或用 make desktop-dev,它会先停旧实例再起)。
		if errors.Is(err, syscall.EADDRINUSE) {
			slog.Error("pudding-desktop: channel port already in use — another instance is running; quit it first (or use `make desktop-dev`)",
				"addr", addr)
		} else {
			slog.Error("pudding-desktop: start daemon failed", "addr", addr, "err", err)
		}
		os.Exit(1)
	}
	configureFrontendDevServer()

	app := application.New(application.Options{
		Name: "Pudding",
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(webui.FS()),
		},
	})
	if runtime.GOOS == "darwin" {
		bindDesktopNoZoomRects(app)
	}
	themePath := desktopPreferencesPath(d.Home())
	themePreference, err := loadDesktopThemePreference(themePath)
	if err != nil {
		slog.Warn("pudding-desktop: load desktop theme preference", "path", themePath, "err", err)
	}
	initialThemeState := desktopThemeState{
		Theme:    themePreference,
		Resolved: resolveDesktopTheme(themePreference, app.Env.IsDarkMode()),
	}

	// 窗口 chrome(docs/design.md 2.3):macOS 用 HiddenInset 隐藏标题栏,
	// 红绿灯由 NSToolbar inset rule 定位(绝不 cgo setFrame,旧项目踩坑);
	// URL 附 ?shell=mac 让页面启用 --traffic-inset / --toolbar-h 让位。
	//
	// 页面 --toolbar-h 与 cgo 的 kPuddingToolbarHeight 同值:视觉工具条
	// 与双击 zoom 检测区对齐。拖窗只走前端 .drag-region,避免 native
	// invisible titlebar 抢掉按钮区域的 no-drag。
	//
	// 页面由 Wails AssetServer 托管,Wails runtime 可用;核心业务 API
	// 显式走 daemon HTTP,desktop native 状态走 @wailsio/runtime events。
	// Custom workaround #5: NSWindow zoom: swizzle is temporarily disabled.
	// Keep the implementation for reference, but do not install it while the
	// desktop chrome path is being aligned with Wails-native behavior.
	// installZoomSwizzle()
	windowOpts := application.WebviewWindowOptions{
		Title:     "Pudding",
		URL:       launchURL(d.Token(), "http://"+d.Addr(), desktopShell(), initialThemeState),
		Width:     1200,
		Height:    800,
		MinWidth:  760,
		MinHeight: 520,
		// 不透明窗口底:WKWebView 在 zoom/失焦/遮挡等合成空档会先露出
		// 自己的默认 canvas。这里对齐暗色主题底色,避免层间跳色。
		// BackgroundColour: application.NewRGB(28, 28, 28),
	}
	if runtime.GOOS == "darwin" {
		// 启动白闪修复:先隐藏窗口,等 web 首帧导航完成再显示(见下方 darwin hook)。
		// 内容就绪前 WKWebView 是白底,window 深色底兜不住那一帧——只能不显示。
		windowOpts.Hidden = true
		windowOpts.Mac = application.MacWindow{
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				Hide:                 false,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           true,
				HideToolbarSeparator: true,
				ToolbarStyle:         application.MacToolbarStyleUnified,
			},
			Appearance: macAppearanceForTheme(themePreference),
		}
	}
	window := app.Window.NewWithOptions(windowOpts)

	themeManager := newDesktopThemeManager(app, window, themePath, themePreference)
	themeManager.bind()
	themeManager.apply(false)

	var hideAfterFullscreenExit atomic.Bool
	if runtime.GOOS == "darwin" {
		bindMacWindowEvents(window, &hideAfterFullscreenExit, func() {
			themeManager.apply(false)
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

func configureFrontendDevServer() {
	dev := strings.TrimSpace(os.Getenv("PUDDING_DEV_URL"))
	if dev == "" {
		return
	}
	_ = os.Setenv("FRONTEND_DEVSERVER_URL", strings.TrimRight(dev, "/"))
	slog.Info("pudding-desktop: dev url override", "dev", dev)
}

func desktopShell() string {
	if runtime.GOOS == "darwin" {
		return "mac"
	}
	return ""
}

func launchURL(token, apiBase, shell string, theme desktopThemeState) string {
	u := url.URL{Path: "/"}
	q := u.Query()
	q.Set("token", token)
	q.Set("api", apiBase)
	q.Set("theme", string(theme.Theme))
	q.Set("resolved", string(theme.Resolved))
	if shell != "" {
		q.Set("shell", shell)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func bindMacWindowEvents(window *application.WebviewWindow, hideAfterFullscreenExit *atomic.Bool, applyWindowBase func()) {
	var windowShown atomic.Bool
	showOnce := func() {
		if !windowShown.Swap(true) {
			window.Show()
			window.Focus()
		}
	}

	window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		time.AfterFunc(200*time.Millisecond, showOnce)
	})
	time.AfterFunc(3*time.Second, showOnce)

	window.RegisterHook(events.Common.WindowFocus, func(*application.WindowEvent) {
		attachDoubleClickToZoom(window)
		applyWindowBase()
	})

	window.RegisterHook(events.Mac.WindowWillEnterFullScreen, func(*application.WindowEvent) {
		setTitlebarAppearsTransparent(window, false)
		setHideTitle(window, false)
		setUseToolbar(window, false)
		setToolbarStyle(window, application.MacToolbarStyleUnifiedCompact)
	})
	window.RegisterHook(events.Mac.WindowWillExitFullScreen, func(*application.WindowEvent) {
		setTitlebarAppearsTransparent(window, true)
		setHideTitle(window, true)
		setUseToolbar(window, true)
		setToolbarStyle(window, application.MacToolbarStyleUnified)
		setTrafficLightsHidden(window, true)
	})
	window.RegisterHook(events.Mac.WindowDidExitFullScreen, func(*application.WindowEvent) {
		setTitlebarAppearsTransparent(window, true)
		setTrafficLightsHidden(window, false)
	})

	// RegisterHook 不会打开原生事件监听;这里显式订阅。
	window.OnWindowEvent(events.Mac.WindowWillEnterFullScreen, func(*application.WindowEvent) {})
	window.OnWindowEvent(events.Mac.WindowWillExitFullScreen, func(*application.WindowEvent) {})

	window.OnWindowEvent(events.Mac.WindowDidExitFullScreen, func(*application.WindowEvent) {
		if hideAfterFullscreenExit.Swap(false) {
			window.Hide()
		}
	})
}
