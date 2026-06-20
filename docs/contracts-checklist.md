# 契约字段对照 checklist

> 用途:事件协议与 REST payload 的 Go ↔ TS 字段对照(docs/phase-1-plan.md 第 2 节)。  
> 来源:Go = `internal/event/types.go` / `internal/store/store.go`;TS = `web/contracts/`。  
> 规则:改任何一边必须同步另一边并更新本表,契约改动单独提交。

## 事件协议

| kind | seq | 落库 | 专属字段 |
| --- | --- | --- | --- |
| `turn.started` | ✓ | ✓ | `clientMessageID`, `userMessageID` |
| `turn.delta` | — | — | `delta` |
| `turn.completed` | ✓ | ✓ | `assistantMessageID` |
| `turn.failed` | ✓ | ✓ | `error`;有半截输出时 `assistantMessageID` + `interrupted` |
| `turn.cancelled` | ✓ | ✓ | 有半截输出时 `assistantMessageID` + `interrupted` |
| `session.titled` | — | — | `title`;自动标题写回(provisional / LLM 各一次),不落库 |
| `ping` | — | — | — |

公共字段:`sessionID`(全部)、`turnID`(除 ping 与 session.titled)。

SSE 帧格式:lifecycle 事件带 `id: <seq>`;`event: <kind>`;`data: <Event JSON>`。
续传:`Last-Event-ID` header 或 `?after=<seq>`,服务端从 events 表补发缺口。
无位点的全新连接从尾部开始(tail),历史靠 messages 快照,不回放 lifecycle。

## 实体

| 实体 | Go | TS | 字段 |
| --- | --- | --- | --- |
| Session | `store.Session` | `session` | id, title, provider, model, createdAt, updatedAt, running(读取时派生) |
| Message | `store.Message` | `message` | id, sessionID, turnID, role, text, clientMessageID?, interrupted?, createdAt |
| ProviderProfile(设置视图) | `api.providerProfileView` | `providerProfile` | id, displayName, protocol, baseURL, apiKey?, apiKeySet, models |

时间一律 RFC3339 字符串(Go `time.Time` 默认 JSON 编码)。

`protocol` 是**固定枚举**:`openai-compatible | openai-responses | google | anthropic`。新增 protocol 必须
同时落 `registry.SupportedProtocol`(API 校验)、`registry.build`(client 构造)、
web 契约 `providerProfile.protocol` 与设置表单下拉;不在枚举内的 protocol 返回 400。

## REST 请求/响应

| 端点 | 请求 | 响应 | 错误 |
| --- | --- | --- | --- |
| `POST /sessions` | `{title?, provider?, model?}` | 201 Session | — |
| `GET /sessions` | — | `{sessions: []}` | — |
| `GET /sessions/{id}` | — | Session | 404 |
| `PATCH /sessions/{id}` | `{title?, provider?, model?}` | Session | 404 |
| `DELETE /sessions/{id}` | — | 204 | 404 |
| `POST /sessions/{id}/submit` | `{clientMessageID, text}` | 202 `{turnID, userMessageID}`;重复 200 `{duplicate, turnID, userMessageID}` | 400 / 404 / 409 `turn_running` |
| `POST /sessions/{id}/cancel` | — | 202 `{status}` | 404 / 409 `no_running_turn` |
| `GET /sessions/{id}/events` | SSE | event stream | 404 |
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

## settings 约定键

> Go 侧常量:`internal/store/settings_keys.go`。改键名两边同步。
> REST settings 仍是扁平 k=v,value 一律纯字符串;磁盘事实源是
> `<home>/config/settings.yaml`。
> **只放标量偏好**;provider profiles 走 `<home>/config/profiles.yaml` +
> 独立 REST 资源,不进 settings。

| key | 用途 |
| --- | --- |
| `system_prompt` | system instruction,空则用内置默认值 |

session 创建时必须显式写入 `provider` 与 `model`。draft 页可记住"上次选用模型",
但不影响既有 session。
历史上的 `model.default` 与 `provider.openai.*` 过渡键已随 registry 收口删除。

provider model entry 形状:
`{id, displayName?, contextWindow?, capabilities?, limits?, providerOptions?}`。
submit 时 engine 解析成 effective model config,随 turn 写入 `turns.model_config`,
并传入 `provider.Request.Config`;运行中的 turn 不受 profile 后续修改影响。

改 settings 即时生效,不需要重启 daemon;provider 未配置时 submit 会以
`turn.failed` 提示。events 表每 session 保留最近 1000 条 lifecycle 事件。
