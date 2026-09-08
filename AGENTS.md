# AGENTS.md

## 回答风格

1. 回答尽量简短。完成后说明改了什么、验证结果及尚未完成的事项，不把计划或未执行的检查写成已完成。

## 实现原则

1. 修复问题先通过复现、日志、测试或调用链证明根因，再修改；新增功能先明确行为边界和验收条件。
2. 同一业务事实只能有一个权威来源；缓存和 UI overlay 不得成为独立事实源。
3. 新方案替代旧方案时,删除旧路径,不保留双轨。
4. 没有真实失败证据,不添加 fallback、额外状态或兼容分支。
5. 每次修改优先选择状态、分支和机制最少的方案。
6. 完成后检查并删除临时补丁和冗余代码。

## 项目定位

`pudding-core` 是新的 Pudding 源码主线。

产品采用 local-first、完整多会话的 Electron 桌面架构；会话路由、状态和资源归属见下方硬约束。

旧项目 `pudding-core-old` 只能作为参考实现和踩坑记录,不要直接搬旧 `Runtime` 大结构。

按任务需要查阅 [文档索引](docs/README.md)。历史计划和发布报告用于追溯，不直接当作当前行为或待办；发现代码与硬约束冲突时明确指出。

## 架构硬约束

以下编号被源码注释引用，修改时保持现有编号稳定。

1. 后端没有 `focus` 业务概念。
2. 前端可以有本地 `selectedSessionID`,但不能写入后端成为 runtime 状态。
3. 所有针对具体会话的业务 API 必须显式带 `sessionID`，调用方不得从全局 current session store 隐式获取目标。项目、全局配置及会话创建/列表接口按自身资源范围定义，不附加虚构的会话归属。
4. 禁止新增无 session scope 的主路径接口,例如:
   - `POST /submit`
   - `GET /events`
   - `POST /focus`
5. session 是第一等实体,不是 daemon runtime 的附属状态。
6. transport 属于 session,不属于 daemon。
7. hardware 属于 daemon,不属于 session。
8. 跨 turn 的对话历史只来自 canonical messages，不能从 UI overlay 或 provider 内存恢复历史；系统指令、工具定义与项目配置由各自明确的配置来源构建。
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
20. 产品只支持 Electron 桌面端,停止移动端产品开发与兼容;新功能不得新增移动端专用分支,改动触及现有移动端兼容代码时可直接删除。

## 后端技术约束

使用 Go、SQLite、cart v3，以及 HTTP REST / SSE / WebSocket。

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

测试约束:

- 桌面 UI 开发和回归测试前先核对目标环境,避免把已安装的 `com.teatak.pudding` 发布版(daemon `9669`)误当成当前源码启动的开发版。需要测试开发版时,确认页面来自当前 Vite dev server;使用桌面自动化时按可执行文件路径选择 `web/node_modules/electron/dist/Electron.app`。

## 前端技术约束

使用 React、TypeScript、Vite、TanStack Router / Query、Zustand、React Hook Form、Zod、Tailwind、shadcn/ui。

状态边界:

- TanStack Router:URL / 页面位置 / selected session。
- TanStack Query:REST server state cache。
- Zustand:SSE realtime overlay + 本地 UI 状态。
- React Hook Form:表单状态。
- Zod:API payload / form schema runtime validation。

规则:

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

## 验证与交付

按实际改动选择相关检查；检查通过后，只在新增改动、失败或未解决问题需要时扩大或重复验证。纯文档修改检查内容、引用和 diff，无需运行应用全量测试。

| 改动范围 | 验证入口 |
| --- | --- |
| Go 局部逻辑 | `go test -tags 'sqlite_fts5 webrtcaec' ./internal/<相关包>` |
| Go 跨模块或发布验收 | `make test` |
| SQLite 结构或迁移 | `make schema-check`，以及 SQLite 存储测试 |
| Web 逻辑或界面 | `npm --prefix web test`、`npm --prefix web run build`；交互改动补真实桌面回归 |
| Electron shell | `npm run test:electron`；原生行为补对应 smoke 场景 |

- 持久化结构变化同步更新 schema、迁移和版本指纹；已发布迁移与指纹不可改写。迁移测试使用临时数据库，覆盖旧数据保留、失败回滚及重启行为。
- 开发启动入口是 `make desktop-dev`；脚本会重启开发 daemon，运行前检查是否会中断正在执行的任务。端口以启动脚本和实际进程为准，不能仅凭端口或窗口标题认定测试环境。
- 完成前执行 `git diff --check`，检查新增文件及旧路径残留，保留与本次任务无关的已有改动。
