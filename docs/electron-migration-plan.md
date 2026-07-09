# Electron Migration Plan

> 状态:P5 Multi-session Lifecycle 已进入手动验收收尾;P7 旧 native/screencast surface 与旧 desktop shell 清理已完成;P6 External Window 暂停,不进入当前主线验收。
> 日期:2026-07-08。
> 决策:迁移到 Electron shell,浏览器显示改为 Electron `<webview>`;保留 Go daemon 作为业务核心。

## 背景

旧方案里的内嵌浏览器本质是:

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
Electron <webview> -> 直接显示真实 Chromium 页面
Go daemon -> 继续负责 sessions / messages / tools / storage
```

## 目标

- 去掉 screencast 主链路。
- 浏览器画面使用 Electron 原生 Chromium 渲染。
- 所有 session 共用一个持久 profile,登录态全局复用。
- 每个 session 拥有自己的 browser slot/tab,切 session 不串页。
- tab 切换、小组件切换、canvas/browser 切换不销毁 `webContents`。
- internal 由 renderer `<webview>` 承载;外部打开先禁用,后续重新设计。
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
  │   ├─ tabID -> registered webview webContents
  │   └─ headless webContents for LLM tools before UI mounts
  └─ Go daemon child process

Renderer React
  ├─ 继续通过 daemon HTTP/SSE 读写业务状态
  └─ 通过 Electron IPC 注册 webview webContents

Go daemon
  ├─ sessions / messages / turns / tools / SQLite
  ├─ 保留现有 /sessions/{id}/browser/... API
  └─ browser service 改为 Electron bridge client
```

边界:

- Go 是业务事实源。
- Electron main 是浏览器工具 target 的运行态事实源。
- renderer 拥有可见 webview DOM,但 tab metadata 仍通过 daemon/bridge 对账。
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

当前阶段先只保留 internal:

- internal:renderer `<webview>` 显示真实 Chromium 页面。
- BrowserHost 记录 webview `webContents`,供 Go browser tools 调用 CDP。
- external 不进入当前主线。Electron app-owned external window 不能稳定覆盖 passkey/Touch ID 授权。
- 后续如恢复外部授权,应走系统真实 Chrome/Safari 或系统浏览器 callback,再把授权结果带回 internal。

## Screencast 删除计划

已删除:

- `/sessions/{id}/browser/tabs/{tabID}/screencast`
- `BrowserStream.tsx` 旧薄壳和其中的 screencast fallback
- `Page.startScreencast` / `Page.stopScreencast`
- 首帧超时重连逻辑
- WebSocket image frame 协议
- DPR/PNG 清晰度补丁

待确认:

- `Browser state failed to load` 这类 stream 专属错误

保留:

- CDP screenshot 工具能力。
- session-scoped browser REST API。
- browser tab metadata 持久化。

## 迁移阶段

### P0 准备

- 冻结旧 browser 重构,不继续深挖 screencast。
- 梳理 desktop native 能力清单,映射到 Electron IPC。
- 明确 BrowserHost bridge 类型和错误码。
- 更新 `technology-decisions.md`,记录迁到 Electron 的原因。

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
- 开发入口:`make desktop-dev`。

验收:

- Electron 窗口内能打开现有聊天 UI。
- session 列表、消息、submit、SSE 正常。
- 不涉及浏览器能力。

### P2 Native Browser POC

- 在 Electron main 实现 `BrowserHost` 最小版。
- 使用 renderer `<webview>` 打开一个 URL。
- 使用持久 partition:`persist:pudding-default`。
- renderer 在 webview ready/navigation/title/favicon 事件后向 main 注册 `webContentsID`。
- 当前落点:Electron preload 暴露 webview 注册和 browser IPC;前端 Canvas 直接渲染 `ElectronWebviewBrowser`,不再保留 `BrowserStream` 或 screencast fallback。
- 当前落点:Go browser service 在 Electron 桌面下走 Electron bridge;LLM 工具仍调用 session-scoped browser API,底层转到 BrowserHost/CDP。

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
- 已完成:open/list/get/back/forward/reload/close tab/close session/observe/screenshot/click/type/scroll 的基础 bridge。

验收:

- Go browser API 能操作 Electron BrowserHost。
- LLM browser open 能打开当前 session tab。
- session A/B tab 不互串。

### P4 CDP Tool Parity

- 用 `webContents.debugger` 实现 observe/screenshot/click/type/scroll。
- 错误统一映射到 Go browser error。
- DevTools detach、webContents destroyed、navigation timeout 都有 recover 路径。
- 当前落点:Electron BrowserHost 已补齐 observe/screenshot/click/type/scroll;Go `ElectronBridgeService` 已接入并补 bridge fake 测试。

验收:

- 现有 browser tool 测试迁移到 bridge fake。
- 手动能完成打开、观察、点击、输入、滚动、截图。

### P5 Multi-session Lifecycle

- 实现稳定 slot 状态机。
- session 切换只切当前 attach 的 view。
- 非当前 session 的 `webContents` 保活但不显示。
- tab close 幂等且彻底。
- app restart 从 metadata 恢复,无真实页面时不显示假内容。
- 当前落点:
  - Electron bridge 声明支持 metadata recovery;`/browser/state` 可在 Electron 下从持久 URL 重建真实 tab,旧 Chrome manager 仍不恢复 internal metadata。
  - Electron webview surface 区分无浏览器、显式新选项卡、真实页面;新选项卡以 `about:blank` metadata 持久化。
  - BrowserHost 不再 attach view 到窗口,只维护 webview `webContents` 注册表和 LLM 工具 target。
  - 浏览器不再作为 canvas item 落库;Canvas 只负责展示 browser surface。
  - LLM 工具可在画布未打开时通过 BrowserHost 操作 session tab;画布打开后 attach 同一个真实 tab。
  - transcript 中 browser screenshot 附件复用图片预览/lightbox 展示。
  - LLM browser click/type/scroll 会在 webview surface 上显示自动化光标。
  - `closeTab` 在 Electron Host 内幂等,重复关闭返回 lost snapshot。
  - 关闭浏览器时前端进入 session-scoped closing guard,清空旧 tab/payload 缓存并丢弃并发创建结果,避免标题/icon 残留和关闭后复活。

验收:

- session 切换不 loading 漂移。
- 小组件 tab 切换立即恢复画面。
- close tab 不复活。
- 重启后只显示真实恢复出来的页面。

### P6 External Window(暂停)

- P6 不进入当前主线。GitHub passkey/Touch ID 验证显示 Electron Chromium 只能得到 partial passkey support,无法稳定覆盖“真实浏览器授权”场景。
- Electron 路径继续禁用旧外部打开:Toolbar 隐藏按钮,Go 后端不暴露 reveal/internal route。
- 当前落点:前端已删除外部打开/回内部入口和对应 client helper;后端 API surface 已移除 reveal/internal;Chrome manager 中无入口的 `Reveal/Internal/switchMode` 旧切换方法已删除。底层 attach/recover 仍保留为非 Electron manager 的恢复路径。
- 后续如恢复外部授权,应走系统真实 Chrome/Safari 或系统浏览器 callback,而不是 Electron app-owned external window。

暂停原因:

- passkey/Touch ID 授权不是 Electron `<webview>` / Electron BrowserWindow 当前能可靠承诺的能力。
- 当前浏览器主线目标是 internal webview + LLM 工具生命周期稳定,不再被 external 授权回流阻塞。

### P7 删除旧 Desktop Shell / Screencast

- 删除旧 desktop shell。
- 删除 screencast route 和前端 stream。已完成前端 fallback、Go route、BrowserService 接口、manager controller/CDP loop 清理。
- 删除 Chrome process/profile 直接管理代码中不再需要的部分。
- 保留必要的 browser metadata store 和 Go API facade。

Legacy 清理范围:

当前盘点(2026-07-08):

- 运行代码中未发现 `BrowserStream`、`Page.startScreencast`、native attach/bounds 等 UI 显示主链路引用。
- `electron/browser-host.cjs` 仍保留内部 `WebContentsView`,只作为画布未挂载时的 invisible LLM tool target。
- 已删除旧 desktop shell/runtime、旧 desktop CORS 特判和 Makefile 旧入口。
- 已删除旧 shell 的窗口白屏防御、macOS chrome 补丁、窗口尺寸记忆、theme bridge、zoom 排除区域、file drop 和 locale bridge。
- Electron 已接管 theme、locale、window state、external open、directory picker 和 browser webview surface;native file drop 如需恢复,走 Electron IPC 新增。

旧 Electron native browser surface 清理状态:

- 已删除 `web/src/browser/electronNative.tsx` 旧 `WebContentsView` attach/bounds 组件。
- 已删除 `BrowserStream.tsx`;Canvas 直接渲染 `ElectronWebviewBrowser`,Electron 下只走 `<webview>` surface。
- 已删除 `electronBridge.ts` 的 `attach`、`updateBounds`、`detach`、`hasElectronNativeBrowser`、`ElectronBrowserBounds` 等 native surface API。
- 已删除 `electron/main.cjs` / `preload.cjs` 的 native attach/bounds/detach IPC 和 embed mode fallback;`webviewTag` 固定开启。
- 已删除 `CanvasPane` -> `BrowserStream` 的 `nativeSuspended` 无效传参和 no-op cursor 清理。
- 保留 `electron/browser-host.cjs` 内部的 `WebContentsView`,但仅作为 UI webview 尚未注册时的 invisible headless tool target,不再 attach 到主窗口。

BrowserHost 生命周期收口审查(2026-07-09):

- LLM 工具不依赖画布是否打开:画布未挂载时由 `electron/browser-host.cjs` 的 invisible `WebContentsView` 承载真实 `webContents`;画布挂载后 renderer `<webview>` 注册到同一 `sessionID/tabID` slot。
- 画布浏览器 tab 是 session browser slot 的 UI 入口,不是 canvas item;点击 tab 只切换到 browser surface,只有当前 session 没有 tab/state 时才创建新 tab。
- 关闭 tab 等价于关闭当前 session browser slot:`closeSession`/`closeTab` 会销毁 slot webContents 并发出 lost snapshot;前端 close gate 会丢弃并发旧 snapshot,避免关闭后复活。
- 普通 canvas item tab 与 browser surface 共用同一画布区域,但不拥有浏览器生命周期;切普通小组件只隐藏 browser surface,不以 canvas item 方式恢复或重建浏览器。
- `requestBrowserReveal` 是“LLM 工具事件触发当前 session 显示浏览器 surface”的前端 UI 事件,不是旧 external reveal/internal 切换。

清理原则:

- 不再把上述旧 shell 防御代码迁移到 Electron。
- 不零散删除某个补丁,避免留下半残 fallback。
- Electron packaging 后续单独补齐,不阻塞旧 shell 删除。
- Electron browser surface 切到 webview 后,旧 native surface 也按上面清单一次性删除,避免继续在两套嵌入模型之间打补丁。

验收:

- repo 中没有 UI 显示依赖 screencast。
- build/test 不引用旧 browser display 链路。
- screenshot 工具仍可用。

### P8 Packaging

- Electron 打包、签名、公证、自动更新。
- dev/release 数据目录继续隔离。
- crash/log 收集落本地文件。
- 当前落点:`make desktop-bundle` 可生成基础 macOS `.app` bundle,内含 Electron shell、Tray、release daemon、daemon 启动前 dylib、Info.plist、icon、`NSMicrophoneUsageDescription` 和 `pudding://` callback scheme。
- 当前 bundle 使用 ad-hoc codesign;正式 Developer ID 签名、公证、自动更新、正式 crash/log 收集后置。
- 2026-07-09 收尾验证:`make desktop-bundle` 成功,`codesign --verify --deep --strict` 通过,短启动 `dist/Pudding.app` 可拉起 release daemon。

验收:

- macOS bundle 可安装运行。
- release profile 不污染 dev profile。
- 退出 app 后 Go daemon 和 Electron 子进程可控清理。

## 手动验收清单

- session A 打开 Google,session B 打开 Baidu,来回切换不串页。
- 切到 Widgets 再切回 Browser,画面立即恢复。
- canvas/browser 反复切换,页面 DOM 不重载。
- Google 登录后新 session 打开 Google,保持登录态。
- close 当前 tab,当前 session 清空,其他 session 不受影响。
- close 后刷新/切 session 不自动复活旧 tab;重新打开会创建新的真实 webview tab。
- app 关闭/重开后只显示可恢复或新建成功的真实 browser surface,不显示旧截图。
- LLM browser open / observe / click / type / scroll / screenshot 能操作当前 session tab。
- 画布开关、普通 canvas item tab 和 browser surface 来回切换不破坏浏览器生命周期。
- DevTools 打开/关闭后 LLM browser 工具能恢复。
- 导航超时、webContents destroyed、bridge 断开都有可读错误和 recover。
- 语音 runtime / bundle 验收见 `docs/voice-migration-plan.md` 的“最终手动验收清单草案”。

## 风险

- Electron 包体会明显变大。
- Electron native capability 需要继续收敛到 preload/IPC bridge。
- BrowserHost bridge 是新边界,需要测试保护。
- `webContents.debugger` 和 DevTools 互斥场景要重点处理。
- 外部真实浏览器授权回流如后续恢复,需要单独设计 callback / profile 同步方案。

## 参考

- Electron `WebContentsView`: https://www.electronjs.org/docs/latest/api/web-contents-view
- Electron `BrowserView` deprecation: https://www.electronjs.org/docs/latest/api/browser-view
- Electron `Debugger`: https://www.electronjs.org/docs/latest/api/debugger
- Electron persistent session partition: https://www.electronjs.org/docs/latest/api/session
