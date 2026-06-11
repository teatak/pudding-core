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
- turn cancel
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
- submit 必须带客户端生成的 `clientMessageID`;pending overlay 与 canonical message 用 `clientMessageID` 对账替换。
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

风险:

- Wails v3 仍处 alpha,API 可能变动。第一阶段不做 desktop packaging,升级风险后置;引入时锁定版本,升级单独走 PR。

## 5. LLM Provider

第一阶段只做:

- OpenAI-compatible text provider

Provider 规则:

- provider client 不保存跨 turn 事实源。
- provider 只产出模型流(`delta / finish / error`);turn lifecycle 事件由 engine 生成,provider 不拥有业务生命周期。
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
- OpenAI Responses API:只允许无状态模式(`store: false`、每轮全量 input);禁用 `previous_response_id` 等服务端会话状态。reasoning 连续性走 encrypted reasoning items 落 canonical message metadata。
- multimodal input
- realtime/live transport

## 6. 存储

选择:

- SQLite

第一批表:

- `sessions`
- `turns`
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
- `turns` 承载 turn 状态(`running / completed / failed / cancelled`);cancel、并发 409、幂等判断都查 `turns`,turn 状态不塞进 messages / events。
- `events` 是 UI replay / debug event log。
- context builder 只读 canonical messages。
- clear / compact / retention 都必须能从 canonical messages 解释。

写入规则:

- `messages` 存 `clientMessageID`(per-session 唯一索引),承载 submit 幂等。
- assistant 输出进行中只走 SSE + 内存 buffer,不逐 delta 写库;turn 结束一次性写一条 canonical message。
- token delta 不落 `events` 表;`events` 只存粗粒度 lifecycle 事件。
- turn 收尾时 canonical message、`turns` 状态、lifecycle events 在同一事务写入,不允许三者状态不一致。
- SQLite 开 WAL;写入走单 writer,避免并发写冲突。

retention:

- `messages` 不自动清理,只能由显式 clear / compact 改变。
- `events` 按条数或天数滚动清理;只需保住 SSE 断线续传窗口 + 近期 debug 回放。

compaction 形状(后续,先定形不实现):

- compact / summarize 的产物必须落为 canonical message(独立 role,如 `summary`)。
- 不允许 context builder 读 canonical messages 之外的事实源,compaction 也不例外。

## 7. API 形状

第一批 API:

```text
POST   /sessions
GET    /sessions
GET    /sessions/{id}
PATCH  /sessions/{id}          # session 属性:title / model 等
DELETE /sessions/{id}
POST   /sessions/{id}/submit   # body 必须带 clientMessageID
POST   /sessions/{id}/cancel   # 中断当前 turn
GET    /sessions/{id}/events   # SSE,支持 Last-Event-ID 续传
GET    /sessions/{id}/messages
GET    /settings
PUT    /settings
```

说明:

- model 是 session 属性,走 `PATCH /sessions/{id}`,不单设 `POST /sessions/{id}/model`。
- submit 的 `clientMessageID` 同时是 idempotency key:重复提交返回已有 message,不重复触发 turn。
- streaming 必须可中断,`cancel` 与 submit 同批交付,不后补。

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

## 8. 事件协议

turn 状态机:

```text
turn.started → turn.delta* → turn.completed | turn.failed | turn.cancelled
```

规则:

- 每个 session 的事件带单调递增 `seq`,作为 SSE `id` 字段。
- SSE 支持 `Last-Event-ID` 续传;断线重连后从 `events` 表补发缺口。
- `turn.delta` 只走 SSE,不落库;其余 lifecycle 事件落 `events` 表。
- UI overlay 的丢弃时机:收到 `turn.completed`(或 failed / cancelled)且对应 canonical message 可见后,删除该 turn 的 overlay。
- 同一 session 的 events 对所有订阅者广播;daemon 不跟踪"哪个客户端在看"。多客户端同时打开同一 session 是显式支持的能力。

## 9. 安全

- daemon 只 bind loopback。
- daemon 启动时生成 token,所有 HTTP/SSE/WS 请求必须带 token;Wails 启动 daemon 时注入,Web UI 通过启动页握手获取。
- 第一阶段 provider API key 存 SQLite 明文,数据库文件权限 0600;后续评估走 Wails bindings 接系统 keychain。

## 10. 数据目录与通道隔离

两个运行通道,数据目录完全隔离:

| 通道 | home | 默认端口 |
| --- | --- | --- |
| release | `~/.pudding` | `127.0.0.1:9669` |
| dev | `~/.pudding-core-dev` | `127.0.0.1:9670` |

与旧版的关系:

- 旧版已占用 `~/.pudding`(正式)和 `~/.pudding-dev`(旧 dev),新项目对这两个目录**只读都不允许**,完全不接触。
- 新项目开发期一律落在 `~/.pudding-core-dev`,与旧版可并行使用、互不影响。
- release 通道的 `~/.pudding` 只在新版正式替换旧版时启用;届时旧数据按 pre-launch 策略处理(不做迁移,显式切换)。

home 解析顺序:

1. `--home` flag
2. `PUDDING_HOME` 环境变量
3. 构建通道默认值

通道由 ldflags 在构建时注入;**本地 `go build` / `go run` 默认 dev**,release 通道只由发布构建注入。这样开发期误操作永远落在 `~/.pudding-dev`,碰不到正式数据。

home 内容(第一阶段):

```text
<home>/
  pudding.db      # SQLite(含 WAL/SHM)
  daemon.token    # 启动 token
  logs/
```

规则:

- 隔离是绝对的:dev 进程不读写 `~/.pudding` / `~/.pudding-dev`(旧版目录),release 进程不读写 `~/.pudding-core-dev`;不做自动迁移或同步。
- 默认端口按通道错开,两个通道的 daemon 可同时运行,token 各自独立。
- 第一阶段不引入 `config.yaml` / `configs/` 分层文件:bootstrap 参数走 flag/env,业务 settings 走 SQLite `settings` 表。旧仓库的 yaml fragments + legacy override 分层是模板漂移的源头,不再重演。
- 测试一律用临时目录(`t.TempDir()`),禁止触碰任何真实 home。
- dev home 的数据可随时整目录删除重建,不承诺任何保留。

## 11. Audio

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

## 12. 第一阶段验收

第一阶段只验收文本多会话:

- A session streaming 时,B session 可以 submit。
- A/B events 不串。
- A/B messages 不串。
- A/B context 不串。
- 前端切 selected session 没有任何后端 runtime side effect。
- provider 请求不依赖 provider-local history。
- streaming 中可以 cancel,session 立即可再次 submit。
- daemon 重启后 session 列表 / messages / context 完整恢复,不依赖丢失的内存态。
- SSE 断线重连后 transcript 不丢事件、不重复渲染。
- 同一 clientMessageID 重复 submit 不产生重复 message / 重复 turn。

## 13. 暂缓事项

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

## 14. 开放问题

- cancel / failed 时的半截 assistant 输出怎么处理:倾向保留为 canonical message 并标记 `interrupted`(进入后续 context),而不是丢弃;待第一阶段实测再定。
- 同一 session 是否允许并发 turn:第一阶段不允许,streaming 中再 submit 返回 409;后续是否放开排队待定。
