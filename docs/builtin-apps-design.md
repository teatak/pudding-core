# 内置 App 设计

状态: 已实施

## 目标

Pudding 使用统一的 **App** 概念承载需要说明、工具、界面和运行状态共同协作的能力。

- 内置 App 与用户安装 App 在同一个 Apps 页面管理。
- Chat、Work、Code 都能看到精简的 App 索引。
- App 的详细说明按需读取，不常驻系统提示词。
- App 工具只在 App 已加载、已启用且当前模式足够时进入 provider 的 `tools`。
- 内置 App 不可卸载，但可以关闭。
- 不保留 App Toolkit 与新 App 加载机制的双轨兼容代码。

首批内置 App:

| App | 最低模式 | 边界 |
| --- | --- | --- |
| Browser | Work | 浏览器标签页、页面状态、浏览器工具和画布入口 |
| Terminal | Code | 交互终端、后台进程、日志和进程控制 |

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

session 级技术状态，表示模型已经读取过该 App 的使用说明，可以在后续 turn 复用其工具。

- 读取 App 的默认 Skill 成功后加入 `loadedAppIDs`。
- 它不是 UI 激活状态，不打开窗口，也不授予权限。
- 它不把工具写入系统提示词，只控制 `provider.Request.Tools`。
- 降级模式不清除该状态；工具因模式不足而暂时隐藏。
- session 恢复时一并恢复，避免每轮重复读取 Skill。

最终工具可见条件:

```text
app.enabled
&& session.loadedAppIDs contains app.id
&& currentMode >= app.requiredMode
&& tool is declared by app runtime
```

### source

App 定义来源只有两种:

- `builtin`: 由代码中的 registry 提供，不允许卸载。
- `installed`: 来自 `<home>/apps/<id>`，允许卸载。

内置 runtime 绑定只存在于代码中，安装包不能声明或冒充内置 runtime。

## App 定义

统一 API 返回的 App 视图增加:

```json
{
  "id": "browser",
  "source": "builtin",
  "enabled": true,
  "canUninstall": false,
  "requiredMode": "work",
  "defaultSkillID": "browser"
}
```

模型可调用的 App 必须有 `defaultSkillID`，并且该 Skill 必须存在。内置 App 的定义与 Skill 内容由 registry 提供；安装 App 继续从本地包读取。

## 提示词与加载流程

所有模式都包含精简的 `Available Apps` 索引，每个 App 只展示:

- ID、名称和一句描述。
- 最低模式。
- 默认 Skill ID 与简短说明。

不内联 endpoint、完整 Skill 正文或工具列表。

典型流程:

1. Chat 看到 `Browser (requires Work)`，需要浏览器时先请求 Work。
2. 模式批准后调用 `builtin_skill_read(app_id="browser", skill_id="browser")`。
3. Skill 读取成功，engine 将 `browser` 写入当前 session 的 `loadedAppIDs`。
4. 下一次 provider request 加入 Browser 工具 schema。
5. 后续 turn 直接复用，不再要求重复加载。

不支持“模型直接调用未加载工具时由引擎暗中加载”。未加载调用必须返回明确的 `app_not_loaded`，避免把错误调用变成隐式热加载。

## 权限与资源生命周期

App 加载、能力模式和操作审批相互独立:

- App 加载决定工具 schema 是否可见。
- Chat / Work / Code 决定能力上限。
- 项目审批策略决定具体危险操作是否需要询问。

权限撤销、模式降级、项目切换或 App 关闭都不终止已经批准并启动的进程。资源由 daemon 管理，session 只持有显式引用。

## 存储与配置

- 内置 App registry: `internal/app` 代码定义。
- 安装 App: `<home>/apps/<id>`。
- 内置 App 启用覆盖: `<home>/config/settings.yaml`。
- session `loadedAppIDs`: SQLite session 正式 schema。

首个版本尚未发布，schema 直接采用最终结构；不添加运行时迁移、旧字段兼容或双写逻辑。开发数据需要时一次性重建。

## API

保留统一入口:

- `GET /apps`: 合并内置和安装 App。
- `PUT /apps/{id}/enabled`: 切换启用状态。
- `DELETE /apps/{id}`: 仅安装 App 可用；内置 App 返回不可卸载。
- `GET /app-skills/{appID}/{skillID}`: 统一读取内置或安装 Skill。

所有 session 运行态操作继续显式携带 `sessionID`。App 列表和用户级启用配置不属于 session 运行态。

## 管理界面

Apps 页面按以下区域展示:

1. 内置: Browser、Terminal 等，提供启用开关，不显示卸载。
2. 已安装: 用户安装 App，可管理连接和卸载。
3. App Hub: 可安装内容。

`loadedAppIDs` 不作为用户设置展示。画布、浏览器标签和终端窗口继续使用现有 UI，只改变能力归属与加载入口。

## 切换顺序

1. 已建立 registry、启用配置和统一 App API。
2. 已增加 session `loadedAppIDs`、全模式 App 索引和 schema 路由。
3. Browser 已完成端到端路径。
4. Apps 页面已支持内置 App。
5. Terminal 与后台进程已迁移。
6. 安装 App 的 MCP/API 工具已迁移到同一加载路径。
7. App 专用 Toolkit 路径已删除。

Code 专用的 Git、LSP、文件等低频 Toolkit 暂时保留。

## 验收

- Chat 能看到已启用 App 及其最低模式，但看不到其工具 schema。
- Skill 读取后，同一 session 后续 turn 自动复用 App 工具。
- 模式降级隐藏工具但不清除加载状态或终止资源。
- 关闭 App 后不再出现索引、schema 或新调用。
- Browser 已有标签页可恢复对应加载状态。
- 对话中不再出现先调用 Browser 再报 `tool_not_loaded` 的红色错误。
- Go 全量测试、Web 构建和 Desktop 构建通过。
