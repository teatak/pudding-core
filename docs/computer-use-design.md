# macOS Computer Use 设计与实施计划

> 状态:待实施
>
> 日期:2026-08-13
>
> 范围:macOS 桌面应用观察与操作。
>
> 不包含:操作录制、示范学习、工作流录制回放、Windows。
>
> 目标工期:1 名熟悉项目的工程师约 6 周交付可用 MVP,10–14 周达到产品级稳定性。

## 0. 决策摘要

1. Computer Use 作为内置 App 按需加载,不把桌面控制工具常驻全部会话。
2. Go daemon 负责 session 路由、工具生命周期、审批和全局动作串行化。
3. Electron main 负责 macOS 原生能力和权限界面,通过签名 Swift Helper 调用 Accessibility 与 ScreenCaptureKit。
4. 所有操作显式携带 `sessionID`、`appID`、`windowID` 和 `observationID`;后端不保存“当前应用”或“当前焦点”。
5. 原生界面以 AXUIElement 语义元素为主;截图坐标是显式的另一种操作,不能成为失败后的自动 fallback。
6. 写操作不自动重试。结果不确定时返回明确错误,下一步必须先重新观察。
7. macOS Helper 落地后成为 macOS 桌面截图和窗口截图的唯一原生实现,现有 macOS 截图调用迁移后删除旧路径。

## 1. 背景与现有基础

Pudding 已具备 Computer Use 上层闭环的大部分基础:

- [`internal/tool/desktop_screenshot.go`](../internal/tool/desktop_screenshot.go):桌面截图进入 session attachment 和模型上下文。
- [`internal/tool/browser.go`](../internal/tool/browser.go):已有 observe、screenshot、click、type、scroll 工具语义。
- [`electron/browser-bridge-server.cjs`](../electron/browser-bridge-server.cjs):已有 Electron 与 daemon 之间的 loopback token bridge 模式。
- [`internal/engine/approval.go`](../internal/engine/approval.go):已有 session-scoped 审批事件和工具调用暂停/恢复。
- [`internal/app/builtin.go`](../internal/app/builtin.go):已有按需加载的内置 App 机制。

当前缺口集中在 macOS 原生控制层:

- 枚举应用与窗口。
- 读取并归一化 Accessibility 树。
- 对 AX 元素执行点击和赋值。
- 发送键盘、鼠标、滚轮事件。
- 按应用或窗口截图。
- 引导并检查屏幕录制、辅助功能权限。
- 应用授权、安全拦截、错误恢复和真实应用验收。

## 2. 目标

MVP 支持:

- 枚举当前可操作的 macOS 应用。
- 观察指定应用和窗口,返回有限的 AX 文本树与交互元素。
- 截取指定窗口或应用画面并作为 session attachment 送入模型上下文。
- 点击 AX 元素或显式坐标。
- 设置可编辑 AX 元素值。
- 向指定应用输入文本或按键。
- 在指定应用内滚动。
- 每次动作后返回最新窗口摘要和新的 observation。
- 用户可随时取消当前 turn,取消尚未开始的动作和正在等待的原生调用。

产品级目标:

- 多窗口、多显示器和常见缩放比例稳定工作。
- 应用退出、窗口关闭、布局变化、权限撤销时返回可诊断错误。
- 敏感和破坏性操作进入审批。
- Helper 签名、notarization、升级和崩溃恢复稳定。

## 3. 非目标

- 不录制用户操作,不从视频学习流程。
- 不生成或持久化可回放 workflow。
- 不在第一阶段支持 Windows。
- 不自动控制 Pudding 自身、终端、系统安全授权界面或管理员认证。
- 不绕过 CAPTCHA、浏览器安全警告或操作系统权限提示。
- 不把 Computer Use 当作 REST、MCP、内置 Browser 或 CLI 的替代品。存在结构化接口时优先使用结构化接口。
- 不新增后端 focus/current app/current window 状态。
- 不为失败动作增加第二套 AX/坐标/脚本 fallback。

## 4. 架构与所有权

```text
session turn
  -> Computer Use built-in App tools
      -> Go computer.Manager
          -> explicit session/app/window routing
          -> approval policy
          -> one global write queue
          -> token-authenticated loopback bridge
              -> Electron ComputerUseHost
                  -> signed Swift Helper over stdio
                      -> AXUIElement / AXObserver
                      -> ScreenCaptureKit
                      -> CGEvent
```

### 4.1 Go daemon

负责:

- 工具定义、参数校验和结构化结果。
- 显式 session 路由。
- 动作审批、取消和超时。
- 全局写操作队列。
- 截图 attachment 存储和 canonical turn part。
- session 关闭时释放该 session 的 observation。

不负责:

- 保存当前前台应用或窗口。
- 直接调用 macOS Accessibility API。
- 直接持有跨 turn AX 元素引用。
- 在不确定动作结果后自动重试。

### 4.2 Electron main

负责:

- 启动、监督和停止签名 Helper。
- macOS TCC 权限检查与权限设置入口。
- 应用 allowlist/denylist 的唯一持久事实源。
- 对 daemon 暴露仅 loopback、带启动 token 的 Computer bridge。
- 对 renderer 暴露最小权限状态和设置 IPC,不向 renderer 暴露任意原生操作接口。

### 4.3 Swift Helper

负责:

- 调用 Accessibility、ScreenCaptureKit 和 CGEvent。
- 生成归一化 observation。
- 解析 `elementID` 并执行唯一指定动作。
- 在动作前再次验证应用、窗口和 observation。
- 通过 NDJSON stdin/stdout 与 Electron main 通讯,不监听网络端口。

Helper 不保存业务 session、模型上下文或用户消息。

### 4.4 全局桌面资源

真实鼠标键盘和前台应用是全局资源,不能按 session 并行写入:

- 读操作可按实现能力并行,首版统一串行以减少状态。
- 所有写操作经过 daemon 全局队列。
- 队列项必须携带 `sessionID` 和 `callID`。
- 取消 session 只移除该 session 的待执行项,不能影响其它 session。
- 不存在 daemon 级 active session 或 focus session。

## 5. 原生实现

### 5.1 Helper 形态

新增签名 Swift 可执行 Helper,作为 Pudding.app 的嵌套组件打包:

```text
native/macos/computer-use-helper/
  Package.swift
  Sources/PuddingComputerUseHelper/
```

选择独立 Helper 而不是 Node native addon:

- Swift 可直接使用现代 macOS API。
- 原生崩溃不拖垮 Electron main。
- 不绑定 Electron Node ABI。
- 可独立测试、签名和限制通讯面。

Helper 的 bundle identifier、签名 identity 和 designated requirement 必须稳定,否则版本升级可能导致 TCC 权限重新授权。

### 5.2 观察

使用 AXUIElement 获取:

- bundle ID、进程 ID、应用名称。
- window identifier、标题、frame、focused 状态。
- element role、subrole、title、description、identifier、value 摘要、enabled、focused、frame。
- 支持的 AX actions,例如 `AXPress`。

归一化时:

- 默认只返回可见窗口和有限深度的交互元素。
- secure text field 只返回角色和位置,绝不返回值。
- 普通文本设置字符上限,超限标记 truncated。
- `elementID` 是当前 observation 内的短 ID,不是长期实体 ID。
- 不把原始 AX 指针暴露给 daemon 或模型。

### 5.3 截图

使用 ScreenCaptureKit 截取明确的 window 或 app:

- 默认只截目标窗口,不截取其它应用。
- 返回逻辑坐标、像素尺寸和 scale factor。
- 截图存储继续复用 session attachment 主链路。
- 模型不需要视觉信息时只使用 AX observation,减少隐私暴露和 token 成本。

Helper 落地后,macOS 的现有桌面截图 API 和 `builtin_desktop_screenshot` 迁移到同一 ScreenCaptureKit 实现,完成后删除 macOS `kbinani/screenshot` 路径。Windows 路径不在本计划范围。

### 5.4 动作唯一实现

| action | 唯一实现 | 完成条件 |
| --- | --- | --- |
| `click_element` | AX `AXPress` | action 返回成功且目标仍属于指定 app/window |
| `click_point` | CGEvent mouse down/up | 事件已发送到显式坐标 |
| `set_value` | AX set value | 重新读取值与期望一致 |
| `type_text` | CGEvent Unicode keyboard input | 全部事件发送完成 |
| `press_key` | CGEvent key down/up | 完整按键序列发送完成 |
| `scroll` | CGEvent scroll | 事件发送完成并重新观察 |

规则:

- `click_element` 失败不能自动改为 `click_point`。
- `set_value` 失败不能自动改为键盘输入。
- `type_text` 不通过剪贴板粘贴,避免覆盖用户剪贴板和泄漏内容。
- 所有写操作执行前验证目标应用仍存在、窗口仍匹配、observation 未过期。
- 动作执行后重新观察,但不以第二个动作修复第一个动作。

## 6. Observation 契约

示例:

```json
{
  "sessionID": "sess_x",
  "appID": "com.apple.TextEdit",
  "windowID": "window_42",
  "observationID": "obs_x",
  "observedAt": "2026-08-13T08:00:00Z",
  "screenshot": null,
  "text": "window 'Untitled'\n[1] AXTextArea ...",
  "elements": [
    {
      "elementID": "1",
      "role": "AXTextArea",
      "label": "Text",
      "enabled": true,
      "actions": ["set_value"],
      "frame": {"x": 120, "y": 90, "width": 720, "height": 520}
    }
  ]
}
```

约束:

- observation 只在产生它的 `sessionID + appID + windowID` 内有效。
- 默认 TTL 30 秒,容量上限按 session 设置;超限只淘汰旧 observation,不淘汰动作结果。
- action 必须传 `observationID`。
- Helper 在动作前重新解析元素定位信息;结构不匹配返回 `computer_observation_stale`。
- 模型收到 stale 后必须重新 observe,不能重复原动作。
- observation registry 是短期路由缓存,真实界面始终以当前 AX 树为事实源。

## 7. 模型工具

Computer Use 作为 `computer` 内置 App,在 Work 模式按需加载。MVP 只新增三个模型工具:

### `builtin_computer_list_apps`

列出可见应用、窗口、授权状态和稳定 `appID`。不隐式选择应用。

### `builtin_computer_observe`

参数:

```json
{
  "appID": "com.apple.TextEdit",
  "windowID": "window_42",
  "includeScreenshot": false
}
```

返回 observation。`windowID` 在应用只有一个窗口时可省略;多个窗口时省略必须报错,不能使用当前焦点猜测。

### `builtin_computer_act`

参数:

```json
{
  "appID": "com.apple.TextEdit",
  "windowID": "window_42",
  "observationID": "obs_x",
  "action": "set_value",
  "elementID": "1",
  "text": "hello"
}
```

`action` 枚举首版包括:

- `click_element`
- `click_point`
- `set_value`
- `type_text`
- `press_key`
- `scroll`

单一 action 工具让审批、串行化和 transcript 展示共用一个入口。不得同时传元素目标和坐标目标。

新增工具时必须同步:

- `internal/app/builtin.go` 的 Computer App 定义。
- transcript 工具显示名和图标。
- 简体中文、繁体中文、英文 i18n。
- turn activity summary。

## 8. Bridge 契约

daemon 与 Electron 使用独立 `ComputerBridgeServer`,不复用 Browser CDP 操作实现,也不经 renderer 转发。

建议路由:

| 路由 | 用途 |
| --- | --- |
| `POST /computer/apps/list` | 列应用和窗口 |
| `POST /computer/observe` | AX observation,可选截图 |
| `POST /computer/act` | 执行一个显式动作 |
| `POST /computer/session/release` | 释放 session observations 和待执行状态 |
| `GET /computer/permissions` | 读取权限状态 |

所有请求:

- 仅接受启动时生成的 bearer token。
- 除 permissions 外必须携带 `sessionID`。
- body 设置严格大小上限。
- 不接受任意脚本、shell command 或 AppleScript。
- 错误返回稳定 `code`、`message`、`retryable` 和 `outcome`。

`outcome`:

- `not_started`:确认动作没有执行,调用方可在重新观察后决定是否重试。
- `completed`:动作已完成。
- `unknown`:动作可能已执行但响应丢失,必须重新观察,禁止直接重试。

## 9. 错误语义

首版稳定错误码:

- `computer_unavailable`
- `computer_permission_required`
- `computer_app_not_allowed`
- `computer_app_not_found`
- `computer_window_required`
- `computer_window_not_found`
- `computer_observation_not_found`
- `computer_observation_stale`
- `computer_element_not_found`
- `computer_element_not_actionable`
- `computer_secure_input_blocked`
- `computer_action_blocked`
- `computer_action_timeout`
- `computer_action_cancelled`
- `computer_helper_crashed`
- `computer_capture_failed`

超时或连接断开不能统一标记 retryable。只有明确 `outcome=not_started` 才可提示重新观察后重试。

## 10. 权限与安全

### 10.1 macOS 权限

需要:

- Screen Recording:读取目标窗口画面。
- Accessibility:读取 AX 树并执行 AX/输入动作。

设置页显示每项权限状态和“打开系统设置”入口。Pudding 不尝试自动点击系统权限提示或管理员认证。

### 10.2 应用策略

应用控制策略由 Electron main 的 desktop preferences 持久化,不是 SQLite 运行数据。

首版规则:

- 默认不允许任何第三方应用。
- 用户在设置中按 bundle ID 显式允许。
- 默认拒绝 Pudding 自身、终端、密码管理器、Keychain Access、系统安全授权进程。
- 浏览器任务优先使用 Pudding Browser;只有用户明确要求现有外部浏览器登录状态时才使用 Computer Use。
- 目标应用在动作前和动作后都必须与请求 bundle ID 一致。

### 10.3 数据保护

- secure text field 的值永不进入 observation、日志、tool result 或 attachment。
- Helper 日志不记录输入文本、截图 bytes 或完整 AX value。
- 截图只保存到当前 session attachment 目录。
- app/window 标题按现有本地日志策略处理,错误日志默认不输出完整 AX tree。
- 取消 turn 后停止新截图和新动作。

### 10.4 审批

MVP:

- 未 allowlist 的应用直接拒绝,用户必须先在设置中允许。
- observe 和 screenshot 是 read risk。
- `computer_act` 进入独立 Computer Use 风险分类,不能沿用文件路径审批推断。
- secure input、系统权限、管理员认证和明确禁止应用始终拒绝。

产品级补充:

- 对发送消息、上传文件、删除、购买、提交协议、账号与安全设置建立 action-time confirmation。
- 审批内容展示目标 app/window、动作、元素标签和待输入文本摘要。
- 不把网页、文档或应用界面中的指令当作用户授权。

## 11. 状态与持久化

不新增:

- backend focus/current app/current window。
- provider-local Computer Use history。
- SQLite 中的跨 turn AX 元素或截图缓存。

事实源:

| 数据 | 事实源 |
| --- | --- |
| 当前 UI | macOS AX tree / ScreenCaptureKit |
| session 和 turn | SQLite canonical session/turn/message |
| tool call/result | canonical message parts |
| observation | Helper/Electron 内存短期 registry |
| app allowlist | desktop preferences |
| macOS 权限 | TCC/System Settings |

截图 tool result 继续作为 canonical attachment 进入上下文;普通 AX observation 作为 tool result 进入当前 turn,不另建长期数据库表。

## 12. UI

新增设置区:

- Computer Use 总开关。
- Screen Recording 权限状态。
- Accessibility 权限状态。
- 已允许应用列表与撤销按钮。
- Helper 版本和健康状态。

执行时:

- transcript 显示“查看应用”“操作应用”等可读工具名。
- 显示当前正在操作的应用和窗口。
- 提供停止按钮,调用现有 session cancel。
- sensitive approval 使用现有 approval overlay,不增加第二套确认系统。
- 不实现录制红点、时间轴或 workflow 编辑器。

## 13. 实施阶段与工期

### C0 契约与原生可行性,3–4 天

- 冻结 observation、action、error schema。
- Swift Helper 完成 TextEdit/Calculator 的 AX 读取 PoC。
- 验证签名开发构建的 TCC 权限在重启后保持。
- 验证 ScreenCaptureKit window capture 与坐标映射。

验收:

- 能列出目标窗口和交互元素。
- 能对 TextEdit 执行一次 `AXPress`/set value。
- 没有 AppleScript 或坐标 fallback。

### C1 Helper 与 Electron Host,7–9 天

- 实现 NDJSON Helper 协议。
- 实现 app/window list、observe、window screenshot。
- 实现 click/set value/type/key/scroll。
- 实现 Helper 生命周期、超时、崩溃和取消。
- 完成 Helper 嵌套签名和 notarization 验证。

### C2 Daemon、工具与 session 路由,5–7 天

- 新增 `internal/computer` manager 和 Electron bridge client。
- 新增 Computer 内置 App 和三个工具。
- 新增全局写队列、session release 和结构化错误。
- 截图接入 attachment/canonical context。
- 增加 stale observation 和 unknown outcome 契约测试。

### C3 权限、安全与 UI,5–7 天

- 设置页权限状态、打开系统设置和 allowlist。
- 接入工具审批与禁止应用策略。
- 添加 transcript 显示名、图标、activity summary 和三语 i18n。
- macOS 现有桌面截图迁移到 Helper,删除同平台旧截图路径。

### C4 稳定性与验收,7–10 天

- 多窗口、多显示器、Retina 和窗口移动测试。
- Helper crash、应用退出、权限撤销、session cancel 测试。
- 针对 TextEdit、Calculator、Notes 和一个确定性 fixture app 做 smoke。
- 完成打包、签名、notarization 和升级测试。

总计约 27–37 个工程日。并行投入原生与 Go/Web 两名工程师时,日历时间可压到约 4 周,但产品级兼容性验证不能等比例压缩。

## 14. 测试策略

### 单元测试

- observation 归一化、截断和 secure field 脱敏。
- action 参数互斥、stale observation、错误映射。
- 全局队列的 session 隔离和 cancel。
- bridge token、body limit 和 Helper crash。
- tool result attachment 与 canonical context。
- app allowlist 和 denylist。

### 原生集成测试

新增确定性 macOS fixture app,包含:

- button、checkbox、text field、secure field。
- scroll view。
- 两个窗口。
- 点击后改变明确状态的按钮。
- 可移动和缩放的窗口。

测试不依赖第三方应用的易变布局。TextEdit、Calculator、Notes 只作为发布 smoke。

### 关键回归

- 错误 appID 不产生任何输入。
- stale observation 不产生任何输入。
- action timeout 不执行第二种动作。
- 多 session 同时写入时严格串行且不串目标。
- session cancel 后没有迟到点击或输入。
- secure field 内容不出现在日志、结果和截图元数据中。
- Pudding 自身和终端始终无法成为动作目标。

## 15. MVP 完成标准

满足以下条件才算完成:

1. release 签名构建可完成权限授权,重启和升级后状态符合预期。
2. 模型能在显式指定的 TextEdit/fixture app 中连续完成 observe、click、set value、scroll。
3. 每个动作都显式带 session/app/window/observation,不存在后端 focus 状态。
4. 多 session 并发请求不会把动作发送到错误应用或窗口。
5. 写操作失败没有自动 fallback 或自动重试。
6. session cancel 后无迟到输入。
7. secure field 和禁止应用策略有自动测试。
8. 新工具的 transcript 名称、activity summary 和三语 i18n 已同步。
9. macOS 桌面截图只有一个原生实现事实源。
10. `go test ./...`、Electron tests、Web tests、签名 desktop smoke 全部通过。

## 16. 延后事项

以下能力在 MVP 稳定后单独设计,不提前加入状态或兼容分支:

- Windows UI Automation。
- 跨设备或锁屏后台控制。
- drag、文本选择和辅助 AX action。
- 操作录制与 workflow 回放。
- 自动恢复布局变化。
- 基于视觉模型的元素定位。
- 外部应用专用适配器。
