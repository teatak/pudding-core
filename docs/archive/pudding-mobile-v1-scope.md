# Pudding Mobile v1 功能裁剪建议

> 状态:已归档。Pudding 当前产品边界仅支持 Electron Desktop,本文不进入当前主线实施。

建议把 Mobile v1 定义为 **“BYOK 本地 AI 对话 + Canvas”**，而不是 Desktop 的缩小版。

## 功能裁剪

66 项能力中：

- 保留 15 项
- 移动化改造 19 项
- 删除 26 项
- 二期 4 项
- 移动新增 2 项

## 核心边界

- **保留**：多会话、流式生成、取消、上下文管理、Provider/模型配置、六类 Canvas、图片和文件附件。
- **删除**：Work/Code 模式、Project、Git、LSP、终端、命令执行、浏览器自动化、Apps、MCP、Skills、本地语音 runtime。
- **重构**：daemon、REST、SSE、SQLite 改为进程内 TypeScript `AgentEngine` + JSON FileSystem + SecureStore。
- **新增**：`DemoProvider` / Demo 会话、第三方 AI 数据发送同意。
- **项目形态**：建议作为独立 `pudding-mobile` 项目开发，不在 Desktop 主线中增加移动端兼容分支。

## 完整功能裁剪矩阵

决策标记：🔵 核心保留　🟠 移动改造　🟣 移动新增　🩷 二期可选　⚫ 首版移除

### 产品与会话

| 功能 | Desktop 当前形态 | 独立 Mobile v1 | 决策 |
| --- | --- | --- | --- |
| 产品运行形态 | Electron 壳 + 常驻 Go daemon | Expo/React Native 单体 App，TypeScript 本地核心 | 🟠 移动改造 |
| 多会话 | SQLite 中的第一等 session | 本地 JSON session，保留创建、重命名、删除 | 🔵 核心保留 |
| 会话列表 | Rail、分组、拖动排序、上下文菜单 | 普通列表、置顶与滑动操作；取消复杂分组拖动 | 🟠 移动改造 |
| 会话搜索 | SQLite FTS5 全文搜索 | 扫描标题与会话 JSON；数据量大后再加索引 | 🟠 移动改造 |
| 草稿会话 | 路由草稿态，可预选 Project | 新对话页，首次发送后持久化 | 🔵 核心保留 |
| 分屏会话 | 上下分屏并行查看两个 session | 一次只显示一个 session；iPad 首版也不做双会话 | ⚫ 首版移除 |
| 多窗口 / 多 runtime | 多个 Electron 窗口与 runtimeID 显式路由 | 单 App runtime，不提供多窗口工具路由 | ⚫ 首版移除 |
| 后台持续运行 | daemon 持有 turn、硬件和后台进程生命周期 | 以前台为主；进入后台时暂停或取消模型流 | ⚫ 首版移除 |

### 对话、模型与上下文

| 功能 | Desktop 当前形态 | 独立 Mobile v1 | 决策 |
| --- | --- | --- | --- |
| 流式对话 | Provider → engine → SSE overlay | `expo/fetch ReadableStream` → 本地 AgentEngine 事件 | 🔵 核心保留 |
| 取消生成 | session cancel API 中断 turn | `AbortController` 直接中断 Provider 请求 | 🔵 核心保留 |
| Canonical messages | turn 结束事务落库，delta 不落库 | turn 结束原子写 session JSON，delta 只在内存 | 🔵 核心保留 |
| 消息幂等与 overlay | `clientMessageID` 对账，防 HTTP 重试重复提交 | 保留 `messageID`；无网络 API 对账层 | 🟠 移动改造 |
| 排队输入 | 生成中排队、编辑下一条输入 | 首版仅允许停止后重新发送 | ⚫ 首版移除 |
| 上下文压缩 | 自动/手动 compact、阈值和 tail turns 设置 | 固定策略：最近消息 + 自动摘要，不暴露高级参数 | 🟠 移动改造 |
| Chat / Work / Code 模式 | 三级能力与 session/turn lease | 仅“对话 + Canvas”，取消 Work/Code 能力升级 | ⚫ 首版移除 |
| 能力审批 | 目录、命令、工具能力请求与批准/拒绝 | 仅保留第三方 AI 数据发送同意与系统权限弹窗 | 🟠 移动改造 |
| Provider 配置 | YAML profiles、预设、动态模型与 Base URL | OpenAI/Anthropic BYOK；Key 存 SecureStore，其余存 JSON | 🟠 移动改造 |
| 自定义 Provider | OpenAI-compatible profiles 与模型探测 | 首版只做官方 Provider；自定义 Base URL 二期加入 | 🩷 二期可选 |
| 模型与 Reasoning | 模型选择、reasoning effort、上下文环与细节展示 | 保留模型和 effort；简化上下文统计 | 🔵 核心保留 |
| 个性化 Prompt | 全局 user prompt 设置 | 保留一个系统指令文本框 | 🔵 核心保留 |
| 用量统计 | session usage 与 daily usage 页面 | 首版仅显示当前回复 token；不做日报 | 🟠 移动改造 |

### Canvas

| 功能 | Desktop 当前形态 | 独立 Mobile v1 | 决策 |
| --- | --- | --- | --- |
| Canvas 工具循环 | UI runtime 通过 MCP WebSocket 注册 `canvas_*` 工具 | `canvas_*` 成为 AgentEngine 内置本地工具，无 MCP | 🟠 移动改造 |
| 内容类型 | Markdown、表格、图表、图册、时间线、Grid | 六类全部保留，复用 payload/schema | 🔵 核心保留 |
| Grid patch | upsert、replace、remove、move、reorder | 保留同一数据契约，本地原子更新 | 🔵 核心保留 |
| 自由窗口 | 拖动、缩放、层级、最大化与窗口位置持久化 | 纵向卡片流 + 单项全屏；iPad 使用两列 Grid | ⚫ 首版移除 |
| Canvas 导航 | Workspace 标签栏与窗口聚焦 | 会话内“对话 / Canvas”切换，卡片点击进入详情 | 🟠 移动改造 |
| 表格与图表 | Recharts、横向表格与桌面尺寸 | `react-native-svg` 图表；表格横向滚动 | 🟠 移动改造 |
| 导出 | CSV/JSON 保存路径、Finder reveal | 生成临时文件后调用系统 Share Sheet | 🟠 移动改造 |
| 收藏小组件 | 全局 Saved Canvas、冲突检测与跨会话打开 | 首版只允许复制 Canvas item；收藏库放二期 | 🩷 二期可选 |
| 最近关闭 / 恢复 | Closed Canvas 列表、恢复与清空 | 简单删除确认，不维护回收历史 | ⚫ 首版移除 |
| Demo 会话 | 没有专用内置 DemoProvider | 预置会话 + 本地流式 DemoProvider + Canvas 工具演示 | 🟣 移动新增 |

### Code 与项目工作区

| 功能 | Desktop 当前形态 | 独立 Mobile v1 | 决策 |
| --- | --- | --- | --- |
| 代码块展示 | 语法高亮、复制、工具详情 | 保留高亮、横向滚动和复制 | 🔵 核心保留 |
| 文件附件 | 本地路径、拖放、文本与图片附件 | 系统 DocumentPicker，只读上传文本/图片 | 🟠 移动改造 |
| Project 实体 | 多 rootDirs、授权策略、Project-owned Code root | 不建立项目概念 | ⚫ 首版移除 |
| 项目文件 UI | 目录树、搜索、标签、Monaco 编辑器、Markdown 预览 | 仅附件预览，不浏览或编辑工作区 | ⚫ 首版移除 |
| 文件工具 | list/read/stat/search/slice/write/patch/delete/move | 不向模型开放设备文件系统读写 | ⚫ 首版移除 |
| 项目检查与指令 | 语言、manifest、AGENTS.md 检测 | 无项目根，不提供 inspect/instructions | ⚫ 首版移除 |
| Git | status/diff/init/stage/commit/sync/publish/branch | 全部删除 | ⚫ 首版移除 |
| LSP | symbols/definition/references/diagnostics/rename | 不打包语言服务器 | ⚫ 首版移除 |
| 命令与沙箱 | test/build/lint/format/codegen 与 macOS sandbox | 禁止 shell 与代码执行 | ⚫ 首版移除 |
| 终端 / 后台进程 | PTY、xterm、持续进程、停止与轮询 | 全部删除 | ⚫ 首版移除 |
| Turn 文件变更 | 变更追踪、diff、Canvas 文件定位 | 无文件写入，因此不产生 turn diff | ⚫ 首版移除 |

### 工具、Apps 与浏览器

| 功能 | Desktop 当前形态 | 独立 Mobile v1 | 决策 |
| --- | --- | --- | --- |
| Canvas App | runtime App + MCP 动态工具注册 | 合并进产品核心，不再是可装卸 App | 🟠 移动改造 |
| 确认 / 收集输入工具 | UI runtime input-flow tools | 本地 AgentEngine 原生弹 Sheet | 🔵 核心保留 |
| Web 搜索 / 抓取 | Tavily 搜索、网页抓取、天气与时间工具 | 首版无额外服务 Key；二期接 Provider 原生搜索 | 🩷 二期可选 |
| 内置浏览器 | Electron WebView、多标签、历史、截图与自动化 | 链接交给系统浏览器，不做页面操控 | ⚫ 首版移除 |
| MCP | WebSocket runtime MCP、stdio/HTTP App MCP | 首版不支持第三方 MCP | ⚫ 首版移除 |
| Apps 系统 | 安装、启停、OAuth、连接、MCP override、资源 | 全部删除，只保留内置 Canvas 工具 | ⚫ 首版移除 |
| Skills | 全局 Skill 注册、读取、资源与删除 | 不做技能包；少量模板可内置为 starter prompts | ⚫ 首版移除 |

### 媒体、系统能力与设置

| 功能 | Desktop 当前形态 | 独立 Mobile v1 | 决策 |
| --- | --- | --- | --- |
| 图片与相机 | 上传图片、桌面摄像头工具与附件缓存 | ImagePicker/Camera → App 沙箱附件 | 🔵 核心保留 |
| 桌面截图 | 显示器截图与模型附件 | 不提供录屏权限；用户可手动选择截图 | ⚫ 首版移除 |
| 语音输入 | 本地 ASR、VAD、AEC、降噪和 runtime 下载 | 首版依赖系统键盘听写；不维护本地语音模型 | ⚫ 首版移除 |
| TTS | daemon 驱动语音输出与设备绑定 | 二期可接系统 Speech API | 🩷 二期可选 |
| 音频设备绑定 | session 输入/输出绑定与 daemon 硬件所有权 | 交给 iOS/Android 系统音频路由 | ⚫ 首版移除 |
| 路径与文件管理器 | 目录选择、Finder reveal、拖放绝对路径、文件监听 | 只使用系统 Picker、Share Sheet 与 App 沙箱 | 🟠 移动改造 |
| 设置中心 | General、Provider、Skills、Tools、Usage、Voice、About | Provider、模型、外观、隐私、存储、关于 | 🟠 移动改造 |
| 语言与主题 | 三语、系统主题、编辑器字体与排版参数 | 保留三语和明暗主题；删除编辑器字体设置 | 🔵 核心保留 |
| 桌面 Shell 能力 | 托盘、窗口状态、原生菜单、右键菜单、全屏、日志目录 | 全部移除，遵循系统导航与生命周期 | ⚫ 首版移除 |

### 数据、安全与分发

| 功能 | Desktop 当前形态 | 独立 Mobile v1 | 决策 |
| --- | --- | --- | --- |
| 业务通信层 | loopback REST、SSE、WebSocket、启动 token | 全部删除；UI 直接调用进程内 repository/engine | ⚫ 首版移除 |
| 持久化 | SQLite 运行数据 + YAML 配置 + 附件目录 | 版本化 JSON + FileSystem 附件 + 原子替换 | 🟠 移动改造 |
| Provider Key | 本地 Provider profile 配置 | 用户 BYOK，Keychain/Keystore SecureStore | 🔵 核心保留 |
| 本地优先 | daemon/store 是事实源 | 设备文件是事实源；仅模型请求离开设备 | 🔵 核心保留 |
| 第三方 AI 同意 | 依赖用户配置 Provider 的隐含选择 | 首次请求前明确披露发送内容、Provider 与撤回入口 | 🟣 移动新增 |
| 认证与配对 | daemon bearer token、移动二维码配对与 device token | 无 daemon 认证；可选 Face ID 保护 Key/打开 App | ⚫ 首版移除 |
| 更新机制 | electron-updater、release/preview channel | App Store 二进制更新；兼容改动可用 EAS Update | 🟠 移动改造 |
| 发布渠道 | DMG/ZIP、签名、公证和更新源 | TestFlight → App Store；Android 后续复用同一代码库 | 🟠 移动改造 |

## Desktop 与 Mobile 优劣势

### Desktop

优势：

- 支持 Project、文件系统、Git、LSP、终端和命令执行，适合完整 Code Agent。
- daemon 可以稳定承载后台任务、长时间生成和多会话资源。
- 大屏更适合复杂 Canvas、自由窗口和多栏工作流。
- MCP、Apps、Skills 和浏览器自动化扩展能力更强。

劣势：

- Electron + Go daemon + SQLite 架构较重，安装、更新和跨平台维护成本高。
- 权限面和安全边界更复杂。
- 不适合拍照、移动查看和随手对话等手机场景。

### Mobile

优势：

- 单体 App，架构、安装和分发更简单。
- 适合随手对话、图片/文件输入、Canvas 查看和系统分享。
- iOS 与 Android 可以复用 React Native 代码。
- BYOK 不需要平台承担模型费用，也不需要云端 API 网关保存统一密钥。

劣势：

- BYOK 增加首次配置门槛。
- 移动系统限制后台运行，生成过程更容易受锁屏、切后台和网络切换影响。
- 无法替代 Desktop 的 Code、终端、Git、MCP 和本地自动化能力。
- 小屏需要把自由 Canvas 改造成卡片流或单项全屏。
- iOS 上线还需处理隐私披露、第三方 AI 数据发送同意和审核 Demo。

## 推荐技术形态

```text
Expo / React Native UI
        │
        ├── TypeScript AgentEngine
        ├── Provider Adapter（用户 BYOK）
        ├── Canvas 内置工具
        ├── JSON FileSystem（会话与 Canvas）
        └── SecureStore（Provider Key）
```

Mobile v1 不包含 Go daemon、本地 HTTP 服务、REST、SSE、WebSocket runtime bridge 和 SQLite。

## 最终建议

先交付 **Demo 会话 + BYOK 对话 + Canvas + 本地 JSON + 图片/文件附件 + 系统分享**。如果未来要让手机具备完整 Code Agent 能力，应另行设计“连接 Desktop 或云端 runtime”的模式，而不是继续扩张独立 Mobile v1。
