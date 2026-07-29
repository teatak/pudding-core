# Pudding CLI-first Code 工具收敛计划

> 状态:M5 已完成,总进度 100%
> 决策日期:2026-07-12
> 目标工期:9-13 个工作日

## 1. 结论

Code 能力先补齐 CLI 执行闭环,再收敛工具并把可选能力统一为 session 级 App。

本次不是把所有专用能力改成 shell 命令,而是建立清晰分层:

```text
CLI                    通用搜索、检查、Git 只读、测试、构建、项目脚本
Core tools             授权、项目指令、文件预览、原子多文件 Patch
Session-loaded Apps    LSP、Git 写入、文件管理、Browser、Image Capture、API/MCP
```

实施期间现有工具继续可用。只有 CLI 等价路径通过真实项目 Eval 后,才直接删除确认
淘汰的工具定义、调用分支、展示和测试;不保留旧名称、兼容别名或迁移代码。

## 2. 当前基线

`builtin_command_run` 已具备:

- Project cwd 校验。
- 单一 `command` 字符串与固定非交互 shell。
- 最小环境变量继承与显式 env。
- 100ms-10min timeout。
- 进程组取消。
- stdout/stderr 各 64KiB 头尾截断。
- test/build/lint/check 分类、验证状态与诊断解析。
- 终端式工具结果展示。

M2 后剩余主要缺口:

- CLI 可执行文件发现与跨平台错误还不完整。
- Code 一次向模型暴露 Chat + Work + Code 的全部工具 schema。
- 专用文件/Git/LSP 工具是否真的提升成功率尚无真实 Eval 结论。

## 3. 范围

### 3.1 本次交付

- 增强前台非交互 CLI。
- 统一命令字符串并支持管道、重定向和简单复合命令。
- 增加非 canonical 的实时命令输出事件。
- 增加 session-scoped 后台命令会话及 poll/write/stop 操作。
- 建立真实项目 Code Eval。
- 确定 CLI 替代工具矩阵。
- 实现固定 Core 与 session-scoped App 动态加载。
- 仅删除通过验收的冗余工具;当前样本不足的实现转入内置 App,不强行删除。

### 3.2 暂不交付

- 完整 TUI 语义、终端尺寸动态调整和 ANSI 结构化解析。
- 交互式密码、sudo、TUI 或远程 SSH 会话。
- 后台进程跨 daemon 重启恢复。
- Windows 实机适配。接口与进程抽象保持可移植,Windows 另行验收。
- 任意第三方二进制插件热加载。

## 4. CLI 目标形态

### 4.1 前台命令

继续使用 `builtin_command_run`,只接受一个完整命令字符串:

```jsonc
{
  "scope": "project",
  "command": "rg 'TODO' internal | head -20",
  "cwd": ".",
  "timeout_ms": 120000
}
```

规则:

- `command` 是唯一命令入口,模型不能提供 shell executable 或启动参数。
- `auto` 使用 shell AST 拆分静态命令段并逐段应用负面风险规则;未知命令名本身不触发审批。
- Unix 命令固定使用 `/bin/sh -c`;macOS daemon 启动时只捕获一次用户交互式登录
  shell 的受控环境快照,不会在每次命令执行时加载登录配置。Windows 后续使用 PowerShell
  非交互模式。
- 安全管道、简单复合命令和 Project 内重定向可自动执行;变量展开、命令替换等
  无法可靠静态审阅的结构降级为审批。
- approval 展示完整 command、cwd、env key、timeout 与风险说明。
- macOS 的 `ask/auto` 默认使用 Project 或 session 临时工作区 CLI 沙箱;风险命令
  经用户批准后仅该次原始调用绕过沙箱。`full` 始终绕过沙箱。其他平台的沙箱
  适配另行验收。

### 4.2 实时输出

命令运行期间新增 `turn.tool` 输出事件:

```jsonc
{
  "phase": "output",
  "stream": "stdout",
  "content": "ok  github.com/...\n"
}
```

- 50-100ms 合并小输出,单事件有字节上限。
- SSE output delta 只进入前端 overlay,不落 canonical message。
- 命令结束后仍只落一次有界最终结果。
- cancel 后停止输出、杀进程组并以 cancelled 收尾。
- 前端终端卡在运行时滚动显示,最终结果替换 overlay。

### 4.3 后台进程

后台启动由 Code Core 的 `builtin_command_run(background=true)` 提供;
`builtin_command_session` 同属 Code Core,统一读取、输入和停止。

约束:

- 进程由 daemon 管理,归属 session,不属于前端 focus。
- 授权在启动时形成快照;之后切换 `code -> work/chat`、修改或移除 Project,
  不撤销已经批准的进程,也不自动终止它。
- 返回不可猜测的 `processID`。
- 每 session 最多 4 个后台进程,全局有上限。
- 输出使用有界 ring buffer;poll 返回 offset 与截断信息。
- 不设置 `max_runtime_ms`;长任务持续运行,直到自然退出、用户停止、session 删除
  或 daemon 退出。
- 显式 stop 与资源清理在 Unix 先向进程组发送 `SIGTERM`,等待 2 秒后再以
  `SIGKILL` 兜底。
- 运行中的进程不因无 poll 过期;结束结果保留 30 分钟后回收。
- `process.started/finished/stopped/removed` 作为 session-scoped 瞬时事件通知前端;
  事实快照和日志仍由 REST 读取,事件不落库。
- composer 展示运行中任务和默认折叠的最近完成任务;展开单项后按需读取有界
  日志,不做固定全量轮询。
- Session Rail 显示后台任务数量;删除含运行任务的 session 前明确提示。
- 普通后台命令提供有界、串行的 stdin 写入;需要交互式 CLI/REPL 时显式设置
  `tty:true`,由独立 agent PTY 承载,不接管用户手动打开的桌面终端。

## 5. 工具去留矩阵

| 当前能力 | 第一阶段 | 目标状态 | 原因 |
| --- | --- | --- | --- |
| `request_capability` | 保留 | Core | 授权与模式边界不可由 CLI 替代 |
| `project_inspect` | 删除 | CLI / Project Files App | 按任务读取真实目录、manifest 与 Git 状态 |
| `project_instructions` | 删除 | Code prompt | 根级 `AGENTS.md` 每轮自动注入,子目录规则按目标检查 |
| `file_read` | 保留 | Project Files App | 支持图片、附件和文件预览，按需加载 |
| `file_list/stat/search/slice` | 并行验证 | CLI 候选 | `rg/find/sed/tail/stat` 可覆盖 |
| `file_write/patch` | 保留 | Project Files App | 普通覆盖与原子多文件 Patch 按需加载 |
| `file_delete/move/copy` | 保留 | Project Files App | 文件管理风险与跨平台语义更适合结构化工具 |
| `command_run` | 增强 | Core | 通用执行原语 |
| `git_status/diff/log` | 并行验证 | CLI 候选 | Git CLI 输出稳定且模型熟悉 |
| `git_stage/unstage/commit` | 保留 | Source Control App | 需要结构化审批和漂移检查 |
| `code_symbols/definition/references/diagnostics/rename` | 保留 | Code Intelligence App | 语义结果和 WorkspaceEdit 安全校验不可由通用 shell 等价替代 |
| `skill_validate` | 保留 | Skill Authoring 内置 App | 直接创建或编辑 Skill 后执行校验，会话级按需加载 |
| Browser | 保留 | Browser 内置 App | 非本地 CLI 能力 |
| REST/GraphQL | 保留 | 对应安装 App | 连接配置、凭据注入与结构化请求 |
| Apps/MCP | 保留 | 按 App 动态加载 | 外部系统能力 |
| Canvas/UI | 保留 | `chat.core` / Canvas App | 通用 UI 交互属于 Chat Core，画布能力由 Desktop App 动态注册 |

CLI 候选在删除前必须满足:

- macOS dev/release 环境命令稳定可发现。
- 成功率不低于专用工具。
- 输出大小与 token 使用不显著恶化。
- transcript 仍能清楚展示输入、输出、exit code 与复制操作。
- `ask/auto/full` 行为没有退化。

## 6. App 动态加载

### 6.1 不是动态加载二进制

所有工具实现继续编译进 daemon。动态加载只决定本轮哪些 `ToolDef` schema 进入
`provider.Request.Tools`。

### 6.2 能力归属

```go
type AppDefinition struct {
    ID           string
    RequiredMode store.AgentMode
    Tools        []ToolRef
}
```

首批内置 App:

- `project-files`
- `source-control`
- `code-intelligence`
- `camera`

Browser、Canvas、Skill Authoring、App Authoring 以及安装 App 的 API/MCP 工具同样
使用 session 级 App 加载。`builtin_request_user_input` 直接归入默认 `chat.core`。

### 6.3 默认 Code coding 工具集

Code 默认 coding 工具为 4 个:

- `request_capability`
- `builtin_app_load`
- `builtin_command_run`
- `builtin_command_session`

其中 `request_capability` 已包含在 Chat Core 中。Code 继续继承 Chat 的 9 个基础工具,
所以 daemon 默认合计 12 个；连接 Desktop runtime 后再增加
`builtin_request_user_input`。不会为了压低数字
静默移除 Code 模式中的 Chat 能力。

Project 根级指令在 Code turn 构建时自动注入 system context；项目识别由 CLI 与
Project Files App 按任务需要完成,不再占用两个常驻工具 schema。

系统 prompt 只放短 App 索引,不放未加载工具完整 schema。

### 6.4 session 状态

- App 加载写入 session 的 `loadedAppIDs`,后续 turn 自动复用。
- App 可由用户从会话中关闭,不卸载 App 本体。
- mode 提升后重新计算可调用 App,不能越过 capability。
- 加载 App 不等于授权;每次实际调用仍走 Project path 与审批策略。
- `builtin_app_load` 结果进入 canonical parts,便于追溯。
- mode 降级隐藏工具但保留加载状态。
- 未加载 App 的工具调用返回 `app_not_loaded`,未知工具返回 `unknown_tool`。

### 6.5 缓存稳定

- Core 与内置 App 的工具归属必须静态定义。
- ToolDef 始终按 tool name 排序。
- 相同 `loadedAppIDs` 必须生成字节稳定的 schema JSON。
- 下一 turn 复用相同 App 集合,避免重复加载并保持 prompt cache 稳定。
- 安装 App 定义以 App id 与版本/hash 标识。

## 7. Engine 改造

工具定义选择已从单纯按能力等级累计过滤拆分为:

```text
All definitions
  -> CoreDefinitionsForMode(mode)
  -> AppendLoadedAppDefinitions(session.loadedAppIDs, mode)
  -> provider.Request.Tools
```

主要修改点:

- `internal/tool`:固定 Core 归属、App 工具归属与稳定排序。
- `MultiRunner`:按当前已加载 App 聚合动态 definitions。
- `Engine`:从 session 读取 `loadedAppIDs`。
- `buildProviderRequest`:按 Core + loaded Apps 选择 schema。
- `executePendingTools`:App load 后标记 toolset changed。
- tool loop:复用现有 mode changed 重建路径,保留 messages 并重建 Tools。
- approval:区分 capability required、App 未加载与 unknown tool。
- App 状态跨 turn 保留;后台进程同样跨 turn 继续运行。

Provider adapter 不需要新的协议;OpenAI、Anthropic、Google 继续消费普通
`provider.Request.Tools`。

## 8. Eval 与验收

### 8.1 固定任务

至少建立以下本地 fixture / 真实项目任务:

1. 陌生项目结构分析并读取适用指令。
2. 使用 CLI 定位文本与文件。
3. 修复单文件编译错误并通过最小测试。
4. 完成一次多文件原子 Patch、审批前 Diff 与 Turn Diff 审阅。
5. 处理测试失败后重新运行。
6. 执行 Git status/diff/log。
7. LSP definition/references/diagnostics/rename。
8. 长输出截断、实时显示与 cancel。
9. 启动、检查并停止 dev server。
10. shell 越界、危险命令与自定义 env 审批。

### 8.2 指标

- 任务完成率。
- 最终测试/构建状态。
- 工具调用总数与重复率。
- CLI fallback 成功率。
- approval 数量与拒绝恢复率。
- active ToolDef 数量和 tools schema estimated tokens。
- App 加载次数与会话复用率。
- cached input tokens。
- 总耗时与首个有效操作延迟。

### 8.3 删除门槛

- deterministic runner/integration tests 100% 通过。
- 真实模型固定任务至少 8/10 完成。
- 典型 Code turn 新增 coding tools 不超过 15 个;含继承 Chat 工具的总数不超过 24 个。
- tools schema estimated tokens 比当前 Code 基线下降至少 50%。
- 不出现同一 App 每 turn 重复加载。
- 没有 capability、Project path 或 approval 绕过。
- 被替代工具连续测试中没有不可恢复优势。

## 9. 实施阶段

### M0:设计与基线,0.5 天

- 落本文档。
- 固化 M0 当时的 53 个工具、schema token、调用率与真实 Code turn 样本。
- 建立 CLI/专用工具去留矩阵。

验收:边界和指标确定,不改运行行为。

### M1:前台 CLI,2-3 天

状态:已完成(2026-07-12)。

- 单一 `command` schema 与固定 shell launcher。
- shell AST 风险分类、重定向边界与审批详情。
- stdout/stderr delta、合并与最终 canonical 结果。
- transcript 运行态输出。
- cancel、timeout、长输出和非零退出测试。

验收:常见 test/build/search 管道可用,风险命令不会自动越权。

### M2:后台进程,1.5-2 天

状态:已完成(2026-07-12)。

- session-scoped process manager。
- `command_run(background=true)` 与统一 `command_session` 工具。
- 普通 stdin 与显式 PTY 输入。
- 输出 ring buffer、结束结果 retention、上限与 shutdown cleanup。
- 生命周期事件、按需日志 REST、Composer 与 Session Rail 展示。
- 启动授权快照;模式或 Project 变化不撤销已运行进程。
- Unix 优雅停止与强制终止兜底。

验收:模型能启动本地服务、读取启动结果并可靠停止。

### M3:CLI 优先与 Code Eval,1.5-2 天

状态:已完成(2026-07-12)。结果见 `docs/code-cli-eval-report.md`。

- Code prompt 改为 CLI-first。
- 运行固定任务并记录基线。
- 判断 file/Git read 候选是否可删除或仅隐藏。
- 不满足门槛的工具继续保留。

验收:形成有数据支撑的最终工具矩阵。

### M4:App 动态加载,3-4 天

状态:已完成(2026-07-12)，并于 2026-07-22 将全部可选能力迁移为 session 级 App。

- App registry/loader。
- session loaded set 与稳定排序。
- tool loop 动态重建。
- capability/approval 安全测试。
- prompt App 索引、transcript 与 i18n。
- 先覆盖内置工具,再覆盖 Apps/MCP。

验收:默认 Code coding 工具 4 个,常见任务通过 App 按需增加工具。

### M5:收尾,1-1.5 天

状态:已完成(2026-07-12)。

- 复核删除门槛;真实 Git/LSP 样本不足,本阶段不删除实现,统一留在内置 App。
- 更新工具使用率报告分组。
- 全量 Go 测试、真实 LSP 集成、Web build、desktop build。
- CLI fixture 与当前仓库构建完成自动验收;真实模型固定任务继续作为后续删除门槛。

验收:无兼容别名、无 migration、无未使用工具展示映射；现有专用能力可通过
内置或安装 App 按需恢复。

## 10. 总工期与进度汇报

预计总工期:9-13 个工作日。

每完成一个大阶段汇报总进度:

| 阶段 | 累计进度 |
| --- | ---: |
| M0 | 5% |
| M1 | 30% |
| M2 | 45% |
| M3 | 60% |
| M4 | 90% |
| M5 | 100% |

若 M3 数据显示 CLI 没有提高成功率,暂停 M4 的工具删除部分,只实施动态暴露;
不得为了减少数字而牺牲安全边界或真实任务完成率。
