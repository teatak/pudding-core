# 技术选型与当前架构

> 核对日期：2026-09-27，依据两仓当前源码与公共契约。保留原章节编号，供源码注释引用。
> 第一阶段的范围、估算和旧方案见[历史决策](archive/plans/technology-decisions-phase-1.md)；未完成事项只维护在[跨仓待办](backlog.md)。

## 1. 产品定位

Pudding 是 local-first、多会话的 Electron 桌面产品：

- `pudding-core` 维护 Go daemon、公共契约、持久化与工具执行。
- `pudding-desktop` 维护 React UI、Electron shell、macOS Helper 与应用发布。
- 产品只支持 Electron 桌面端；独立 daemon API 不等于独立 Web 或移动端产品。
- 会话业务调用显式携带 `sessionID`；项目、全局配置、会话创建与列表按各自资源范围定义。
- 后端没有 focus/current session 业务状态；前端选中的会话只属于路由和本地 UI。

## 2. 后端

采用 Go、SQLite、cart v3。REST 用于业务请求和快照，SSE 用于会话事件，WebSocket 用于 MCP、browser tools 与 realtime bridge。

音频输入、工具循环、Apps/MCP、附件、画布数据、上下文压缩、输入队列、协作与定时任务已有实现，不能再按第一阶段的“暂不做”解释。功能入口见[文档索引](README.md)。

core 独立构建，不读取 sibling desktop。公共运行协议版本和浏览器限额只来自 [contracts/runtime.json](../contracts/runtime.json)。desktop 锁定准确 core SHA，并从该版本生成 `web/contracts/`。

## 3. 前端

客户端采用 React、TypeScript、Vite、TanStack Router/Query、Zustand、React Hook Form、Zod、Tailwind 与 shadcn/ui。

| 状态 | 权威来源／边界 |
| --- | --- |
| 页面位置、selected session | TanStack Router |
| REST 快照 | TanStack Query |
| SSE overlay、未提交草稿与本地交互 | Zustand；不是 canonical 历史 |
| 表单 | React Hook Form + Zod |
| transcript | messages query + live event overlay，以 `clientMessageID` 对账 |

API 函数显式接收目标会话；query key 带实际资源作用域。localStorage 只保存 UI 偏好，不保存 messages。切换会话不写入后端 focus，也不改变音频输入归属。

## 4. 桌面

Electron 启动 daemon、承载 UI、托管 Chromium 网页与系统能力；Go 继续拥有业务会话。

| 通道 | 用途 |
| --- | --- |
| daemon loopback REST / SSE / WebSocket | 业务协议、事件及双向工具桥接 |
| Electron IPC / preload | 原生窗口、文件选择、系统通知、外链、权限和更新 |
| Vite 开发页面／外置 `web/dist` | UI 资源；不作为业务 API 代理 |

core 默认只提供 API；显式 `-ui-dir` 可挂载外置 UI。桌面构建、签名、公证和更新已有流程，见 [desktop 发布说明](https://github.com/teatak/pudding-desktop/blob/main/docs/releasing.md)。旧 shell 和 screencast 方案不再是当前路径。

## 5. LLM Provider

[Registry](../internal/provider/registry/registry.go) 已支持 `openai-compatible`、`openai-responses`、`google`、`anthropic`。Ollama 等兼容端点通过 profile 配置接入，不需要独立跨轮历史。

- 模型由 `session.provider + session.model` 显式选择；没有全局默认 profile/model 回退。
- profiles 与模型元数据来自 `<home>/config/profiles.yaml`；scalar settings、web tools 配置分别来自 `settings.yaml`、`web.yaml`。用户补充提示词来自 `<home>/pudding.md`。
- provider/model/effective config 在 BeginTurn 时生成快照；运行中的 turn 不受随后配置编辑影响。
- 每次请求由 canonical messages、当前输入以及明确的系统指令、工具和项目配置构造；provider client 不持有跨 turn 事实。
- provider 只产出模型 delta / finish / error；生命周期事件由 engine 生成。
- Responses 不以服务端会话 ID 续聊，reasoning continuation 按协议从 canonical 数据构造。
- 图片与音频的模型投递按模型能力及 adapter 支持处理，不能把全部附件类型视为已原生支持。见 [附件说明](https://github.com/teatak/pudding-desktop/blob/main/docs/attachments.md)。

模型预设只是创建配置的模板，见 [provider-presets.md](provider-presets.md)。

## 6. 存储

[当前 schema](../internal/store/schema.sql) 定义运行数据，包括 sessions、turns、messages、events、queued_inputs、projects、工具文件变更、画布、浏览器、协作关系及定时任务。配置文件不迁入 SQLite；不再使用第一阶段“第一批／后续表”作为当前结构清单。

- canonical messages 是对话历史的事实源；turn 状态由 turns 保存，UI 不自建另一套状态。
- token delta 不逐条落库。最终 assistant 输出、turn 收尾状态与 lifecycle events 按既有事务契约提交；工具循环的 canonical 结果供后续请求和回读使用。
- SQLite 使用 WAL；持久化变更同步 schema、迁移和指纹，不改写已发布迁移。
- 正式升级基线从 `0.1.1` 的 schema v1 起保留连续迁移链；迁移前备份、失败回滚。
- 上下文压缩已实现，摘要落 canonical message；预算、保留边界与工具循环内触发见 [context-compaction.md](context-compaction.md)。

## 7. API 形状

完整路由以 [server.go](../internal/api/server.go) 和[契约对照](contracts-checklist.md)为准，基本会话路径为：

```text
POST   /sessions
GET    /sessions
GET    /sessions/{id}
PATCH  /sessions/{id}
DELETE /sessions/{id}
POST   /sessions/{id}/submit
POST   /sessions/{id}/cancel
GET    /sessions/{id}/events
GET    /sessions/{id}/messages
GET    /sessions/{id}/queued-inputs
```

submit 必须携带 `clientMessageID`，重复请求按同一输入对账。运行中的新输入可进入持久队列；显式 steer、编辑／撤回队列、重试有各自接口，不用第二个并发 turn 模拟。

禁止无会话作用域的 `/submit`、`/events` 和后端 `/focus`。全局配置、项目和定时计划等资源按自己的资源范围定义。cart 使用整段参数，静态路由优先于参数，catch-all 只出现在末尾。

## 8. 事件协议

[internal/event/types.go](../internal/event/types.go) 与 [contracts/events.ts](../contracts/events.ts) 定义字段；[契约对照](contracts-checklist.md)逐项区分 seq 和落库行为。

```text
turn.started → turn.delta / turn.tool / … → turn.completed | turn.failed | turn.cancelled
```

当前持久化 lifecycle 事件带 session 内单调递增 seq，作为 SSE `id`；`Last-Event-ID` 或 `?after=` 用于续传。`turn.delta`、`turn.tool` 等即时事件不落库、无 seq，不能声称断线后可逐 token 重放。历史和最终内容通过 canonical 快照恢复。

新连接默认从尾部订阅；消息历史由 messages API 读取。overlay 在终态及对应 canonical 数据可见后对账清除。`turn.delta.part` 已区分 text/thought，工具流使用 `turn.tool`；原先拟议的 `partType`、`estimatedSteps` 和 `turn.progress` 不是现行协议。

注意：`AGENTS.md` 第 12 条概括为“session 事件带 seq”，而实现仅持久化事件带 seq；这里明确记录该范围差异，本次整理不改协议或硬约束。

## 9. 安全

- daemon 只监听 loopback，请求按启动 token 认证；Electron 启动页面注入连接信息，前端读取后清理地址栏。
- provider API key 当前保存在本地 YAML，文件权限 0600；不能描述为已接入系统 Keychain。
- 模式、App 加载、项目目录和工具审批是不同边界。App load 不直接授予文件目录或操作权限。
- Code CLI 的 Ask / Auto / Full、host 请求和授权复用以 [沙箱说明](code-cli-sandbox-design.md)及当前 engine/tool 策略为准。

## 10. 数据目录与通道隔离

| 通道 | 默认 home | 默认 daemon 地址 |
| --- | --- | --- |
| dev | `~/.pudding-dev` | `127.0.0.1:9679` |
| release | `~/.pudding` | `127.0.0.1:9669` |

home 解析顺序为 `--home`、`PUDDING_HOME`、构建通道默认值。本地构建默认 dev；release 只由发布构建注入。两通道不自动同步或迁移数据；测试只使用临时 home 和独立端口。

常用内容包括 `data/pudding.db`、`config/*.yaml`、`pudding.md`、`daemon.token`、日志、附件与运行资源。实际路径以 [internal/home](../internal/home) 为准。不要只凭端口或窗口标题判定测试环境，桌面回归需核对源码 Electron 和实际页面来源。

## 11. Audio

当前保留 PortAudio、Sherpa ONNX、WebRTC AEC/NS 的语音输入与原音消息；硬件属于 daemon，输入通过明确 session binding 路由。TTS 与 speaker output 已移除，不是等待完成的旧迁移步骤。详见 [Voice Input Architecture](https://github.com/teatak/pudding-desktop/blob/main/docs/voice-migration-plan.md)。

## 12. 验证边界

多会话隔离、幂等、cancel、SSE 恢复和重启历史保持是持续契约；Go、schema、Web 与 Electron 检查按各仓 AGENTS 的实际改动范围选择。测试入口不等于本次已执行记录；纯文档整理不运行产品全量测试。

## 13. 历史范围与未完成事项

第一阶段的“暂不上 Electron”“后续增加 Gemini/Anthropic/Responses”“压缩先定形不实现”已过期，保留于[历史决策](archive/plans/technology-decisions-phase-1.md)。当前需要推进的重点只更新 [backlog.md](backlog.md)；历史设想不自动成为待办。

## 14. 并发与中断

同一 session 不并发执行两个 turn，但新 submit 可以进入 `queued_inputs` 并在上一轮结束后推进；因此“运行时一律返回 409、排队未排期”不再成立。其他会话可独立运行。

失败或取消保留已产生的部分 canonical 输出并标注中断；不能因取消把已发生的工具副作用当成未执行，也不能由 UI 或 provider 内存恢复历史。
