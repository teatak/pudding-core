# 托管浏览器实施计划

> 状态:MVP 实现中。  
> 范围:Pudding 通过托管浏览器执行网页观察与操作。  
> 目标:先做可见托管浏览器 MVP,验证多 session 架构、用户信任感和工具链路。

## 1. 定义

托管浏览器是 Pudding 启动并管理的一份专用浏览器实例。它可以使用系统已安装的 Chrome/Chromium,但必须使用 Pudding 专用 profile、专用调试端口和 session-scoped tab 绑定。

它不是直接接管用户日常使用中的 Chrome 窗口。

## 2. 架构原则

必须遵守:

- 后端没有 browser focus / current tab / current session 概念。
- 所有浏览器 API 和工具调用必须显式带 `sessionID`。
- daemon 拥有浏览器进程资源;session 拥有 browser transport、tab 绑定和操作上下文。
- browser 工具结果若要进入模型上下文,必须落为 canonical tool result message。
- 不新增无 session scope 的主路径接口,例如 `/browser/action`、`/browser/events`。
- 新增 LLM 可调用浏览器工具时,必须同步 transcript 工具显示名与 i18n 文案。

## 3. MVP 范围

MVP 使用可见托管浏览器窗口,预计 3-5 个工作日。

功能:

- 检测系统 Chrome/Chromium。
- 以独立 `user-data-dir` 启动可见浏览器。
- 每个 session 显式创建或绑定 tab。
- 支持打开 URL、截图、读取页面标题/URL、读取简化 DOM、点击、输入、滚动。
- 支持工具超时、取消和错误返回。
- 工具结果进入 transcript。
- 前端展示当前 session 的浏览器状态、URL、标题、截图缩略图和“打开窗口”操作。

非目标:

- 不打包 Chromium。
- 不接管用户已有 Chrome profile。
- 不做 Chrome extension。
- 不在 Pudding 主窗口内嵌完整网页。
- 不做付款、提交表单、发消息等高风险动作自动确认。

## 4. 显示方式

MVP 采用独立可见浏览器窗口:

- 网页本体显示在 Pudding 启动的 Chrome/Chromium 窗口。
- Pudding 主界面只显示 browser panel:状态、URL、标题、截图缩略图、暂停/继续、打开窗口。
- 用户可直接看见 AI 正在操作哪里,必要时可手动接管。

后续可增加 CDP screencast 预览,但不作为 MVP 前置条件。

## 5. 后端设计

### 5.1 资源模型

```text
daemon
  └─ browser manager
       └─ browser process(profile, debug port)
            └─ session binding
                 └─ tab/page
```

建议实体:

- `BrowserInstance`:进程、profile 路径、CDP endpoint、健康状态。
- `BrowserSessionBinding`:sessionID、instanceID、tabID、lease 状态。
- `BrowserTab`:tabID、targetID、URL、title、lastScreenshotAt。

### 5.2 API 草案

只提供 session-scoped 路由:

| API | 用途 |
| --- | --- |
| `POST /sessions/{id}/browser/tabs` | 为 session 创建或绑定 tab |
| `GET /sessions/{id}/browser/tabs` | 查看当前 session 的 tab 列表 |
| `GET /sessions/{id}/browser/tabs/{tabID}` | 查看 tab 快照 |
| `POST /sessions/{id}/browser/tabs/{tabID}/open` | 打开 URL |
| `POST /sessions/{id}/browser/tabs/{tabID}/observe` | 读取标题、URL、文本和简化元素列表 |
| `POST /sessions/{id}/browser/tabs/{tabID}/screenshot` | 截图 |
| `POST /sessions/{id}/browser/tabs/{tabID}/click` | 点击 selector 或坐标 |
| `POST /sessions/{id}/browser/tabs/{tabID}/type` | 向 selector 或当前焦点输入文本 |
| `POST /sessions/{id}/browser/tabs/{tabID}/scroll` | 滚动窗口或 selector |
| `POST /sessions/{id}/browser/tabs/{tabID}/release` | 释放绑定 |

浏览器实时状态通过现有 session SSE 发送,事件仍然挂在 `/sessions/{id}/events`。

### 5.3 工具草案

内置工具建议:

- `builtin_browser_open`
- `builtin_browser_observe`
- `builtin_browser_screenshot`
- `builtin_browser_click`
- `builtin_browser_type`
- `builtin_browser_scroll`

所有工具参数必须带 `tabID` 或通过当前 session 的唯一 browser binding 明确解析。若 session 下有多个 tab,必须让模型或用户指定,不能从后端全局 focus 推断。

## 6. 前端设计

新增 browser panel,位置可先放在 transcript 右侧/下方的工具区,不阻塞主聊天。

展示:

- 状态:未启动、启动中、可用、操作中、错误。
- 当前 URL、标题。
- 最近截图缩略图。
- 打开 URL。
- 手动 observe / screenshot / click / type / scroll。
- 暂停 AI 操作。
- 释放 tab。

transcript:

- 浏览器工具显示友好名称,不暴露 snake_case。
- 截图结果可以显示缩略图。
- DOM 观察结果默认折叠。
- 点击/输入/滚动显示简洁操作摘要。

## 7. 安全与隐私

MVP 约束:

- 默认使用 Pudding 专用 profile,不读取用户日常 Chrome profile。
- daemon 只 bind loopback,所有请求继续使用启动 token。
- profile 放在 dev/release 隔离目录:
  - dev:`~/.pudding-dev/browser-profiles/...`
  - release:`~/.pudding/browser-profiles/...`
- 密码框输入、付款、发消息、删除数据等动作先不自动执行。
- 截图和 DOM 结果进入 transcript 前需要截断与摘要,避免大内容污染上下文。

后续再做:

- 高风险动作确认。
- tab lease 冲突提示。
- profile 清理/导出/删除。
- 真实用户 Chrome extension。

## 8. Chromium 策略

MVP 不打包 Chromium:

- 优先检测系统 Chrome。
- 其次检测 Chromium。
- 找不到时提示用户安装或后续触发下载。

后续选项:

| 方案 | 安装包体积 | 磁盘占用 | 说明 |
| --- | ---: | ---: | --- |
| 使用系统 Chrome | +0 | profile 另算 | MVP 推荐 |
| 首次使用下载 Chromium | +0 | 约几百 MB | 中期推荐 |
| 随包附带 Chromium | +150-250MB+ | 约 300-500MB+ | release 稳定但包更大 |

## 9. 交付切片

| 切片 | 内容 | 验收 |
| --- | --- | --- |
| B1 进程管理 | 检测 Chrome、启动可见托管浏览器、独立 profile | 能看到独立窗口,退出 daemon 后可清理进程 |
| B2 tab 绑定 | session-scoped tab 创建/列表/释放 | 两个 session 不共享隐式 tab |
| B3 观察能力 | open、observe、screenshot | transcript 有工具结果,刷新后 canonical 可回看 |
| B4 操作能力 | click、type、scroll | 可完成简单网页表单操作 |
| B5 前端 panel | 状态、URL、标题、截图、打开窗口 | 用户能知道 AI 操作的是哪个浏览器 |
| B6 稳定性 | 超时、取消、错误、基础测试 | 操作失败不破坏 turn 收尾 |

## 10. 验收场景

MVP 完成需跑通:

1. 新 session 打开托管浏览器,访问 `https://example.com`,截图并在 transcript 展示。
2. 同时打开两个 session,各自绑定不同 tab,互不串页。
3. 对一个简单表单执行点击和输入,失败时返回可读错误。
4. 用户取消 turn 后,正在执行的浏览器操作停止或超时退出。
5. 重启前端后,已完成的 browser tool result 仍能从 canonical messages 渲染。

## 11. 后续路线

MVP 稳定后再做:

- CDP screencast 嵌入式预览。
- 高风险动作确认流。
- 首次使用自动下载 Chromium。
- Chrome extension 接入用户真实浏览器和登录态。
- 更强的 accessibility tree 观察与 selector 生成。
