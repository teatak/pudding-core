# Workspace 拖拽性能与 WebView 保活层改造计划

> 状态:核心改造已实施；持续 Performance trace 作为发布前验收  
> 日期:2026-08-10  
> 范围:Electron 桌面端 Agent Console 与 Workspace 的横向拖拽、Workspace 显隐、浏览器 WebView 生命周期。  
> 结论:继续使用 Renderer `<webview>`，但把 WebView 运行时从 Workspace 布局树中分离到固定父节点的保活层；Workspace 只负责浏览器 Chrome 和 viewport 占位，不再负责 WebView 生命周期。

## 0. 结论

当前拖拽卡顿的核心不是 `pointermove` 本身，而是一次宽度变化会扩散到整个 Stage，并让 Workspace 内所有保留挂载的 Surface 重新布局。现有隐藏方式使用 `visibility:hidden + absolute inset-0`，不会卸载 WebView，但隐藏节点仍然跟随 Workspace 尺寸变化。

本次改造采用以下单一路径:

1. `<webview>` 继续作为 Electron 浏览器承载方式，不恢复 `WebContentsView`。
2. 新增应用级 `BrowserKeepAliveLayer`，所有 WebView 从创建到关闭始终挂载在该层，DOM 父节点和 React key 不变化。
3. `BrowserWorkspaceSurface` 只渲染标签栏、工具栏、查找栏和 `BrowserViewportPlaceholder`。
4. 活动 WebView 通过 CSS Anchor 直接跟随 Placeholder；非活动 WebView 使用固定保活尺寸，不响应 Workspace resize。
5. Workspace 收起时可以直接 `display:none`；保活层仍挂载，但所有 WebView 退出可见呈现。
6. LLM 在隐藏状态执行输入操作时，通过显式 automation presentation handshake 获得非零 viewport、焦点和 ready 确认。
7. Dock 拖拽直接修改 Agent Console 容器宽度，不再修改 Stage 祖先上的继承 CSS 变量。
8. 普通隐藏 Surface 退出布局；只有当前活动 Surface 响应实时宽度。

目标链路:

```text
pointermove
  -> 更新 Agent Console 元素真实宽度
  -> Workspace flex 获得剩余真实宽度
  -> 更新当前活动 Surface
  -> 如果当前 Surface 是 Browser，CSS Anchor 让唯一活动 WebView 同步获得真实宽度
```

禁止重新引入以下路径:

- `WebContentsView` / `BrowserView` 原生浮层。
- native attach / detach / updateBounds 与 `<webview>` 并行的双轨实现。
- screencast、截图流或 headless view 作为 UI fallback。
- 在活动区与保活层之间移动、Portal 或重新创建同一个 `<webview>`。
- 仅通过 debounce、延迟真实宽度或 transform 假预览掩盖布局成本。

## 1. 当前基线与已确认问题

### 1.1 Dock 拖拽修改 Stage 祖先变量

`web/src/App.tsx` 的 `startDockResize` 在每次 `pointermove` 中执行:

```ts
stageNode.style.setProperty("--agent-console-dock-width", `${width}px`);
```

该变量设置在 Agent Console 和 Workspace 的共同祖先上。CSS 自定义属性可继承，更新祖先变量会扩大样式重算范围；随后 flex 布局改变 Workspace 宽度。

当前实现已经避免在每次 move 中调用 React state，但仍然无法隔离浏览器样式计算、DOM layout 和 ResizeObserver 反馈。

### 1.2 Workspace 同时挂载所有 Surface

`web/src/components/workspace/WorkspacePane.tsx` 同时保留:

- 所有 Canvas item。
- Project Browser。
- 所有文件预览。
- 多 session Browser Surface 和其中的所有 WebView。
- 多 session Terminal Surface。
- 隐藏的 TerminalSizeProbe。

Surface 普遍使用:

```text
absolute inset-0 + visibility:hidden
```

`visibility:hidden` 只停止绘制和命中，不会让节点退出 layout。`inset-0` 又把隐藏节点尺寸绑定到 Workspace，因此拖拽时隐藏 Surface 仍会获得新尺寸，并可能触发其 ResizeObserver。

### 1.3 WebView 生命周期与 Workspace 布局耦合

当前 `BrowserWorkspaceSurface` 在 Workspace 内渲染每个 tab 的 `ElectronWebviewBrowser`。这样可以保证 session、Surface 和 tab 切换时 `<webview>` 不被 React unmount，但代价是整个 Browser UI 必须常驻 Workspace 布局树。

真正需要保活的只有:

- `<webview>` DOM 节点。
- 对应 Electron guest `webContents`。
- 与 BrowserHost 的注册关系。
- 页面 URL、history、表单、滚动和 renderer 状态。

浏览器工具栏、查找栏、空状态、Project、Canvas 和 Terminal 不应因为 WebView 生命周期而被迫使用同一隐藏策略。

### 1.4 `display:none` 不能直接套在当前 WebView 路径上

当前点击工具在 BrowserHost 派发 CDP mouse event 前，会让 Renderer `<webview>` 获取焦点并等待 lifecycle ack。`display:none` 元素不能获得焦点；其 guest viewport 也可能变成零尺寸，进一步影响 screenshot、坐标命中和可见性判断。

因此本方案不把 `<webview>` 简单改成 `display:none`。它为隐藏 WebView 保留稳定、非零的运行时尺寸，并为需要输入的自动化建立显式 presentation lease。

### 1.5 不采用 WebContentsView

`WebContentsView` 不属于 Renderer DOM，无法被 CSS stacking context、`z-index`、Radix Portal、菜单、Dialog、Tooltip 和浮动 Agent Console 可靠覆盖。解决遮挡需要让每个浮层都与主进程协调隐藏原生 View，容易形成遗漏和第二套可见性状态。

仓库已经删除过旧 native attach / bounds / detach 路径。本次不恢复它。

## 2. 目标与非目标

### 2.1 必须做到

- 拖拽期间 Agent Console 和 Workspace 使用真实宽度，视觉最多落后一个 animation frame。
- 非活动 WebView 不因 Workspace 拖拽或窗口 resize 改变尺寸。
- Workspace 收起时可以完全退出布局，不再移动到窗口外。
- session 切换、tab 切换、Workspace 收起/展开不会改变 WebView 的 `webContentsID`。
- WebView DOM 父节点从创建到关闭保持不变。
- LLM 在 Workspace 关闭、其他 Surface 活动或其他 tab 活动时仍可操作目标 tab。
- 自动化点击前必须等待目标 WebView 具有非零 viewport 且成功聚焦。
- 菜单、Dialog、Tooltip、查找栏和浮动控制台继续通过普通 DOM stacking 覆盖浏览器内容。
- BrowserHost 仍以显式 `sessionID + tabID` 管理 tab，不新增后端 focus。
- 关闭 tab 是唯一销毁对应 WebView 和 guest `webContents` 的业务操作。

### 2.2 非目标

- 不在本次重写 BrowserHost 的 CDP command、navigation 或 credential 逻辑。
- 不改变 browser tool schema、Go API 或 session routing。
- 不恢复旧 external browser、screencast 或 native view。
- 不用降低拖拽采样率、延迟到 pointerup 或 transform 预览代替真实布局。
- 不在没有性能证据前直接引入 Transcript 完整虚拟化。
- 不为失败场景增加并行 WebView 保活 fallback。

## 3. 目标架构

```text
App / BrowserRuntimeProvider
├─ SessionRail
├─ AgentConsole
├─ Workspace
│  └─ BrowserWorkspaceSurface
│     ├─ BrowserTabs / Toolbar / FindBar
│     └─ BrowserViewportPlaceholder
│
└─ BrowserKeepAliveLayer              始终 mounted
   ├─ BrowserRuntimeHost(session A, tab 1)
   │  └─ ElectronWebviewBrowser
   ├─ BrowserRuntimeHost(session A, tab 2)
   │  └─ ElectronWebviewBrowser
   └─ BrowserRuntimeHost(session B, tab 1)
      └─ ElectronWebviewBrowser
```

### 3.1 BrowserRuntimeProvider

Provider 是 Renderer 内 WebView mount/presentation 的唯一事实源，负责:

- 监听 Electron `webview-required` 和 `browser-updated` 事件。
- 按 `sessionID + tabID` 维护需要挂载的 runtime tab 集合。
- 保证每个 runtime key 只创建一个 `ElectronWebviewBrowser`。
- 注册当前 Workspace Browser Placeholder。
- 管理每个 runtime 的 presentation mode、anchor 归属和 automation lease。
- 在 token 更换或 tab lost/closed 时显式清理 runtime。

Provider 不拥有 canonical browser metadata。URL、title、favicon、history 能力和状态继续来自现有 BrowserHost snapshot / REST query。Provider 只拥有 Renderer presentation 事实。

现有 `useElectronRequiredBrowserTabs` 应从 `WorkspacePane` 提升到 Provider，避免 Workspace 是否挂载决定 WebView 是否存在。迁移后删除 `WorkspacePane` 中用于维持 WebView mount 的 `retainedBrowserTabs` 和 `mountedBrowserTabs` 双重合并路径。

### 3.2 BrowserKeepAliveLayer

Layer 必须满足:

- 在 App 的稳定位置渲染，不能位于可被收起或条件卸载的 Workspace 子树内。
- 从 App 启动到 token 变化保持同一个 React 父节点。
- Layer 本身 `position:fixed; inset:0; pointer-events:none`，不参与 Stage flex layout。
- 每个 runtime host 使用唯一的 CSS anchor name。
- 只有用户可见的 runtime host 允许 `pointer-events:auto`。
- 通过普通 Renderer `z-index` 放在 Browser Chrome 下方、全局 Overlay 下方。
- 不在 active/inactive 切换时改变 runtime host 的 React key 或 DOM parent。

Layer 不读取 Workspace 的 CSS 宽度，也不测量 DOM rect。活动 runtime 由 CSS Anchor 直接引用 Placeholder 的位置和尺寸。

### 3.3 BrowserViewportPlaceholder

Placeholder 位于 `BrowserWorkspaceSurface` 的网页内容区域，负责:

- 标识当前用户可见的 `sessionID + tabID`。
- 提供真实网页 viewport 位置和尺寸。
- 声明与 `sessionID + tabID` 一一对应的唯一 `anchor-name`。
- 只注册当前可见 runtime key，不执行测量或逐帧同步。

Placeholder 不保存 WebView，不渲染 browser page，也不持有 BrowserHost 生命周期。

`BrowserRuntimeHost` 使用 `position-anchor`、`anchor()` 和 `anchor-size()` 直接跟随 Placeholder。React 只处理 runtime 创建/销毁和 presentation mode 切换，不承载逐帧坐标，也不保留 ResizeObserver / RAF 布局控制器。

### 3.4 WebView presentation 状态

每个 runtime 只有以下状态:

```ts
type BrowserRuntimePresentation = "standby" | "visible" | "automation";
```

关闭不是 presentation state。关闭时 runtime 从集合删除并 unmount。

#### standby

- 用于非活动 tab、其他 session、非 Browser Surface和 Workspace 收起状态。
- 使用统一的 1024×720 非零保活尺寸。
- `visibility:hidden`、`pointer-events:none`。
- 不绑定 anchor，不跟随 Workspace 或窗口变化。
- 不允许 Renderer 用户输入命中。

#### visible

- 仅允许一个用户可见 runtime。
- 绑定当前 Placeholder anchor。
- `visibility:visible`、`opacity:1`、`pointer-events:auto`。
- 拖拽时由浏览器布局引擎同步获得真实位置和尺寸。

#### automation

- 用于 Workspace 关闭或目标 tab 非活动时的 LLM 输入准备。
- 当前可见 tab 继续绑定 anchor；隐藏 tab 使用统一保活尺寸。
- 保持非零 layout box，允许 `<webview>` 获取焦点。
- 对用户不可见且不可命中，例如 `visibility:visible; opacity:0; pointer-events:none`。
- automation lease 结束后回到之前的 `visible` 或 `standby`。

`automation` 不是第二个浏览器实例，也不创建新的 WebView；它只是同一 runtime 的短暂 presentation。

统一 runtime viewport 是明确产品常量，不是失败 fallback；当前固定为 1024×720。

## 4. 生命周期

### 4.1 LLM 创建隐藏 tab

```text
BrowserHost 建立 pending slot
  -> webview-required(sessionID, tabID, requestID)
  -> BrowserRuntimeProvider 新增 runtime
  -> KeepAliveLayer mount <webview>，standby + 非零 viewport
  -> dom-ready / getWebContentsId
  -> registerWebview
  -> BrowserHost pending slot 绑定同一 webContents
```

该流程不要求 Workspace 打开，也不要求目标 session 当前显示。

### 4.2 用户显示浏览器

```text
Browser Surface / tab selection 改变
  -> Placeholder 注册 sessionID + tabID
  -> runtime 绑定同名 CSS anchor
  -> 目标 runtime -> visible
  -> 原 visible runtime -> standby
```

切换不移动 DOM 节点，不重新 attach guest。

### 4.3 Workspace 收起

```text
workspaceOpen = false
  -> Placeholder unregister
  -> 当前 visible runtime -> standby
  -> Workspace UI display:none
```

不再通过 absolute positioning 把 Workspace 移出窗口，也不改变 KeepAliveLayer 的 mount 状态。

### 4.4 隐藏状态下执行 LLM 操作

#### 不需要宿主焦点的命令

纯 metadata、navigation 和确定不依赖 viewport/focus 的 CDP 命令可以直接使用已注册 `webContents`。是否跳过 presentation lease 必须由 BrowserHost 的明确 action 分类决定，不能由调用方猜测。

#### 需要 viewport 或焦点的命令

click、坐标相关 screenshot、可见性判断以及当前实现中要求宿主焦点的输入操作走统一握手:

```text
BrowserHost automation-start(requestID, sessionID, tabID, action)
  -> Provider 为目标 runtime 获取 automation lease
  -> runtime 进入 automation，绑定可用 anchor 或应用固定非零保活尺寸
  -> 下一次 layout 后 focus({ preventScroll: true })
  -> 校验 isConnected、webviewReady、非零 rect、document.activeElement
  -> automation-lifecycle-complete(requestID, ok)
  -> BrowserHost 执行 CDP/input command
  -> automation-end(requestID)
  -> 释放 lease并恢复之前 presentation
```

要求:

- 一个 requestID 只能完成一次。
- 新 lease 必须串行，不允许两个隐藏 WebView 同时争抢宿主焦点。
- cancel、timeout、tab close、renderer reload 和 token change 必须释放 lease。
- 焦点恢复继续复用现有 host focus snapshot/lease 逻辑。
- Provider ready 之前 BrowserHost 不得提前 dispatch click。

### 4.5 关闭 tab

```text
显式 close(sessionID, tabID)
  -> BrowserHost slot lost/closed
  -> Provider 删除 runtime
  -> React unmount <webview>
  -> guest webContents 销毁
```

重复 close 保持幂等。切 Surface、收起 Workspace、切 session 和隐藏窗口都不能进入该路径。

## 5. Workspace 其他 Surface 的处理

WebView 保活层落地后，其他 Surface 不再承担浏览器生命周期。

### 5.1 普通 DOM Surface

以下非活动 Surface 的第一版统一使用 `display:none`，保持 React state 但退出 layout:

- CanvasItemSurface。
- ProjectBrowserSurface。
- FilePreviewSurface。
- BrowserWorkspaceSurface 的非活动 Chrome。
- WorkspaceEmpty。

如果后续证明某类 Surface 可以无损 remount，再单独改为条件渲染；本次不同时扩大生命周期变更范围。

### 5.2 Terminal

PTY 和 terminal UI 生命周期分离:

- daemon terminal 继续运行。
- 非活动 XTerm 保持 mounted 或按明确恢复能力处理，但必须暂停 ResizeObserver 和 `fit()`。
- 只有活动 terminal 接收实时尺寸。
- `TerminalSizeProbe` 不应在每次 Workspace resize 中无条件执行 `fit()`；应改为稳定测量源或仅在需要创建/显示 terminal 时测量。

Terminal 优化在 WebView 隔离后根据 Performance profile 决定是否同批完成，但不得继续让非活动 terminal 响应拖拽。

## 6. Dock resize controller

### 6.1 单一宽度写入点

新增稳定的 Agent Console element ref。拖拽时直接写:

```ts
consoleNode.style.width = `${width}px`;
```

Workspace 通过 `flex:1` 获得剩余真实宽度。删除拖拽期间写入 Stage 祖先 `--agent-console-dock-width` 的路径；初始/键盘调整也统一通过同一 layout 函数设置宽度。

ratio 只在以下时机进入 React/localStorage:

- pointerup。
- pointercancel / blur 的统一 finish。
- 键盘调整。

逐帧宽度只属于 layout controller 的命令式状态，不形成第二份持久状态。

### 6.2 每帧合并

pointermove 只保存最新 `clientX` 并请求一个 animation frame。frame 内:

1. 计算整数 console width。
2. 未变化则退出。
3. 更新 console element width。
4. CSS Anchor 在同一次布局中更新唯一活动 WebView 的位置和尺寸。

每帧合并不是拖拽性能方案本身；只有在隐藏 Surface 已隔离后使用，避免重复处理同一帧内的多个 pointer event。

### 6.3 resize phase

使用一个 `idle | resizing` phase，复用现有拖拽开始/结束边界。该 phase 只用于:

- cursor/user-select shield。
- 暂停非必要的 resize 副作用。
- 在结束时执行一次最终一致性校正。

不要在逐帧更新中切 React phase 或发布 Zustand state。

## 7. Transcript 与观察器边界

WebView 和隐藏 Surface 隔离完成后重新采集 Performance profile。只有证据仍显示 Transcript resize 是主要长任务时，再实施以下收口:

- viewport/content ResizeObserver 合并为同一帧一次处理。
- resize start 捕获一次 bottom/history 状态和 anchor。
- 拖拽期间取消旧 anchor restore frame chain，不能为每次 observer 回调启动新的多帧恢复。
- bottom mode 每帧最多一次贴底校正。
- history mode 使用同一个 resize anchor，结束后做最终恢复。
- 后续仍超预算时，再评估历史 turn 的 `content-visibility:auto`；不直接开始完整虚拟化。

该阶段不能通过停止真实宽度变化来规避文本换行。

## 8. 实施阶段

### P0 基线与生命周期验证

- 使用 release/Vite production build 录制当前拖拽 Performance trace。
- 分别覆盖:长 Transcript、Canvas、Project、活动 Browser、隐藏多 Browser tab、Terminal。
- 记录 Recalculate Style、Layout、ResizeObserver、XTerm fit、长任务和帧时间。
- 实机验证固定保活尺寸 + `visibility:hidden` 不改变 `webContentsID`、URL、history、表单和滚动状态。
- 用当前 Electron 运行时探针验证 `anchor-name`、`position-anchor`、`anchor()`、`anchor-size()` 的实际布局结果。
- 实机验证 `opacity:0 + 非零保活尺寸` 可以完成当前 click focus handshake 和 CDP screenshot。

P0 验证失败时先证明具体失败原因，再修改 presentation 细节；不新增第二套承载方式。

### P1 Browser runtime ownership 上移

- 新增 BrowserRuntimeProvider / BrowserKeepAliveLayer。
- 将 `useElectronRequiredBrowserTabs` 提升出 WorkspacePane。
- 将 `ElectronWebviewBrowser` 从 BrowserWorkspaceSurface 移入 KeepAliveLayer。
- 保证 runtime key 和父节点稳定。
- BrowserWorkspaceSurface 改为 Chrome + Placeholder。
- 删除 WorkspacePane 的 retained/mounted Browser DOM 路径。

验收:

- Workspace 收起/展开和 session/tab 切换期间 `webContentsID` 不变。
- Workspace `display:none` 后 WebView 仍可注册、导航和执行工具。

### P2 Presentation 与 automation handshake

- 实现 standby / visible / automation presentation。
- Placeholder 与唯一活动 runtime 使用同名 CSS anchor。
- 删除 ResizeObserver、RAF、手动 left/top/width/height 和布局 invalidation key。
- automation-start 等待 layout/focus ready 后 ack。
- cancel/timeout/close/reload 清理 lease。
- 删除当前立即 focus、可能早于 React commit 的竞态路径。

验收:

- Workspace 关闭时 click/type/scroll/screenshot/observe 全部有明确结果。
- 自动化结束后宿主输入焦点正确恢复。
- 不出现工具执行导致错误 tab 抢占用户可见 Surface。

### P3 隐藏 Surface 退出布局

- Workspace 关闭时直接 `display:none`。
- 普通非活动 Surface 使用 `display:none`。
- 非活动 terminal 暂停 size observer/fit。
- 保留需要的组件状态，不保留响应式隐藏 layout。

验收:

- 拖拽时非活动 Surface 的 ResizeObserver 回调为零。
- Workspace 重新打开后 UI state 和资源标签正确恢复。

### P4 Dock resize 写入范围收口

- 直接写 Agent Console element width。
- 删除 Stage 祖先变量的 live resize 写入。
- 每帧合并最新 pointer position。
- pointerup 后保存 ratio，最终 width 与持久值一致。

验收:

- 活动 Browser/Canvas/Project/Transcript 都实时获得真实宽度。
- handle 与内容视觉最多相差一帧。
- 左右 Dock、键盘调整、窗口 resize 和最小宽度约束一致。

### P5 Profile 驱动的剩余优化

- 重新采集与 P0 相同 trace。
- 如果 Transcript 仍为主要成本，合并其 resize/anchor observer。
- 如果 Terminal 仍为主要成本，收口 XTerm fit 和 size probe。
- 没有 trace 证据的优化不进入本轮。

## 9. 测试矩阵

### 9.1 WebView 生命周期

- 新 tab 在 Workspace 关闭时创建并成功 register。
- Workspace 关闭/打开前后 `getWebContentsId()` 相同。
- Canvas、Project、Terminal、Browser 间切换后 ID 相同。
- session 主/副 pane 切换后 ID 相同。
- Browser tab 切换后各自 ID 不变。
- URL、back/forward history、scroll、form value 和登录态保留。
- close tab 只销毁目标 runtime，重复 close 不复活。
- renderer reload 按现有 canonical snapshot 重新建立 runtime，不绑定旧 lost slot。

### 9.2 LLM browser tools

以下场景分别执行 open/back/forward/reload、observe、screenshot、click、type、scroll:

- Workspace 打开且目标 tab 活动。
- Workspace 打开但 Canvas/Project 活动。
- Workspace 收起。
- 目标 tab 属于当前 session 的后台 tab。
- 目标 tab 属于可见 secondary session。
- 页面正在 navigation/loading。
- 自动化中用户切换 tab、关闭 Workspace、取消 turn 或关闭 tab。

必须断言:

- input action 在 ready ack 前不 dispatch。
- requestID 不串 tab。
- timeout 后 lease 被清理。
- 用户原焦点在 automation end 后恢复。

### 9.3 UI 与 stacking

- Browser toolbar/menu/find/credential UI 正常显示。
- Dialog、Tooltip、Toast、App 菜单和浮动 Agent Console 能覆盖 Browser 内容。
- macOS traffic lights、全屏、窗口最大化、左右 Dock 和工作区抽屉正确。
- 浏览器页面 focus、输入法、复制粘贴、文本选择和快捷键不回归。
- Browser 页面 HTML fullscreen 和 popup 保持现有行为。

### 9.4 拖拽性能

使用 production build 和一致数据集，录制至少 5 秒连续往返拖拽:

- 长 Transcript + 4 个隐藏 WebView。
- Canvas + 4 个隐藏 WebView。
- Project tree/editor + 4 个隐藏 WebView。
- 活动 Browser + 3 个隐藏 WebView。
- Terminal + 4 个隐藏 WebView。

必须满足:

- 非活动 WebView 的固定保活尺寸在拖拽期间不变化。
- 活动 WebView 只通过 CSS Anchor 参与一次正常布局，不产生额外测量/写入链。
- 非活动普通 Surface 不产生 ResizeObserver 回调。
- pointermove 不触发 React render。
- resize handle 与真实内容宽度最多相差一个 frame。
- 不出现由重复 layout/observer 链导致的连续 50ms 以上主线程长任务。
- 相比 P0，拖拽期间 Recalculate Style + Layout 总耗时显著下降；第一阶段目标至少降低 50%。

帧率以硬件和活动网页复杂度为背景指标，不用单一 FPS 掩盖长任务。活动 Browser 自身响应式重排属于必要成本，验收重点是宿主不能再让隐藏资源放大成本。

## 10. 预计修改范围

前端核心:

- `web/src/App.tsx`
- `web/src/browser/useElectronRequiredBrowserTabs.ts`
- `web/src/browser/ElectronWebviewBrowser.tsx`
- `web/src/components/workspace/BrowserWorkspaceSurface.tsx`
- `web/src/components/workspace/WorkspacePane.tsx`
- `web/src/components/workspace/useWorkspaceBrowserSurface.ts`
- 新增 BrowserRuntimeProvider / BrowserKeepAliveLayer / BrowserViewportPlaceholder。

Electron handshake:

- `web/src/browser/electronBridge.ts`
- `electron/preload.cjs`
- `electron/main.cjs`
- `electron/browser-host.cjs`

后续按 profile 决定:

- `web/src/components/transcript/TranscriptList.tsx`
- `web/src/terminal/TerminalSurface.tsx`
- Canvas/Project/FilePreview Surface wrappers。

不应修改 Go browser API、SQLite schema、session routing 或工具 schema。

## 11. 删除与收尾

新路径完成后必须删除:

- WorkspacePane 中仅用于维持 WebView mount 的 retained browser state。
- BrowserWorkspaceSurface 内部渲染所有 `<webview>` 的旧路径。
- inactive WebView 的 `absolute inset-0 + invisible` 响应式隐藏逻辑。
- Stage 祖先 CSS 变量的 live resize 写入。
- automation start 立即 focus、没有等待实际显示/layout 的竞态处理。
- 为临时调试添加的 performance mark、日志和 feature flag。

最终只保留一个 Browser runtime ownership、一个 presentation controller 和一条 automation ready handshake。

## 12. 实施完成定义

只有同时满足以下条件才算完成:

1. WebView 生命周期与 Workspace mount/layout 解耦。
2. Workspace 收起后完全退出布局。
3. 非活动 WebView 尺寸稳定，不响应拖拽。
4. 活动 WebView 仍使用实时真实宽度。
5. 隐藏状态下全部 Browser tool 场景通过实机测试。
6. DOM Overlay 不被浏览器内容遮挡。
7. production Performance trace 达到第 9.4 节指标。
8. Web、Electron 单元测试、构建和 `git diff --check` 全部通过。
9. 旧路径、临时 fallback 和冗余状态已删除。
