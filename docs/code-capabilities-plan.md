# Pudding Code 能力设计与计划

> 状态:C0 Project 收口与 C1 文件写工具审批安全收口已落地;C1.5 命名收口是 C2 前置。
> 目标:在现有 multi-session / workspace tool 架构上,把 Pudding 从"可读写文件"
> 推进到"可信的工程协作 agent"。

## 0. 结论

Code 能力不新增一套 `/code/*` 主路径 API,而是继续走现有 tool loop:

```text
session submit
  -> engine turn loop
  -> project capability approval
  -> internal/tool Runner
  -> canonical messages.parts + turn.tool events
  -> transcript / activity UI
```

原因:

- 当前代码已经有 `session.workspaceDirs`、`activeMode=workspace`、
  `request_capability`、文件工具和 tool part UI;这些能力要收口到 Project。
- code 操作天然属于 session 的上下文与 transport,不是 daemon 全局 focus。
- REST 继续负责 session / approval / snapshot;具体读文件、改文件、跑命令、
  git 操作都应作为 LLM 可调用工具出现。

## 1. 当前地基

已具备:

- Project 已是代码工作区与审批设置的主实体,session 通过 `projectID`
  绑定 Project。
- `ActiveMode=workspace`、`ModeLease=session` 仍由 session 承载。
- `request_capability` 应请求 Project 能力与项目目录授权。
- engine 已有 tool loop、`turn.tool` 事件、tool_result canonical 落库。
- `internal/tool` 已有 workspace 路径解析与沙箱校验。
- 内置文件工具已覆盖 list/read/stat/search/slice/write/patch/delete/move/copy。
- 前端已有 thought/tool/approval overlay,并能合并 `tool_use` + `tool_result`。

主要缺口:

- 没有 command/PTY/process runner。
- 没有 git 专用工具。
- command/git/network 等高风险工具还未接入统一审批策略。
- 没有 patch proposal / diff review / 局部接受能力。
- 工具卡片还没有针对 code/git/command 的专门展示与 i18n 命名。

## 2. 产品目标

MVP 要做到:

- 绑定一个 Project。
- 能安全搜索、阅读、解释代码。
- 能提出并应用小范围代码修改。
- 能运行测试/构建命令并汇报结果。
- 能展示本轮读了什么、改了什么、跑了什么。
- 能通过 cancel 中断模型流、工具执行和长命令。

中期目标:

- diff 预览与用户确认后应用。
- git status/diff/log/stage/commit 的受控工作流。
- 专门的 Code Activity Panel。
- 与 MCP code 工具互通,但不依赖 MCP 才能完成核心本地 code 工作。

非目标:

- 不做后端全局 current workspace / focus。
- 不做旧 Runtime 兼容层。
- 不把 WebSocket 当普通 command/SSE 替代品。
- 不在第一版做完整 IDE。
- 不让模型静默执行危险 git 或 shell 操作。

## 3. 架构原则

### 3.1 Project-owned code root

Project 是代码工作区与审批设置的唯一事实源,取代 `session.workspaceDirs`:

```text
Session.projectID -> Project.rootDirs / approvalMode / code settings
```

Project 承载:

- `rootDirs`
- `approvalMode`
- git root / command policy / 默认测试命令等后续 code 配置

Session 只承载:

- `projectID`
- 当前对话上下文
- provider/model/mode
- turn 状态

不长期保留 `session.workspaceDirs` 与 `Project.rootDirs` 两套事实源。
迁移阶段可以一次性 schema/data migration,但 API 与新代码应只读写 Project。

临时访问也不应退回 raw workspace dirs。若需要"仅本轮访问",使用同一 Project
模型:

- 已存在 Project:创建 turn-scoped project binding。
- 不存在 Project:创建临时 Project,绑定到当前 turn,turn 结束后归档或清理。

tool call 每次通过 `sessionID/turnID -> Project binding -> rootDirs + approvalMode`
解析授权范围。

不新增 daemon 级 `currentWorkspace`。

### 3.2 命名边界

对用户与工具协议来说,应统一使用 Project,不要继续扩散 workspace:

- UI 文案使用"项目 / Project"。
- 数据模型使用 `Project.rootDirs`、`Session.projectID`。
- 文件工具的用户可见 scope 应从 `workspace` 改为 `project`。
- `request_capability.targetMode` 应从 `workspace` 改为 `project`。
- approval payload 里的目录字段应从 `workspaceDirs` 收口到 `rootDirs` 或
  `projectDirs`。

但内部能力档与代码字段名暂时保留 `ModeWorkspace` / `activeMode=workspace` /
`WorkspaceDirs`:

- 它表达的是"会话具备本地代码/文件能力",不是 Project 实体。
- 改名会牵动 session mode、turn mode、模型能力和历史数据,收益低于风险。
- 这不是最终命名,后续必须统一改成 Project/code 语义。

### 3.3 Daemon-owned process

command process 是 daemon-owned resource:

- daemon 负责 spawn、timeout、kill process group。
- session/turn 只拥有调用意图和输出上下文。
- `POST /sessions/{id}/cancel` 必须取消 provider stream 与正在运行的命令。

### 3.4 Canonical 只存结果摘要

canonical messages 继续是 context 唯一事实源,但不要把无限日志塞进去:

- tool_result content 存结构化摘要 + 截断输出。
- 大 stdout/stderr 可保存为 attachment 或 temp artifact。
- token delta / command output delta 不落 canonical。

### 3.5 工具优先,REST 兜底

新增 code 能力优先表现为 tool:

- `builtin_command_run`
- `builtin_git_status`
- `builtin_git_diff`
- `builtin_git_log`
- `builtin_git_stage`
- `builtin_git_commit`

REST 只在需要 UI 决策、审批、快照时补充,不新增无 session scope 的 code API。

## 4. 审批模式

审批首先是 Project 级设置,在 composer 里显示和切换。它类似 Codex 的三档
模式,而不是每个工具都让用户手动配置 allow/ask/deny。

建议暴露三档:

| 模式 | 含义 | 适合场景 |
| --- | --- | --- |
| `ask` 请求批准 | 编辑外部文件、使用网络、执行命令等都先问用户 | 最保守,默认给新用户 |
| `auto` 替我审批 | 低风险操作自动允许,检测到风险时再问用户 | 推荐默认,减少打扰 |
| `full` 完全访问权限 | 不弹审批,允许访问互联网和本机用户可访问的文件 | 高信任本地工作流 |

这三档是产品语义。内部仍需要风险分类,用于 `auto` 模式判断哪些操作要问:

| 内部风险 | 示例 | `ask` | `auto` | `full` |
| --- | --- | --- | --- | --- |
| `read` | file read/search/stat, git status/diff/log | 问 | 放行 | 放行 |
| `write` | file write/patch/move/delete, git stage | 问 | 问 | 放行 |
| `command` | test/build/lint/format/codegen | 问 | 低风险放行,其余问 | 放行 |
| `network` | web fetch, install/download/push | 问 | 问 | 放行 |
| `destructive` | rm, reset, clean,覆盖式 checkout | 问或拒绝 | 问或拒绝 | 放行或强确认 |

`full` 是用户明确选择的高权限模式,不是"最危险的模式不需要设计"。它仍需保留
基础边界:

- daemon 只 bind loopback 且请求带 token。
- 授权仍显式挂在 Project/session binding 上,不是 daemon 全局 focus;`full` 可表示
  "all user files" 这种宽授权,不必逐个 workspace 目录确认。
- 对明显不可恢复操作可保留二次强确认,但这是产品选择,不是普通审批流。

实现方式:

- 在 Project 上增加 `approvalMode = ask | auto | full`。
- composer 展示当前 session 绑定 Project 的审批模式;切换即更新 Project。
- chat 尚未绑定 Project 时,composer 显示"无项目",需要 code 能力时走创建/绑定
  Project 的 approval flow。
- 在 `internal/tool` 增加 `ClassifyToolCall(name,args)`。
- engine 在真正 `Runner.Call` 前用 `project.approvalMode + risk` 决策:
  - allow:直接执行。
  - ask:发 `approval.requested`。
  - deny/strong confirm:返回拒绝或进入二次确认。
- 新增 `ApprovalKindToolCall`,用于展示具体工具、路径、命令和风险原因。

第一阶段至少要把现有文件写工具纳入这套模式,否则 code agent 信任边界太宽。

## 5. Command Runner 设计

### 5.1 Tool 定义

```jsonc
{
  "name": "builtin_command_run",
  "input": {
    "cwd": "relative/or/absolute/path inside authorized Project root",
    "argv": ["go", "test", "./internal/tool"],
    "env": {"KEY": "value"},
    "timeout_ms": 120000,
    "max_output_chars": 20000
  }
}
```

约束:

- 第一版只接受 `argv`,不接受裸 shell 字符串。
- `cwd` 必须解析到当前 Project 授权范围内。
- 默认不继承敏感环境变量;只注入最小环境。
- `timeout_ms` 有上限。
- stdout/stderr 合并记录时保留 channel 标记。
- 超出输出上限返回截断标记。
- cancel 时 kill process group。

### 5.2 Result 形状

```jsonc
{
  "ok": true,
  "cwd": "...",
  "argv": ["go", "test", "./internal/tool"],
  "exitCode": 0,
  "durationMs": 1234,
  "stdout": "...",
  "stderr": "...",
  "truncated": false
}
```

前端可据此展示:

- command line
- 运行中/成功/失败
- exit code
- stdout/stderr
- duration

### 5.3 是否需要 PTY

第一版不需要 PTY:

- 测试、构建、lint 都可以用非交互 `exec.CommandContext`。
- PTY 会带来交互输入、终端尺寸、转义序列、长期进程管理等复杂度。

后续如果要做 interactive terminal,另起 `builtin_terminal_*` 或 WebSocket bridge。

## 6. Git 工具设计

Git 不应只靠 command runner,因为 UI 和权限需要结构化信息。

第一批只读:

- `builtin_git_status`
- `builtin_git_diff`
- `builtin_git_log`

第二批写操作:

- `builtin_git_stage`
- `builtin_git_unstage`
- `builtin_git_commit`
- `builtin_git_branch_create`

暂不开放:

- reset
- clean
- checkout/switch 覆盖工作树
- rebase
- push

写操作必须 approval。commit result 要返回 commit hash、message、files。

## 7. Patch Review 设计

现有 `builtin_file_patch` 适合小范围 exact replacement,但不适合作为长期 code UX。

建议分两层:

### 7.1 MVP: edit 后展示 diff

流程:

```text
agent read/search
  -> file_patch/file_write
  -> git_diff
  -> 前端工具卡展示 diff
  -> agent 总结改动与验证
```

优点:改动小,复用现有文件工具。

缺点:用户确认发生在改动之后,安全性不足。

因此 MVP 前必须给写工具加 approval。

### 7.2 正式: patch proposal

新增工具:

- `builtin_patch_propose`:生成 patch proposal,不落盘。
- `builtin_patch_apply`:应用已批准 proposal。

proposal payload:

```jsonc
{
  "proposalID": "patch_...",
  "workspaceRoot": "...",
  "files": [
    {
      "path": "internal/foo.go",
      "oldText": "...",
      "newText": "...",
      "unifiedDiff": "..."
    }
  ]
}
```

proposal 可先存在 engine memory 或 temp artifact;正式再考虑 SQLite 表。
用户局部接受/拒绝需要前端 diff UI 后再实现。

## 8. 前端设计

短期复用 transcript tool card,但为 code 工具做专门 renderer:

- file read/search:显示路径、命中数、可展开内容。
- file write/patch/delete/move/copy:显示路径、变更摘要、风险标记。
- command:显示命令、状态、exit code、stdout/stderr。
- git diff:显示文件列表与 diff。
- git status:显示 staged/unstaged/untracked。

同时必须补:

- 工具显示名映射与 i18n。
- 不直接暴露 `builtin_file_patch` 这类 snake_case 名称。
- 左侧 Project section 的每个 Project 行右侧提供"新建会话"入口。
  点击后创建一个继承该 Project 的 session,即 `POST /sessions`
  时显式带 `projectID`。
- composer 顶部显示当前 Project 与审批模式。
- approval bar 区分 Project 绑定/授权 与 tool-call approval。

中期再做 Code Activity Panel:

- 数据源仍来自 turns/messages/tool parts。
- 不引入新的后端 focus。
- 可按 turn 聚合:Read / Edit / Test / Git。

## 9. 事件与存储

需要先做 Project 存储收口:

- 新增 `projects` 表。
- `sessions` 增加 `project_id`。
- 移除或迁移 `sessions.workspace_dirs`;新 API 不再读写它。
- `POST /sessions` 支持 `projectID`;从 Project section 创建会话时必须显式传入。
- turn-scoped 临时访问也引用 Project,不保存 raw dirs。

第一版可以不改 `turn.tool` 事件协议:

- `turn.tool phase=running` 表示命令开始。
- `turn.tool phase=ok/error` 携带结构化 result content。
- 终端后 refetch turn,以 canonical `messages.parts` 兜底。

后续如需实时命令输出,再新增字段或事件:

- `turn.tool_output` live-only。
- 或在 `turn.tool` 增加 `stdoutDelta/stderrDelta`。

这应作为单独契约变更,同步 Go/TS/docs。

## 10. 实施切片

### C0: Project 收口

当前状态:基础完成。

- 新增 Project 实体与 `sessions.project_id`。
- Project 承载 `rootDirs` 与 `approvalMode`。
- 迁移/删除 `session.workspaceDirs`,新代码只读写 Project。
- 左侧 Project section 支持从 Project 直接新建 session。
- composer 显示当前 Project 与三档审批模式。
- `request_capability` 从"授权 workspaceDirs"改为"创建/绑定 Project"。

验收:

- 同一个 repo 的多个 session 可绑定同一个 Project。
- 从 Project section 新建的 session 天然带 `projectID`,并继承
  `rootDirs/approvalMode`。
- session 不再暴露 `workspaceDirs` 作为新契约字段。
- tool resolver 只通过 Project 解析本地文件授权。

### C1: 审批安全收口

当前状态:文件写工具部分完成。

- 给现有文件写工具接入 tool-call approval。
- 补工具显示名与 i18n。
- 更新 `docs/contracts-checklist.md` 中 approval kind。

验收:

- 已完成:未审批时模型不能写/删/移 Project 文件。
- 已完成:approval 通过后同一 turn 可继续。
- 待补:cancel pending approval 的专项测试。

### C1.5: Project 命名收口

时机:在 C2 Command Runner 之前完成。

原因:

- C2 会新增 command 工具;若继续沿用 `workspace` 命名,command/git/file
  三套工具都会扩散旧概念。
- 当前 Project 已是目录与审批设置的事实源,用户侧再显示 workspace 会制造两套
  心智模型。
- 越早改 tool schema,模型学到旧参数名的成本越低。

改动范围:

- `builtin_file_*` schema 中 `scope:"workspace"` 改为 `scope:"project"`。
- 文件 resolver 接受 `project` scope,并从当前 session/turn 绑定的 Project
  读取 `rootDirs`。
- `request_capability.targetMode:"workspace"` 改为 `targetMode:"project"`;后端仍可
  映射到内部 `ModeWorkspace` 能力档。
- approval payload 与文案从 `workspaceDirs` 改成 Project/rootDirs 语义。
- 前端、测试、文档里的用户可见 workspace 文案改为 project/项目。

暂不改:

- `ModeWorkspace`
- `activeMode=workspace`
- 内部 Go 字段/变量名里的 `WorkspaceDirs`

这些属于内部能力档,先保留。`targetMode:"project"` 是工具协议层命名,
不要求同时改底层 session mode。等 C1.5/C2 稳定后,单独做内部命名统一。

验收:

- 用户界面不再出现"工作区目录"作为主要概念,统一为"项目目录"。
- 模型调用文件工具时使用 `scope:"project"`。
- 模型请求项目能力时使用 `request_capability targetMode:"project"`。
- 旧 `scope:"workspace"` 与 `targetMode:"workspace"` 不兼容,统一改掉。
- C2 command runner 的 `cwd` 与权限描述全部基于 Project。

### C2: Command Runner

- 新增 `builtin_command_run`。
- 非 shell argv。
- cwd Project root 沙箱。
- timeout、截断、process group cancel。
- 单测覆盖 cwd 越界、timeout、exit code、stdout/stderr 截断。

验收:

- agent 能运行 `go test ./internal/tool` 这类命令。
- 用户 cancel 能停止长命令。

### C3: Git Read Tools

- 新增 git status/diff/log。
- 只读,Project scoped。
- diff 输出截断并返回文件统计。

验收:

- agent 能在修改后展示 git diff。
- 前端 tool card 能读出文件数与 diff。

### C4: Code Tool Renderers

- 前端按 tool name + JSON content 渲染 code 卡片。
- command/git/file 工具显示结构化摘要。
- i18n 覆盖所有新旧 code 工具名。

验收:

- transcript 不暴露 snake_case 工具名作为主显示。
- command/git/file 结果可展开查看。

### C5: Patch Proposal

- 新增 patch proposal/apply 工具。
- approval payload 带 diff。
- 前端先支持整包接受/拒绝。

验收:

- agent 可先提出 diff,用户批准后才落盘。

### C6: Git Write Workflow

- stage/unstage/commit。
- 每步 approval。
- commit 前自动展示 status/diff 摘要。

验收:

- agent 可完成"改代码 -> 跑测试 -> commit"。
- 不支持 push/reset/clean 等危险动作。

## 11. 测试策略

后端:

- `internal/tool` 单测:路径沙箱、command、git、policy。
- `internal/engine` 单测:tool-call approval、cancel、turn 收尾事务。
- `internal/store` 单测:如新增 proposal 表才补。

前端:

- tool renderer 纯组件测试。
- overlayStore approval/tool event 合并测试。
- 手动验证 command/git 卡片展开与长输出截断。

端到端:

- 创建 session -> 绑定 Project -> 搜索文件 -> patch -> command test -> git diff。

## 12. 开放问题

- tool-call approval 的 session 级记忆是否落 SQLite,还是只在运行内存?
- command runner 是否允许继承用户 shell PATH?
- 对 `npm install`、`go get` 等网络写操作是否完全禁止,还是逐次审批?
- patch proposal 是否先用 temp artifact,还是直接建 SQLite 表?
- Code Activity Panel 放 transcript 内,还是 canvas/right pane?

建议答案:

- 第一版 approval 记忆只做 turn/session 内存态,避免长期策略复杂化。
- PATH 可继承,但 env 白名单化。
- 网络写操作先逐次审批。
- patch proposal 先 temp artifact,等 UI 稳定再落表。
- 先 transcript renderer,再抽 Code Activity Panel。
