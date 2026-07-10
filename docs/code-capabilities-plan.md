# Pudding Code 能力设计与计划

> 状态:C0、C1、C1.5、C1.6、C2、C3、C4、C5、C6、C7 与 C8 已落地。
> 目标:在现有 multi-session / Project tool 架构上,把 Pudding 从"可读写文件"
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

- 当前代码已经有 Project、`activeMode=project`、`request_capability`、文件工具
  和 tool part UI。
- code 操作天然属于 session 的上下文与 transport,不是 daemon 全局 focus。
- REST 继续负责 session / approval / snapshot;具体读文件、改文件、跑命令、
  git 操作都应作为 LLM 可调用工具出现。

## 1. 当前地基

已具备:

- Project 已是代码目录与审批设置的主实体,session 通过 `projectID`
  绑定 Project。
- `ActiveMode=project`、`ModeLease=session` 由 session 承载。
- `request_capability` 应请求 Project 能力与项目目录授权。
- engine 已有 tool loop、`turn.tool` 事件、tool_result canonical 落库。
- `internal/tool` 已有 Project 路径解析与沙箱校验。
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

- 不做后端全局 current Project / focus。
- 不做旧 Runtime 兼容层。
- 不把 WebSocket 当普通 command/SSE 替代品。
- 不在第一版做完整 IDE。
- 不让模型静默执行危险 git 或 shell 操作。

## 3. 架构原则

### 3.1 Project-owned code root

Project 是代码目录与审批设置的唯一事实源:

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

API 与新代码只读写 Project,不保留第二套目录事实源。

"仅本轮访问"使用独立的 `ProjectAccessGrant`:

- grant 只保存授权目录与 turnID,不写入 Project 表。
- turn 结束时随 engine turn 状态一起清理。
- 只有 session scope 的"记住授权"才创建或绑定持久 Project。

tool call 通过持久 Project roots 与当前 turn grant roots 的并集解析授权范围;
审批策略只来自持久 Project,无 Project 的临时授权使用默认 `auto`。

不新增 daemon 级 `currentProject`。

### 3.2 命名边界

用户、工具协议和内部代码统一使用 Project:

- UI 文案使用"项目 / Project"。
- 数据模型使用 `Project.rootDirs`、`Session.projectID`。
- 文件工具 scope 为 `project`。
- `request_capability.targetMode` 为 `project`。
- approval payload 使用 `projectDirs`。
- 内部能力档为 `ModeProject` / `activeMode=project`;工具调用上下文使用
  `ProjectDirs`。
- 旧 mode、scope 与字段名不在运行时兼容。

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
  "all user files" 这种宽授权,不必逐个项目目录确认。
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
    "scope": "project",
    "cwd": "relative/or/absolute/path inside authorized Project root",
    "argv": ["go", "test", "./internal/tool"],
    "env": {"KEY": "value"},
    "timeout_ms": 120000
  }
}
```

约束:

- 第一版只接受 `argv`,不接受裸 shell 字符串。
- `cwd` 必须解析到当前 Project 授权范围内。
- 默认只继承 PATH、HOME、临时目录、locale 和常见 toolchain 路径变量;
  其他变量必须通过 `env` 显式提供。
- `timeout_ms` 默认 60 秒,上限 10 分钟。
- stdout/stderr 分开保存,每路最多保留 64 KiB 头尾内容。
- 超出输出上限返回独立截断标记。
- cancel 时 kill process group。
- cwd 校验不是 OS 文件系统沙箱;命令仍拥有 daemon 用户的进程权限,因此必须
  经过 Project 授权与审批策略。

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
  "stdoutTruncated": false,
  "stderrTruncated": false,
  "timedOut": false,
  "cancelled": false
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
  "projectRoot": "...",
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
- session 不保存独立目录列表;新 API 只读写 Project。
- `POST /sessions` 支持 `projectID`;从 Project section 创建会话时必须显式传入。
- turn-scoped 临时访问保存为 `ProjectAccessGrant`,不创建 Project。

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
- 删除 session 上的目录字段,新代码只读写 Project。
- 左侧 Project section 支持从 Project 直接新建 session。
- composer 显示当前 Project 与三档审批模式。
- `request_capability` 改为创建/绑定 Project 或授予本轮目录访问。

验收:

- 同一个 repo 的多个 session 可绑定同一个 Project。
- 从 Project section 新建的 session 天然带 `projectID`,并继承
  `rootDirs/approvalMode`。
- session 不再暴露独立目录字段。
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

状态:已完成(2026-07-10)。

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
- `request_capability.targetMode` 使用 `project`。
- approval payload 与文案使用 `projectDirs` / Project 语义。
- 前端、测试、文档里的用户可见文案统一为 project/项目。

验收:

- 用户界面不再出现"工作区目录"作为主要概念,统一为"项目目录"。
- 模型调用文件工具时使用 `scope:"project"`。
- 模型请求项目能力时使用 `request_capability targetMode:"project"`。
- 旧 `scope:"workspace"` 与 `targetMode:"workspace"` 不兼容,统一改掉。
- C2 command runner 的 `cwd` 与权限描述全部基于 Project。

### C1.6: Project 与 AccessGrant 分离

状态:已完成(2026-07-10)。

- 内部能力档统一为 `ModeProject` / `activeMode=project`。
- 内部目录字段与 resolver 统一为 Project 命名。
- Project 删除 `Temporary`;Project 表只保存持久实体。
- turn scope 授权保存为 engine 内存中的 `ProjectAccessGrant`,turn 结束即清理。
- session scope 授权才创建、复用或扩展 Project。
- SQLite 对旧 mode 值做一次性数据迁移,运行时协议不接受旧值。

验收:

- turn scope 授权后可访问批准目录,但 Project 列表不增加。
- session scope 授权创建或绑定 Project。
- Go、API 与 web 契约统一使用 `project`。

### C2: Command Runner

状态:已完成(2026-07-10)。

- 已新增 `builtin_command_run`。
- 使用非 shell argv 与 Project cwd 约束。
- 使用最小继承环境,支持显式 `env`。
- 已实现 timeout、64 KiB 头尾截断、process tree cancel。
- 已接入 ask/auto/full 审批与 command/destructive 风险分类。
- `auto` 仅放行常见测试/构建/检查命令;自定义 env、绝对路径或父目录参数会
  降级为询问。
- 已补命令显示名、图标与三语 i18n。
- 单测覆盖 cwd 越界、真实 `go test`、timeout、cancel、exit code、环境隔离和
  stdout/stderr 截断。

验收:

- 已完成:agent 能运行 `go test ./internal/tool` 这类命令。
- 已完成:用户 cancel 能停止长命令及其子进程。

### C3: Git Read Tools

状态:已完成(2026-07-10)。

- 已新增 `builtin_git_status`、`builtin_git_diff`、`builtin_git_log`。
- 三个工具只读且 Project scoped;仓库根不得越出授权 Project 目录。
- Git 子进程使用最小环境,禁用 pager、external diff、textconv、fsmonitor 与
  optional locks。
- status 返回分支、upstream、ahead/behind、文件状态和分类计数。
- diff 同时返回 128 KiB 头尾截断 patch 与不截断的逐文件增删统计。
- log 返回最多 100 条结构化提交元数据,无提交仓库返回空列表。
- `ask` 模式请求审批,`auto` 与 `full` 自动允许只读 Git 操作。
- 已补三语显示名与 Git 图标。

验收:

- 已完成:agent 能在修改后读取并展示 git diff。
- 已完成:tool result 提供 `fileCount`、`files` 与 `diff`,供前端卡片读取。

### C4: Code Tool Renderers

状态:已完成(2026-07-10)。

- transcript 按 tool name + JSON content 选择 command、Git 或 file renderer。
- command 展示 argv、cwd、exit code、duration、stdout/stderr 与截断状态。
- git status 展示分支、ahead/behind、分类计数与文件状态列表。
- git diff 展示逐文件增删统计、staged 状态与可滚动 unified diff。
- git log 展示 hash、subject、author 与提交时间。
- file 工具展示路径、移动/复制方向、读写统计、目录项、搜索命中或文件内容。
- 列表限制可见项数量,大输出使用稳定高度滚动区;原始 JSON 仅在用户展开
  “原始数据”后渲染。
- code 工具详情不再直接显示内部 snake_case 名称,标题和字段使用三语 i18n。
- 已用真实成功/失败 file tool result 验证 desktop UI,并验证 390px 视口无横向
  溢出。

验收:

- 已完成:transcript 不暴露 snake_case 工具名作为主显示。
- 已完成:command/git/file 结果可展开查看,并保留按需展开的原始数据。

### C5: Patch Proposal

状态:已完成(2026-07-10)。

- 已新增 `builtin_patch_propose` 与 `builtin_patch_apply`。
- propose 接受最多 16 个同 Project root 的 UTF-8 regular file 全文更新、创建或
  删除,只生成 unified diff,不写工作树。
- proposal 是 session-scoped daemon memory,TTL 2 小时,最多保留 128 个。
- proposal 记录源文件 SHA-256;approval details 和 apply 前均重新验证授权与漂移。
- apply 预写同目录临时文件,再通过 backup + rename 应用;中途失败逆序回滚。
- unified diff 超过 256 KiB 时拒绝生成,要求 agent 拆分,避免用户批准未展示内容。
- `auto` 与 `ask` 对 apply 请求审批;`full` 跳过普通审批但仍执行 proposal/hash
  校验。
- approval payload 包含 proposalID、路径、文件统计和完整 diff。
- composer 支持查看 diff、整包应用或拒绝;transcript 已补 propose/apply 结构化
  卡片和三语显示名。

验收:

- 已完成:agent 可先提出 diff,工作树保持不变,用户批准后才落盘。
- 已完成:文件在等待审批期间发生漂移时,apply 整包拒绝且不产生部分写入。

### C6: Git Write Workflow

状态:已完成(2026-07-10)。

- 已新增 `builtin_git_stage`、`builtin_git_unstage` 与 `builtin_git_commit`。
- stage/unstage 只接受最多 128 个显式文件路径,统一转成 repository-relative
  literal path,不接受目录、任意 pathspec 或 Git 参数。
- Git 写入前校验 worktree root、git dir、common dir 与 index 仍位于 Project
  授权目录;linked worktree 的元数据越界时拒绝写入。
- stage 使用 `hash-object` + 单次 `update-index --index-info`,不执行仓库 clean
  filter;单文件上限 64 MiB,单次总输入上限 256 MiB。
- commit 禁用 hooks、签名与 auto-gc,不支持 amend;merge/rebase/cherry-pick 等
  Git operation state 存在时拒绝普通提交。
- commit 审批自动附带 branch、status 计数、逐文件统计和 staged diff;审批时记录
  HEAD + index 指纹,审批等待期间发生漂移则拒绝提交。
- `ask` 与 `auto` 对 stage/unstage/commit 逐步审批;`full` 不弹普通审批,但仍
  生成和校验 commit staged 快照。
- composer 支持 commit staged diff 审阅;transcript 已补三个工具的结构化结果、
  图标与三语显示名。

验收:

- 已完成:agent 可完成"改代码 -> 跑测试 -> stage -> commit"。
- 已完成:不支持 push/reset/clean/amend 等危险 Git 写操作。

### C7: Code Search, Project Inspect 与文件定位

状态:已完成(2026-07-10)。

目标:

- 减少 agent 为理解项目而进行的重复 list/read 调用。
- 让代码搜索覆盖常见工程检索需求,同时保持结构化、只读和有界输出。
- 让搜索结果从 transcript 直接进入画布对应行,形成“搜索 -> 阅读”短路径。

#### C7.1 增强代码搜索

在现有 `builtin_file_search` 上扩展,不新增语义重复的搜索工具:

- 保留 literal 搜索,新增 `regex` 模式。
- 支持大小写敏感开关。
- 支持 `include_globs`、`exclude_globs`,按 Project 相对路径过滤。
- 支持 0-5 行上下文,每个 match 返回 `lineStart`、`lineEnd` 与有界 excerpt。
- 继续跳过二进制文件、超限文件和常见生成目录。
- 非法正则或 glob 返回明确结构化错误,不回退成另一种搜索模式。

搜索结果仍限制最多 500 个命中;上下文只用于定位和预览,不替代
`builtin_file_read` / `builtin_file_slice`。

#### C7.2 Project Inspect

新增只读工具 `builtin_project_inspect`:

- 输入为 `scope:"project"` 与可选的 Project 内目录 `path`。
- 返回授权根、检查目录、Git root、语言、manifest、项目指令文件和建议验证命令。
- 第一版只识别稳定的文件标记,例如 `go.mod`、`package.json`、`Cargo.toml`、
  `pyproject.toml`、`AGENTS.md`、`CONTRIBUTING.md`。
- `package.json` 只读取 scripts 名称并生成 argv 建议,不执行脚本。
- 扫描有固定深度、文件数和字节上限;结果按需生成,不写入 Project 或 SQLite。

Project 仍只承载 `rootDirs` 与 `approvalMode`;代码语言、脚本和指令文件可能随
工作树变化,不能成为持久事实源。

#### C7.3 文件定位 UX

- file search renderer 展示搜索模式、命中数和扫描文件数。
- 每个命中可点击,在画布文件 tab 中打开该命中的上下文片段。
- 预览使用真实起始行号并标记为局部快照;同一路径复用已有文件 tab。
- `builtin_project_inspect` 使用结构化卡片展示语言、manifest、指令和建议命令;
  主标题与字段补齐三语 i18n,不暴露 snake_case 工具名。

验收:

- agent 能用 regex + glob 在授权 Project 内搜索,不能越过 Project root。
- 大仓库搜索有界,非法模式返回可恢复错误。
- 用户点击搜索命中后,画布打开对应路径并从正确行号显示上下文。
- agent 可一次读取项目类型、Git root、指令文件和候选验证命令。
- Go 单测与 Web build 通过。

### C8: 编辑与验证闭环

状态:已完成(2026-07-10)。

目标:

- 降低修改大文件时传输完整 `new_text` 的 token 成本。
- 继续复用补丁 proposal 的 diff 审批、漂移检测和多文件原子应用。
- 把测试、lint、build 的结果从原始终端文本提升为可定位的结构化诊断。
- 不新增绕过 command 审批策略的自动执行路径。

#### C8.1 Patch V2 局部 edits

`builtin_patch_propose.files[]` 支持三种互斥操作:

- `new_text`:创建文件或显式全文替换。
- `delete:true`:删除已有文本文件。
- `edits`:对已有文件执行一组有序 exact replacement。

单个 edit 结构:

```jsonc
{
  "old_text": "existing text",
  "new_text": "replacement text",
  "replace_all": false
}
```

约束:

- 每个文件最多 64 个 edits,按数组顺序应用到内存快照。
- `old_text` 必须非空;默认只能唯一匹配,多处匹配必须显式 `replace_all`。
- 任意 edit 不匹配或存在歧义时,整个 proposal 生成失败,工作树不变。
- 后端基于最终内存文本生成 unified diff;apply 流程继续使用原有 hash 校验、
  backup + rename 与失败回滚。
- 修改已有文件时优先使用 edits;创建文件或确实需要全文替换时才使用
  `new_text`。

#### C8.2 Command verification metadata

不新增 `builtin_project_verify`;继续使用 `builtin_command_run`:

- 根据 argv 识别 `test | lint | build | check` 等验证类型。
- result 增加 `verificationKind`、`verificationStatus` 与 `diagnostics`。
- 非验证命令保持普通 terminal result,不伪造“已验证”状态。
- 非零退出码仍是命令正常完成,但验证状态为 `failed`。
- timeout / cancel 分别映射为 `timed_out` / `cancelled`。

第一版解析以下稳定格式:

- Go / GCC 风格:`path:line:column: message`。
- TypeScript 风格:`path(line,column): error TSxxxx: message`。
- ESLint stylish 风格:文件路径行后跟 `line:column severity message rule`。

诊断只接受 Project 内路径,最多返回 200 条。每条诊断携带 `path`、
`relativePath`、行列、severity、message、可选 code/source,以及文件上下文片段。
原始 stdout/stderr 继续保留,解析失败不改变命令结果。

#### C8.3 验证 UX

- command 卡片优先展示“测试通过 / 测试失败 / 构建通过”等验证状态。
- 诊断列表展示文件、行列、severity、message,并限制最大高度。
- 点击 Project 内诊断后,在画布文件 tab 中打开对应上下文和真实行号。
- 最终回答仍由 agent 汇报改动和验证;系统不自动 stage 或 commit。

验收:

- agent 可以用局部 edits 提出多文件补丁,无需发送完整旧文件。
- 任意 edit 冲突时 proposal 不产生,apply 的原子性与漂移保护不退化。
- 常见 Go、TypeScript、ESLint 错误能转成结构化诊断。
- 用户点击诊断能在画布定位对应文件行。
- 普通命令不会被错误标记为验证通过。
- Go 单测、Web build 与 `git diff --check` 通过。

## 11. 测试策略

后端:

- `internal/tool` 单测:路径沙箱、command、git read/write、patch、policy。
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
