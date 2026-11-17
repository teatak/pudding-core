# 助手的项目发现与任务派发

固定的个人助手会话提供 `builtin_assistant_project`，与现有助手任务工具共用 Engine 分发和会话身份检查。它只补充项目查询与创建，不修改任务调度、目录授权或工作会话的能力规则。

## 查询已有项目

```json
{"action":"list","query":"pudding-core"}
```

`query` 可匹配项目名称、ID 或目录；省略时列出全部项目。返回 `{ok, projects}`，每个项目包含 `id`、`name`、`rootDirs` 和现有审批配置。查询不读取项目文件，也不授予目录访问权。

助手使用返回的真实 `id` 作为 `builtin_assistant_task_dispatch` 的 `project_id`。多个匹配项都返回，由用户依据名称和目录消除歧义；不把名字当作 ID，也不让用户手工查找内部 ID。

## 创建项目

```json
{"action":"create","name":"pudding-core","root_dirs":["/absolute/path/to/pudding-core"]}
```

仅在用户希望建立项目时调用。目录必须是已存在的绝对目录；不提供目录时可以建立仅有名称的项目记录。工具复用原有审批界面显示名称和目录范围，确认后调用现有 `Store.CreateProject`。拒绝时不创建记录。

新项目固定使用 Ask 审批模式；工具没有 Full Access 参数。它不新建磁盘目录、不初始化 Git、不改绑当前助手会话，也不把助手提升到 Code。确认的目录成为该项目的根目录；工作会话仍需按原规则获得 Code 能力后使用文件和命令工具。

## 连续工作流

用户提供名称或目录 → 查询项目 → 必要时确认创建 → 使用返回 ID 派发 → 工作会话申请所需能力 → 执行和回报。

新建的工作会话即使关联项目仍从 Work 开始。已存在的工作会话保持原能力；任务派发的审批规则不变。一次性任务不必先建项目：可以省略 `project_id`，在任务中明确已知目录，由工作会话申请 Code／目录访问。

## MCP 调试

通过 `/mcp` 调用 `pudding_mcp_session`，传入 `{"sessionID":"personal"}` 绑定已存在的个人助手会话，再刷新 `tools/list`，即可看到并调用这个工具。普通调试会话和工作会话不继承助手专属工具。

项目工具的创建操作会等待 Pudding 原有确认流程；它不会因为调用方是外部 MCP 就跳过确认。该能力无需数据库迁移、额外 REST 接口或 Desktop 协议升级。
