# Browser CDP Unification Plan

> 状态:Implemented
> 日期:2026-07-13
> 范围:Electron `<webview>` 路径与 Go browser manager 路径。
> 目标:所有网页导航、观察、截图和交互统一通过 CDP 执行。
> 架构决策:移除临时 headless `WebContentsView`,复用当前不会随 tab/session/Canvas 切换卸载的持久 `<webview>`。

## 1. 背景

改造前 browser 实现混合使用以下机制:

- CDP:`Page`、`Runtime`、`Input`。
- Electron:`webContents.loadURL()`、`goBack()`、`reload()`、`capturePage()`、`sendInputEvent()`。
- 页面 DOM:`el.click()`、value setter、`scrollBy()`。
- renderer `<webview>`:`loadURL()`、`capturePage()`。

混合路径带来以下问题:

- 同一操作在 Electron 与 Go 路径中行为不一致。
- 自动 fallback 可能重复执行 click/type 等有副作用动作。
- `about:blank` 与目标 URL 并发导航会产生状态覆盖竞态。
- 工具返回成功时,页面可能只收到导航意图,尚未完成主 frame commit。
- 截图、滚动和输入的测试需要分别覆盖多个实现。

本计划将页面操作收敛到 CDP,并把 Electron 限制在常驻 `<webview>` 容器生命周期。

## 2. 边界

### 2.1 必须使用 CDP

- open / back / forward / reload。
- observe。
- viewport / full-page screenshot。
- pointer click。
- type / clear / shortcut。
- wheel scroll。
- 页面 URL、title、history 与 navigation 状态读取。

### 2.2 保留 Electron API

以下操作不属于页面自动化,继续由 Electron 管理:

- 创建、持久挂载和销毁 renderer `<webview>`。
- sessionID + tabID 到 `webContents` 的显式绑定。
- partition/profile、IPC、窗口显示和 canvas surface。
- webview 被用户或系统销毁后的资源清理。

不使用 `Target.createTarget` 替代 Electron 容器创建。CDP 创建的 target 无法自然绑定到现有可见 `<webview>`,会破坏当前 UI 生命周期。

明确禁止:

- BrowserHost 创建临时或隐藏 `WebContentsView`。
- Canvas 收起、session 切换时卸载 live tab 的 `<webview>`。
- browser 工具在对应 `<webview>` 尚未注册时创建替代 target。

当前桌面实现已经满足 webview 持久挂载的主体条件:

- `BrowserSurface` 会为所有 live tab 持续渲染 `<ElectronWebviewBrowser>`。
- 非活动 tab 只通过 CSS `invisible` 隐藏,不会卸载。
- `retainedBrowserTabs` 会保留切换 session 前已经挂载的 browser surface。
- 桌面 Canvas 收起时 `CanvasPane` 仍保持挂载。

因此本计划不新增另一套 `BrowserWebviewPool`;只补齐首次创建握手和 renderer 不可用时的明确失败语义。

## 3. 目标架构

```text
session + tab
  -> existing persistent BrowserSurface(renderer)
      -> persistent <webview> (one per live tab)
      -> BrowserHost slot(main process)
          -> persistent CDP session
              -> serialized command queue
              -> navigation generation
              -> event waiters
              -> structured CDP errors
```

每个 live tab 只有一个持久 CDP session:

- renderer 使用现有 BrowserSurface 创建 `<webview>` 后立即注册到 BrowserHost。
- BrowserHost 在注册完成后 attach 一次。
- 同一 tab 的写操作串行执行。
- 导航使用单调递增 generation,旧事件不能覆盖新导航。
- Canvas 只切换布局和可见性,不创建、替换或销毁 webview。
- session 切换后,后台 tab 的 webview 仍由现有 retained browser surface 保留。
- webview 销毁时,取消 pending command、detach CDP 并将 slot 标记为 lost。
- Go manager 同样按 target 复用持久 WebSocket CDP session,关闭 tab/process 时统一释放。

### 3.1 新 tab 创建握手

```text
daemon OpenNewTab(sessionID, url)
  -> BrowserHost 创建 pending slot(tabID)
  -> IPC 通知 renderer 把 pending tab 加入现有 browser surfaces
  -> webview dom-ready
  -> renderer 注册 webContentsID
  -> BrowserHost attach CDP
  -> Page.navigate(url)
  -> 主 frame commit
  -> 工具返回成功
```

BrowserHost 等待 webview 注册应有明确超时。renderer 未启动、刷新中或已崩溃时返回 `browser_webview_not_ready`,禁止降级创建 `WebContentsView`。

### 3.2 常驻与显示

- 复用当前 CanvasPane/BrowserSurface 的持久渲染机制。
- `<webview>` DOM 节点在 tab 生命周期内保持挂载。
- Canvas 打开时只把对应 webview 切换到可见布局。
- Canvas 关闭时使用 CSS 隐藏,不能从 DOM 移除。
- session 切换继续通过 `retainedBrowserTabs` 保留已挂载 webview。
- renderer reload、Apps 视图或其他导致 CanvasPane 不存在的状态下,工具返回 `browser_webview_not_ready`,不创建替代 target。
- 常驻 tab 采用硬上限:单 session 8 个、全局 16 个;超限返回 `browser_tab_limit_reached`,不隐式淘汰或卸载旧 webview。

## 4. 操作映射

| 业务操作 | CDP 实现 | 完成条件 |
| --- | --- | --- |
| open | `Page.navigate` | 主 frame navigation committed |
| back / forward | `Page.getNavigationHistory` + `Page.navigateToHistoryEntry` | 目标 history entry committed |
| reload | `Page.reload` | 当前主 frame reload committed |
| observe | `Runtime.evaluate` | 返回当前 document 快照 |
| screenshot | `Page.getLayoutMetrics` + `Page.captureScreenshot` | 返回有效 PNG |
| click | `Runtime.evaluate` 定位 + `Input.dispatchMouseEvent` | mouseReleased 命令成功 |
| type | `Runtime.evaluate` 聚焦 + `Input.dispatchKeyEvent` | 真实键盘序列成功且最终值指纹与预期完全一致 |
| clear / shortcut | `Input.dispatchKeyEvent` | 完整 keyDown/keyUp 序列成功 |
| scroll | `Input.dispatchMouseEvent(type="mouseWheel")` | wheel 命令成功 |

`Runtime.evaluate` 属于 CDP。允许它读取 DOM、聚焦元素和计算坐标,但不再通过它执行 `el.click()`、value setter 或 `scrollBy()` 等写操作。

## 5. 错误与重试

### 5.1 禁止写操作自动 fallback

以下操作失败后直接返回结构化错误,不自动换实现:

- navigate / reload / history navigation。
- click。
- type / clear。
- scroll。

原因:CDP 命令可能已经生效但响应丢失,自动 fallback 或重试可能造成重复点击、重复输入或重复提交。

### 5.2 错误码

当前统一为:

- `browser_webview_not_ready`
- `cdp_detached`
- `cdp_command_failed`
- `cdp_command_timeout`
- `navigation_timeout`
- `navigation_failed`
- `element_not_found`
- `element_not_interactable`
- `element_not_editable`
- `screenshot_failed`
- `file_url_not_allowed`
- `browser_tab_limit_reached`

错误结果应携带 `retryable`。只有调用方明确发起新一次工具调用时才执行重试。

`file://` 仅允许访问当前 session 所属 Project 根目录内已存在的 regular file。daemon 负责解析 symlink 和签发根目录授权,Electron 只接受带 bridge token 的授权,并在页面跳转、历史导航和新窗口时持续校验同一范围。session 切换 Project、Project 根目录更新或 Project 删除时,宿主立即清除旧授权;仍停留在 `file://` 的 tab 关闭,普通 HTTP(S) tab 保留。

## 6. 实施阶段

### C0 基线与契约

- 锁定所有 browser API、tool schema 和结果字段。
- 为每个业务操作写出预期 CDP command sequence。
- 增加 exactly-once 写操作约束。
- 保存现有 Google/Baidu、React input、截图验收场景。

验收:

- 单元测试能断言只执行 CDP,失败后没有 Electron/DOM fallback。
- 不改变用户可见行为。

### C1 持久 CDP session

- 在 BrowserHost 中新增 per-slot CDP session。
- 增加串行 command queue、timeout、detach 和 event waiter。
- 增加 navigation generation。
- 移除当前每条命令 attach/detach 的 `withDebugger()` 模式。

验收:

- 同一 tab 连续发送命令不会重复 attach。
- webview 销毁后旧命令全部取消。
- 打开 DevTools 或 debugger 被占用时返回明确错误。

### C2 持久 webview 创建握手

- 复用现有 `CanvasPane`、`BrowserSurface` 和 `retainedBrowserTabs`。
- 移除 BrowserHost 的临时 `WebContentsView`、`headlessView` 和恢复逻辑。
- 新 tab 先建立 pending slot,再通过 IPC 通知 renderer 挂载 `<webview>`。
- `<webview>` 初始只挂载 `about:blank`,`dom-ready` 后立即注册。
- BrowserHost 等待注册完成,超时返回结构化错误。
- Canvas 继续只控制 webview 的可见布局,不重新创建节点。
- renderer reload 后按 canonical browser tab snapshot 重建 browser surfaces,原 tab 在恢复前不可执行工具。

验收:

- browser 工具执行前必定存在已注册 webview。
- Canvas 收起和 session 切换不改变 `webContentsID`。
- BrowserHost 中不存在 `new WebContentsView(...)`。
- renderer 不可用时明确失败,不创建替代 target。

### C3 CDP 导航

- BrowserHost 使用 `Page.navigate` 打开目标 URL。
- back / forward / reload 改为 Page 域。
- 工具成功必须等待主 frame commit,不能只返回导航意图。

验收:

- `newTab:true` 不会被迟到的 `about:blank` 覆盖。
- 快速连续打开 A/B 时最终只能显示 B。
- 工具结果 URL/title 与可见 webview 一致。

### C4 Observe 与 Screenshot

- observe 只使用 `Runtime.evaluate`。
- 删除 `executeJavaScript` fallback。
- screenshot 只使用 `Page.captureScreenshot`。
- full-page screenshot 使用 `Page.getLayoutMetrics.contentSize`。
- 删除 renderer/webContents `capturePage` 路径。

验收:

- 可见和隐藏 tab 返回相同 observe 结构。
- viewport/full-page PNG 尺寸正确且非空。

### C5 Click、Type 与 Scroll

- pointer click 只使用 `Input.dispatchMouseEvent`。
- type / clear / 快捷键统一使用 `Input.dispatchKeyEvent`。
- scroll 使用 `mouseWheel`。
- 删除 `el.click()`、DOM value setter、`scrollBy()` 和 `sendInputEvent()` fallback。
- 命令响应不确定时返回错误,不执行第二种实现。

验收:

- React 受控 input 正常更新。
- 输入后按长度和哈希校验完整预期值,拒绝受控组件回滚造成的假成功。
- CDP 响应失败时不会重复点击或重复输入。
- selector scroll 与页面 scroll 均产生真实 wheel 行为。

### C6 Go 路径对齐

- Go manager 与 Electron 使用相同 operation semantics。
- 对齐 command sequence、timeout、结果字段和错误码。
- scroll 改为 CDP wheel。
- 删除 Go DOM 写操作 fallback。
- 保留 session-scoped tab routing 和原子 `OpenNewTab`。

验收:

- 同一测试向量可验证 Electron 与 Go 两条路径。
- provider/tool 层不需要区分 browser implementation。

### C7 清理与稳定性

- 删除页面操作中的:
  - `WebContentsView` / `headlessView`
  - `executeJavaScript`
  - `webview.capturePage` / `webContents.capturePage`
  - `sendInputEvent`
  - `webContents.loadURL/goBack/goForward/reload`
  - `el.click()` / value setter / `scrollBy()`
- 补 Electron dev smoke test。
- 为常驻 webview 增加单 session 8、全局 16 的硬上限。
- Project scope 变化时撤销旧 `file://` grant,并阻止历史记录回到旧文件。
- 跑全量 Go、Web build、Electron tests。
- 更新 browser 架构文档和故障排查说明。

验收:

- 页面自动化路径只出现 CDP command。
- 每个 live tab 只有一个常驻 webview/webContents。
- session/tab 路由无隐式 focus。
- 关闭和切换 session 不残留 CDP waiter/session。

## 7. 测试矩阵

### 单元测试

- 每个操作断言准确的 CDP command 和参数。
- attach/detach 生命周期。
- command timeout 和 execution context destroyed。
- 写操作失败后无 fallback、无重复命令。
- stale navigation generation 被丢弃。

### 集成测试

- 新 tab 打开 Google/Baidu。
- 同一 tab 快速连续导航。
- 两个 session 同时持有不同 tab。
- React controlled input、textarea、contenteditable。
- pointer click 与 wheel scroll。
- viewport/full-page screenshot。
- Canvas 显示/隐藏和 session 切换前后 `webContentsID` 保持一致。
- renderer reload 后 pool 重建期间工具明确返回 not ready。

### 回归测试

- `go test ./...`
- Web `npm run build`
- Electron tests。
- `npm run smoke:electron-browser`:真实 Electron `<webview>` + CDP 自动 smoke。

## 8. 时间评估

估算假设:

- 一名熟悉当前 Pudding browser 代码的工程师全职实施。
- 不扩展跨域 iframe、复杂 shadow DOM 和文件上传能力。
- 不改变现有 browser tool schema。
- 估算包含实现、测试、review 修正和开发版 smoke,不包含发布观察期。

| 阶段 | 预计人时 | 预计工作日 |
| --- | ---: | ---: |
| C0 基线与契约 | 6-8h | 1d |
| C1 持久 CDP session | 12-16h | 1.5-2d |
| C2 持久 webview 创建握手 | 8-12h | 1-1.5d |
| C3 CDP 导航 | 8-12h | 1-1.5d |
| C4 observe / screenshot | 8-12h | 1-1.5d |
| C5 click / type / scroll | 12-16h | 1.5-2d |
| C6 Go 路径对齐 | 8-12h | 1-1.5d |
| C7 清理与稳定性 | 12-16h | 1.5-2d |
| 合计 | 74-104h | 9-13d |

建议预留 20% 风险缓冲:

- 工程完成:约 9-13 个工作日。
- 含风险缓冲:约 11-16 个工作日。
- 一人日历时间:约 2-3.5 周。

若只改 Electron dev 主路径、暂不对齐 Go manager,可缩短到约 6-9 个工作日,但会留下两套行为语义,不建议作为最终状态。

## 9. 主要风险

| 风险 | 影响 | 缓解方式 |
| --- | --- | --- |
| Electron DevTools 与持久 debugger 冲突 | CDP attach 被断开 | 明确错误、自动重新建 session,但不重放写操作 |
| renderer 未启动或 reload | 新 tab 无法创建或旧 tab 暂不可用 | 注册超时、not-ready 错误、pool 重建状态 |
| 常驻 webview 数量过多 | Chromium 内存增长 | tab 数量上限、明确 close、后台资源指标 |
| Canvas 误卸载 webview | 页面状态和 CDP session 丢失 | 保持现有 retained surface、组件生命周期测试 |
| 页面长期请求导致等待不结束 | open/reload 超时 | 以主 frame commit 为成功条件,不等待 network idle |
| CDP 命令成功但响应丢失 | 重复副作用 | 写操作不自动重试/fallback |
| full-page screenshot 内容过大 | 内存和响应过大 | 尺寸/像素上限与结构化错误 |
| React/富文本输入差异 | 输入状态未提交 | 真实 Input 域测试矩阵,保留明确失败而非 DOM fallback |

## 10. 完成定义

以下条件全部满足才视为完成:

1. Electron 和 Go 页面操作均通过 CDP。
2. BrowserHost 不再创建 `WebContentsView` 或其他替代 target。
3. 每个 live browser tab 只有一个由现有 BrowserSurface 持有的常驻 `<webview>`。
4. Electron 只管理容器生命周期,不直接驱动页面行为。
5. 写操作没有自动 fallback 或隐式重试。
6. 工具成功表示目标操作已达到定义的完成条件。
7. 新 tab、连续导航、React 输入、滚动和截图场景通过。
8. 全量测试、Web build 和 Electron dev smoke 通过。
9. `file://` 只在当前 session 的 Project 根目录内生效,symlink 越界与未授权 renderer 请求均被拒绝;Project scope 变化后旧 grant 同步撤销。
10. 常驻 webview 超过单 session 8 个或全局 16 个时明确失败,不卸载已有 tab。

## 11. 实施验证

- `go test ./...`
- `go test -race ./internal/browser ./internal/api`
- `npm run test:electron`
- `npm run smoke:electron-browser`
- `npm run build` (`web/`)

自动 Electron dev smoke 覆盖 `file://`、多 tab、多 session 保活、back/forward、screenshot 和 Project grant 撤销。Google/Baidu 外网导航仅保留为发布前人工网络验收,不作为可重复测试的依赖。
