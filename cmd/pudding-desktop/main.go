// pudding-desktop 是 Pudding 的桌面壳:daemon 同进程内嵌(单二进制),
// 窗口加载带 token 的本地 URL(内存直传,无手贴),tray 常驻。
// 通道单端口,attach-or-start:端口空闲则内嵌启动 daemon;被活的
// pudding daemon 占用(如 CLI 先起)则直接挂上去,壳退化为纯视图。
// 边界:壳只做启动与系统集成,不碰 session runtime
// (docs/technology-decisions.md 第 4 节)。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/teatak/pudding-core/internal/daemon"
	"github.com/teatak/pudding-core/internal/home"
)

// attachExisting 在通道端口被占时探测占用者:用 home 下持久化的
// daemon.token 鉴权访问 /sessions,通了说明是同通道的活 pudding
// daemon(token 是 load-or-create 的,跨宿主同一把),返回直连 URL。
func attachExisting(addr string) (string, bool) {
	dir, err := home.Resolve("")
	if err != nil {
		return "", false
	}
	raw, err := os.ReadFile(home.TokenPath(dir))
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", false
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/sessions", addr), nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	return fmt.Sprintf("http://%s/?token=%s", addr, token), true
}

func main() {
	addr := home.DefaultAddr()
	// d 为 nil 表示 attach 模式:daemon 归别的进程管,壳只是视图,
	// 退出时不做 Shutdown,也没有 serve 错误可监听。
	var openURL string
	d, err := daemon.Start(daemon.Options{Addr: addr, DefaultModel: "mock-model"})
	switch {
	case err == nil:
		openURL = d.OpenURL()
	default:
		url, ok := attachExisting(addr)
		if !ok {
			slog.Error("pudding-desktop: start daemon failed and no live pudding daemon to attach",
				"addr", addr, "err", err)
			os.Exit(1)
		}
		slog.Info("pudding-desktop: attached to existing daemon", "addr", addr)
		openURL = url
		d = nil
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

	// 内嵌模式才有 serve 生命周期要管;attach 模式 daemon 归外部进程,
	// 它退出后页面自然断连,壳不代管。
	if d != nil {
		// daemon serve 异常退出时带走壳,避免空窗口假活
		go func() {
			if err := <-d.ServeErr(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("pudding-desktop: daemon serve", "err", err)
				app.Quit()
			}
		}()
	}

	if err := app.Run(); err != nil {
		slog.Error("pudding-desktop: run", "err", err)
	}

	if d != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Shutdown(shutdownCtx); err != nil {
			slog.Error("pudding-desktop: shutdown daemon", "err", err)
		}
	}
}
