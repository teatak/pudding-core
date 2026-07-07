# Electron Migration Plan

> 状态:P3 Go Browser Bridge 基础控制已接入。  
> 日期:2026-07-07。  
> 决策:迁移到 Electron shell,浏览器显示改为原生 `WebContentsView`;保留 Go daemon 作为业务核心。

## 背景

当前 Wails 方案里的内嵌浏览器本质是:

```text
Chrome CDP screencast -> WebSocket -> React img/canvas
```

这带来几个根问题:

- 画面是截图流,不是原生 DOM/GPU 渲染,清晰度很难达到真正浏览器水平。
- session 切换时需要重连 WebSocket 和等待首帧,容易卡在 loading。
- screencast start/stop、target 恢复、首帧超时会互相打断。
- internal/external 依赖外部 Chrome/profile,登录态和 tab 生命周期难以稳定统一。

新方向:

```text
Electron native WebContentsView -> 直接显示真实 Chromium 页面
Go daemon -> 继续负责 sessions / messages / tools / storage
```

## 目标

- 去掉 screencast 主链路。
- 浏览器画面使用 Electron 原生 Chromium 渲染。
- 所有 session 共用一个持久 profile,登录态全局复用。
- 每个 session 拥有自己的 browser slot/tab,切 session 不串页。
- tab 切换、小组件切换、canvas/browser 切换不销毁 `webContents`。
- internal/external 只是同一个 `webContents` 在主窗口和外部窗口之间移动。
- LLM 继续能通过 CDP 控制页面。
- 应用重启后只恢复可验证或新建成功的真实 `webContents`,不显示假内容。

## 非目标

- 不把 Go daemon 改成 Node 后端。
- 不把业务 REST/SSE 迁到 Electron IPC。
- 不新增无 session scope 的业务 API。
- 不继续优化 screencast 清晰度和首帧恢复作为长期路线。

## 目标架构

```text
Electron main
  ├─ app windows / tray / native dialogs
  ├─ BrowserHost
  │   ├─ global persistent profile: persist:pudding-default
  │   ├─ sessionID -> BrowserSlot
  │   ├─ tabID -> WebContentsView / webContents
  │   ├─ internal attach: main window contentView
  │   └─ external attach: BrowserWindow
  └─ Go daemon child process

Renderer React
  ├─ 继续通过 daemon HTTP/SSE 读写业务状态
  └─ 仅通过 Electron IPC 汇报浏览器容器 bounds/attach/detach

Go daemon
  ├─ sessions / messages / turns / tools / SQLite
  ├─ 保留现有 /sessions/{id}/browser/... API
  └─ browser service 改为 Electron bridge client
```

边界:

- Go 是业务事实源。
- Electron main 是浏览器运行态事实源。
- renderer 不直接拥有 browser tab 生命周期。
- 所有 browser bridge 消息必须显式带 `sessionID` 和 `tabID`。

## BrowserHost 状态模型

```text
empty
  -> metadata_only
  -> creating
  -> live_internal
  -> live_external
  -> detached
  -> recovering
  -> closed
  -> lost
```

关键规则:

- `tabID` 是 Pudding 稳定 ID,`webContents.id` 只是运行态 ID。
- profile 全局唯一:`persist:pudding-default`。
- `webContents` 尽量长期保活,切 session 只 detach/attach view。
- close tab 必须销毁当前 session 的 `webContents`,清 slot,不影响其他 session。
- 重启后先进入 `metadata_only`;只有成功创建或绑定真实 `webContents` 后才显示页面。

## Bridge 协议

Go daemon 保留现有 session-scoped HTTP API:

```text
POST /sessions/{id}/browser/tabs
GET  /sessions/{id}/browser/tabs
GET  /sessions/{id}/browser/state
POST /sessions/{id}/browser/tabs/{tabID}/open
POST /sessions/{id}/browser/tabs/{tabID}/back
POST /sessions/{id}/browser/tabs/{tabID}/forward
POST /sessions/{id}/browser/tabs/{tabID}/reload
POST /sessions/{id}/browser/tabs/{tabID}/observe
POST /sessions/{id}/browser/tabs/{tabID}/screenshot
POST /sessions/{id}/browser/tabs/{tabID}/click
POST /sessions/{id}/browser/tabs/{tabID}/type
POST /sessions/{id}/browser/tabs/{tabID}/scroll
POST /sessions/{id}/browser/tabs/{tabID}/recover
POST /sessions/{id}/browser/close
```

内部实现改为转发到 Electron BrowserHost:

```json
{
  "requestID": "...",
  "sessionID": "...",
  "tabID": "...",
  "action": "open|observe|click|type|scroll|screenshot|close|recover",
  "params": {}
}
```

返回统一 slot snapshot:

```json
{
  "sessionID": "...",
  "tabID": "...",
  "status": "live_internal",
  "url": "...",
  "title": "...",
  "canGoBack": false,
  "canGoForward": false,
  "profileID": "default",
  "runtimeID": "webContents:12",
  "version": 7
}
```

## LLM 控制

LLM 工具仍走 Go:

```text
model tool call
  -> Go tool executor
  -> /sessions/{id}/browser/... service
  -> Electron BrowserHost
  -> webContents.debugger / webContents API
```

实现原则:

- `observe/click/type/scroll/screenshot` 通过 `webContents.debugger` 发送 CDP 命令。
- `open/back/forward/reload` 优先用 `webContents` 原生 API。
- screenshot 仍可保留为工具结果,但不是 UI 显示链路。
- DevTools 打开导致 debugger detach 时,BrowserHost 要重新 attach 或返回可恢复错误。

## Internal / External

Electron 后 external 不再依赖系统 Chrome:

- internal:同一个 `WebContentsView` 挂在主窗口浏览器区域。
- external:把同一个 `webContents` 移到独立 `BrowserWindow`。
- return internal:从外部窗口移回主窗口。
- 多 session external:多个 session 各自的 `webContents` 可放在多个外部窗口。
- 因为 profile 同属 Electron persistent session,授权 cookie/localStorage 可直接回到 internal。

## Screencast 删除计划

迁移完成后删除:

- `/sessions/{id}/browser/tabs/{tabID}/screencast`
- `BrowserStream.tsx`
- `Page.startScreencast` / `Page.stopScreencast`
- 首帧超时重连逻辑
- WebSocket image frame 协议
- DPR/PNG 清晰度补丁
- `Browser state failed to load` 这类 stream 专属错误

保留:

- CDP screenshot 工具能力。
- session-scoped browser REST API。
- browser tab metadata 持久化。

## 迁移阶段

### P0 准备

- 冻结当前 Wails browser 重构,不继续深挖 screencast。
- 梳理 Wails bindings 能力清单,映射到 Electron IPC。
- 明确 BrowserHost bridge 类型和错误码。
- 更新 `technology-decisions.md`,记录从 Wails 迁到 Electron 的原因。

验收:

- 有迁移分支。
- 有 browser bridge 类型草案。
- 现有 Go/API 测试保持可跑。

### P1 Electron Shell POC

- 新增 Electron app skeleton。
- Electron main 启动 Go daemon 子进程。
- renderer 加载现有 Vite/React UI。
- REST/SSE 仍直连 daemon。
- macOS 使用隐藏标题栏,红绿灯固定为 `trafficLightPosition: { x: 18, y: 18 }`;会话 toolbar、画布 toolbar、rail toggle、全局拖拽带统一通过 `electron-mac` 的 CSS vars 对齐。
- 开发入口:`make electron-dev`。

验收:

- Electron 窗口内能打开现有聊天 UI。
- session 列表、消息、submit、SSE 正常。
- 不涉及浏览器能力。

### P2 Native Browser POC

- 在 Electron main 实现 `BrowserHost` 最小版。
- 使用 `WebContentsView` 打开一个 URL。
- 使用持久 partition:`persist:pudding-default`。
- renderer 只汇报浏览器区域 bounds。
- 当前落点:Electron preload 暴露最小 browser IPC;前端 `BrowserStream` 在 Electron 壳内自动切到原生 `WebContentsView`,非 Electron 继续使用旧 screencast。
- 暂未迁移:Go browser bridge、LLM CDP 工具仍走旧 API。

验收:

- Google/Baidu 页面清晰度接近普通浏览器。
- resize 不糊、不靠截图流。
- 切 canvas/browser 不销毁页面。

### P3 Go Browser Bridge

- Go `internal/browser` 改成 bridge client。
- 现有 `/sessions/{id}/browser/...` API 不变。
- Electron BrowserHost 支持 open/state/tabs/close/recover。
- 当前落点:Electron main 启动 loopback bridge server,daemon 通过 `PUDDING_ELECTRON_BROWSER_BRIDGE_URL/TOKEN` 切换到 `ElectronBridgeService`。
- 已完成:open/list/get/back/forward/reload/close tab/close session 的基础 bridge。
- 未完成:observe/screenshot/click/type/scroll 仍返回 unavailable,等待 P4 用 `webContents.debugger` 接入。

验收:

- Go browser API 能操作 Electron BrowserHost。
- LLM browser open 能打开当前 session tab。
- session A/B tab 不互串。

### P4 CDP Tool Parity

- 用 `webContents.debugger` 实现 observe/screenshot/click/type/scroll。
- 错误统一映射到 Go browser error。
- DevTools detach、webContents destroyed、navigation timeout 都有 recover 路径。

验收:

- 现有 browser tool 测试迁移到 bridge fake。
- 手动能完成打开、观察、点击、输入、滚动、截图。

### P5 Multi-session Lifecycle

- 实现稳定 slot 状态机。
- session 切换只切当前 attach 的 view。
- 非当前 session 的 `webContents` 保活但不显示。
- tab close 幂等且彻底。
- app restart 从 metadata 恢复,无真实页面时不显示假内容。

验收:

- session 切换不 loading 漂移。
- 小组件 tab 切换立即恢复画面。
- close tab 不复活。
- 重启后只显示真实恢复出来的页面。

### P6 External Window

- external 打开改为 Electron app-owned window。
- 同一个 `webContents` 在 internal/external 之间移动。
- 多 session external 支持多个窗口。

验收:

- Google 登录后切回 internal 仍登录。
- session A external 不影响 session B internal。
- external 窗口关闭后 slot 状态明确,可恢复或关闭。

### P7 删除 Wails / Screencast

- 删除 Wails desktop shell。
- 删除 screencast route 和前端 stream。
- 删除 Chrome process/profile 直接管理代码中不再需要的部分。
- 保留必要的 browser metadata store 和 Go API facade。

Wails legacy 清理范围:

- `cmd/pudding-desktop/main.go`:启动隐藏窗口、等待 `WebViewDidFinishNavigation` 再显示的 WKWebView 白屏防御,Electron 路径不需要。
- `cmd/pudding-desktop/chrome_darwin.go`:红绿灯、toolbar、fullscreen、双击 zoom 等 Wails/macOS chrome 补丁,Electron 由 `BrowserWindow` 配置和前端 CSS 处理。
- `cmd/pudding-desktop/window_preferences.go`:Wails 窗口尺寸记忆,Electron 已由 main process 持久化 window state。
- `cmd/pudding-desktop/theme.go`:Wails theme bridge,Electron 已由 preload/main IPC 和 `nativeTheme` 负责。
- `cmd/pudding-desktop/no_zoom_rects.go`:Wails zoom 排除区域,Electron 侧用 drag/no-drag 区域处理。
- `cmd/pudding-desktop/file_drop.go`、`cmd/pudding-desktop/locale.go`:删除前需要确认 Electron 已补齐文件拖拽和 locale bridge。
- `web` 中 `@wailsio/runtime` fallback、`internal/api/cors.go` 中 `wails://` origin 特判、`Makefile` 中 `desktop/desktop-dev` 入口,随 Wails shell 一起移除。

清理原则:

- 不再把上述 Wails 防御代码迁移到 Electron。
- 不零散删除某个补丁,避免留下半残 Wails fallback。
- Electron packaging、file drop、locale、theme、窗口状态全部稳定后,一次性删除 Wails shell。

验收:

- repo 中没有 UI 显示依赖 screencast。
- build/test 不引用 Wails browser display 链路。
- screenshot 工具仍可用。

### P8 Packaging

- Electron 打包、签名、公证、自动更新。
- dev/release 数据目录继续隔离。
- crash/log 收集落本地文件。

验收:

- macOS dev build 可安装运行。
- release profile 不污染 dev profile。
- 退出 app 后 Go daemon 和 Electron 子进程可控清理。

## 手动验收清单

- session A 打开 Google,session B 打开 Baidu,来回切换不串页。
- 切到 Widgets 再切回 Browser,画面立即恢复。
- canvas/browser 反复切换,页面 DOM 不重载。
- Google 登录后新 session 打开 Google,保持登录态。
- external 打开授权,回 internal 后 cookie 生效。
- 两个 session 同时 external,互不抢窗口。
- close 当前 tab,当前 session 清空,其他 session 不受影响。
- close 后刷新/切 session 不自动复活旧 tab。
- app 重启后 metadata 存在但真实页面未恢复时,UI 不显示旧截图。
- DevTools 打开/关闭后 LLM browser 工具能恢复。
- 导航超时、webContents destroyed、bridge 断开都有可读错误和 recover。

## 风险

- Electron 包体会明显变大。
- Electron native capability 需要替换 Wails bindings。
- BrowserHost bridge 是新边界,需要测试保护。
- `webContents.debugger` 和 DevTools 互斥场景要重点处理。
- 外部窗口移动同一个 `webContents` 的边界要做 POC 验证。

## 参考

- Electron `WebContentsView`: https://www.electronjs.org/docs/latest/api/web-contents-view
- Electron `BrowserView` deprecation: https://www.electronjs.org/docs/latest/api/browser-view
- Electron `Debugger`: https://www.electronjs.org/docs/latest/api/debugger
- Electron persistent session partition: https://www.electronjs.org/docs/latest/api/session
