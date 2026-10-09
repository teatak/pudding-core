# 内置插件设计

状态: 已实施

## 目标

Pudding 使用统一的**插件**概念承载需要说明、工具、界面和运行状态共同协作的能力。

- 内置插件与用户安装插件在同一个插件页面管理。
- Chat、Work、Code 都能看到精简的插件索引。
- 插件的详细说明按需读取，不常驻系统提示词。
- 插件工具只在插件已加载、已启用且当前模式足够时进入 provider 的 `tools`。
- 内置与安装插件都可以临时关闭；内置插件不可卸载。
- 不保留旧的 turn 级可选工具加载机制与插件加载机制的双轨兼容代码。

当前内置插件:

| 插件 | 最低模式 | 边界 |
| --- | --- | --- |
| Browser | Work | 浏览器标签页、页面状态和页面交互工具 |
| Subagents（子任务调度） | Work | 主会话派发、补充、等待和停止子任务；关闭后已接受任务继续收尾，结果回收不依赖工具开关 |
| Computer Use | Work | 本机应用的观察与操作 |
| Image Capture | Chat | 用户明确要求时从本地屏幕或相机采集图像 |
| Studio | Chat | 列出和打开内容，创建、读取与修改原生文档；Core 执行，定时任务和子会话无需 Desktop |
| Skill Authoring | Code | Skill 校验与创作说明 |
| Plugin Authoring | Code | 插件校验、保存与创作说明 |
| Widget Authoring（小组件创作） | Code | 由已连接 Desktop 动态提供的小组件源码编辑、提交、预览、检查与启用工具 |

文件、Git 与代码导航工具属于 Code Core，不再作为插件加载。

`builtin_command_run` 与 `builtin_command_session` 都属于 Code Core。前者执行一次性命令
或启动后台会话,后者负责读取、输入和停止,不再通过 Terminal 插件动态加载。

## 概念

### enabled

用户级开关。关闭后:

- 不出现在模型可见的插件索引中。
- 不向 provider 提供工具 schema。
- 拒绝新的插件工具调用。
- 不强制终止已经启动的浏览器标签页或 Studio 内容。

重新启用后可以继续使用既有资源。

### 本机实现可用性

本机工具是否存在，以 daemon 实际装配的 runner 为准。插件目录与 runner 的工具定义共用
`BuiltinRunner.ToolAvailable`，不保存另一套平台能力开关。过滤发生在用户启用覆盖之前：

- 未提供 Computer Use bridge 时，不列出 Computer Use 插件、工具或可加载 Skill。
- 非 macOS 不装配相机实现，Image Capture 只保留已装配的桌面截图工具。
- 某个内置插件的工具全部不可用时，隐藏整个插件；只缺少部分工具时保留其余工具。
- 用户原有启用偏好不被改写，也不能使缺少实现的工具重新出现。

这里的可用性表示实现已装配，不代表屏幕、相机等操作已经获得系统权限或通过实机验收。

### loadedPluginIDs

session 级技术状态，表示模型已经显式加载该插件，可以在后续 turn 复用其工具。

- `builtin_plugin_load` 校验选中的 Skill 并完成状态写入后加入 `loadedPluginIDs`；工具结果持久化 Skill 引用，不保存正文副本。
- 它不是 UI 激活状态，不打开窗口，也不授予权限。
- 它不把工具写入系统提示词，只控制 `provider.Request.Tools`。
- 降级模式不清除该状态；工具因模式不足而暂时隐藏。
- session 恢复时一并恢复，避免每轮重复加载插件。

最终工具可见条件:

```text
plugin.enabled
&& session.loadedPluginIDs contains plugin.id
&& currentMode >= plugin.requiredMode
&& tool implementation is available
&& tool is declared by plugin runtime
&& (plugin.runtime is empty || its provider for the current turn is connected)
```

### source

插件定义仍使用两种产品来源:

- `builtin`: 由代码中的 registry 提供，不允许卸载。
- `installed`: 来自 `<home>/plugins/<id>`，允许卸载。

`builtin` 可以由 daemon 常驻提供，也可以由 Electron Desktop 客户端运行时动态注册。动态内置插件额外带 `runtime`，断开后从该运行时的插件列表和模型上下文撤下。安装包不能声明或冒充内置 runtime。

### Runtime-provided 插件

小组件创作属于 Runtime-provided 插件。工具实现、UI 状态和渲染继续归客户端所有，daemon 不实现小组件构建与渲染，也不保存窗口焦点。

客户端通过 UI MCP 连接注册:

- 稳定到当前客户端窗口生命周期的 `runtimeID` 与 `runtime` 类型。
- 插件manifest、默认 Skill 元数据及正文。
- 带 `pluginID` 的工具定义。

HTTP 请求通过 `X-Pudding-Runtime-ID` 显式声明来源。daemon 仅把该身份放进本次请求或 turn 的 context，用它解析插件索引、工具 schema 和工具调用目标；不保存全局 current runtime，也不根据“最后连接窗口”猜测目标。

```text
Electron Desktop
  ├─ 插件manifest + Skill
  ├─ tool definitions + handlers
  └─ local UI state
             ↕ runtimeID-scoped MCP
daemon
  └─ ephemeral registry and turn-scoped call routing
```

协议本身不依赖小组件的具体 UI 实现,但当前产品只注册 Electron Desktop runtime。没有来源 runtime 的语音或后台 turn 不获得任何客户端 UI 工具。

## 插件定义

统一 API 返回的插件视图增加:

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

带使用说明的插件必须让 `defaultSkillID` 指向已存在的 Skill。纯工具插件可以没有默认
Skill，加载结果会明确返回 `instructionsLoaded: false`。常驻内置插件由 daemon registry
提供；动态内置插件由对应客户端 runtime 提供；安装插件继续从本地包读取。

`tools` 是管理界面的只读能力清单。内置插件显式声明工具；安装插件的 REST / GraphQL 工具由 endpoint 推导，MCP 工具由运行时探测补充。该字段不改变实际工具路由和权限判定。

## 提示词与加载流程

所有模式都包含精简的 `Available Plugins` 索引，每个插件只展示:

- ID、名称和一句描述。
- 最低模式。
- 默认 Skill ID 与简短说明。

不内联 endpoint、完整 Skill 正文或工具列表。

典型流程:

1. Chat 看到 `Browser (requires Work)`，需要浏览器时先请求 Work。
2. 模式批准后调用 `builtin_plugin_load(plugin_id="browser")`。
3. Plugin Load 返回默认 Skill 的引用，并将 `browser` 写入当前 session 的 `loadedPluginIDs`。
4. 下一次 provider request 加入 Browser 工具 schema，并在对应工具结果位置解析引用、提供当前 Skill 正文。
5. 后续 turn 直接复用，不再要求重复加载。

`builtin_skill_read` 只读取 Available Skills，不接受 `plugin_id`，也不修改插件状态。不支持“模型直接调用未加载工具时由引擎暗中加载”；未加载调用必须返回明确的 `app_not_loaded`。

### 引用型技能结果

- `builtin_plugin_load` 的默认/指定 Skill 与 `builtin_skill_read` 的全局 Skill 统一返回 `reference`（`kind`、`pluginID`（仅插件）、`skillID`）。标识只通过注册的插件/Skill service 解析，不能指定任意文件。
- canonical `tool_result.content` 只保存引用和加载元数据。请求发送前，由 contextbuilder 对已经合并历史和本轮工具结果的请求解析当前正文；不放进系统提示词，不写入 provider 内存或其他独立事实源。
- 同一插件最新一次成功的技能加载是当前选择；全局 Skill 按 ID 保留最新引用。旧引用只保留记录，标为 `superseded`。插件卸载后不解析其正文；禁用、runtime 缺失、能力不足或读取失败均不回退旧正文。
- 老会话已有的结构化加载结果，在请求投影时根据其注册标识解析；原始记录不改写。压缩不固化技能正文，后续请求从 canonical messages 找回被压缩的最新引用，只保留原调用/结果对，不恢复周边旧对话。
- `instructionStatus=current` 表示当前解析结果；`unavailable` 附带读取错误；`unloaded`、`capability_required`、`superseded` 不携带正文。历史摘要中的旧操作规则不覆盖当前引用正文。
- 引用存放在已有 JSON 工具结果内，不新增 session 选择副本或 SQLite 列。技能内容不变时请求投影保持一致；正文更新会影响该工具结果位置及后续上下文的前缀缓存。

## 权限与资源生命周期

插件加载、能力模式和操作审批相互独立:

- 插件加载决定工具 schema 是否可见。
- Chat / Work / Code 决定能力上限。
- 项目审批策略决定具体危险操作是否需要询问。

权限撤销、模式降级、项目切换或插件关闭都不终止已经批准并启动的进程。资源由 daemon 管理，session 只持有显式引用。

## 存储与配置

- 内置插件registry: `internal/plugin` 代码定义。
- 安装插件: `<home>/plugins/<id>`。
- 插件启用覆盖: `<home>/config/settings.yaml`。
- session `loadedPluginIDs`: SQLite session 正式 schema。
- Runtime 插件注册: 仅存在于当前 UI MCP 连接和 turn context，不持久化。

SQLite schema v1 已随正式签名的 `0.1.1` 固化。后续调整 `loadedPluginIDs` 或其他持久化字段时必须增加逐版本迁移；不通过双写或旧字段别名维持兼容。

## API

保留统一入口:

- `GET /plugins`: 合并常驻内置、当前请求 runtime 提供和安装插件。
- `PUT /plugins/{id}/enabled`: 切换启用状态。
- `DELETE /plugins/{id}`: 仅安装插件可用；内置插件返回不可卸载。
- `GET /plugin-skills/{pluginID}/{skillID}`: 统一读取内置或安装 Skill。

所有 session 运行态操作继续显式携带 `sessionID`。插件列表和用户级启用配置不属于 session 运行态。

## 管理界面

插件页面按以下区域展示:

1. 内置: Browser、Computer Use、Skill Authoring、Plugin Authoring、小组件创作等，提供启用开关，不显示卸载。
2. 已安装: 用户安装插件，可临时关闭、管理连接和卸载。
3. Pudding Hub: 可安装内容。

内置与已安装列表使用同一套横排图标入口，只展示启用状态；开关操作放在详情页。详情页独立展示 Tools；没有 endpoint 时不显示 Endpoints 区块。

插件自带的工具只在插件详情中展示，不进入“设置 → 内置工具”；后者仅列出 Core 工具。

`loadedPluginIDs` 不作为用户设置展示。Studio 内容、浏览器标签和终端窗口继续使用现有 UI，只改变能力归属与加载入口。

## 切换顺序

1. 已建立 registry、启用配置和统一插件API。
2. 已增加 session `loadedPluginIDs`、全模式插件索引和 schema 路由。
3. Browser 已完成端到端路径。
4. 插件页面已支持内置插件。
5. 后台进程已并入 Code Core,Terminal 内置插件已删除。
6. 安装插件的 MCP/API 工具已迁移到同一加载路径。
7. 旧的可选工具加载路径已删除。
8. 画布创作（现为小组件创作）已迁移为 Desktop 动态注册的 Runtime-provided 插件。
9. `skill-creator` 与 `plugin-creator` 已归入对应 Authoring 插件；校验与保存工具随
   插件会话级加载。
10. `builtin_request_user_input` 已并入默认 `chat.core`。

11. 图像采集已迁移为 Image Capture 内置插件；Git、LSP 与结构化文件工具后来回到 Code Core。

## 验收

- Chat 能看到已启用插件及其最低模式，但看不到其工具 schema。
- `builtin_plugin_load` 成功后，同一 session 后续 turn 自动复用插件工具。
- 模式降级隐藏工具但不清除加载状态或终止资源。
- 关闭插件后不再出现索引、schema 或新调用。
- Browser 已有标签页可恢复对应加载状态。
- 小组件创作只对发起 turn 的已连接 Desktop runtime 可见，两个窗口之间不会串发工具调用。
- Desktop 断开后小组件创作工具立即撤下，重连后保留原 session 加载状态并恢复。
- 对话中不再出现先调用 Browser 再报 `tool_not_loaded` 的红色错误。
- Go 全量测试、Web 构建和 Desktop 构建通过。
