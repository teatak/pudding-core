# Agent 工具调用契约

归属：`pudding-core`。本文说明文件定位、附件分析、后台命令与参数诊断的当前行为。
工具名称和 JSON schema 以 [`internal/tool/builtin.go`](../internal/tool/builtin.go) 为准。
相关样本见 [2026-09-23 问题记录](archive/reports/agent-tooling-issues-2026-09-23.md)。

## 1. 项目路径

`scope` 继续表示工具的文件区，项目操作使用 `scope="project"`。

| 场景 | 规则 |
| --- | --- |
| 一个授权项目 root | 项目文件路径、命令/Git 的 `cwd` 可相对该 root；支持省略 `cwd` 的工具使用该 root |
| 多个授权项目 root | 指向单个目标的文件路径、代码工具路径、命令/Git 的 `cwd` 必须是绝对路径 |
| 多 root 下枚举项目 | `builtin_file_list` 使用 `scope="project", path="."` 返回授权 root 列表 |
| Git 已选定 `cwd` 后的 `paths` | 仍是仓库内路径，不要求每个 Git operand 都是绝对路径 |
| 命令已选定 `cwd` 后的相对文件参数 | 仍相对该 `cwd`，经过现有命令权限检查 |
| `plugin`、`skill`、`temp` | 保留各自文件区的相对路径语义 |
| `widget` | Code 会话以显式 `widget_id` 读取已打开草稿中的 `widget.json`、`src/`、`assets/`；`builtin_file_patch` 每次只编辑一个源码文件并检查 `expectedDraftHash`，返回新的 `draftHash`。隐藏 `.base`、`fixtures/`，不扩大命令或 Git 权限。|

多 root 不根据“哪个目录里恰好存在这个文件”选择目标，也不默认向首个 root 写入。
预审、执行与项目文件变更追踪共用项目路径解析。
授权目录重叠时，Git 使用完整授权集合验证实际仓库，目录排列顺序不改变结果；仅授权
仓库子目录仍不允许操作父仓库。`gitDir`、`commonDir` 和 `index` 也分别需要位于授权
集合内，可以位于另一个显式授权目录。Git 的绝对文件参数接受授权目录的符号链接别名，
末级符号链接仍作为 Git 条目处理，不替换成它指向的文件。

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
`.code` 等保留目录不能通过 `scope="temp"` 访问；符号链接解析后的目标与新建文件的
真实父目录同样受此限制。路径别名不能绕过保留目录检查。
粘贴附件的 `sessions/draft/blobs/...` key 只有在当前会话的 canonical 消息中存在匹配的
`OriginTemp` 附件引用时才能导出；其他会话的引用或仅知道 key 不构成访问授权。
归档保留产物，删除会话时清理。清理前关闭新 turn 的入口、取消并等待当前工具结束，避免
删除后被尚未结束的导出重新创建目录。
归档请求在等待工具退出时取消，归档状态可能已经提交。重试该会话的归档会继续完成
清理，并保留原归档时间；请求等待仍可取消，不承诺断连后自动完成清理。归档重试、
恢复和删除继续由同组生命周期锁串行化。

`scope="project"` 导出仍要求显式 `path`，并保留 `overwrite` 选项。

实现：[附件导出](../internal/tool/attachment_export.go)、
[会话产物目录](../internal/home/session_artifacts.go)、
[命令预审集成](../internal/engine/command_artifacts.go)。

### 一次性验证产物

无需先上传附件即可调用 `builtin_temp_workspace({"action":"open"})`，返回当前会话
scratch 目录的 `scope`、`path` 和 `absolutePath`。文件工具使用 temp 相对路径，命令使用绝对路径。
这适合截图、验证日志和预览副本，不进入项目 diff。保存正式输出后调用 `action="clear"`，
仅清理 scratch，保留导出的附件；存在运行中的后台命令时拒绝清理。会话资源释放或正常关闭
daemon 时自动回收。清理不按 `_preview` 等文件名猜测，也不接受调用方指定目录。

## 3. 后台命令发现

```json
{"action":"list"}
```

`builtin_command_session` 的 `list` 不要求 `process_id`，只接受 `action`。
它返回当前会话由现有后台进程管理器持有的进程快照，不读取全系统进程，也不附带日志。
已结束的进程遵循现有保留和清理规则；结果可能同时包含运行中和仍保留的已结束进程。
将返回的 `processes[].processID` 作为 `process_id`，继续使用原有 `poll`、`write`、`stop`。
macOS、Linux 和 BSD 的后台命令退出时先清理原进程组的剩余成员，再发布完成状态和释放运行配额；长期任务应
保持受管理的前台命令运行，并用 `background=true` 交给工具管理。输出中的 UTF-8 字符
可跨读取块，stdout 与 stderr 分别拼接未完整字符；拒绝启动也会及时释放已创建的管道。

实现：[后台进程管理](../internal/tool/background_process.go)。

前台和后台命令的 `ok` 表示工具调用完成，不表示进程成功。进程结果以 `exitCode`、
可取得的 `signal`、`error` 和超时／取消或显式停止原因表达；无输出的信号终止同样是
已结束的异常退出。`sandboxDenied` 只附带明确的沙箱拒绝文本证据，不覆盖进程原因。
通用 `Operation not permitted` 保留在原始日志中，不能单独判定为沙箱拒绝。

验证元数据只来自静态直接调用；管道、取反、复合 shell 表达式不生成验证结论。
Unix 命令使用 POSIX `/bin/sh`，不会自动切换 shell 或改写执行选项。长前台结果的模型
预览将 stdout／stderr 分别表示为 `head`、`tail`、`chars`、`omitted_chars`，并保留退出
及执行边界元数据。`preview_only` 表示模型预览省略；`stdoutTruncated`／`stderrTruncated`
表示执行输出本身已截断。`result_ref` 回读 canonical 结果，不恢复已被生产端截掉的输出。

## 4. 搜索与精确补丁

模型看到的 `file_search` 结果会从真实 `lineStart/lineEnd/line/excerpt` 派生
`numberedExcerpt`，每个上下文行都有明确行号。仅在坐标和行数一致时编号；路径、匹配行、
范围和 `truncated` 等定位信息仍保留。Canonical 结果和 UI 数据格式不变。

`context_lines` 仍为 0–5，越界返回错误，不静默改写参数。搜索结果用于定位；准备补丁时
仍应读取 `file_slice` 的自然顺序完整源行，尤其不能将截断的搜索预览作为 `old_lines`。
`file_patch` 保持精确匹配和整批校验后写入。每个 hunk 可显式设置 `relocate: true`：
仅在 `start_line` 不匹配或越界时，按非空 `old_lines` 在修改前文件中的唯一精确匹配
重定位；纯插入不能重定位，无匹配或多匹配仍拒绝。指定位置已精确匹配时照常使用，
无需全文件唯一。全部 hunk 解析后重新检查重叠，审批与执行继续绑定同一文件快照。
失败的 `recovery.candidates[]` 各含 `startLine` 和该候选附近最多五行的编号上下文；
最多返回五个候选，完整匹配数及候选／源行截断标记仍保留。候选反馈取代原来的
仅坐标数组。唯一候选的上下文未截断且目标明确时，可直接修正 `start_line` 或设置
`relocate: true` 重试，不强制重读；有歧义、截断或目标不明确时再读取局部源行。

成功结果的 `files[].relocations[]` 仅记录实际移动的 hunk，包含原输入数组中从 1 开始的
`hunk`、请求的 `requestedStartLine` 和解析后的 `startLine`；两个坐标都属于修改前文件。
成功结果的 `files[].lineRanges[]` 从同一份 review diff 派生，描述修改后文件的
`startLine` 和 `lineCount`。`lineCount=0` 是删除边界，可位于新 EOF 后一行；删除整个
文件时数组为空。后续编辑应重新读取这些位置附近的源行，不沿用本批的原始坐标。

超大工具结果继续使用受限 preview 和 `result_ref` 回读。预览额外保留有界错误分类、
资源数值、路径提示与 recovery 坐标（成功结果的重定位列表省略时标记 `relocationsTruncated`），不把完整源文件重复放回模型上下文。

实现：[模型结果投影](../internal/tool/context_result.go)、[精确补丁](../internal/tool/patch.go)。

## 5. 参数错误与请求诊断

`file_write` 与 `file_patch` 共用严格 JSON 参数检查。错误区分空参数、非对象、非法或
未闭合 JSON、未知字段、缺失字段、类型错误；按适用情况返回 `errorKind`、`field`、
`expected`、`offset` 和 `receivedBytes`。`offset` 指向原始输入字节位置。
显式空字符串 `content` 仍允许清空文件；缺少或 null 内容不会写入。
字段名按 JSON schema 精确区分大小写，嵌套对象也拒绝未知字段。补丁 `old_lines` 与
`new_lines` 的成员必须是字符串；`null` 不等于空行。`[]` 和 `[""]` 保留原有语义，
任一文件参数无效时整批拒绝，所有文件保持不变。

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

macOS 默认命令环境按 `DEVELOPER_DIR` 或 `xcode-select --print-path` 选择工具链，
在 PATH 的 `/usr/bin` 位置之前加入所选工具链的真实工具目录，并仅在未指定时设置
同一工具链的 `SDKROOT`。自定义 PATH 的优先目录、显式 SDK／编译器及工具链设置继续
生效；选择发生在审批和可执行文件定位之前，既有授权与实际调用使用同一环境。
仅查询系统 `xcode-select` 与目录元数据，不在宿主执行调用者目录内的发现程序。
没有安装开发工具时不安装或选择替代工具链。命令中显式写出的 `/usr/bin/...` 入口不改写，
其原始错误输出仍保留；开发命令应使用 PATH 解析后的工具或所选工具的真实路径。

pnpm 默认 store 来自既有隔离 `PNPM_HOME`，不再向所有包管理器注入 pnpm 专用的
`npm_config_store_dir`，也不再将这个变量视为隐式授权的沙箱路径。
macOS 隔离的 `COREPACK_HOME` 初始没有宿主的 lastKnownGood；默认设置
`COREPACK_DEFAULT_TO_LATEST=0`，无版本约束的项目沿用隔离缓存的已知版本（空缓存使用
Corepack 内置版本），不自动升级到注册表最新版。项目 `packageManager`、显式版本和调用方的该环境变量仍优先，缓存只在
原有沙箱目录内写入。旧 Corepack 对显式 pnpm 11+ 的入口不兼容需要更新 Corepack，
不通过修改包入口或开放宿主缓存规避；见 [上游修复](https://github.com/nodejs/corepack/pull/776)。

实现：[macOS 命令沙箱](../internal/tool/command_runner_darwin.go)。

## 7. 命令的文件变更记录

前台命令的显式写入目标按执行位置记录。可确定的静态 `cd path && ...` 会将后续相对
目标解析到切换后的目录，保留逻辑 `cd ..` 与物理文件路径的区别；子 shell 的目录变化不影响外层。遇到动态目录或无法确定的
控制流时，不将相对目标猜到初始 `cwd`；绝对目标和本轮已观察的文件仍按既有规则追踪。
这不会扫描整个项目，也不将后台进程的变更归入当前 turn。

实现：[命令变更追踪](../internal/tool/mutation_tracking.go)。

## 8. Git 仓库探测与配置隔离

项目 Git API 与结构化 Git 工具共用仓库探测错误分类。只有 Git 明确报告非仓库，才返回
`not_git_repository`；配置、权限等导致的探测失败返回 `git_discovery_failed`，工具保留
错误详情。Git 不可用、超时和取消分别保留 `git_unavailable`、`timed_out`、`cancelled`。
Git status API 仅对明确非仓库返回 `available=false`；探测执行失败返回错误响应。

Git 子进程保留调用方显式设置的 `GIT_CONFIG_GLOBAL`、`GIT_CONFIG_SYSTEM`、
`GIT_CONFIG_NOSYSTEM` 和 `XDG_CONFIG_HOME`，使沙箱和测试的配置隔离在后续调用中继续
生效。未设置时使用既有 host 配置行为；其他 Git 环境覆盖不因此加入继承白名单。

实现：[Git 仓库探测](../internal/projectgit/repository.go)、
[Git 环境](../internal/projectgit/exec.go)、[结构化 Git 工具](../internal/tool/git.go)。

## 9. 网页搜索与抓取

`builtin_web_search` / `builtin_web_fetch` 归属内置 Web Search & Fetch（`web-search-fetch`）插件。
插件默认启用，Chat / Work / Code 均可按会话显式加载；未加载返回 `plugin_not_loaded`，
关闭后返回 `plugin_disabled`，不会通过旧的 Core 工具入口绕过开关。工具名称和历史结果保持兼容，
不依赖 Desktop runtime，因此定时任务和子会话也可加载使用。`GET /tools/builtin` 不再列出这两个插件工具。

`builtin_web_search` / `builtin_web_fetch` 共用 `config/plugin-connections.yaml` 中
`bindings.web-search-fetch.active` 指向的连接；支持多个 Tavily、DeepSeek 凭据，切换无需重启。
配置入口为 Plugins → Web Search & Fetch → Connections。保存连接不切换、不启用插件；
“设为当前使用”只切换生效连接，启停只更新插件开关。删除当前连接后保持未选择，不跨连接回退。

旧 `config/web.yaml` 一次性迁移并移除，保留上一 release 单条记录的凭据和原选择。
`PATCH /tools/web` 的 `providers` 仍可保存/清除 Key；显式提供任一 `searchProvider` /
`fetchProvider` 切换共用连接，同时提供不同值返回 400，整个请求不写入。
`GET /tools/web` 返回相同的搜索、抓取服务商；旧 `web` ID、技能路径和工具名称继续兼容。

DeepSeek 使用独立的 `/v1/tools/search` 和 `/v1/tools/fetch`。搜索默认返回 5 条、最多 10 条，
`query` 最多 128 个 Unicode 字符；保留 `snippet`、供模型阅读的 `summary`、来源 URL、
日期和排序信息，不生成综合 `answer`。正文按需通过 Fetch 获取并沿用 UTF-8 安全截断。
`depth`、`topic`、`include_answer` 属于 Tavily，DeepSeek 在结果中报告 `ignored_parameters`。

统一区分鉴权、余额、访问限制、无可用内容、限流与超时。408、429、5xx 和网络超时最多重试一次，
总时间受调用方取消与 15 秒超时约束；超过短重试窗口的 `Retry-After` 交回调用方处理。
失败不跨服务商重试。接口参考 [DeepSeek 搜索与抓取](https://api-docs.deepseek.com/zh-cn/guides/search/)。

## 10. 可选的下一步建议

`builtin_set_next_prompt({"text":"帮我补上测试"})` 在 Chat / Work / Code 的工具列表中提供，
用于任务完成后的输入框建议。`text` 是用户可直接发送的指令，规范化为空白分隔的单行文本，
最多 200 个 Unicode 字符；建议本身不授权执行下一步。

模型应在同一次响应中先输出完整正文，再单独调用此工具。引擎在当前响应有非空正文、
且全部调用均为该工具时，保存原生调用和结果后走正常收尾，不追加模型请求；仍处理取消、
新插入的用户输入及未完成的子任务。思考内容、空白或更早响应的正文不满足收尾条件。
混合其他工具时继续正常循环。没有调用或参数无效时，不为补建议单独请求模型。

本地仍保存完整调用、结果和原生状态，供桌面恢复建议。构建后续用户轮次的请求时，
成对过滤该工具的调用与结果；含该工具的原生续传链不回放，改用其余 canonical 正文及
业务工具记录，避免隐藏状态重新带回调用或错配响应。历史压缩也排除建议工具，避免摘要
重新引入建议。当前尚未结束的工具循环仍使用完整调用／结果及原生状态完成协议续接。
系统提示词及工具描述明确说明这些语义：无需等待确认或因历史缺失重复调用，Tab 只填草稿，
用户实际发送后才构成新任务；当前任务答案须完整保留在普通正文中。

桌面从已完成 turn 的末尾工具结果恢复建议，不显示工具卡片；新任务清除旧建议。
只有空输入框显示建议并允许 Tab 填入，填入不自动发送；没有建议时使用默认 placeholder。
复用现有消息与 SSE 工具协议，无新增数据库或客户端契约字段。

## 前端视觉验证入口

Code 模式在 Browser 出现在可用插件列表时，引导加载该插件，通过 `builtin_browser_open`
打开授权项目内的 `file://` HTML/SVG 或本地开发服务器，再用 `builtin_browser_observe`
检查页面、`builtin_browser_screenshot` 返回视口截图附件。修改后复用 tabID 并 reload。
已有可见页不等于插件已加载；单张截图不证明动画连续性。指定动画时间、多帧采样及任意
SVG 几何查询仍不在此工具契约中。
