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
| `ping` | — | — | — |

公共字段:`sessionID`(全部)、`turnID`(除 ping)。

SSE 帧格式:lifecycle 事件带 `id: <seq>`;`event: <kind>`;`data: <Event JSON>`。
续传:`Last-Event-ID` header 或 `?after=<seq>`,服务端从 events 表补发缺口。
无位点的全新连接从尾部开始(tail),历史靠 messages 快照,不回放 lifecycle。

## 实体

| 实体 | Go | TS | 字段 |
| --- | --- | --- | --- |
| Session | `store.Session` | `session` | id, title, provider, model, createdAt, updatedAt |
| Message | `store.Message` | `message` | id, sessionID, turnID, role, text, clientMessageID?, interrupted?, createdAt |
| ProviderProfile(脱敏视图) | `api.providerProfileView` | `providerProfile` | name, type, baseURL, apiKeySet, extra?, createdAt, updatedAt |

时间一律 RFC3339 字符串(Go `time.Time` 默认 JSON 编码)。

## REST 请求/响应

| 端点 | 请求 | 响应 | 错误 |
| --- | --- | --- | --- |
| `POST /sessions` | `{title?, model?}` | 201 Session | — |
| `GET /sessions` | — | `{sessions: []}` | — |
| `GET /sessions/{id}` | — | Session | 404 |
| `PATCH /sessions/{id}` | `{title?, model?}` | Session | 404 |
| `DELETE /sessions/{id}` | — | 204 | 404 |
| `POST /sessions/{id}/submit` | `{clientMessageID, text}` | 202 `{turnID, userMessageID}`;重复 200 `{duplicate, turnID, userMessageID}` | 400 / 404 / 409 `turn_running` |
| `POST /sessions/{id}/cancel` | — | 202 `{status}` | 404 / 409 `no_running_turn` |
| `GET /sessions/{id}/events` | SSE | event stream | 404 |
| `GET /sessions/{id}/messages` | — | `{messages: []}` | 404 |
| `GET /settings` | — | `{settings: {}}` | — |
| `PUT /settings` | `{k: v}` | 204 | 400 |
| `GET /providers` | — | `{providers: []}`(脱敏) | — |
| `POST /providers` | `{name, type, baseURL?, apiKey?, extra?}` | 201 profile | 400 / 409 `profile_exists` |
| `GET /providers/{name}` | — | profile(脱敏) | 404 |
| `PATCH /providers/{name}` | `{type?, baseURL?, apiKey?, extra?}`,apiKey 非空才覆盖 | 200 profile | 400 / 404 |
| `DELETE /providers/{name}` | — | 204 | 404 |

鉴权:`Authorization: Bearer <token>` 或 `?token=`(EventSource 用),401 统一 `{"error":"unauthorized"}`。

## settings 约定键

> Go 侧常量:`internal/store/settings_keys.go`。改键名两边同步。
> settings 是扁平 k=v,一键一行,value 一律纯字符串(非 JSON/YAML 文档);
> 键名里的点号只是命名约定,不是嵌套结构。`PUT /settings` 按键 merge,不整体覆盖。
> **只放标量偏好**;结构化实体(如 provider profiles)走独立表 + 独立 REST 资源,
> 不进 settings(technology-decisions 第 5 节)。

| key | 用途 |
| --- | --- |
| `system_prompt` | system instruction,空则用内置默认值 |
| `provider.default` | session.provider 为空时的默认 profile 名,再空回落 `default` |
| `model.default` | 模型解析:`session.model` > 此键 > `--model` flag |
| `provider.openai.base_url` | **过渡键**:`default` profile 不存在时的兜底(env `PUDDING_OPENAI_BASE_URL` 再兜底);轨道 E3 落地后删除 |
| `provider.openai.api_key` | 同上,过渡键 |

改 settings 即时生效,不需要重启 daemon;provider 未配置时 submit 会以
`turn.failed` 提示。events 表每 session 保留最近 1000 条 lifecycle 事件。
