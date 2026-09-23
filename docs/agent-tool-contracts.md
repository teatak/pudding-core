# Agent 工具调用契约

归属：`pudding-core`。本文说明文件定位、附件分析、后台命令与参数诊断的当前行为。
工具名称和 JSON schema 以 [`internal/tool/builtin.go`](../internal/tool/builtin.go) 为准。
相关样本见 [2026-09-23 问题记录](agent-tooling-issues-2026-09-23.md)。

## 1. 项目路径

`scope` 继续表示工具的文件区，项目操作使用 `scope="project"`。

| 场景 | 规则 |
| --- | --- |
| 一个授权项目 root | 项目文件路径、命令/Git 的 `cwd` 可相对该 root；支持省略 `cwd` 的工具使用该 root |
| 多个授权项目 root | 指向单个目标的文件路径、代码工具路径、命令/Git 的 `cwd` 必须是绝对路径 |
| 多 root 下枚举项目 | `builtin_file_list` 使用 `scope="project", path="."` 返回授权 root 列表 |
| Git 已选定 `cwd` 后的 `paths` | 仍是仓库内路径，不要求每个 Git operand 都是绝对路径 |
| 命令已选定 `cwd` 后的相对文件参数 | 仍相对该 `cwd`，经过现有命令权限检查 |
| `app`、`skill`、`temp` | 保留各自文件区的相对路径语义 |

多 root 不根据“哪个目录里恰好存在这个文件”选择目标，也不默认向首个 root 写入。
预审、执行与项目文件变更追踪共用项目路径解析。

主要错误分别为 `invalid_scope`、`project_dirs_required`、`absolute_path_required`、
`path_not_authorized`、`path_not_found`。通用文件路径解析的非法 scope 返回 `allowedScopes`；多 root 相对路径
返回 `projectRoots` 和改用绝对路径的提示。已授权目录内的文件不存在不会提示申请更多权限。
`file_stat` 对可定位的缺失文件仍返回 `exists=false`。

实现：[projectpath](../internal/projectpath/projectpath.go)、
[文件路径错误](../internal/tool/file.go)、[命令策略](../internal/tool/policy.go)。

## 2. 临时附件与分析产物

导出到临时文件区：

```json
{"scope":"temp","attachmentKey":"<当前会话的附件 key>"}
```

此模式自动分配唯一目录，不接受 `path` 或 `overwrite`，包括显式空值。
结果包含 `scope`、temp 相对 `path`、`absolutePath` 和实际复制的 `bytes`。

后续使用方式：

- 文件工具使用返回的 `scope/path`；媒体工具使用 `source="file"` 加 `scope/path`。
- 命令使用返回的 `absolutePath`；衍生文件写在同一个会话产物目录中。
- 需要保存到项目时，用 `file_copy` 将 temp 文件复制到明确的项目目标。

产物位于现有 `<home>/temp/session-artifacts/<session>/` 下，可跨调用和 turn 复用。
命令预审和 macOS 沙箱只增加当前会话产物目录的读写权限；不会将该目录加入
`ProjectDirs`，也不会将它作为新的项目 `cwd` 或直接可执行文件授权根。
无项目的 Code 会话继续使用现有 Code scratch 作为命令工作目录。

附件 key 仍校验当前会话归属。`temp` 文件工具保留原有全局文件区语义；新增的是附件导出
的会话归属、命令访问范围和清理规则，不将整个 temp 文件区改成逐会话隔离。
归档保留产物，删除会话时清理。清理前关闭新 turn 的入口、取消并等待当前工具结束，避免
删除后被尚未结束的导出重新创建目录。

`scope="project"` 导出仍要求显式 `path`，并保留 `overwrite` 选项。

实现：[附件导出](../internal/tool/attachment_export.go)、
[会话产物目录](../internal/home/session_artifacts.go)、
[命令预审集成](../internal/engine/command_artifacts.go)。

## 3. 后台命令发现

```json
{"action":"list"}
```

`builtin_command_session` 的 `list` 不要求 `process_id`，只接受 `action`。
它返回当前会话由现有后台进程管理器持有的进程快照，不读取全系统进程，也不附带日志。
已结束的进程遵循现有保留和清理规则；结果可能同时包含运行中和仍保留的已结束进程。
将返回的 `processes[].processID` 作为 `process_id`，继续使用原有 `poll`、`write`、`stop`。

实现：[后台进程管理](../internal/tool/background_process.go)。

## 4. 搜索与精确补丁

模型看到的 `file_search` 结果会从真实 `lineStart/lineEnd/line/excerpt` 派生
`numberedExcerpt`，每个上下文行都有明确行号。仅在坐标和行数一致时编号；路径、匹配行、
范围和 `truncated` 等定位信息仍保留。Canonical 结果和 UI 数据格式不变。

`context_lines` 仍为 0–5，越界返回错误，不静默改写参数。搜索结果用于定位；准备补丁时
仍应读取 `file_slice` 的自然顺序完整源行，尤其不能将截断的搜索预览作为 `old_lines`。
`file_patch` 保持精确匹配和整批校验后写入；候选行号用于重新定位，不自动重放候选。

超大工具结果继续使用受限 preview 和 `result_ref` 回读。预览额外保留有界错误分类、
资源数值、路径提示与 recovery 坐标，不把完整源文件重复放回模型上下文。

实现：[模型结果投影](../internal/tool/context_result.go)、[精确补丁](../internal/tool/patch.go)。

## 5. 参数错误与请求诊断

`file_write` 与 `file_patch` 共用严格 JSON 参数检查。错误区分空参数、非对象、非法或
未闭合 JSON、未知字段、缺失字段、类型错误；按适用情况返回 `errorKind`、`field`、
`expected`、`offset` 和 `receivedBytes`。`offset` 指向原始输入字节位置。
显式空字符串 `content` 仍允许清空文件；缺少或 null 内容不会写入。

`truncated_json` 只表示收到的 JSON 未闭合，不能据此推断 provider 输出上限。
补丁实际资源超限另返回 `metric / actual / limit / unit`，分别描述文件大小、hunk/文件
数量、累计源/目标文本或 review diff。`actual` 是拒绝检查点的实测值；累计检查可能在
处理完全部文件之前拒绝，不等于原始 JSON 字节数。

普通 turn 工具循环的诊断日志通过 `sessionID / turnID / providerCallIndex` 关联，
记录 provider/model、最终请求体实际使用的输出限制、流终态/finish reason、usage，
以及当前循环工具参数的字节数、JSON 状态与错误 offset。未发送输出限制时明确记录缺省。
日志不记录参数正文、prompt、凭据或任意上游错误正文；原有 `turn.error` 继续保留原错误。
此处的请求日志范围是 `requestKind=tool_loop`，不包含压缩或标题请求。

实现：[参数错误](../internal/tool/argument_error.go)、
[请求体诊断](../internal/provider/request_log.go)、
[工具循环日志](../internal/engine/provider_log.go)。

## 6. macOS Git 默认 ignore

命令沙箱保留既有 Git 配置边界。对继承环境决定的现有默认全局 ignore 文件增加精确
只读权限，使 Git 能继续应用该文件中的规则；不开放整个用户配置目录。工具传入的环境
覆盖不能借此为任意外部 ignore 路径增加授权。

`xcrun` 缓存噪音尚无本轮可复现证据，未增加猜测性的环境覆盖。

实现：[macOS 命令沙箱](../internal/tool/command_runner_darwin.go)。
