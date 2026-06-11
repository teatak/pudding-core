# 轨道 E:Web UI

> 背景:daemon 已可运行(`make dev && ./bin/puddingd --mock`,默认
> `127.0.0.1:9670`),REST + SSE 契约已冻结。你的任务是交付第一版多 session
> 文本聊天 UI。通用纪律见 docs/phase-1-plan.md 第 6 节;硬约束见 AGENTS.md
> (前端技术约束一节务必通读)。

## 范围

`web/` 下的完整前端:脚手架、session 列表、transcript、composer、cancel、
settings 页。范围外(不做):消息搜索、虚拟滚动、移动端适配、i18n、E2E 测试。

## 契约(只读,不得修改)

- `web/contracts/events.ts` / `web/contracts/api.ts`:**直接 import 使用,禁止复制粘贴**。
  为什么:契约文件是 Go ↔ TS 的同源镜像,复制出去的副本不会跟着契约更新。
- 端点与错误码:docs/contracts-checklist.md。
- SSE 行为:lifecycle 事件带 `id:`(seq),浏览器 EventSource 重连时自动带
  Last-Event-ID,服务端补发缺口;`turn.delta` 无 id、不补发。

## 技术栈(固定,不另选型)

React + TypeScript + Vite + TanStack Router + TanStack Query + Zustand +
React Hook Form + Zod + Tailwind + shadcn/ui。
shadcn 官方组件一律 `npx shadcn@latest add <component>` 引入;
扩展能力新增业务包裹组件,不改官方组件源码。

## 状态边界(硬规则)

| 层 | 负责 | 不负责 |
| --- | --- | --- |
| Router | URL / 页面位置 / selectedSessionID | 任何后端状态 |
| Query | sessions / messages / settings 快照 | 实时流 |
| Zustand | SSE overlay(delta 累积、turn 状态)+ 本地 UI | canonical messages 长存 |
| RHF + Zod | 表单 | — |

- query key 形状:`["sessions"]`、`["session", id]`、`["session", id, "messages"]`。
- **API 调用必须显式传 sessionID**,禁止从全局 store 隐式取 target。
  为什么:后端没有 focus 概念,这是整个架构的第一原则(AGENTS.md 硬约束 1-4)。
- selectedSessionID 只存在于前端(URL 参数),切换时只做:拉 messages、
  连 events、更新本地 UI,**没有任何后端写操作**。
- localStorage 只存 UI 偏好(主题、布局),不存 messages / token。

## 关键实现指引

1. **transcript = messages query + live overlay**:
   - canonical 来自 `GET /sessions/{id}/messages`(Query 缓存);
   - streaming 中的 assistant 文本按 `turnID` 在 Zustand overlay 累积 delta;
   - 收到 `turn.completed / failed / cancelled` → invalidate messages query,
     **等新数据到达后**再清该 turn 的 overlay(先清会闪空)。
2. **submit 流程**:生成 `clientMessageID`(uuid)→ pending 气泡(overlay)→
   `POST /sessions/{id}/submit`。canonical user message 出现后用
   `clientMessageID` 对账替换 pending 气泡。
   - 409 `turn_running`:提示"正在回复中",不丢用户输入(留在 composer 或排队重试);
   - 200 `duplicate`:静默吸收,不得出现第二个气泡。
   为什么:submit 不直接写 canonical,以 SSE/refetch 为准,重试不产生重复
   (AGENTS.md 硬约束 13)。
3. **SSE hook**:每个打开的 session 一条 EventSource;事件先过
   `sessionEvent.safeParse`(zod),不合法记 console 丢弃;
   断线靠 EventSource 自动重连 + Last-Event-ID 续传,重连成功后
   invalidate messages query 兜底 delta 丢失。
4. **cancel**:streaming 中显示停止按钮 → `POST /sessions/{id}/cancel`;
   半截输出会以 `interrupted: true` 的 message 落库,气泡上给出"已中断"标记。
5. **token**:dev 模式从 `VITE_PUDDING_TOKEN` 环境变量注入(值在
   `~/.pudding-core-dev/daemon.token`),或启动页手动粘贴存 sessionStorage。
   REST 走 `Authorization: Bearer`;SSE 走 `?token=`(EventSource 加不了 header)。
6. Vite dev proxy:`/sessions`、`/settings` → `http://127.0.0.1:9670`。
7. settings 页:`GET /settings` 展示 + `PUT /settings` 保存(RHF + zod),
   第一版就是 key-value 编辑,不做分类导航。

## 验收(对照 docs/technology-decisions.md 第 12 节)

- 双 session 并行:A streaming 时切到 B 提交,两边 transcript 各自正确,互不串。
- 切换 session 无任何后端写请求(network 面板验证)。
- 刷新页面后 transcript 从 messages query 完整恢复。
- devtools offline 几秒再恢复:transcript 不丢条目、不重复渲染。
- streaming 中 cancel:气泡停住并带"已中断"标记,马上可再次 submit。
- 同一条消息快速双击发送:只出现一个气泡(duplicate 吸收)。
- `cd web && npm run build` 通过;基础组件来自 shadcn 官方安装。

## 旧项目可参考资产(可选)

旧仓库 `../pudding-core-old/web/apps/pudding-web/src/` 下有几个**纯展示**资产可搬或参考:

- `transcript/TranscriptTurn.tsx` —— 消息气泡样式(user/assistant/折叠/复制按钮)
- `canvas/markdown.ts` —— 自研 markdown → HTML 渲染(无代码高亮,够第一版)
- `theme/theme.ts` —— Light/Dark/System 三态切换
- `transcript/MascotHint.tsx` —— 状态驱动的吉祥物动画(可选,锦上添花)

**只搬展示层**:旧项目的 store / hook / fetch / 路由(`useSessionTranscript`、
`sessionListStore`、`useRoute` 等)是单会话 focus 形状,一行都不要搬,
那正是本仓库要消灭的架构。

## 禁区

- 不改 `web/contracts/`、不改任何 Go 代码。契约问题 PR 里提,主线改完你 rebase。
- 不新增 Redux / 自研 server cache / 其他状态库。
- 不把 selected session 写到后端,不调用任何不存在于 checklist 的端点。

分支:`track/web`。
