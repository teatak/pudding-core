# 契约字段对照 checklist

> 用途:事件协议与 REST payload 的 Go ↔ TS 字段对照(docs/phase-1-plan.md 第 2 节)。  
> 来源:Go = `internal/event/types.go` / `internal/store/store.go`;TS = `web/contracts/`。  
> 规则:改任何一边必须同步另一边并更新本表,契约改动单独提交。

## 事件协议

| kind | seq | 落库 | 专属字段 |
| --- | --- | --- | --- |
| `turn.started` | ✓ | ✓ | `clientMessageID`, `userMessageID`, `text` |
| `turn.delta` | — | — | `part(text/thought)`, `delta` |
| `turn.tool` | — | — | `callID`, `name`, `phase`, `argsDelta?`, `ok?`, `content?`, `summaryKind?`, `summaryCount?`, `attachments?`;最终以 message.parts 兜底 |
| `turn.completed` | ✓ | ✓ | `assistantMessageID` |
| `turn.failed` | ✓ | ✓ | `error`;有半截输出时 `assistantMessageID` + `interrupted` |
| `turn.cancelled` | ✓ | ✓ | 有半截输出时 `assistantMessageID` + `interrupted` |
| `audio.bindings` | — | — | `inputOwner`, `outputOwner`, `inputLevel`;音频 owner 快照 |
| `audio.input_level` | — | — | `inputLevel`;mic owner 的波形音量 |
| `approval.requested` | — | — | `approvalID`, `approvalKind`(`capability`/`skill_draft`/`tool_call`), `title`, `reason`, `risk?`, `payload?` |
| `approval.resolved` | — | — | `approvalID`, `approvalKind`(`capability`/`skill_draft`/`tool_call`), `status`, `reason?`, `payload?` |
| `session.titled` | — | — | `title`;自动标题写回(provisional / LLM 各一次),不落库 |
| `ping` | — | — | — |

公共字段:`sessionID`(全部)、`turnID`(turn / approval 事件)。

SSE 帧格式:lifecycle 事件带 `id: <seq>`;`event: <kind>`;`data: <Event JSON>`。
续传:`Last-Event-ID` header 或 `?after=<seq>`,服务端从 events 表补发缺口。
无位点的全新连接从尾部开始(tail),历史靠 turns 快照,不回放 lifecycle。

## 实体

| 实体 | Go | TS | 字段 |
| --- | --- | --- | --- |
| Session | `store.Session` | `session` | id, title, provider, model, activeMode(chat/project), modeLease, projectID?, createdAt, updatedAt, running(读取时派生) |
| Project | `store.Project` | `project` | id, name, rootDirs, approvalMode, createdAt, updatedAt |
| ConversationTurn | `store.ConversationTurn` | `conversationTurn` | id, sessionID, clientMessageID, status, provider?, model?, mode?, error?, createdAt, updatedAt, messages[] |
| ContentPart | `store.ContentPart` | `contentPart` | type(text/thought/tool_use/tool_result), text?, id?, name?, args?, ok?, content?, summaryKind?, summaryCount? |
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
| `POST /sessions/{id}/cancel` | — | 202 `{status}` | 404 / 409 `no_running_turn` |
| `GET /sessions/{id}/audio/bindings` | — | `{bindings: {inputOwner, outputOwner, inputLevel}}` | 404 / 503 |
| `POST /sessions/{id}/audio/input` | `{enabled}` | 200 `{ok, bindings}` | 400 / 404 / 503 |
| `POST /sessions/{id}/audio/output` | `{enabled}` | 200 `{ok, bindings}` | 400 / 404 / 503 |
| `GET /sessions/{id}/approvals` | — | `{approvals: []}` pending approval 快照 | 404 |
| `POST /sessions/{id}/approvals/{approvalID}/approve` | `{scope?: "turn" \| "session", projectDirs?: string[]}` | 202 `{status}` | 404 |
| `POST /sessions/{id}/approvals/{approvalID}/deny` | `{reason?}` | 202 `{status}` | 404 |
| `GET /sessions/{id}/events` | SSE | event stream | 404 |
| `GET /sessions/{id}/turns` | `before?`, `limit?` | `{turns: [], hasMore}` | 404 |
| `GET /sessions/{id}/messages` | — | `{messages: []}` | 404 |
| `GET /settings` | — | `{settings: {}}` | — |
| `PUT /settings` | `{k: v}` | 204 | 400 |
| `GET /providers` | — | `{providers: []}` | — |
| `POST /providers` | `{id, displayName, protocol, baseURL?, apiKey?, models?}` | 201 profile | 400 / 409 `profile_exists` |
| `GET /providers/{name}` | — | profile | 404 |
| `PATCH /providers/{name}` | `{displayName?, protocol?, baseURL?, apiKey?, models?}`,apiKey 非空才覆盖 | 200 profile | 400 / 404 |
| `DELETE /providers/{name}` | — | 204 | 404 |
| `GET /providers/{name}/models` | — | `{models: []}`(代理真实端点,60s 缓存)。**仅配置表单的候选来源**,选择器只显示 profile.models | 404 / 502 |

鉴权:`Authorization: Bearer <token>` 或 `?token=`(EventSource 用),401 统一 `{"error":"unauthorized"}`。

## LLM Tool 契约

| tool | capability | args | result |
| --- | --- | --- | --- |
| `builtin_command_run` | `project` | `{scope:"project", argv:string[], cwd?, env?, timeout_ms?}` | `{ok, argv, cwd, exitCode, stdout, stderr, stdoutTruncated, stderrTruncated, timedOut, cancelled, durationMs, reason?, error?}` |
| `builtin_git_status` | `project` | `{scope:"project", cwd?}` | `{ok, cwd, repoRoot, head, branch, upstream, detached, ahead, behind, clean, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_git_diff` | `project` | `{scope:"project", cwd?, staged?}` | `{ok, cwd, repoRoot, staged, diff, truncated, files, fileCount, additions, deletions}` |
| `builtin_git_log` | `project` | `{scope:"project", cwd?, limit?}` | `{ok, cwd, repoRoot, commits, count}` |
| `builtin_git_stage` | `project` | `{scope:"project", cwd?, paths:string[]}` | `{ok, status:"staged", cwd, repoRoot, paths, pathCount, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_git_unstage` | `project` | `{scope:"project", cwd?, paths:string[]}` | `{ok, status:"unstaged", cwd, repoRoot, paths, pathCount, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_git_commit` | `project` | `{scope:"project", cwd?, message}` | `{ok, status:"committed", cwd, repoRoot, commit, files, fileCount, stagedCount, unstagedCount, untrackedCount, conflictedCount}` |
| `builtin_patch_propose` | `project` | `{scope:"project", files:[{path, new_text?, delete?}]}` | `{ok, status:"proposed", proposalID, projectRoot, files, fileCount, additions, deletions, diff, expiresAt}` |
| `builtin_patch_apply` | `project` | `{proposal_id}` | `{ok, status:"applied", proposalID, projectRoot, files, fileCount, additions, deletions, warnings}` |

`builtin_command_run` 不解析 shell 字符串。cwd 必须位于当前 Project/turn grant
授权目录中;默认 timeout 为 60 秒,最大 10 分钟;stdout/stderr 各保留最多
64 KiB 头尾内容。命令审批由 Project 的 `ask | auto | full` 决定。

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

`builtin_patch_propose` 每个 file 必须且只能提供完整 `new_text` 或
`delete:true`;第一版只支持同一 Project root 内的 UTF-8 regular file,单次最多
16 个文件。proposal 为 session-scoped daemon memory,TTL 2 小时。生成 proposal
不写文件;`builtin_patch_apply` 审批 payload 必须携带完整 unified diff。审批前和
apply 前均校验源文件 hash;任一文件漂移则整包拒绝。apply 先准备同目录临时文件,
再通过 backup + rename 提交,失败时逆序回滚。

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

session 创建时必须显式写入 `provider` 与 `model`。能力档为 `chat` / `project`;
无授权默认 `activeMode=chat, modeLease=none`;绑定 Project 或 session scope
审批通过后写入 `activeMode=project, modeLease=session`。
draft 页可记住"上次选用模型",
但不影响既有 session。
历史上的 `model.default` 与 `provider.openai.*` 过渡键已随 registry 收口删除。

provider model entry 形状:
`{id, displayName?, contextWindow?, capabilities?, limits?, providerOptions?}`。
submit 时 engine 解析成 effective model config,随 turn 写入 `turns.model_config`,
并传入 `provider.Request.Config`;运行中的 turn 不受 profile 后续修改影响。

改 settings 即时生效,不需要重启 daemon;provider 未配置时 submit 会以
`turn.failed` 提示。events 表每 session 保留最近 1000 条 lifecycle 事件。
