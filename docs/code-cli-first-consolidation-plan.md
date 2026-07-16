# Pudding CLI-first Code 工具收敛计划

> 状态:M5 已完成,总进度 100%
> 决策日期:2026-07-12
> 目标工期:9-13 个工作日

## 1. 结论

Code 能力先补齐 CLI 执行闭环,再收敛工具和实现 turn 级 Toolkit 动态加载。

本次不是把所有专用能力改成 shell 命令,而是建立清晰分层:

```text
CLI                    通用搜索、检查、Git 只读、测试、构建、项目脚本
Core tools             授权、项目指令、文件预览、Patch Proposal / Apply
Lazy toolkits          LSP、Git 写入、文件管理、Browser、API、Apps/MCP
```

实施期间现有工具继续可用。只有 CLI 等价路径通过真实项目 Eval 后,才直接删除确认
淘汰的工具定义、调用分支、展示和测试;不保留旧名称、兼容别名或迁移代码。

## 2. 当前基线

`builtin_command_run` 已具备:

- Project cwd 校验。
- argv 直执行,不隐式经过 shell。
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
- 增加受控 shell script 模式。
- 增加非 canonical 的实时命令输出事件。
- 增加 session-scoped 后台进程 start/poll/stop。
- 建立真实项目 Code Eval。
- 确定 CLI 替代工具矩阵。
- 实现固定 Toolkit Catalog 与 turn-scoped 动态加载。
- 仅删除通过验收的冗余工具;当前样本不足的实现转入 lazy toolkit,不强行删除。

### 3.2 暂不交付

- 完整 PTY、终端尺寸和 ANSI 交互模拟。
- 交互式密码、sudo、TUI 或远程 SSH 会话。
- 后台进程跨 daemon 重启恢复。
- Windows 实机适配。接口与进程抽象保持可移植,Windows 另行验收。
- 任意第三方二进制插件热加载。

## 4. CLI 目标形态

### 4.1 前台命令

继续使用 `builtin_command_run`,支持两种互斥输入:

```jsonc
{
  "scope": "project",
  "argv": ["go", "test", "./..."],
  "cwd": ".",
  "timeout_ms": 120000
}
```

```jsonc
{
  "scope": "project",
  "script": "rg 'TODO' internal | head -20",
  "cwd": ".",
  "timeout_ms": 120000
}
```

规则:

- `argv` 与 `script` 必须且只能提供一个。
- argv 保持默认推荐路径;`auto` 使用负面风险规则,未知命令名本身不触发审批。
- script 使用平台固定 shell,模型不能提供 shell executable 或启动参数。
- Unix 使用 `/bin/sh -lc`;Windows 后续使用 PowerShell 非交互模式。
- script 无法可靠静态解析,统一标记 `LowRisk=false`;`ask/auto` 请求审批,
  `full` 才可自动执行。
- approval 展示完整 script、cwd、env key、timeout 与风险说明。
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

后台能力属于 Terminal 内置 App,不进入默认工具集:

- `builtin_command_start`
- `builtin_command_poll`
- `builtin_command_stop`

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
- 第一版不提供 stdin 和 PTY。

## 5. 工具去留矩阵

| 当前能力 | 第一阶段 | 目标状态 | 原因 |
| --- | --- | --- | --- |
| `request_capability` | 保留 | Core | 授权与模式边界不可由 CLI 替代 |
| `project_inspect` | 保留 | Core | 提供有界项目结构与验证建议 |
| `project_instructions` | 保留 | Core | 保证 AGENTS/CLAUDE/CONTRIBUTING 作用域 |
| `file_read` | 保留 | Core | 支持图片、附件和 Canvas 文件预览 |
| `file_list/stat/search/slice` | 并行验证 | CLI 候选 | `rg/find/sed/tail/stat` 可覆盖 |
| `file_write/patch` | 保留观察 | Lazy 或删除 | 文本修改优先 Patch Proposal |
| `file_delete/move/copy` | 保留 | `code.files-write` | 文件管理风险与跨平台语义更适合结构化工具 |
| `command_run` | 增强 | Core | 通用执行原语 |
| `git_status/diff/log` | 并行验证 | CLI 候选 | Git CLI 输出稳定且模型熟悉 |
| `git_stage/unstage/commit` | 保留 | `code.git-write` | 需要结构化审批和漂移检查 |
| `patch_propose/apply` | 保留 | Core | diff review、hash 漂移与原子应用 |
| `code_symbols/definition/references/diagnostics/rename` | 保留 | `code.lsp` | 语义结果和 WorkspaceEdit 安全校验不可由通用 shell 等价替代 |
| `skill_validate` | 保留 | `code.skill` | 直接创建或编辑 Skill 后执行校验 |
| Browser | 保留 | Browser 内置 App | 非本地 CLI 能力 |
| REST/GraphQL | 保留 | 对应安装 App | 连接配置、凭据注入与结构化请求 |
| Apps/MCP | 保留 | 按 App 动态加载 | 外部系统能力 |
| Canvas/UI | 保留 | UI Toolkit | 需要结构化前端协议 |

CLI 候选在删除前必须满足:

- macOS dev/release 环境命令稳定可发现。
- 成功率不低于专用工具。
- 输出大小与 token 使用不显著恶化。
- transcript 仍能清楚展示输入、输出、exit code 与复制操作。
- `ask/auto/full` 行为没有退化。

## 6. Toolkit 动态加载

### 6.1 不是动态加载二进制

所有工具实现继续编译进 daemon。动态加载只决定本轮哪些 `ToolDef` schema 进入
`provider.Request.Tools`。

### 6.2 Catalog

```go
type ToolPluginManifest struct {
    ID         string
    Capability store.AgentMode
    Summary    string
    Keywords   []string
    ToolNames  []string
    Default    bool
}
```

首批固定 Toolkit:

- `code.files-read`
- `code.files-write`
- `code.git-read`
- `code.git-write`
- `code.lsp`
- `code.skill`
- `code.app`
- `work.camera`

Browser、Terminal 以及安装 App 的 API/MCP 工具使用 session 级 App 加载,不再属于 Toolkit。

### 6.3 默认 Code coding 工具集

Code 默认 coding 工具为 8 个:

- `request_capability`
- `builtin_toolkit_load`
- `builtin_project_inspect`
- `builtin_project_instructions`
- `builtin_command_run`
- `builtin_file_read`
- `builtin_patch_propose`
- `builtin_patch_apply`

此外继续继承 Chat 的 9 个基础工具,所以当前默认合计 17 个;不会为了压低数字
静默移除 Code 模式中的 Chat 能力。

系统 prompt 只放短 Toolkit 索引,不放未加载工具完整 schema。

### 6.4 turn 状态

- Toolkit 按 turn 加载,不写 session 或 SQLite。
- 同一 turn 只允许单调增加,不卸载。
- mode 提升后重新计算可加载 Catalog,不能越过 capability。
- 加载 Toolkit 不等于授权;每次实际调用仍走 Project path 与审批策略。
- `builtin_toolkit_load` 结果进入 canonical parts,便于追溯。
- turn 结束或取消时清理 active set。
- 隐藏但已知的工具调用返回 `tool_not_loaded`,未知工具返回 `unknown_tool`。

### 6.5 缓存稳定

- 默认工具集和 Toolkit 内容必须静态定义。
- ToolDef 始终按 `plugin ID -> tool name` 排序。
- 相同 active set 必须生成字节稳定的 schema JSON。
- 每 turn 最多接受 2 次 Toolkit 扩展。
- 下一 turn 重置为固定默认集,保证基础 prompt cache 前缀稳定。
- 本地定义缓存键使用 `pluginID + version/hash`。

## 7. Engine 改造

工具定义选择已从单纯按能力等级累计过滤拆分为:

```text
All definitions
  -> CatalogForMode(mode)
  -> DefinitionsForTurn(mode, activeToolkits)
  -> provider.Request.Tools
```

主要修改点:

- `internal/tool`:Manifest、Catalog、Toolkit loader 与固定排序。
- `MultiRunner`:聚合 definitions 的同时聚合 manifests。
- `Engine`:增加 `turnID -> active toolkits` 临时状态。
- `buildProviderRequest`:按 turn active set 选择 schema。
- `executePendingTools`:Toolkit load 后标记 toolset changed。
- tool loop:复用现有 mode changed 重建路径,保留 messages 并重建 Tools。
- approval:区分 capability required、tool not loaded 与 unknown tool。
- turn 收尾只清理 Toolkit 临时状态;后台进程跨 turn 继续运行。

Provider adapter 不需要新的协议;OpenAI、Anthropic、Google 继续消费普通
`provider.Request.Tools`。

## 8. Eval 与验收

### 8.1 固定任务

至少建立以下本地 fixture / 真实项目任务:

1. 陌生项目结构分析并读取适用指令。
2. 使用 CLI 定位文本与文件。
3. 修复单文件编译错误并通过最小测试。
4. 完成多文件 Patch Proposal、审批与 Apply。
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
- Toolkit 扩展次数。
- cached input tokens。
- 总耗时与首个有效操作延迟。

### 8.3 删除门槛

- deterministic runner/integration tests 100% 通过。
- 真实模型固定任务至少 8/10 完成。
- 典型 Code turn 新增 coding tools 不超过 15 个;含继承 Chat 工具的总数不超过 24 个。
- tools schema estimated tokens 比当前 Code 基线下降至少 50%。
- 每 turn Toolkit 变化不超过 2 次。
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

- `argv | script` 互斥 schema。
- 固定 shell launcher。
- shell 风险分类与审批详情。
- stdout/stderr delta、合并与最终 canonical 结果。
- transcript 运行态输出。
- cancel、timeout、长输出和非零退出测试。

验收:常见 test/build/search 管道可用,风险命令不会自动越权。

### M2:后台进程,1.5-2 天

状态:已完成(2026-07-12)。

- session-scoped process manager。
- start/poll/stop 工具。
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

### M4:Toolkit 动态加载,3-4 天

状态:已完成(2026-07-12)。加入 `code.app` 后默认 Code 为 17/40 个非 App
schema;schema JSON 从 32585 B 降到 14082 B,减少 56.8%。

- Manifest/Catalog/loader。
- turn active set 与稳定排序。
- tool loop 动态重建。
- capability/approval 安全测试。
- prompt Toolkit 索引、transcript 与 i18n。
- 先覆盖内置工具,再覆盖 Apps/MCP。

验收:默认 Code coding 工具 8 个,常见任务新增 coding tools 不超过 15 个。

### M5:收尾,1-1.5 天

状态:已完成(2026-07-12)。

- 复核删除门槛;真实 Git/LSP 样本不足,本阶段不删除实现,统一留在 lazy toolkit。
- 更新工具使用率报告分组。
- 全量 Go 测试、真实 LSP 集成、Web build、desktop build。
- CLI fixture 与当前仓库构建完成自动验收;真实模型固定任务继续作为后续删除门槛。

验收:无兼容别名、无 migration、无未使用工具展示映射;加入 `code.app` 后默认
Code schema 仍减少 56.8%,现有专用能力可通过 Toolkit 按需恢复。

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
