# Pudding Chat / Work / Code 模式设计

> 状态:已完成
> 决策日期:2026-07-12

## 1. 目标

Pudding 使用三档内部能力模式:

```text
chat < work < code
```

模式表达 agent 当前可使用的能力,Project 表达本地目录、审批设置和会话归属。
两者不再复用同一个名称:

- `ModeChat = "chat"`
- `ModeWork = "work"`
- `ModeCode = "code"`
- `Session.projectID`、`Project.rootDirs`、`Project.approvalMode` 与工具
  `scope:"project"` 保持不变。

## 2. 产品语义

| 模式 | 定位 | 能力 |
| --- | --- | --- |
| Chat | 查询与观察 | 对话、历史、附件、Skills、Web Search/Fetch、Weather、Desktop Screenshot、Canvas/UI |
| Work | 操作外部系统 | Chat + 完整 Browser、Apps/MCP、REST/GraphQL、Camera |
| Code | 操作本地项目 | Work + Project、File、Command、Git、LSP、Patch、Skill Draft |

Browser 是不可拆分的能力包。以下工具必须全部属于 Work:

- status / open / observe / screenshot
- back / forward / reload / close
- click / type / scroll

`DesktopScreenshot` 属于 Chat;`BrowserScreenshot` 跟随 Browser 整组属于 Work。

## 3. 工具归属

### 3.1 Chat

- `request_capability`
- `builtin_time_get_current`
- `builtin_web_search`
- `builtin_web_fetch`
- `builtin_history_search`
- `builtin_history_get_message`
- `builtin_skill_read`
- `builtin_attachment_read_image`
- `builtin_weather_get`
- `builtin_desktop_screenshot`
- Browser MCP 提供的 Canvas / UI 工具

### 3.2 Work

- `builtin_rest_request`
- `builtin_graphql_request`
- `builtin_graphql_introspect`
- `builtin_graphql_search`
- `builtin_camera_capture`
- 全部 `builtin_browser_*`
- 已安装 App 的 MCP 工具

### 3.3 Code

- `builtin_project_*`
- `builtin_code_*`
- `builtin_file_*`（附件读取除外）
- `builtin_command_run`
- `builtin_git_*`
- `builtin_patch_*`
- `builtin_skill_validate`
- `builtin_skill_submit`

工具可见性按最低模式累积。Code 因此继承 Work 与 Chat;工具名称收敛是后续独立工作。

## 4. 能力提升

`request_capability.targetMode` 只接受:

```text
work | code
```

- Chat 可请求 Work 或 Code。
- Work 可请求 Code。
- 模型不能请求降级。
- Code 可再次请求 Code,但只用于追加 Project 目录授权。
- Work 不接受 `projectDirs`。
- Code 没有持久 Project 或当前 turn grant 时必须选择目录。
- turn scope 只影响当前 turn;session scope 更新 `Session.activeMode`。

普通新会话默认 Chat。由 Project 创建的会话默认 Code。Project 绑定不因切换到
Chat 或 Work 而删除。

## 5. Composer

Composer 只显示当前 session 持久模式的弱提示图标,不提供点击操作:

- Chat:`MessageCircle`
- Work:`Briefcase`
- Code:`Code2`

图标尺寸 12-14px,无按钮容器、背景、边框、hover、tooltip 或 focus,使用低对比度
颜色并禁用 pointer events。turn scope 临时能力不写入 session,因此不改变该提示。

## 6. Prompt

- 增加 `mode_work.md`。
- Code prompt 固定使用 `mode_code.md`,不保留其他名称分支。
- Chat prompt 说明查询、观察能力。
- Work prompt 增加 Browser 与外部系统操作规则。
- Code prompt 增加 Project 本地开发规则。
- Installed Apps 索引只在 Work / Code prompt 中出现;Chat 不暴露不可调用的 App 工具。

## 7. 一次性数据迁移

项目尚未发布,不在运行代码中保留 migration 或兼容别名。本机开发数据库直接执行
一次性迁移:

```text
旧 chat      -> work
旧 project   -> code
旧 workspace -> code
```

迁移覆盖 `sessions.active_mode`、`turns.mode`、`queued_inputs.mode` 与历史
`request_capability` 消息。开发库同时删除 `workspace_dirs`、`projects.temporary`、
queued input 旧附件列和已废弃空表。迁移完成后删除所有启动迁移函数;Normalize、API
schema、prompt 和工具协议均不接受 `workspace` 或 `project` mode。

新数据库继续以 `chat` 为默认值。迁移命令与兼容别名不进入仓库;FTS 等当前 schema
初始化不承担旧结构升级。

## 8. 验收标准

- Store、API 与前端只接受 `chat | work | code`。
- Chat 中没有 Browser、Apps/MCP、REST/GraphQL 或 Camera。
- Work 中完整 Browser 工具组可用。
- Code 中所有 Project 工具可用,且必须显式携带 Project scope/sessionID。
- Project 创建会话默认 Code,普通会话默认 Chat。
- Work/Code capability approval 的 turn/session scope 正确。
- Composer 图标不可点击且不产生 hover/focus 现象。
- 本机数据迁移完成,代码中不存在旧 mode migration 或兼容分支。
- 全量 Go 测试和 Web build 通过。
