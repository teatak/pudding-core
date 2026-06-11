# 技术选型决策

> 状态:初始方向。  
> 目标:为新的 `pudding-core` 定技术边界,避免从旧项目继承单会话 runtime 形状。

## 1. 产品定位

`pudding-core` 是 Pudding 的新源码主线:

- 本地优先的个人 AI daemon。
- 完全多会话。
- 后端所有业务操作显式指定 `sessionID`。
- 前端只有本地 selected session,后端没有 focus 业务状态。
- Desktop / Web / Tray 都是 daemon 的客户端。

一句话:

```text
Pudding Core = local-first multi-session AI daemon + app core.
```

## 2. 后端

选择:

- Go
- SQLite
- `cart v3` router
- SSE
- WebSocket 只用于 MCP / browser tools / realtime bridge

理由:

- Go 适合本地 daemon、音频硬件层、跨平台打包。
- SQLite 适合 local-first 单机数据。
- `cart v3` 使用整段参数和 `static > parameter > catch-all` 匹配优先级,可直接支持 `/sessions/search` 与 `/sessions/:id` 共存。
- SSE 足够承载 session event stream,比 WebSocket 更简单。
- WebSocket 保留给真正双向协议,不拿来替代普通事件流。

首批后端范围:

- session lifecycle
- text submit
- streaming events
- canonical messages
- OpenAI-compatible text provider
- basic settings

暂不做:

- audio runtime
- MCP tools
- canvas/widgets
- desktop packaging
- release updater

## 3. 前端

选择:

- React
- TypeScript
- Vite
- TanStack Router
- TanStack Query
- Zustand
- React Hook Form
- Zod
- Tailwind
- shadcn/ui

规则:

- 使用 shadcn 官方组件时,必须通过 `npx shadcn@latest add <component>` 引入。
- 扩展 shadcn 能力时新增业务包裹组件,不直接修改官方组件源码。
- 前端维护 `selectedSessionID`,不把 selected/focus 写入后端。
- 前端切换 selected session 只做:
  - 拉 `/sessions/{id}/messages`
  - 连接 `/sessions/{id}/events`
  - 更新本地 UI 状态
- 第一版就引入 Router / Query / Zustand / Form / Zod,避免后续补状态管理造成重构。

状态边界:

| 技术 | 负责 |
| --- | --- |
| TanStack Router | URL / 页面位置 / selected session |
| TanStack Query | REST server state cache |
| Zustand | SSE realtime overlay + 本地 UI 状态 |
| React Hook Form | 表单状态 |
| Zod | API payload / form schema runtime validation |

具体规则:

- Router 只表达前端位置,不写后端 focus。
- Query 管后端快照:
  - `GET /sessions`
  - `GET /sessions/{id}`
  - `GET /sessions/{id}/messages`
  - `GET /settings`
  - `GET /models`
- Zustand 管实时和本地 UI:
  - session event stream
  - per-session runtime overlay
  - transcript live buffer
  - composer drafts
  - pane layout
  - settings dialog open
  - theme
- canonical messages 不长期存 Zustand。
- transcript 渲染 = `messages query` + `live event overlay`。
- submit 不直接写 canonical messages;只允许 pending overlay,最终以 SSE / refetch 为准。
- 所有 query key 必须显式带作用域:

```ts
["sessions"]
["session", sessionID]
["session", sessionID, "messages"]
["session", sessionID, "events-overlay"]
```

- API 调用必须显式传 `sessionID`;禁止从全局 current session store 隐式取 target。
- localStorage 只持久化 UI 偏好,不存 messages。

暂不上:

- Redux
- Electron
- React Query 以外的自研 server cache
- 用 Zustand 承载全部后端快照

## 4. 桌面

选择:

- Wails v3

边界:

- Desktop shell 负责启动 daemon、承载 Web UI、提供系统集成。
- daemon 核心业务协议仍然是 HTTP/SSE/WebSocket。
- desktop native/system capabilities 必须走 Wails bindings。
- Desktop 不拥有 session runtime。

通讯边界:

| 通道 | 用途 |
| --- | --- |
| HTTP REST | 业务请求和快照,如 sessions/messages/settings/model |
| SSE | session-scoped event stream |
| WebSocket | MCP / browser tools / realtime bridge |
| Wails bindings/events | desktop native/system capabilities |

规则:

- session / submit / messages / settings / provider config 走 HTTP REST。
- `/sessions/{id}/events` 走 SSE。
- MCP / browser tools / 需要双向长连接的能力走 WebSocket。
- 文件选择、保存文件、Reveal in Finder/Explorer、外链打开、窗口状态、全屏/titlebar、tray、系统通知、更新安装、native dialogs 走 Wails bindings。
- Wails bindings 不承载核心 session runtime。
- HTTP/SSE/WS 不硬凹 desktop native 能力。

不选择:

- Electron:太重。
- Tauri:当前 Go daemon + Wails 更贴合。

## 5. LLM Provider

第一阶段只做:

- OpenAI-compatible text provider

Provider 规则:

- provider client 不保存跨 turn 事实源。
- 每次请求都由 canonical messages + current input 构造。
- 不做 provider-local history handoff。
- 不做旧接口兼容层。

Provider 输入模型:

```text
system instruction
+ canonical messages
+ tool specs
+ current user input
```

后续再加:

- Google Gemini
- Anthropic
- Ollama
- multimodal input
- realtime/live transport

## 6. 存储

选择:

- SQLite

第一批表:

- `sessions`
- `messages`
- `events`
- `settings`

后续表:

- `attachments`
- `tool_calls`
- `skills`
- `audio_assets`
- `speaker_profiles`

规则:

- `messages` 是 LLM context 的事实源。
- `events` 是 UI replay / debug event log。
- context builder 只读 canonical messages。
- clear / compact / retention 都必须能从 canonical messages 解释。

## 7. API 形状

第一批 API:

```text
POST /sessions
GET  /sessions
GET  /sessions/{id}
POST /sessions/{id}/submit
GET  /sessions/{id}/events
GET  /sessions/{id}/messages
POST /sessions/{id}/model
```

禁止第一阶段出现:

```text
POST /submit
GET  /events
POST /focus
GET  /info
```

说明:

- `/submit` 和 `/events` 没有 session id,会重新引入隐式全局状态。
- `/focus` 是前端 UI 状态,不属于后端。
- `/info` 容易变成大杂烩;需要什么信息就设计明确 endpoint。

Router 规则:

```text
/sessions/search  # static
/sessions/:id     # whole-segment parameter
/files/*path      # final catch-all
```

禁止中段参数:

```text
/user_:name
/con:tact
/files/:name.json
```

## 8. Audio

第一阶段不做音频。

后续音频原则:

- hardware belongs to daemon.
- transport belongs to session.
- mic / speaker / ASR / TTS 都通过 owner/binding 显式路由。
- 切前端 selected session 不改变 audio owner。

候选技术:

- malgo 或 PortAudio
- Sherpa ONNX
- WebRTC AEC
- Kokoro / Edge / macOS say 等 TTS

音频必须在文本多会话架构稳定后再接。

## 9. 第一阶段验收

第一阶段只验收文本多会话:

- A session streaming 时,B session 可以 submit。
- A/B events 不串。
- A/B messages 不串。
- A/B context 不串。
- 前端切 selected session 没有任何后端 runtime side effect。
- provider 请求不依赖 provider-local history。

## 10. 暂缓事项

暂缓:

- audio runtime
- KWS / VAD / AEC
- speaker recognition
- MCP browser tools
- canvas/widgets
- skill system
- release updater
- desktop package
- public website

这些能力可以参考 `pudding-core-old`,但不能直接搬旧 `Runtime` 结构。
