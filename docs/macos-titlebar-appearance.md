# macOS titlebar / toolbar appearance 记录

> 状态:验证方向。  
> 日期:2026-06-17。  
> 背景:Wails v3 + macOS 自定义标题栏 / native toolbar / 亮暗主题同步。

## 1. 现象

在 macOS 下使用:

```go
Mac: application.MacWindow{
	TitleBar: application.MacTitleBar{
		AppearsTransparent:   true,
		Hide:                 false,
		HideTitle:            true,
		FullSizeContent:      true,
		UseToolbar:           true,
		HideToolbarSeparator: true,
	},
	Appearance: application.DefaultAppearance,
}
```

普通窗口下可以获得想要的自定义标题区和红绿灯 inset。

但进入 fullscreen 后,如果系统主题 / app 主题组合发生错位,toolbar 背景、
失焦红绿灯、菜单栏外观可能不跟随当前 appearance 正确刷新。

对照实验:

- `UseToolbar: true`
- `AppearsTransparent: false`

fullscreen 下 toolbar 背景和 appearance 表现稳定,但普通窗口失去网页自定义
标题区的完整控制。

## 2. 判断

问题核心不是 `UseToolbar` 本身,而是:

```go
AppearsTransparent: true
```

开启后,titlebar / toolbar 背景由透明合成路径处理。native toolbar 仍存在,
负责红绿灯位置和 fullscreen chrome,但 AppKit 不再稳定地重绘 toolbar
背景材料。

`AppearsTransparent: false` 时,AppKit 接管 titlebar / toolbar 背景绘制,
fullscreen、失焦、系统 appearance 变化都更稳定。

## 3. 当前可行方向

普通窗口保持透明标题栏,fullscreen 期间临时恢复系统绘制:

```text
普通窗口:
  AppearsTransparent = true

WindowWillEnterFullScreen:
  AppearsTransparent = false

WindowWillExitFullScreen:
  AppearsTransparent = true
```

这个方案只切一个 AppKit 状态,比手动调整红绿灯位置、反复切
`UseToolbar`、改 `styleMask` 更干净。

## 4. 实现草图

Wails v3 当前没有公开 runtime setter,需要 darwin cgo 补一个很薄的调用:

```go
static void setTitlebarAppearsTransparent(void *nsWindowPtr, bool transparent) {
	NSWindow *window = (NSWindow *)nsWindowPtr;
	if (!window) return;
	dispatch_async(dispatch_get_main_queue(), ^{
		[window setTitlebarAppearsTransparent:transparent];
	});
}
```

事件绑定:

```go
window.RegisterHook(events.Mac.WindowWillEnterFullScreen, func(*application.WindowEvent) {
	setTitlebarAppearsTransparent(window, false)
})

window.RegisterHook(events.Mac.WindowWillExitFullScreen, func(*application.WindowEvent) {
	setTitlebarAppearsTransparent(window, true)
})

// Wails 需要显式订阅,否则 native 事件不一定打开。
window.OnWindowEvent(events.Mac.WindowWillEnterFullScreen, func(*application.WindowEvent) {})
window.OnWindowEvent(events.Mac.WindowWillExitFullScreen, func(*application.WindowEvent) {})
```

## 5. 验证点

最小 demo 已验证:仅切 `AppearsTransparent` 可以触发 toolbar 重新绘制。

迁回 `pudding-core` 前继续确认:

- 系统深色 + app light:fullscreen toolbar 背景应切到浅色。
- 系统浅色 + app dark:fullscreen toolbar 背景应切到深色。
- 失焦状态红绿灯颜色跟随当前 app appearance。
- 退出 fullscreen 后普通窗口仍是自定义透明标题栏。
- zoom / resize / focus 不引入红绿灯位置漂移。

## 6. 不做

暂不走这些方向:

- 手动移动红绿灯 frame。
- fullscreen 期间切 `UseToolbar`。
- swizzle `zoom:` 或 titlebar 私有视图。
- 前端用 CSS 模拟 native toolbar 背景。

原因:这些方案变量多,容易在 fullscreen / zoom / 失焦 / 多屏组合下产生漂移。

