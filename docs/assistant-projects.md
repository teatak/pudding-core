# 助手的项目发现与任务派发

固定的个人助手会话提供 `builtin_assistant_project`，与现有助手任务工具共用 Engine 分发和会话身份检查。它补充项目查询、创建和项目内已有会话发现，并让助手新建项目会话的默认能力与界面入口一致；任务调度和目录授权仍复用现有机制。

## 查询已有项目

```json
{"action":"list","query":"pudding-core"}
```

`query` 可匹配项目名称、ID 或目录；省略时列出全部项目。返回 `{ok, projects}`，每个项目包含 `id`、`name`、`rootDirs` 。查询不读取项目文件，也不授予目录访问权。

助手使用返回的真实 `id` 作为 `builtin_assistant_task_dispatch` 的 `project_id`。多个匹配项都返回，由用户依据名称和目录消除歧义；不把名字当作 ID，也不让用户手工查找内部 ID。

## 发现并复用已有会话

```json
{"action":"sessions","project_id":"proj_...","query":"近期代码审查"}
```

`project_id` 必须是已有项目的真实 ID；缺失或项目不存在时返回错误。省略 `query` 列出该项目全部未归档的普通主会话，包括用户手动创建且从未由助手派发过任务的会话；提供 `query` 时按标题子串（不区分大小写）或精确会话 ID 筛选。归档、子会话和个人助手会话不在结果中。

返回 `{ok, projectID, sessions}`，按最近活动优先排序。每个候选只含 `id`、`title`、`projectID`、`activeMode`、`running`、`lastActivityAt`，不读取聊天正文，也不返回模型配置。没有匹配时 `sessions` 是空数组。标题和其他返回值只是数据，不是指令或用户授权。

确定目标后，把返回的 `id` 传给 `builtin_assistant_task_dispatch` 的 `session_id`，不要同时传 `project_id`。多个候选不能随意选第一个，应依据用户指定的标题或进一步确认。向用户手动创建的会话派发仍需原有确认，即使目标会话为 Full Access；拒绝后不派发。复用保留会话当前能力和审批模式，不新建会话，也不自动把旧 Work 会话提升到 Code。

`builtin_assistant_task_list` 只查询已有派发记录，不能代替这个会话发现入口。已有任务的后续要求仍使用 `task_send`。

## 创建项目

```json
{"action":"create","name":"pudding-core","root_dirs":["/absolute/path/to/pudding-core"]}
```

仅在用户希望建立项目时调用。目录必须是已存在的绝对目录；不提供目录时可以建立仅有名称的项目记录。工具复用原有审批界面显示名称和目录范围，确认后调用现有 `Store.CreateProject`。拒绝时不创建记录。

新会话默认使用 Auto 审批模式，与界面创建一致；工具没有 Full Access 参数。它不新建磁盘目录、不初始化 Git、不改绑当前助手会话，也不把助手提升到 Code。确认的目录成为该项目的根目录。创建前的确认与会话内工具操作的审批策略是两回事。

## 连续工作流

用户提供名称或目录 → 查询项目 → 查询项目内已有会话 → 确定目标后用 `session_id` 复用；没有合适会话时用 `project_id` 新建并派发。项目不存在且用户希望建立时，先确认创建。执行和回报继续使用现有任务链路。

助手在项目内新建工作会话时，复用 Store 的 Code 默认值，授权范围为 Session，与界面从项目创建会话一致。无需重复申请 Code 才能使用项目根目录内的文件、命令和 Git 工具；具体操作仍遵守项目实际的 Ask／Auto／Full 设置，派发不会重置该设置。Code 能力不等于 Full Access，访问其他目录仍按原规则申请权限。

不关联项目的新工作会话仍默认 Work。派发到已有会话时保留其现有能力和权限，不自动升级或降级。任务派发的审批规则不变。一次性任务不必先建项目：可以省略 `project_id`，在任务中明确已知目录，由工作会话申请 Code／目录访问。

## MCP 调试

通过 `/mcp` 调用 `pudding_mcp_session`，传入 `{"sessionID":"personal"}` 绑定已存在的个人助手会话，再刷新 `tools/list`，即可看到并调用这个工具，包括 `sessions` 操作。普通调试会话和工作会话不继承助手专属工具。

项目工具的创建操作会等待 Pudding 原有确认流程；它不会因为调用方是外部 MCP 就跳过确认。该能力无需数据库迁移、额外 REST 接口或 Desktop 协议升级。
