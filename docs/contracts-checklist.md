# 契约字段对照 checklist

> 用途:事件协议与 REST payload 的 Go ↔ TS 字段对照(docs/phase-1-plan.md 第 2 节)。  
> 来源:Go = `internal/event/types.go` / `internal/store/store.go`;TS = `web/contracts/`。  
> 规则:改任何一边必须同步另一边并更新本表,契约改动单独提交。

## 事件协议

| kind | seq | 落库 | 专属字段 |
| --- | --- | --- | --- |
| `turn.started` | ✓ | ✓ | `clientMessageID`, `userMessageID`, `text` |
| `input.steered` | ✓ | ✓ | `turnID`, `clientMessageID`, `userMessageID`, `text`;输入已被当前 turn 接收并将在下一采样边界生效 |
| `turn.delta` | — | — | `part(text/thought)`, `delta` |
| `turn.tool` | — | — | `callID`, `name`, `phase`, `argsDelta?`, `stream?`, `content?`, `ok?`, `summaryKind?`, `summaryCount?`, `attachments?`;`phase=output` 的 stdout/stderr 只进前端 overlay,最终以 message.parts 兜底 |
| `turn.completed` | ✓ | ✓ | `assistantMessageID` |
| `turn.failed` | ✓ | ✓ | `error`;有半截输出时 `assistantMessageID` + `interrupted` |
| `turn.cancelled` | ✓ | ✓ | 有半截输出时 `assistantMessageID` + `interrupted` |
| `audio.bindings` | — | — | `inputOwner`, `inputMode`, `outputOwner`, `inputLevel`;音频 owner 快照 |
| `audio.input_level` | — | — | `inputLevel`;mic owner 的波形音量 |
| `approval.requested` | — | — | `approvalID`, `approvalKind`(`capability`/`tool_call`), `title`, `reason`, `risk?`, `payload?` |
| `approval.resolved` | — | — | `approvalID`, `approvalKind`(`capability`/`tool_call`), `status`, `reason?`, `payload?` |
| `process.started/finished/stopped/removed` | — | — | `turnID?`, `callID?`, `payload`(BackgroundProcess 快照);前端刷新 REST 快照,不落库 |
| `session.titled` | — | — | `title`;自动标题写回(provisional / LLM 各一次),不落库 |
| `ping` | — | — | — |

公共字段:`sessionID`(全部)、`turnID`(turn / approval 事件)。

SSE 帧格式:lifecycle 事件带 `id: <seq>`;`event: <kind>`;`data: <Event JSON>`。
续传:`Last-Event-ID` header 或 `?after=<seq>`,服务端从 events 表补发缺口。
无位点的全新连接从尾部开始(tail),历史靠 turns 快照,不回放 lifecycle。

## 实体

| 实体 | Go | TS | 字段 |
| --- | --- | --- | --- |
| Session | `store.Session` | `session` | id, title, provider, model, activeMode(chat/work/code), modeLease, projectID?, createdAt, updatedAt, running(读取时派生), backgroundProcessCount(读取时派生) |
| Project | `store.Project` | `project` | id, name, rootDirs, approvalMode, createdAt, updatedAt |
| ConversationTurn | `store.ConversationTurn` | `conversationTurn` | id, sessionID, clientMessageID, status, provider?, model?, mode?, error?, createdAt, updatedAt, messages[], fileChanges[] |
| TurnFileChange | `store.TurnFileChange` | `turnFileChange` | id, sessionID, turnID, rootPath, kind(added/modified/deleted/renamed), originalPath?, path, additions, deletions, binary, tooLarge, oldSize, newSize, oldContent?, newContent? |
| ContentPart | `store.ContentPart` | `contentPart` | type(text/thought/tool_use/tool_result), text?, id?, name?, args?, ok?, content?, summaryKind?, summaryCount?, attachments?(tool_result user-display artifacts) |
| Message | `store.Message` | `message` | id, sessionID, turnID, role, kind?, text, parts[], turnIndex?, clientMessageID?, interrupted?, createdAt |
| QueuedInput | `store.QueuedInput` | `queuedInput` | sessionID, clientMessageID, text, status, provider?, model?, mode?, modelConfig?, turnID?, createdAt, updatedAt |
| ProviderProfile(设置视图) | `api.providerProfileView` | `providerProfile` | id, displayName, protocol, baseURL, apiKey?, apiKeySet, models |

时间一律 RFC3339 字符串(Go `time.Time` 默认 JSON 编码)。

`protocol` 是**固定枚举**:`openai-compatible | openai-responses | google | anthropic`。新增 protocol 必须
同时落 `registry.SupportedProtocol`(API 校验)、`registry.build`(client 构造)、
web 契约 `providerProfile.protocol` 与设置表单下拉;不在枚举内的 protocol 返回 400。

## REST 请求/响应

| 端点 | 请求 | 响应 | 错误 |
| --- | --- | --- | --- |
| `POST /sessions` | `{title?, provider, model, projectID?}` | 201 Session | — |
| `GET /sessions` | — | `{sessions: []}` | — |
| `GET /sessions/{id}` | — | Session | 404 |
| `PATCH /sessions/{id}` | `{title?, provider?, model?, projectID?, activeMode?, modeLease?}` | Session | 404 |
| `DELETE /sessions/{id}` | — | 204 | 404 |
| `POST /sessions/{id}/submit` | `{clientMessageID, text}` | 202 `{turnID, userMessageID}`;重复 200 `{duplicate, turnID, userMessageID}` | 400 / 404 / 409 `turn_running` |
| `POST /sessions/{id}/turns/{turnID}/steer` | `{clientMessageID, text?, parts[]}` | 202 `{turnID, userMessageID}`;重复 200 `{duplicate, turnID, userMessageID}` | 400 / 404 / 409 `turn_not_active` |
| `POST /sessions/{id}/queued-inputs/{clientMessageID}/steer` | `{turnID}` | 202 `{turnID, userMessageID}`;重复 200 `{duplicate, turnID, userMessageID}` | 400 / 404 / 409 `turn_not_active` / `queued_input_editing` |
| `POST /sessions/{id}/cancel` | — | 202 `{status}` | 404 / 409 `no_running_turn` |
| `GET /sessions/{id}/audio/bindings` | — | `{bindings: {inputOwner, inputMode, outputOwner, inputLevel}}` | 404 / 503 |
| `POST /sessions/{id}/audio/input` | `{enabled, mode?: "transcribe" \| "raw"}` | 200 `{ok, bindings}` | 400 / 404 / 409 / 503 |
| `POST /sessions/{id}/audio/output` | `{enabled}` | 200 `{ok, bindings}` | 400 / 404 / 503 |
| `GET /sessions/{id}/approvals` | — | `{approvals: []}` pending approval 快照 | 404 |
| `POST /sessions/{id}/approvals/{approvalID}/approve` | `{scope?: "turn" \| "session", projectDirs?: string[]}` | 202 `{status}` | 404 |
| `POST /sessions/{id}/approvals/{approvalID}/deny` | `{reason?}` | 202 `{status}` | 404 |
| `GET /sessions/{id}/events` | SSE | event stream | 404 |
| `GET /sessions/{id}/turns` | `before?`, `limit?` | `{turns: [], hasMore}` | 404 |
| `GET /sessions/{id}/turns/{turnID}/file-changes/{changeID}` | — | TurnFileChange;按需返回旧/新正文 | 404 |
| `GET /sessions/{id}/messages` | — | `{messages: []}` | 404 |
| `GET /sessions/{id}/processes` | — | `{processes: [{processID,turnID?,callID?,status,running,cwd,argv?,script?,shell?,exitCode?,startedAt,finishedAt?,reason?,error?}]}` | 404 |
| `GET /sessions/{id}/processes/{processID}` | `offset?`, `max_bytes?`, `tail_bytes?` | `{process,output,oldestOffset,nextOffset,tailOffset,truncated,hasMore}` | 400 / 404 |
| `DELETE /sessions/{id}/processes/{processID}` | — | 204 | 404 |
| `GET /sessions/{id}/project/tree` | `rootID?`, `path?` | 无 `rootID` 时返回 `{projectID,roots}`;否则返回当前目录 `{rootID,path,entries,truncated,totalCount}` | 400 / 403 / 404 |
| `GET /sessions/{id}/project/file` | `rootID`, `path` | UTF-8 文本文件 `{rootID,path,name,content,mime,size,mtime,revision}` | 400 / 403 / 404 / 413 / 415 |
| `PUT /sessions/{id}/project/file` | `{rootID,path,content,expectedRevision}` | 原子保存后的文件快照 | 400 / 403 / 404 / 409 / 413 / 415 |
| `POST /sessions/{id}/project/entries` | `{rootID,parentPath,name,type:"file"|"dir"}` | 201 `{rootID,path,name,type}` | 400 / 403 / 404 / 409 |
| `PATCH /sessions/{id}/project/entries` | `{rootID,path,name}` | 重命名后的 `{rootID,path,name,type}` | 400 / 403 / 404 / 409 |
| `DELETE /sessions/{id}/project/entries` | `rootID`, `path` | 204;目录递归删除 | 400 / 403 / 404 |
| `GET /sessions/{id}/project/resources/{rootID}/{path}` | — | 项目内图片资源 | 400 / 403 / 404 / 413 / 415 |
| `GET /settings` | — | `{settings: {}}` | — |
| `PUT /settings` | `{k: v}` | 204 | 400 |
| `GET /providers` | — | `{providers: []}` | — |
| `POST /providers` | `{id, displayName, protocol, baseURL?, apiKey?, models?}` | 201 profile | 400 / 409 `profile_exists` |
| `GET /providers/{name}` | — | profile | 404 |
| `PATCH /providers/{name}` | `{displayName?, protocol?, baseURL?, apiKey?, models?}`,apiKey 非空才覆盖 | 200 profile | 400 / 404 |
| `DELETE /providers/{name}` | — | 204 | 404 |
| `GET /providers/{name}/models` | — | `{models: []}`(代理真实端点,60s 缓存)。**仅配置表单的候选来源**,选择器只显示 profile.models | 404 / 502 |

鉴权:`Authorization: Bearer <token>` 或 `?token=`(EventSource 用),401 统一 `{"error":"unauthorized"}`。

`steer` 只接受 URL 中仍在运行的精确 `turnID`,不会在竞态下静默创建下一 turn 或进入
延迟队列。引导输入作为同一 turn 的 canonical user message 持久化,在当前完整输出后的
安全采样边界加入 provider context。普通 `submit` 在已有 running turn 时仍进入
`queued_inputs`,供 Composer 的“稍后发送”使用。队列消息可原子提升为当前 turn 的引导输入;
提升失败时消息继续留在队列中。

## LLM Tool 契约

| tool | capability | args | result |
| --- | --- | --- | --- |
| `builtin_app_load` | `chat` | `{app_id, skill_id?}` | `{ok, appID, skillID, content, newlyLoaded, alreadyLoaded}`;显式加载 App，失败不修改 session |
| `builtin_app_save` | `code` | `{operation:create|update,app_id,version,files[]}` | 完整文本包经隔离校验后替换已安装 App；失败保留旧版本；不接受凭据 |
| `builtin_command_run` | `code` | `{scope:"project", command:string, cwd?, env?, timeout_ms?, background?, tty?}` | 前台返回 `{ok, command, shell, cwd, exitCode, stdout, stderr, ...}`;`background:true` 返回 `{ok, processID, status, running, command, tty, ...}` |
| `builtin_command_session` | `code` | `{action:"poll"|"write"|"stop", process_id, offset?, max_bytes?, wait_ms?, data?}` | poll 返回有界输出与 offset;write 返回 `bytesWritten`;stop 返回终态 |
| `builtin_git_status` | `code` | `{scope:"project", cwd?}` | `{ok, cwd, repoRoot, head, branch, upstream, detached, ahead, behind, clean, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_git_diff` | `code` | `{scope:"project", cwd?, staged?}` | `{ok, cwd, repoRoot, staged, diff, truncated, files, fileCount, additions, deletions}` |
| `builtin_git_log` | `code` | `{scope:"project", cwd?, limit?}` | `{ok, cwd, repoRoot, commits, count}` |
| `builtin_git_stage` | `code` | `{scope:"project", cwd?, paths:string[]}` | `{ok, status:"staged", cwd, repoRoot, paths, pathCount, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_git_unstage` | `code` | `{scope:"project", cwd?, paths:string[]}` | `{ok, status:"unstaged", cwd, repoRoot, paths, pathCount, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_git_commit` | `code` | `{scope:"project", cwd?, message}` | `{ok, status:"committed", cwd, repoRoot, commit, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_file_patch` | `code` | `{scope:"project", files:[{path, new_text?, edits?, delete?}]}` | `{ok, status:"applied", projectRoot, files, fileCount, additions, deletions, warnings}`;Project Files App 加载后可用，一次调用校验并原子应用整个批次 |

`builtin_command_run` 接受完整 `command`,由固定平台 shell 执行,模型不能指定 shell
executable。cwd 必须位于当前 Project/turn grant 授权目录中;前台默认 timeout 为 60 秒,
最大 10 分钟;stdout/stderr 各保留最多 64 KiB 头尾内容。后台命令没有运行时上限,
通过 `builtin_command_session` 读取、输入或停止;`tty:true` 仅用于需要终端的交互命令。
命令审批由 Project 的 `ask | auto | full` 决定。`auto` 用 shell AST 拆分静态命令段并
逐段应用负面风险规则:未知命令名不会仅因未知而审批;删除、Git 外部写入、发布、凭证、
显式网络工具、提权/系统操作、越界路径、wrapper 和动态 shell 结构仍请求
审批。常规 Git `clone/fetch/pull` 与依赖下载自动允许。安全的自定义环境和后台命令沿用
同一套风险判断,不额外强制审批。

macOS desktop daemon 启动时通过用户交互式登录 shell 捕获一次开发环境快照,仅保留
`PATH`、工具链目录、证书和 SSH Agent socket 等受控白名单变量;捕获失败或超时则
回退到 daemon 进程环境。Agent 命令仍由固定的 `/bin/sh -c` 执行,不会为每条命令
重复加载用户 shell 配置。`PATH` 还会补充 Homebrew 与常见用户工具链目录;
direct/sandbox runner 均必须按这份合并后的环境解析可执行文件,不能依赖 Electron
进程启动时的精简环境。

`ask` 和 `auto` 下,低风险前台与后台 CLI 默认在当前 Project 或 session 临时工作区
沙箱中运行;风险命令经用户批准后,仅该次原始调用绕过 CLI 沙箱。`full` 不套 CLI
沙箱。沙箱拒绝是普通命令结果,通过
`sandboxDenied=true` 展示,不得静默用 `full` 重跑。当前 macOS 实现允许 Project
读写、受控缓存读写、工具链只读、外部出站网络以及本地开发服务器;用户其他目录仍
拒绝,服务监听仅允许 loopback。显式通配监听在 `auto` 下需要审批。
非 macOS 平台在 `ask` / `auto` 下明确返回不支持,不会静默退回直接执行;`full`
保持完整访问语义。

Code 不要求预先绑定 Project。没有 Project 或 turn grant 时,engine 为 session
懒创建 `<home>/temp/code/<sessionID>` 作为唯一项目作用域;它不创建 Project 记录,
并在删除 session 时清理。

后台进程归属 session,每 session 最多 4 个、全局最多 32 个。普通进程支持串行 stdin,
交互进程可显式分配 PTY;单次输入最多 64 KiB。stdout/stderr 共用 1 MiB 有界 ring
buffer,通过 offset 增量读取。运行中的进程不因
无 poll 过期;结束结果保留 30 分钟。显式 stop、session 删除与 daemon shutdown
会终止整个子进程组。后台进程保留启动时的 Project roots 和沙箱模式快照;后续
能力、审批模式或 Project 变更不限制或终止已启动进程。

Git 只读工具固定禁用 pager、external diff、textconv 与可选索引写入。工具先解析
仓库根，并确认仓库根仍在当前 Project 授权目录内。`builtin_git_diff` 的 patch
最多保留 128 KiB 头尾内容，`files` 与增删统计不随 patch 截断。

Git 写工具只接受固定结构参数和显式 literal file paths,不开放任意 Git 参数。
写入前必须额外确认 git dir/common dir/index 位于 Project 授权目录。stage 不执行
仓库 clean filters;commit 不执行 hooks 或签名,且审批 payload 必须包含 staged
status/diff 摘要。commit 执行前重新校验审批时记录的 HEAD + index 指纹。第一版
不提供 push/reset/clean/amend。

transcript 对 command、Git、file tool result 使用结构化 renderer。折叠标题必须使用
i18n 显示名和结构化摘要,不得把内部 snake_case tool name 作为主显示。原始 args /
result 只在“原始数据”二级 disclosure 展开后渲染。

`builtin_file_patch` 每个 file 必须且只能提供 `new_text`、`edits` 或
`delete:true` 中的一种;只支持同一 Project root 内的 UTF-8 regular file,单次最多
16 个文件。审批前根据本次 tool call 生成完整 unified diff 和源文件 hash 快照,
审批通过后消费同一快照;模型无需再次提交 patch id。任一文件漂移则整包拒绝。
apply 先准备同目录临时文件,再通过 backup + rename 提交,失败时逆序回滚。修改完成后
由 Turn 文件 Diff 记录本轮产物,供 transcript 与项目浏览器统一审阅。

## settings 约定键

> REST settings 仍是扁平 k=v,value 一律纯字符串;磁盘事实源是
> `<home>/config/settings.yaml`。
> **只放标量偏好**;provider profiles 走 `<home>/config/profiles.yaml` +
> 独立 REST 资源,web tools 配置走 `<home>/config/web.yaml` + 独立 REST
> 资源,都不进 settings。主对话 system instruction 由 `internal/prompt`
> 组装,用户补充提示词读取 `<home>/pudding.md`,不读取 `settings.yaml`
> 的 `system_prompt`。

| key | 用途 |
| --- | --- |
| — | 当前无主路径设置键 |

session 创建时必须显式写入 `provider` 与 `model`。能力档为
`chat < work < code`;普通会话默认 `activeMode=chat, modeLease=none`,由 Project
创建的会话默认 `activeMode=code, modeLease=session`。session scope 能力审批持久更新
activeMode,turn scope 只影响当前 turn。
draft 页可记住"上次选用模型",
但不影响既有 session。
历史上的 `model.default` 与 `provider.openai.*` 过渡键已随 registry 收口删除。

provider model entry 形状:
`{id, displayName?, contextWindow?, capabilities?, limits?, providerOptions?}`。
submit 时 engine 解析成 effective model config,随 turn 写入 `turns.model_config`,
并传入 `provider.Request.Config`;运行中的 turn 不受 profile 后续修改影响。

改 settings 即时生效,不需要重启 daemon;provider 未配置时 submit 会以
`turn.failed` 提示。events 表每 session 保留最近 1000 条 lifecycle 事件。
