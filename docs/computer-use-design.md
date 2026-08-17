# macOS Computer Use 设计与实施计划

> 状态:C0–C3 已实现,C4 确定性 Fixture smoke 已通过,真实应用与发布验收待实施
>
> 日期:2026-08-14
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
5. 原生操作优先使用 AXUIElement `press`、`set_value`、`select` 与受限 `submit`;仅当截图观察明确提供坐标且目标 App 保持前台时允许受限指针动作。
6. 写操作不自动重试。结果不确定时返回明确错误,下一步先通过 `use_app` 刷新窗口并观察当前状态。
7. 当前能力不监听键盘或鼠标,不录制或回放操作;仅 `submit` 在 AXConfirm 不可用时向已验证的目标进程发送一次无修饰键的 Return。
8. 应用生命周期采用 session ownership:只有当前 session 新启动并取得 `launchID` 的进程才能普通退出;已运行应用永不归属,永不强杀。

### 0.1 当前 C0 落地

已实现 `native/macos/computer-use-helper`:

- 无提示读取 Accessibility 与 Screen Recording 权限状态,权限请求必须通过显式参数触发。
- 列出已安装的前台 GUI App 身份与运行状态,不读取窗口标题或内容;窗口只由 `use_app` 返回。
- 观察指定 bundle ID 的有限 AX 树,表格和 outline 优先返回可见行,普通值截断,secure text field 永不返回值。
- 使用当前元素路径与语义特征生成指纹 ID;界面结构变化时动作拒绝匹配,不按旧遍历序号误操作。
- 仅支持 `press`、`set_value`、`select`、受限 `submit` 与截图绑定的单击、左键双击、右键、左键拖拽和滚轮;不开放任意键盘或组合键。
- 在一次原生请求中观察显式 `windowID` 并按需截取同一窗口到 PNG,不录屏、不监听用户输入、不录制或回放工作流。
- 按 bundle ID 启动应用,并按精确 bundle ID + PID 发出普通退出请求;原生层不保存 session ownership。

开发命令:

```bash
make computer-use-helper-test
make computer-use-helper-dev
./bin/Pudding\ Computer\ Use.app/Contents/MacOS/PuddingComputerUseHelper permissions
./bin/Pudding\ Computer\ Use.app/Contents/MacOS/PuddingComputerUseHelper list-apps
```

开发构建默认使用本机 `Pudding Dev Local` 代码签名证书保持 TCC 身份稳定;可通过 `PUDDING_COMPUTER_USE_DEV_IDENTITY` 指定其它开发证书。缺少该证书时回退到 ad-hoc 签名,辅助功能权限需要手动添加且重新构建后可能失效。

当前也已完成 C1 原生 Host:

- Helper 支持串行 NDJSON 常驻协议,每个请求和响应显式携带 request ID。
- Electron `ComputerUseHost` 按需启动 Helper并将所有请求排成单队列,负责消息大小、超时、取消、崩溃和退出清理。
- 超时、取消或协议失步会终止当前 Helper;已发送写操作返回 `outcome=unknown`,只读操作返回 `outcome=not_started`,均不自动重试。
- 开发启动自动构建并使用 `Pudding Computer Use.app`;arm64/x86_64 发布 runtime 都包含该后台 App。
- Mach-O 内嵌固定 identifier `com.teatak.pudding.computer-use-helper`,发布校验拒绝身份漂移。

当前也已完成 C2 daemon 与模型工具:

- Electron 暴露独立 loopback bearer-token bridge;除权限查询外所有请求必须显式带 `sessionID`。
- Go `computer.Manager` 保存 session-scoped 短期 observation,动作快照单次消费,所有 session 的写动作全局串行。
- 内置 App ID 为 `computer-use`,包含 `list_apps`、`use_app`、`quit_app`、`observe`、`act` 五个 Work 模式工具。
- 每个 session 首次访问一个 app 时统一审批观察和操作;同 session 后续访问同 app 不再重复审批,项目的“完全允许”模式也不能跳过首次审批。
- 动作传输中断返回 `outcome=unknown` 且不可重试;成功后观察失败仍明确保留动作已完成事实。
- transcript 已有可读显示名、图标、分组和简中/繁中/英文文案。

当前已通过确定性 Fixture App 和 Calculator 验证真实 `press`/`set_value`/`select`/`submit`、session-owned quit 与已运行 App 非 ownership 分支。签名安装包和跨版本升级实测仍属于 C4。

## 1. 背景与现有基础

Pudding 已具备 Computer Use 上层闭环的大部分基础:

- [`internal/tool/desktop_screenshot.go`](../internal/tool/desktop_screenshot.go):桌面截图进入 session attachment 和模型上下文。
- [`internal/tool/browser.go`](../internal/tool/browser.go):已有 observe、screenshot、click、type、scroll 工具语义。
- [`electron/browser-bridge-server.cjs`](../electron/browser-bridge-server.cjs):已有 Electron 与 daemon 之间的 loopback token bridge 模式。
- [`internal/engine/approval.go`](../internal/engine/approval.go):已有 session-scoped 审批事件和工具调用暂停/恢复。
- [`internal/app/builtin.go`](../internal/app/builtin.go):已有按需加载的内置 App 机制。

当前缺口集中在 macOS 原生控制层:

- 枚举已安装应用,并在明确使用某个 App 时发现其窗口。
- 读取并归一化 Accessibility 树。
- 对 AX 元素执行点击和赋值。
- 按应用或窗口截图。
- 引导并检查屏幕录制、辅助功能权限。
- 应用授权、安全拦截、错误恢复和真实应用验收。

## 2. 目标

MVP 支持:

- 枚举当前可操作的 macOS 应用。
- 观察指定应用和窗口,返回有限的 AX 文本树与交互元素。
- 截取指定窗口或应用画面并作为 session attachment 送入模型上下文。
- 对支持 `AXPress` 的 AX 元素执行一次 press。
- 设置可编辑 AX 元素值。
- 选择可选中的表格行或列表项。
- 对支持 `AXConfirm` 的输入框执行确认提交。
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
                      -> AXUIElement
                      -> ScreenCaptureKit
```

### 4.1 Go daemon

负责:

- 工具定义、参数校验和结构化结果。
- 显式 session 路由。
- 动作审批、取消和超时。
- 全局写操作队列。
- 截图 attachment 存储和 canonical turn part。
- session 关闭时释放该 session 的 observation,并尽力普通退出 session-owned app。

不负责:

- 保存当前前台应用或窗口。
- 直接调用 macOS Accessibility API。
- 直接持有跨 turn AX 元素引用。
- 在不确定动作结果后自动重试。

### 4.2 Electron main

负责:

- 启动、监督和停止签名 Helper。
- macOS TCC 权限检查与权限设置入口。
- 对 daemon 暴露仅 loopback、带启动 token 的 Computer bridge。
- 对 renderer 暴露最小权限状态和设置 IPC,不向 renderer 暴露任意原生操作接口。

### 4.3 Swift Helper

负责:

- 调用 Accessibility 和 ScreenCaptureKit。
- 生成归一化 observation。
- 解析 `elementID` 并执行唯一指定动作。
- 在动作前再次验证应用、窗口和 observation。
- 通过 NDJSON stdin/stdout 与 Electron main 通讯,不监听网络端口。

Helper 不保存业务 session、模型上下文或用户消息。

### 4.4 全局桌面资源

本机应用 UI 是全局资源,不能按 session 并行写入:

- 原生读写请求统一经过 Electron Host 单队列,与串行 Helper 保持一致。
- 所有写操作经过 daemon 全局队列。
- daemon bridge 请求必须显式携带 `sessionID`;Helper 请求携带独立 request ID。
- 取消 session 只移除该 session 的待执行项,不能影响其它 session。
- 不存在 daemon 级 active session 或 focus session。

## 5. 原生实现

### 5.1 Helper 形态

新增签名 Swift Helper,并作为具有固定 bundle ID 的 `Pudding Computer Use.app` 嵌套到 Pudding.app:

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

开发版使用 `com.teatak.pudding.dev.computer-use-helper`,发布版使用 `com.teatak.pudding.computer-use-helper`。Helper 的 bundle identifier、签名 identity 和 designated requirement 必须稳定,否则版本升级可能导致 TCC 权限重新授权。

### 5.2 观察

使用 AXUIElement 获取:

- bundle ID、进程 ID、应用名称。
- window identifier、标题、frame、focused 状态。
- element role、subrole、title、description、identifier、value 摘要、enabled、focused、frame。
- 支持的 AX actions,例如 `AXPress` 与 `AXConfirm`,以及可写 selection 属性。

归一化时:

- 默认只返回可见窗口和有限深度的交互元素;table/outline 使用 `AXVisibleRows`,并以每行的绝对 `AXIndex` 生成路径。缺少唯一绝对索引的可见行子树仅可读取,不暴露动作。
- secure text field 只返回角色和位置,绝不返回值或任何动作。
- 普通文本设置 UTF-8 字节上限,AX value 超限时标记 `valueTruncated`。
- `elementID` 是当前 observation 内的短 ID,不是长期实体 ID。
- 不把原始 AX 指针暴露给 daemon 或模型。

### 5.3 截图

使用 ScreenCaptureKit 截取明确的 window:

- 默认只截目标窗口,不截取其它应用。
- 返回逻辑坐标、像素尺寸和 scale factor。
- 截图存储继续复用 session attachment 主链路。
- 模型不需要视觉信息时只使用 AX observation,减少隐私暴露和 token 成本。
- 模型请求截图时,AX observation 与截图在同一个 Helper 请求内完成,不在两次调用之间暴露可插入的 Computer Use 操作。

Computer Use 窗口截图由 Helper 的 ScreenCaptureKit 单一路径实现。现有全桌面截图是不同能力,不作为 Computer Use 失败后的 fallback。

Helper 在观察、截图和操作目标窗口期间保持一个只针对该 `windowID` 的轻量 ScreenCaptureKit stream。每步结束后采用 5 秒空闲释放;同一窗口的新操作会重置计时,避免连续操作时系统标识反复闪烁。stream 帧直接丢弃,不录制、不保存;它只用于让 macOS 显示系统窗口共享标识。标识由系统跟随窗口移动和缩放,也可能出现在普通桌面截图中。Pudding 不再自绘第二套悬浮提示。

### 5.4 动作唯一实现

| action | 唯一实现 | 完成条件 |
| --- | --- | --- |
| `press` | AX `AXPress` | action 返回成功且目标仍属于指定 app/window |
| `set_value` | AX set value | 重新读取值与期望一致 |
| `select` | AX `AXSelected=true` 或父容器 `AXSelectedRows` | 重新读取 selection 包含目标元素 |
| `submit` | 优先 AX `AXConfirm`;否则仅向目标 PID 发送一次 Return key-down/key-up | App 必须活跃,元素必须已聚焦、启用、非安全且为可编辑单行文本控件;action 返回成功后重新观察 |
| `click` / `drag` / `scroll` | 目标 App 已前台时，通过系统 HID 发送受限指针事件 | 只接受同一 `observe-capture` 截图左上角像素坐标;执行前重新校验窗口尺寸和 scale factor;窗口只移动时按新 origin 映射;成功只代表事件已投递 |

规则:

- 任一动作失败都不自动改用坐标、键盘、AppleScript 或剪贴板;坐标点击必须由模型显式选择且绑定截图 observation。
- 所有写操作执行前验证目标应用仍存在、窗口仍匹配、observation 未过期。
- 动作执行后重新观察,但不以第二个动作修复第一个动作。

## 6. Observation 契约

示例:

```json
{
  "sessionID": "sess_x",
  "appID": "com.apple.TextEdit",
  "windowID": 42,
  "observationID": "obs_x",
  "observedAt": "2026-08-13T08:00:00Z",
  "screenshot": null,
  "text": "window 'Untitled'\n[1] AXTextArea ...",
  "elements": [
    {
      "elementID": "ax_...",
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
- 默认 TTL 30 秒,每个 session 最多保留 16 个未过期 observation;超限直接失败。
- action 必须传 `observationID`。
- Helper 在动作前重新遍历指定窗口并按指纹重新解析元素;找不到或不唯一时拒绝执行。
- 首个 action 前显式 observe 一次;每次成功 action 返回的新 observation 直接作为下一次 action 的输入,连续操作之间不重复 observe。
- action 只返回 `observationError`、或当前 observation stale/过期时,才重新 observe。
- 模型收到 stale 后必须重新 observe,不能重复原动作。
- observation registry 位于 Go daemon 内存,是短期路由缓存;真实界面始终以当前 AX 树为事实源。

## 7. 模型工具

Computer Use 作为 `computer-use` 内置 App,在 Work 模式按需加载。当前提供五个模型工具:

### `builtin_computer_list_apps`

仅在需要从应用名发现 `appID` 时使用。列出已安装前台应用的身份、运行状态和可控状态,不返回权限、窗口标题或窗口内容。

### `builtin_computer_use_app`

按 `appID` 使用应用：默认在后台启动或复用现有进程，不激活、不抬升已运行 App，并返回 `windowStatus` 与 PID 绑定的当前窗口。用户明确要求显示、聚焦或切换到该 App，或者后续需要坐标指针动作时传 `foreground=true`；该模式可激活或重新打开窗口。只有 `windowStatus=ready` 时才能继续观察。仅当当前 session 确实新启动该进程时返回 `launchID + PID`;应用原本已运行时不返回 `launchID`,不获得关闭权。

### `builtin_computer_quit_app`

只接受当前 session 持有的 `launchID`,并对其对应的 bundle ID + PID 发出普通退出请求。绝不 force quit。返回 `closed=false` 时保留 ownership,停止自动操作并请用户处理未保存内容或确认窗口。

### `builtin_computer_observe`

参数:

```json
{
  "appID": "com.apple.TextEdit",
  "windowID": 42,
  "includeScreenshot": false
}
```

返回 observation。`windowID` 必填,不能使用当前焦点猜测。`includeScreenshot=true` 时,AX 观察和窗口截图在同一个 Helper 请求内完成。

### `builtin_computer_act`

参数:

```json
{
  "appID": "com.apple.TextEdit",
  "windowID": 42,
  "observationID": "obs_x",
  "action": "set_value",
  "elementID": "ax_...",
  "value": "hello"
}
```

`action` 只允许 `press`、`set_value`、`select`、`submit`、`click`、`drag` 和 `scroll`;只有 `set_value` 必须传 `value`。指针动作禁止 `elementID`/`value`,必须传同一截图 observation 中的坐标,原点为窗口截图左上角。`click` 支持单次左/右键和左键双击；`drag` 使用左键起止坐标；`scroll` 使用正数向下/向右的像素 delta。指针动作要求目标 App 已显式切到前台,只确认事件投递,不宣称 App 状态已改变。截图 observation 只能消费一次,动作后的普通 observation 不能继续指针操作。`submit` 只会出现在活跃 App 内已聚焦、启用、非安全、可编辑的单行文本控件上。单一 action 工具让审批、串行化和 transcript 展示共用一个入口。

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
| `POST /computer/apps/list` | 列已安装应用身份与运行状态,不读窗口内容 |
| `POST /computer/apps/use` | 默认后台启动或复用一个明确 bundle ID；显式 `foreground=true` 时激活或重新打开，为坐标指针动作建立前台目标，并返回当前可发现窗口及 window ID |
| `POST /computer/apps/quit` | 普通退出一个明确 bundle ID + PID |
| `POST /computer/observe` | AX observation |
| `POST /computer/observe-capture` | 同一次 Helper 请求内完成 AX observation 与窗口截图 |
| `POST /computer/act` | 执行一个显式动作 |
| `POST /computer/pointer` | 执行一次由截图尺寸与 scale factor 约束的 click、drag 或 scroll |
| `GET /computer/permissions` | 读取权限状态 |

所有请求:

- 仅接受启动时生成的 bearer token。
- 除 permissions 外必须携带 `sessionID`。
- body 设置严格大小上限。
- 不接受任意脚本、shell command 或 AppleScript。
- 错误返回稳定 `code`、`message`、`retryable` 和 `outcome`;权限错误另带机器可读的 `permission` 或 `permissions`。

`outcome`:

- `not_started`:确认动作没有执行,调用方可在重新观察后决定是否重试。
- `completed`:动作已完成。
- `unknown`:动作可能已执行但响应丢失,必须重新观察,禁止直接重试。

## 9. 错误语义

首版稳定错误码:

- `computer_unavailable`
- `computer_permission_required`
- `computer_permission_denied`
- `computer_app_not_found`
- `computer_app_not_installed`
- `computer_launch_not_owned`
- `computer_window_required`
- `computer_window_not_found`
- `computer_observation_not_found`
- `computer_observation_stale`
- `computer_app_not_foreground`
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

- Screen & System Audio Recording:识别目标窗口并按需读取单帧画面。
- Accessibility:读取 AX 树并执行 AX 动作。

当前不监听键盘或鼠标,因此不申请 Input Monitoring。只发送两类受限合成事件：目标进程的单次 Return，以及目标 App 已前台时截图绑定的单击、左键双击、右键、左键拖拽和滚轮；不开放任意键盘或组合键。

Swift Helper 是 Computer Use 两项权限的系统事实源;Pudding 主进程的桌面截图权限单独显示,不得代替 Helper 权限。只有实际 Computer Use 请求缺少相关权限时才显示结构化引导,模型不生成权限说明。引导和设置页读取同一 Electron 权限控制器;用户切回 Pudding 后自动复检,齐全时恢复原请求。只有系统已报告授权、重启 Helper 后仍返回同一权限错误时才显示 Pudding 重启按钮。Pudding 不尝试自动点击系统权限提示或管理员认证。

### 10.2 应用策略

应用硬性禁止策略由 Swift Helper 单一实现。daemon 在 SQLite 中持久化 `sessionID + appID` 授权,在内存中保存短期 observation 和 session-owned launch registry。

首版规则:

- Helper 当前硬拒绝 Pudding 自身/父应用、终端、常见密码管理器、Keychain Access 和系统安全授权进程。
- 其余可控应用由模型显式指定 bundle ID 和 window ID。目标 GUI App 的启动和退出必须使用 Computer Use 工具,不得使用 shell `open`、`osascript` 或 AppleScript。每个 session 首次启动、观察、操作或退出一个 app 时申请一次确认,该确认同时授权当前 session 后续启动、观察、操作和 session-owned 退出同一 app。
- `apps` 是已安装应用身份清单,不是授权列表;窗口只以 `use_app` 返回值为准,不得用 `list_apps` 刷新窗口或引导用户去设置页添加应用。
- 浏览器任务优先使用 Pudding Browser;只有用户明确要求现有外部浏览器登录状态时才使用 Computer Use。
- 目标应用在动作前和动作后都必须与请求 bundle ID 一致。

### 10.3 数据保护

- secure text field 的值永不进入 observation、日志、tool result 或 attachment。
- Helper 日志不记录输入文本、截图 bytes 或完整 AX value。
- 截图只保存到当前 session attachment 目录。
- AX 值与 app/window 标题按 UTF-8 字节上限截断,结果标记值截断状态;Helper 响应另有整体大小上限,错误日志不输出完整 AX tree。
- 取消 turn 后停止新截图和新动作。

### 10.4 审批

MVP:

- observe/screenshot 与 launch/quit/act 共用 SQLite 中同一份 `sessionID + appID` 授权。
- 当前 session 首次访问 app 时触发一次 Computer Use 对话审批;批准后,该 session 启动、观察、操作和 session-owned 退出同一 app 都不再询问;Pudding 重启后仍然有效。
- 不同 session 或不同 app 必须重新审批;只有删除 session 时才通过外键级联清除其 app 授权。
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
| observation | Go daemon 内存短期 registry |
| session-owned launch | Go daemon 内存 launch registry |
| session app 授权 | SQLite `computer_app_grants(session_id, app_id)` |
| 固定禁止应用 | Swift Helper `AppPolicy` |
| macOS 权限 | TCC/System Settings |

截图 tool result 继续作为 canonical attachment 进入上下文;普通 AX observation 作为 tool result 进入当前 turn,不另建长期数据库表。

## 12. UI

新增设置区:

- Screen Recording 权限状态。
- Accessibility 权限状态。

执行时:

- transcript 显示“查看应用”“操作应用”等可读工具名。
- 显示当前正在操作的应用和窗口。
- 提供停止按钮,调用现有 session cancel。
- sensitive approval 使用现有 approval overlay,不增加第二套确认系统。
- 目标窗口标题栏显示 macOS 自带的窗口共享标识。
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
- 没有 AppleScript 或自动坐标 fallback;受限坐标点击只能显式使用同一截图 observation。

### C1 Helper 与 Electron Host,7–9 天

- 实现 NDJSON Helper 协议。
- 实现 app/window list、observe、window screenshot。
- 实现 AX press/set value/select、受限 submit 和截图绑定的受限指针动作;不开放通用 CGEvent 输入动作。
- 实现 Helper 生命周期、超时、崩溃和取消。
- 完成 Helper 嵌套签名和 notarization 验证。

### C2 Daemon、工具与 session 路由,5–7 天（已实现）

- 新增 `internal/computer` manager 和 Electron bridge client。
- 新增 Computer Use 内置 App 和五个工具。
- 新增全局写队列、session release 和结构化错误。
- 截图接入 attachment/canonical context。
- 增加 stale observation 和 unknown outcome 契约测试。

### C3 权限、安全与 UI,5–7 天（已实现）

- 设置页显示 Accessibility 与 Screen Recording 状态,可显式申请权限或打开对应系统设置;不显示或申请 Input Monitoring。
- 应用读取和操作不做设置页白名单,统一使用现有对话审批;同一 session 对同一 app 只确认一次。
- 固定禁止应用策略由 Helper 单一实现,覆盖 Pudding 自身/父应用、终端、系统安全界面和常见密码管理器。
- 已添加 transcript 显示名、图标、activity summary、审批说明和三语 i18n。

### C4 稳定性与验收,7–10 天

- 多窗口、多显示器、Retina 和窗口移动测试。
- Helper crash、应用退出、权限撤销、session cancel 测试。
- 针对 TextEdit、Calculator、Notes 和一个确定性 fixture app 做 smoke。
- 完成打包、签名、notarization 和升级测试。

当前已新增 `PuddingComputerUseFixture`，提供可编辑文本框、500 行虚拟表格、secure field、按钮、checkbox 和两个窗口。以下命令通过实际签名 Helper 验证 launch、窗口发现、可见行遍历、`set_value`、`submit`、`select`、`press`、secure field 脱敏和普通 quit：

```bash
make computer-use-fixture-smoke
```

完整产品链路使用确定性 scripted provider，覆盖 App 加载、同一 session+app 一次授权、Engine、Manager、Electron bridge、签名 Helper、fixture 操作和 session-owned quit：

```bash
make computer-use-product-smoke
```

真实 Calculator 验收会通过同一产品链路执行两阶段清除后再输入 `1 → + → 1 → =`，确认显示结果为 `2`，并使用本 session 获得的 `launchID` 正常关闭：

```bash
make computer-use-calculator-smoke
```

已运行 App 的非 ownership 分支由测试夹具先启动 Calculator，再让 session 完成同一操作。`use_app` 必须不返回 `launchID`，任务结束后 Calculator 必须仍以同一 PID 运行；最后仅由测试夹具清理：

```bash
make computer-use-calculator-existing-smoke
```

这些 smoke 都依赖本机已向开发版 `Pudding Computer Use.app` 授予 Accessibility 与 Screen Recording，不进入无 TCC 环境的普通单元测试。目标 App 必须在运行前关闭，测试不会接管或关闭原本已运行的 App。

自动回归已覆盖：Helper crash 后重启、请求 timeout、在途 cancel 终止 Helper、原生请求串行化、排队请求取消不影响在途请求、权限撤销或目标 App 退出后不重试旧动作,以及 observe+capture 单请求协议。

发布链路会在签名前把嵌套 Helper 的版本同步为外层 Pudding 版本。`make desktop-verify` 对 staged app、ZIP 和 DMG 中的 Helper 统一检查 bundle ID、Developer ID/Team ID、designated requirement、`LSUIElement`、屏幕捕获用途说明、架构和可移植依赖。

`make desktop-update-test` 允许从尚未包含 Helper 的旧版本升级，但要求升级后的 App 包含并通过上述 Helper 校验。首个含 Helper 的签名版本发布后，后续版本使用以下严格门禁，要求升级前后 Helper 的 identifier、Team ID 和完整 designated requirement 完全一致：

```bash
make desktop-computer-use-update-test
```

Preview 对应使用 `make desktop-preview-update-test` 和 `make desktop-preview-computer-use-update-test`。严格门禁必须先在 `/Applications/Pudding.app` 安装一个更旧、且已经包含 Helper 的同通道签名版本。

总计约 27–37 个工程日。并行投入原生与 Go/Web 两名工程师时,日历时间可压到约 4 周,但产品级兼容性验证不能等比例压缩。

## 14. 测试策略

### 单元测试

- observation 归一化、截断和 secure field 脱敏。
- action 参数互斥、stale observation、错误映射。
- 全局队列的 session 隔离和 cancel。
- bridge token、body limit 和 Helper crash。
- tool result attachment 与 canonical context。
- Computer Use 的 `sessionID + appID` 一次审批、session 隔离和固定 denylist。

### 原生集成测试

新增确定性 macOS fixture app,包含:

- button、checkbox、text field、secure field。
- scroll view。
- 两个窗口。
- 点击后改变明确状态的按钮。
- 能分别记录单击、双击、右键、拖拽和滚轮的指针靶区。
- 可移动和缩放的窗口。

测试不依赖第三方应用的易变布局。TextEdit、Calculator、Notes 只作为发布 smoke。

### 关键回归

- 错误 appID 不产生任何输入。
- stale observation 不产生任何输入。
- action timeout 不执行第二种动作。
- 多 session 同时写入时严格串行且不串目标。
- session cancel 后没有迟到点击或输入。
- 单击、双击、右键、拖拽和滚轮分别到达前台 Fixture，后台目标一律拒绝。
- secure field 内容不出现在日志、结果和截图元数据中。
- Pudding 自身和终端始终无法成为动作目标。

## 15. MVP 完成标准

满足以下条件才算完成:

1. release 签名构建可完成权限授权,重启和升级后状态符合预期。
2. 模型能在显式指定的 TextEdit/fixture app 中连续完成 observe、press、set value、select 和 submit。
3. 每个动作都显式带 session/app/window/observation,不存在后端 focus 状态。
4. 多 session 并发请求不会把动作发送到错误应用或窗口。
5. 写操作失败没有自动 fallback 或自动重试。
6. session cancel 后无迟到输入。
7. secure field 和禁止应用策略有自动测试。
8. 新工具的 transcript 名称、activity summary 和三语 i18n 已同步。
9. `go test ./...`、Electron tests、Web build、Swift tests、签名 desktop smoke 全部通过。

## 16. 延后事项

以下能力在 MVP 稳定后单独设计,不提前加入状态或兼容分支:

- Windows UI Automation。
- 跨设备或锁屏后台控制。
- drag、文本选择和辅助 AX action。
- 操作录制与 workflow 回放。
- 自动恢复布局变化。
- 基于视觉模型的元素定位。
- 外部应用专用适配器。
