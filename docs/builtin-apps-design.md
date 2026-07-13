# 内置 App 设计

状态: 已实施

## 目标

Pudding 使用统一的 **App** 概念承载需要说明、工具、界面和运行状态共同协作的能力。

- 内置 App 与用户安装 App 在同一个 Apps 页面管理。
- Chat、Work、Code 都能看到精简的 App 索引。
- App 的详细说明按需读取，不常驻系统提示词。
- App 工具只在 App 已加载、已启用且当前模式足够时进入 provider 的 `tools`。
- 内置与安装 App 都可以临时关闭；内置 App 不可卸载。
- 不保留 App Toolkit 与新 App 加载机制的双轨兼容代码。

首批内置 App:

| App | 最低模式 | 边界 |
| --- | --- | --- |
| Browser | Work | 浏览器标签页、页面状态和页面交互工具 |
| Terminal | Code | 交互终端、后台进程、日志和进程控制 |
| Canvas | Chat | 由已连接 Desktop 动态提供的画布组件与可视化工具 |

普通一次性命令 `builtin_command_run` 仍属于 Code Core，不依赖 Terminal App。

## 概念

### enabled

用户级开关。关闭后:

- 不出现在模型可见的 App 索引中。
- 不向 provider 提供工具 schema。
- 拒绝新的 App 工具调用。
- 不强制终止已经启动的浏览器标签页、终端或后台进程。

重新启用后可以继续使用既有资源。

### loadedAppIDs

session 级技术状态，表示模型已经显式加载该 App，可以在后续 turn 复用其工具。

- `builtin_app_load` 成功返回 Skill 并完成状态写入后加入 `loadedAppIDs`。
- 它不是 UI 激活状态，不打开窗口，也不授予权限。
- 它不把工具写入系统提示词，只控制 `provider.Request.Tools`。
- 降级模式不清除该状态；工具因模式不足而暂时隐藏。
- session 恢复时一并恢复，避免每轮重复加载 App。

最终工具可见条件:

```text
app.enabled
&& session.loadedAppIDs contains app.id
&& currentMode >= app.requiredMode
&& tool is declared by app runtime
&& (app.runtime is empty || its provider for the current turn is connected)
```

### source

App 定义仍使用两种产品来源:

- `builtin`: 由代码中的 registry 提供，不允许卸载。
- `installed`: 来自 `<home>/apps/<id>`，允许卸载。

`builtin` 可以由 daemon 常驻提供，也可以由 Desktop、React Native 等客户端运行时动态注册。动态内置 App 额外带 `runtime`，断开后从该运行时的 App 列表和模型上下文撤下。安装包不能声明或冒充内置 runtime。

### Runtime-provided App

Canvas 属于 Runtime-provided App。工具实现、UI 状态和渲染继续归客户端所有，daemon 不实现 Canvas 业务，也不保存窗口焦点。

客户端通过 UI MCP 连接注册:

- 稳定到当前客户端窗口生命周期的 `runtimeID` 与 `runtime` 类型。
- App manifest、默认 Skill 元数据及正文。
- 带 `appID` 的工具定义。

HTTP 请求通过 `X-Pudding-Runtime-ID` 显式声明来源。daemon 仅把该身份放进本次请求或 turn 的 context，用它解析 App 索引、工具 schema 和工具调用目标；不保存全局 current runtime，也不根据“最后连接窗口”猜测目标。

```text
Desktop / React Native
  ├─ App manifest + Skill
  ├─ tool definitions + handlers
  └─ local UI state
             ↕ runtimeID-scoped MCP
daemon
  └─ ephemeral registry and turn-scoped call routing
```

同一协议可供未来 React Native UI App 使用。没有来源 runtime 的语音或后台 turn 不获得任何客户端 UI 工具。

## App 定义

统一 API 返回的 App 视图增加:

```json
{
  "id": "browser",
  "source": "builtin",
  "runtime": "desktop",
  "enabled": true,
  "canUninstall": false,
  "requiredMode": "work",
  "defaultSkillID": "browser",
  "tools": [
    { "name": "builtin_browser_open" }
  ]
}
```

模型可调用的 App 必须有 `defaultSkillID`，并且该 Skill 必须存在。常驻内置 App 由 daemon registry 提供；动态内置 App 由对应客户端 runtime 提供；安装 App 继续从本地包读取。

`tools` 是管理界面的只读能力清单。内置 App 显式声明工具；安装 App 的 REST / GraphQL 工具由 endpoint 推导，MCP 工具由运行时探测补充。该字段不改变实际工具路由和权限判定。

## 提示词与加载流程

所有模式都包含精简的 `Available Apps` 索引，每个 App 只展示:

- ID、名称和一句描述。
- 最低模式。
- 默认 Skill ID 与简短说明。

不内联 endpoint、完整 Skill 正文或工具列表。

典型流程:

1. Chat 看到 `Browser (requires Work)`，需要浏览器时先请求 Work。
2. 模式批准后调用 `builtin_app_load(app_id="browser")`。
3. App Load 返回默认 Skill，并将 `browser` 写入当前 session 的 `loadedAppIDs`。
4. 下一次 provider request 加入 Browser 工具 schema。
5. 后续 turn 直接复用，不再要求重复加载。

`builtin_skill_read` 只读取 Available Skills，不接受 `app_id`，也不修改 App 状态。不支持“模型直接调用未加载工具时由引擎暗中加载”；未加载调用必须返回明确的 `app_not_loaded`。

## 权限与资源生命周期

App 加载、能力模式和操作审批相互独立:

- App 加载决定工具 schema 是否可见。
- Chat / Work / Code 决定能力上限。
- 项目审批策略决定具体危险操作是否需要询问。

权限撤销、模式降级、项目切换或 App 关闭都不终止已经批准并启动的进程。资源由 daemon 管理，session 只持有显式引用。

## 存储与配置

- 内置 App registry: `internal/app` 代码定义。
- 安装 App: `<home>/apps/<id>`。
- App 启用覆盖: `<home>/config/settings.yaml`。
- session `loadedAppIDs`: SQLite session 正式 schema。
- Runtime App 注册: 仅存在于当前 UI MCP 连接和 turn context，不持久化。

SQLite schema v1 已随正式签名的 `0.1.1` 固化。后续调整 `loadedAppIDs` 或其他持久化字段时必须增加逐版本迁移；不通过双写或旧字段别名维持兼容。

## API

保留统一入口:

- `GET /apps`: 合并常驻内置、当前请求 runtime 提供和安装 App。
- `PUT /apps/{id}/enabled`: 切换启用状态。
- `DELETE /apps/{id}`: 仅安装 App 可用；内置 App 返回不可卸载。
- `GET /app-skills/{appID}/{skillID}`: 统一读取内置或安装 Skill。

所有 session 运行态操作继续显式携带 `sessionID`。App 列表和用户级启用配置不属于 session 运行态。

## 管理界面

Apps 页面按以下区域展示:

1. 内置: Browser、Terminal 等，提供启用开关，不显示卸载。
2. 已安装: 用户安装 App，可临时关闭、管理连接和卸载。
3. App Hub: 可安装内容。

内置与已安装列表使用同一套横排图标入口，只展示启用状态；开关操作放在详情页。详情页独立展示 Tools；没有 endpoint 时不显示 Endpoints 区块。

App-owned tools 只在 Apps 详情中展示，不进入“设置 → 内置工具”；后者仅列出 Core 与 Toolkit 管理的工具。

`loadedAppIDs` 不作为用户设置展示。画布、浏览器标签和终端窗口继续使用现有 UI，只改变能力归属与加载入口。

## 切换顺序

1. 已建立 registry、启用配置和统一 App API。
2. 已增加 session `loadedAppIDs`、全模式 App 索引和 schema 路由。
3. Browser 已完成端到端路径。
4. Apps 页面已支持内置 App。
5. Terminal 与后台进程已迁移。
6. 安装 App 的 MCP/API 工具已迁移到同一加载路径。
7. App 专用 Toolkit 路径已删除。
8. Canvas 已迁移为 Desktop 动态注册的 Runtime-provided App。

Code 专用的 Git、LSP、文件等低频 Toolkit 暂时保留。

## 验收

- Chat 能看到已启用 App 及其最低模式，但看不到其工具 schema。
- `builtin_app_load` 成功后，同一 session 后续 turn 自动复用 App 工具。
- 模式降级隐藏工具但不清除加载状态或终止资源。
- 关闭 App 后不再出现索引、schema 或新调用。
- Browser 已有标签页可恢复对应加载状态。
- Canvas 只对发起 turn 的已连接 Desktop runtime 可见，两个窗口之间不会串发工具调用。
- Desktop 断开后 Canvas 工具立即撤下，重连后保留原 session 加载状态并恢复。
- 对话中不再出现先调用 Browser 再报 `tool_not_loaded` 的红色错误。
- Go 全量测试、Web 构建和 Desktop 构建通过。
