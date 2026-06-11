# AGENTS.md

## 回答风格

1. 回答尽量简短。

## 项目定位

`pudding-core` 是新的 Pudding 源码主线。

核心目标:

- local-first
- fully multi-session
- explicit session routing
- no backend focus state
- daemon-owned hardware resources
- session-owned transports and context

旧项目 `pudding-core-old` 只能作为参考实现和踩坑记录,不要直接搬旧 `Runtime` 大结构。

## 架构硬约束

1. 后端没有 `focus` 业务概念。
2. 前端可以有本地 `selectedSessionID`,但不能写入后端成为 runtime 状态。
3. 所有业务 API 必须显式带 `sessionID`。
4. 禁止新增无 session scope 的主路径接口,例如:
   - `POST /submit`
   - `GET /events`
   - `POST /focus`
5. session 是第一等实体,不是 daemon runtime 的附属状态。
6. transport 属于 session,不属于 daemon。
7. hardware 属于 daemon,不属于 session。
8. context 只来自 canonical messages。
9. provider client 不保存跨 turn 事实源。
10. 不做旧接口兼容层。若旧调用方与新结构冲突,迁移调用方后删除旧路径。

## 后端技术约束

使用:

- Go
- SQLite
- cart v3
- HTTP REST
- SSE
- WebSocket

`cart v3` 路由规则:

- 只支持整段参数: `/sessions/:id`
- catch-all 只能是最后一段: `/files/*path`
- 匹配优先级: `static > parameter > catch-all`
- 禁止中段参数:
  - `/user_:name`
  - `/con:tact`
  - `/files/:name.json`

HTTP/SSE/WS 分工:

- REST:业务请求和快照。
- SSE:`/sessions/{id}/events` session-scoped event stream。
- WebSocket:MCP / browser tools / realtime bridge。

禁止把 WebSocket 当作普通 REST/SSE 的替代品。

## 桌面约束

使用 Wails v3。

通讯边界:

- 核心业务协议走 HTTP REST / SSE / WebSocket。
- desktop native/system capabilities 走 Wails bindings/events。

Wails bindings 负责:

- file picker
- save file
- reveal file
- external URL open
- window state
- fullscreen / titlebar
- tray
- system notification
- updater / install
- native dialogs

Wails bindings 不承载核心 session runtime。

## 前端技术约束

使用:

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

状态边界:

- TanStack Router:URL / 页面位置 / selected session。
- TanStack Query:REST server state cache。
- Zustand:SSE realtime overlay + 本地 UI 状态。
- React Hook Form:表单状态。
- Zod:API payload / form schema runtime validation。

规则:

- API 调用必须显式传 `sessionID`。
- 禁止从全局 current session store 隐式取 API target。
- canonical messages 不长期存 Zustand。
- transcript 渲染 = `messages query` + `live event overlay`。
- submit 不直接写 canonical messages;只允许 pending overlay,最终以 SSE / refetch 为准。
- localStorage 只持久化 UI 偏好,不存 messages。

shadcn 规则:

- Web UI 使用 shadcn 时,如果官方有组件,必须通过 `npx shadcn@latest add <component>` 引入官方组件。
- Web UI 需要扩展 shadcn 官方组件能力时,必须新增业务包裹组件承载扩展,禁止直接修改官方组件源码。

## 第一阶段范围

第一阶段只做文本多会话:

- session lifecycle
- text submit
- streaming events
- canonical messages
- OpenAI-compatible text provider
- basic settings

验收:

- A session streaming 时,B session 可以 submit。
- A/B events 不串。
- A/B messages 不串。
- A/B context 不串。
- 前端切 selected session 没有任何后端 runtime side effect。
- provider 请求不依赖 provider-local history。

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
