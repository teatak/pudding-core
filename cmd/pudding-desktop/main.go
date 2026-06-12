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
	// 红绿灯悬浮在页面上;URL 附 ?shell=mac 让页面启用 --traffic-inset 让位。
	// 拖拽区由页面侧 --wails-draggable 标注(48px 工具条带),隐形标题栏
	// 同高作原生兜底;全屏检测与双击缩放由页面经注入的 window.wails 自治
	// (state/shell.ts),壳不再下发 ExecJS。
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
			InvisibleTitleBarHeight: 48,
		}
	}
	window := app.Window.NewWithOptions(windowOpts)

	// 关窗 = 隐藏:daemon 常驻后台,tray 可唤回;退出只走 tray 菜单
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
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
