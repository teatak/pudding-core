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
11. streaming 必须可中断:`POST /sessions/{id}/cancel` 与 submit 同批交付。
12. session 事件带单调递增 seq;SSE 必须支持 `Last-Event-ID` 续传。
13. submit 必须带 `clientMessageID`,作为幂等键和 overlay 对账键。
14. assistant 输出 turn 结束才落 canonical message;token delta 不落库。
15. turn 收尾的 canonical message、`turns` 状态、lifecycle events 同一事务写入。
16. daemon 只 bind loopback,所有请求带启动 token。
17. provider 只产出模型流(delta / finish / error);turn lifecycle 事件由 engine 生成。
18. dev 与 release 数据目录严格隔离:dev 用 `~/.pudding-dev`,release 用 `~/.pudding`(仅发布构建);本地构建默认 dev 通道;测试只用临时目录。
19. provider profiles / model metadata 的事实源是 `<home>/config/*.yaml`,不是 SQLite;SQLite 只承载运行数据。

## 后端技术约束

使用:

- Go
- SQLite
- cart v3
- HTTP REST
- SSE
- WebSocket

HTTP/SSE/WS 分工:

- REST:业务请求和快照。
- SSE:`/sessions/{id}/events` session-scoped event stream。
- WebSocket:MCP / browser tools / realtime bridge。

禁止把 WebSocket 当作普通 REST/SSE 的替代品。

## 桌面约束

使用 Electron shell + Go daemon。

通讯边界:

- 核心业务协议走 daemon loopback HTTP REST / SSE / WebSocket。
- desktop native/system capabilities 走 Electron IPC/preload bridge。

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
- pending overlay 与 canonical message 用 `clientMessageID` 对账替换。
- localStorage 只持久化 UI 偏好,不存 messages。
- 新增或改名任何 LLM 可调用工具(内置工具、MCP 工具、App 工具)时,必须同步适配 transcript 的工具显示名与 i18n 文案;对话折叠行不得直接暴露 `snake_case` 工具名。
- 所有加载旋转指示器必须使用 `@/components/Spinner`,禁止新增 Lucide `Loader2`、其他 SVG spinner 或手写重复实现。

shadcn 规则:

- Web UI 使用 shadcn 时,如果官方有组件,必须通过 `npx shadcn@latest add <component>` 引入官方组件。
- Web UI 需要扩展 shadcn 官方组件能力时,必须新增业务包裹组件承载扩展,禁止直接修改官方组件源码。
