# 任务：定时执行与 Agent-to-Agent 协作

> 历史方案（2026-09-21）：本轮定时任务以 [定时任务首版实施计划](scheduled-tasks-plan.md) 为唯一实施依据。以下保留原始草案用于追溯，不作为待办。
> 本轮采用独立菜单管理、绑定原会话执行；下文每次运行新建 Session、外部 Agent、结果审查、callback、审批持久化及 worktree 等设计均不属于当前实施范围。

> 归属：跨仓设计草案；core 维护任务调度、数据与 API 设计，desktop 承载任务视图。
> `internal/`、`contracts/` 路径相对 core；`web/`、`electron/` 和 `make desktop-dev` 属于 `pudding-desktop`。
> 规划中的接口和文件不表示已实现；下文链接到 desktop 的现有界面仅作为产品参考。

> 日期：2026-09-13。状态：设计草案，尚未实现。
> 范围：Pudding Electron 桌面端、local-first、单机执行。
> 本文确定任务的行为边界、数据归属、外部接口与验收条件，不代表当前产品已有这些能力。

## 1. 产品目标与首版范围

产品统一使用“任务”这个名称，支持手动、定时、外部 Agent 三种触发方式，共用现有 Session / Turn 执行链。

主要使用场景：

- 用户创建“每天检查项目构建”的任务，到时由 Pudding 执行并留下结果。
- GPT-6 定义目标与验收条件，委派 Pudding 使用选定模型执行；结果就绪后收到 callback，再进行审查。
- 执行遇到审批或必要问题时，用户或获授权的调用方处理后，继续同一次执行。

节省 token 的来源是减少昂贵模型参与执行循环、重复读取上下文和进度询问。调度、等待、事件投递、预算检查由程序完成；是否实际降低总成本必须测量，不能仅比较 GPT-6 自身用量。

首版包含：任务管理、一次性及周期触发、执行记录、结果包、可选外部审查、审批持久化、同机 Agent 接入与可靠 callback。

首版不包含：任务依赖图、自动拆解与多 Agent 分工、跨机器调度、云端常驻执行、任意公网 webhook、自动合并或发布、指定标准 A2A 协议兼容。本文的 A2A 表示 Agent-to-Agent 协作方式。

下文给出的并发、错过执行处理和默认期限是首版设计选型，不是现有产品行为。

## 2. 当前代码基线

| 能力 | 当前事实与实施含义 |
| --- | --- |
| 会话执行 | [API](../internal/api/server.go) 已有会话创建、submit、cancel、turn 和消息读取；[engine](../internal/engine/engine.go) 已有 `clientMessageID` 幂等。复用这些业务语义 |
| 执行完成与事件 | [事件契约](../internal/event/types.go) 已有 turn lifecycle；[SSE](../internal/api/sse.go) 支持 session seq 和 `Last-Event-ID`。没有续传位点时从尾部开始，外部接入不能依赖连接后才看到完成事件 |
| 审批 | [approval.go](../internal/engine/approval.go) 使用内存 map 和 channel 等待；批准、拒绝已有 REST 接口。审批请求与决定事件目前不持久化 |
| 重启恢复 | `Engine.Recover` 将遗留 running turn 收尾为 failed，原因是 daemon restart。当前不能恢复原 Go 调用栈或待审批命令 |
| 用户问题 | [用户问题收集](user-input-flow.md) 已有提问、等待、补答、幂等与 canonical 恢复；不能再建另一套答案事实源 |
| 用量与结果读取 | 已有会话用量和 [工具结果按需读取](context-working-set.md)。任务视图引用这些记录，不另建 token 计数器或完整日志副本 |
| 桌面界面 | [RailPanel](https://github.com/teatak/pudding-desktop/blob/main/web/src/components/session-rail/RailPanel.tsx) 按项目组织会话；[Conversation](https://github.com/teatak/pudding-desktop/blob/main/web/src/components/Conversation.tsx) 复用 Transcript / Composer；[WorkspacePane](https://github.com/teatak/pudding-desktop/blob/main/web/src/components/workspace/WorkspacePane.tsx) 管理项目、网页和产物 |
| 任务系统 | 本轮代码核对未发现本文所需的任务定义、定时调度、A2A 委派或业务结果 callback 主链路，需要新增 |

界面以当前代码和用户提供的 Pudding 截图为基线。[旧设计底座](https://github.com/teatak/pudding-desktop/blob/main/docs/design.md) 中部分导航、品牌和消息展示描述已与当前界面不同，不能照其旧布局重建工作台。实际样式沿用 [styles.css](https://github.com/teatak/pudding-desktop/blob/main/web/src/styles.css)。

鉴权基线更新（2026-09-20）：拆仓已删除 mobile device token 与移动配对路径。外部接入仍须遵守
[AGENTS.md](../AGENTS.md) 的 loopback 与启动 token 边界，不得恢复移动端配对或把 daemon 暴露到公网。

## 3. 用户界面

### 3.1 导航与任务页

- 在现有“新对话、项目、搜索、应用”所在区域增加“任务”入口。
- 保留项目下的会话结构；执行会话仍可从所属项目进入，不建立第二套会话列表事实源。
- 任务页管理任务定义、触发规则、启用或暂停，以及每次执行记录。任务定义不是一条新的聊天记录。
- 列表显示名称、所属项目、触发方式、下次计划时间、最近执行状态；没有定时规则时不显示虚构的下次执行时间。
- 任务详情显示目标、验收条件、执行配置和历史运行。点击一次运行，路由到其现有会话。
- 暂停定时触发与取消当前运行是两个独立操作。暂停不撤回已接受的运行；归档任务前要求其没有未结束运行。

### 3.2 执行会话

继续使用 Pudding 的标题、Transcript、工具折叠行、Composer 和右侧工作区。标题下增加可展开的任务状态条，收起时只展示来源、当前阶段和必要操作；展开后展示任务要求、检查证据和审查结论。

普通会话不出现任务状态条。任务不新增 Chat / Work / Code 之外的第四种能力模式。

产物、文件与 diff 在现有工作区打开；审批和问题沿用现有交互。待审批、需要输入、结果待审查在任务页提供入口，进入后仍处理同一资源。任务执行结束不能使用现有“模型回复结束”的标记冒充验收通过。

运行期间的补充说明仍进入原会话，并记录来源。待审查时新增或修改要求必须使当前结果失去可验收资格，重新进入执行流程；不能边修改产物边接受旧结果。

首版从任务页或外部接口创建任务。不做历史会话自动转任务，也不自动复制已有长会话作为执行上下文。

## 4. 实体与唯一事实源

| 实体 | 职责与事实源 |
| --- | --- |
| Task | SQLite 保存任务定义：标题、目标、验收条件、项目引用、执行配置引用、触发规则、预算、审查策略和定义 revision |
| Run | SQLite 保存某次被接受的执行：任务快照、触发来源、幂等身份、sessionID、流程状态、返工次数、结果引用及原因 |
| Session | 原有第一等会话实体。一条 Run 对应一个独立 Session，任务定义可以产生多条 Run |
| Turn | 原有执行轮次；一次 Run 可以包含多个 Turn，包括补答和返工。Turn 的状态仍由 engine 管理 |
| Result | 一次交付的不可变结果索引，绑定 canonical 提交记录、产物版本及证据引用；不能把活动工作目录本身当作已交付版本 |
| Review | 针对明确 Result revision 的审查决定、问题、身份与时间；审查不等于操作审批 |
| Approval | 被请求操作及其审批决定的持久化权威记录，代替当前内存记录充当事实源 |
| Delivery | callback 投递进度、尝试时间和接收确认；不保存另一份任务完成状态 |

基本关系：`Task → 多次 Run → 各自 Session → 多个 Turn`。同一次 Run 的返工继续原 Session；下一次定时运行使用新 Session。

运行接受时冻结任务定义 revision 和执行请求；之后修改任务只影响未来运行。provider profile / model metadata 仍来自 `<home>/config/*.yaml`，不复制成 SQLite 配置中心。已开始 Turn 继续使用现有模型快照机制；未来调用若配置缺失，任务明确报错，不静默换模型。

聊天历史只来自 canonical messages。任务目标作为明确任务配置来源构建初始输入；工具日志和用户补答不从 Task、Result 摘要、UI overlay 或 provider 内存恢复。

首版建议增加 `tasks`、`task_runs`、`task_results`、`task_reviews`、`approvals`、`task_deliveries` 表；复用现有 sessions、turns、messages、events、queued inputs 和用量记录。具体 DDL 在实施时同步 schema、迁移与版本指纹，已发布迁移不可改写。

## 5. 运行状态与完成条件

Run 持久化流程状态：

| 状态 | 含义 |
| --- | --- |
| `queued` | 已接受，等待执行 |
| `active` | 已开始，本次运行尚在完成目标 |
| `awaiting_review` | 已提交固定版本结果，等待指定外部审查方 |
| `completed` | 满足本次配置的交付规则 |
| `failed` | 无法继续；附明确原因与已有产物引用 |
| `cancelled` | 用户或获授权调用方取消 |

界面的“执行中、待审批、需要输入”是 `active` 的展示阶段，分别根据关联 Turn、未决 Approval 和必要问题推导。不得在 Run、Approval、Zustand 中各存一份互相竞争的审批状态。

主要流转为：`queued → active → awaiting_review → completed`。不要求审查时，合法结果提交后由 `active → completed`。审查要求修改时，`awaiting_review → queued`，在原 Session 启动下一轮。失败、取消是终态；重新尝试创建新 Run，并关联旧 Run。

仅收到 `turn.completed` 不足以完成 Run：该 Turn 可能在提问，或只报告了部分进展。首版增加仅对任务执行会话开放的结果提交工具 `builtin_task_submit_result`，显示名为“提交任务结果”，同步全部 i18n 和 transcript 显示。

该工具的行为边界：

1. 提交摘要、验收项结果、检查和产物引用；每项检查允许 `passed / failed / not_run`，不能隐去失败或未执行项。
2. engine 校验所属 Run / Session、必要审批与问题已处理、产物版本固定，以及任务要求的必要证据是否齐全。
3. 成功提交是本轮终止动作，不再执行后续修改或再次调用模型。它不能与其他工具混在同一批次执行；混合批次在产生副作用前明确拒绝。
4. 在同一事务中写 canonical 收尾、Turn 状态、结果索引、Run 新状态、lifecycle events 和所需 Delivery；事务成功后才通知订阅者。
5. Result 正文从对应 canonical 提交内容读取，工具返回和 UI 只引用结果身份，不另存一份可编辑的完成报告。

审查策略首版只有 `none` 与 `external`。`none` 的“完成”表示结果满足预设交付检查，不表示经过 GPT-6 独立验收。`external` 必须由指定审查方接受当前结果后完成；执行模型不能替自己接受结果。无需在所有定时任务后自动追加 GPT-6 调用。

任务定义中标为必要的检查若为 `failed / not_run`，保留交付记录并以 `acceptance_unmet` 收尾，不进入 completed；主观或需要独立验证的验收条件由外部审查处理，不能仅凭执行模型的自评放行。

若 Turn 正常结束却没有合法结果提交：存在必要问题时等待问题，否则本次运行以 `result_missing` 失败，保留已有内容，不自动生成“成功”结果或启动无限补交循环。

## 6. 触发、排队与定时规则

三种触发都进入同一个 `TaskService.StartRun`，不创建三条执行链。

- 手动：用户在任务页选择“立即执行”。
- 定时：daemon 的普通调度器计算到期时间并触发。
- 外部：获授权 Agent 创建任务或触发已有任务。

接受运行时，在事务中创建 Run 和专用 Session，冻结定义快照，记录幂等身份与排队事件，再返回 `runID / sessionID`。排队阶段也可查询、取消和订阅，不依赖前端当前选中的会话。

### 6.1 首版定时规则

- 使用一份结构化 schedule：指定时间、固定间隔、每天、每周；不同时维护 cron 和 RRULE 两份可编辑事实源。
- 每天、每周使用 IANA 时区及当地时间；实际触发身份使用 UTC。固定间隔从显式起点计算，不从上次任务完成时间漂移。
- 夏令时不存在的当地时间跳过；重复的当地时间仅执行第一次对应时刻。必须有专门测试。
- 修改时区或规则增加定义 revision，原子更新下一触发时间；已接受的 Run 不改变。
- daemon 未运行、机器关机或休眠时不承诺准时运行，不增加系统唤醒或云端代跑。重新启动后默认跳过错过的周期，转到下一个未来时刻；一次性时间已错过则标记该次计划错过，允许用户手动执行。
- 任务持久化调度游标和最近跳过的时间范围、原因，首版不声称提供每个错过时刻的完整运行记录。

### 6.2 幂等、重叠与并发

- 创建 Task 本身要求调用方 requestID，按调用身份去重，避免创建响应丢失后出现多个相同任务。
- 手动和外部触发要求 `requestID`，作用域为调用身份与 Task；同一键、同一请求返回原 Run，不再次执行。同一键不同请求返回 `409`。
- 定时触发唯一身份为 `taskID + scheduleRevision + scheduledForUTC`，数据库约束防止重复到期处理。
- 同一 Task 最多有一个非终态 Run，包括待审批、待输入和待审查。此时定时到期按 `skip_if_busy` 跳过；新的手动或外部触发返回 `409 task_busy`，不默默排出无限队列。
- 首版后台执行并发为 1；审批、必要输入和外部审查等待不占模型执行槽，恢复时回到同一个调度队列。工作目录的使用边界在等待时仍保留。
- 普通手动会话继续使用现有多会话能力，不纳入后台任务队列。
- 所有 engine submit，包括返工与恢复后的新 Turn，都携带持久化的 `clientMessageID`；网络重试不得生成新键。

代码任务首版要求 Git 项目并使用独立 worktree。基线是接受运行时解析出的确定 commit，不自动携带主工作区未提交改动；无法准备 worktree 时明确失败。Chat / Work 任务可无项目。直接修改共享 checkout、非 Git 代码任务及把进行中的代码任务迁移到其他项目，后续另行设计。

## 7. 外部 Agent 接入

### 7.1 边界与权限

首版提供同机受限 Bridge 接入。Bridge 是普通程序，只做调用方鉴权、资源范围校验和协议转换；不持有第二套 Task / Run 状态，不负责模型规划。

- daemon 继续只 bind loopback，所有请求带启动 token。
- 外部 Agent 使用单独配置的 Bridge 凭据；daemon 启动 token 只由受信任 Bridge 持有，不出现在模型参数、结果包或 callback 中。
- Bridge 只暴露任务操作白名单，不开放任意 daemon URL 代理。调用身份从凭据确定，不相信请求正文中的 `agentID`。
- 授权明确允许的项目、任务和操作：创建、触发、读取、补充输入、取消、审查、审批。审批默认不授予；审查权限不隐含发布或命令批准权限。
- Bridge 凭据只限制 API 调用，不宣称能隔离已经具有相同操作系统账户权限的恶意本地进程。
- 外部授权与 callback 目标配置在本地受信任配置中维护，执行模型不能修改。运行中的撤权在后续请求及操作调度前生效，不冒充已经撤销运行中 OS 进程的权限。

不把当前 Codex 桌面任务一定能自动唤醒作为接口前提。接收端必须有普通程序消费 callback 并调用其宿主支持的继续执行接口。OpenAI 的 [App Server](https://learn.chatgpt.com/docs/app-server#lifecycle-overview) 有 thread / turn 生命周期接口，但接入当前桌面任务仍需独立验证。

### 7.2 业务 API 草案

下列路径描述 daemon 业务契约，Bridge 只映射获授权的子集。实现前同步 API / Zod / TypeScript 契约。

| 操作 | 路径与要求 |
| --- | --- |
| 创建、列出定义 | `POST /tasks`、`GET /tasks`；创建要求 requestID；任务是自身资源，可带项目归属，不附加虚构 sessionID |
| 读取、修改定义 | `GET /tasks/{taskID}`、`PATCH /tasks/{taskID}`；修改要求预期 revision |
| 触发运行 | `POST /tasks/{taskID}/runs`；要求 requestID，返回 `202` 与 runID、sessionID |
| 列出执行记录 | `GET /tasks/{taskID}/runs`；游标分页，每项包含 sessionID |
| 运行快照 | `GET /sessions/{sessionID}/task-runs/{runID}` |
| 读取固定结果 | `GET /sessions/{sessionID}/task-runs/{runID}/results/{resultID}` |
| 提交审查 | `POST /sessions/{sessionID}/task-runs/{runID}/reviews`；要求 requestID、resultID、resultRevision、决定及问题 |
| 取消运行 | `POST /sessions/{sessionID}/task-runs/{runID}/cancel`；覆盖尚无 running Turn、等待审查等阶段，并协调现有取消链路 |
| 补充说明 | `POST /sessions/{sessionID}/submit`；任务 session 的输入必须绑定该 Run 并经过其状态校验 |
| 回答必要问题 | 复用 `/sessions/{sessionID}/input-requests/{requestID}`，不创建另一份答案 |
| 待审批、批准、拒绝 | 复用 `/sessions/{sessionID}/approvals` 及其 `/{approvalID}/approve`、`/{approvalID}/deny` |
| 执行事件 | 复用 `GET /sessions/{sessionID}/events` 和 Last-Event-ID；不增加全局 `/events` |

所有 session-scoped 操作验证 Task、Run、Session、Turn / Approval / Result 的真实归属，不能只验证某个 ID 存在。Task 列表只返回调用方可见资源。

任务会话的提交入口统一由 engine / TaskService 检查，不能仅在任务页隐藏按钮：活动运行的补充走现有引导或排队；待审查的新增要求先撤销旧结果的可验收资格；终态运行不能被普通 submit 静默重开。后续目标创建新 Run，读取旧会话不受影响。

新增供 LLM 调用的 Bridge 工具只封装这些操作；不再实现一套工具专属状态机。工具命名落定时，同步 transcript 友好名称和全部 i18n。

## 8. 结果包、审查与 callback

### 8.1 结果包

Result 至少包含：

- taskID、runID、sessionID、交付 revision、对应 Turn 和 canonical 提交引用。
- 简短摘要、验收条件逐项结论、未完成事项。
- 产物引用及版本。代码包含 base commit、固定 patch / tree 身份；未跟踪文件、二进制及删除也必须纳入实际变更范围。
- 检查的真实命令、工作目录、退出码、时间、所针对的产物版本及日志引用。未执行、输出缺失或截断明确标记。
- 执行模型用量，以及来自外部审查方的独立用量记录（若提供）。未知用量不填零。

模型填写的“通过”不等于系统已验证。检查证据需能回读对应 canonical 工具结果；固定结果不能只引用之后可能变化的文件路径。UI 摘要与 callback 只提供有界预览，完整证据按引用读取，复用现有按需回读方式。

外部审查方先读任务要求、变更与证据；必要时独立验证。决定为 `accept` 或 `request_changes`。接受只更新验收状态，不隐式执行 Git merge、push 或发布。

审查要求预期 Result revision 和 Run revision，重复请求返回原决定；过期结果、已取消运行、并发冲突返回 `409`。返工不覆盖旧 Result，产生下一份交付。首版最多返工 2 轮，之后以 `revision_limit` 收尾并保留问题。

### 8.2 事件与投递

新增 `task.run.*` / `task.result.*` 等必要业务事件，沿用 session 的单调 seq 和现有 events 表。turn lifecycle 仍由 engine 产生，provider 不产生任务状态。

应通知的情况：结果待审查、运行终态、待审批、需要必要输入，以及审批失效或被其他入口处理。普通 token delta、重复等待与无变化进度不触发 callback 或 GPT-6 唤醒。

callback 事件信封示例：

```json
{
  "eventID": "session_example:42",
  "type": "task.result.ready",
  "taskID": "task_example",
  "runID": "run_example",
  "sessionID": "session_example",
  "seq": 42,
  "resultID": "result_example",
  "resultRevision": 1
}
```

callback 使用预先登记且属于该调用方的 `destinationID`，任务正文不能指定任意 URL。首版目标只允许同机 loopback，严格校验目标，禁止跟随重定向；使用目标独立密钥认证和 eventID 防重放，不转发 daemon token。

状态提交与 Delivery 创建在同一 SQLite 事务内；提交后 worker 发出 HTTP 请求。接收方持久化 eventID 后返回 2xx，发送方才标记已投递。网络失败或超时进行有限退避重试；到上限后显示“通知失败”，允许重投同一事件，不把已成功执行的任务改成执行失败。

投递按允许重复的至少一次语义设计，不承诺网络层恰好一次；有限重试也不保证必达，耗尽后必须保留未交付记录。接收端按 eventID 去重；返回 2xx 仅代表已接收，不代表 GPT-6 已完成审查。

SSE 消费者保留最后确认的 seq；首次连接先结合 Run 快照和返回位点建立订阅，不能在没有 Last-Event-ID 的情况下假定会收到已发生的事件。重连、callback 与快照都引用同一业务记录。

## 9. 审批与必要输入

### 9.1 正常审批

触发方式不改变审批规则。沿用项目 `ask / auto / full`、能力授权和 [CLI 沙箱](code-cli-sandbox-design.md) 的既有边界。任务配置及外部调用权限可以收紧，不能静默扩大这些权限。

未获授权的操作先形成持久化 Approval，再暂停相关执行。界面显示“待审批”，程序等待并通知用户或指定接收方，不启动新的模型推理来询问进展。审批等待不停止其他无关会话。

| 决定 | 行为 |
| --- | --- |
| 批准 | 校验审批仍有效及操作仍相同，排入恢复执行队列；只放行批准的范围 |
| 拒绝 | 持久化理由，作为工具结果交给原执行流程；允许采用已授权替代方案，不能换个说法重放被拒绝操作 |
| 超时 | 未决审批过期，取消相关等待，本次 Run 以 `approval_timeout` 失败；不视为批准或拒绝 |
| 取消运行 | 使该 Run 所有未决审批失效，停止对应 Turn；保留已有产物 |

审批和外部审查分开：审查通过说明结果符合要求，不批准新的工具操作。创建任务的权限不等于审批权限；只有用户显式委托的外部审批方才能在授予范围内决定，执行模型不能批准自己的提权请求。

普通工具批准绑定具体 call；能力或 Computer Use 授权继续按现有类型说明 Turn / Session 范围，UI 必须显示实际范围，不能把 session 授权标成“仅本次命令”。每次定时运行新建 Session，不继承上一 Run 的临时授权；长期规则需要用户明确配置。

### 9.2 持久化与竞争处理

Approval 记录包含所属 Session / Turn / call、完整操作快照、目标及其版本校验信息、申请理由、权限范围、有效期、决定人、决定和 revision。命令快照包括 argv、cwd、env 及执行边界；敏感字段在外部展示时按调用方读取权限处理。

批准、拒绝、过期与取消用数据库条件更新仲裁。同一决定重复提交返回原结果，冲突决定返回 `409`；相应 lifecycle event 同事务写入。现有内存 map / channel 只保留为当前进程唤醒机制，不再充当待审批事实源。

将现有 `approval.requested`、`approval.resolved` 纳入持久化事件并覆盖续传；不要另保留一套仅适用于任务的内存审批路径。

### 9.3 重启与不确定副作用

daemon 重启仍沿用现有 Recover：原 running Turn 标记失败。审批记录可恢复展示，不代表原调用栈可恢复。

- 原 Turn 的未完成审批转为失效，关联 Run 以 `interrupted` 收尾；旧审批不能再批准。
- 用户显式重新尝试时新建 Run，引用先前记录并从 canonical 信息核对已完成操作；当前代码任务产生的新工作区必须保留可审查的旧产物引用。
- 已批准但不确定是否执行的操作，不凭批准记录自动重放。重新观察真实状态；无法确认副作用时要求处理后再继续。
- callback 未送达可以重投同一事件；重新投递不能重新执行工具。

首版提供审批记录恢复和可控重新尝试，不承诺任意进程崩溃点的无缝续跑或外部副作用恰好一次。

### 9.4 必要输入

复用现有 input request 与 canonical 答案链路。必要问题的超时不是同意，未回答时不得进入依赖该答案的操作。

任务执行需要让调用方明确标记问题是否阻塞交付；此标记属于原 input request 契约，不另存问题副本。任务模式下，必要问题尚无答案时，engine 在工具等待结束后将本轮正常收尾并保留 Run 为 active，不继续调用模型或执行依赖操作；这需要新增 engine 衔接，不能只靠 prompt。问题身份与截止时间从 canonical 提问记录恢复。调用方通过原 requestID 回答，由现有补答规则生成 canonical 输入并在原 Session 排队继续。

普通会话仍沿用当前问题等待行为。首版必要输入和审批的持久化等待期限默认 24 小时，外部审查默认 24 小时，可在任务定义中收紧；deadline 跨重启保持，不因用户打开面板或重试通知而延期。必要输入和审查到期分别以 `input_timeout`、`review_timeout` 结束 Run；迟到答复不自动复活终态运行。

## 10. 事务、恢复与资源生命周期

TaskService 是 daemon 内的 Go 服务，负责状态转换和调度；engine 继续管理实际模型与工具执行。Bridge、UI 和 provider 都不持有另一套运行状态。

关键事务边界：

1. 接受触发：幂等检查、任务未重叠校验、Run / Session 创建、定义快照、排队事件一次提交。
2. 开始 Turn：复用现有 submit 幂等和 BeginTurn 语义，将 Run 绑定的提交身份与 Turn 关联纳入同一业务事务，防止“已提交但未关联”形成孤立执行。
3. 结束 Turn / 交付结果：canonical、Turn 状态、相关 Run 转换、events 和 Delivery 同事务；后续进程只处理已提交记录。
4. 审批或审查决定：状态比较更新、事件及必要投递一次提交，事务之后才唤醒或开始下一轮。

启动顺序：先完成现有 Turn 恢复与任务状态核对，再接收新触发；恢复已接受但未开始的 queued Run，恢复未确认 Delivery；active Run 若其 Turn 被 Recover 中断则失败，不自动重放。已正常结束 Turn、正在等待必要输入的 Run 从 canonical 请求恢复等待；awaiting_review 的固定结果仍有效时可继续等待审查。各等待状态先检查持久化 deadline。

取消与结果提交并发时，以事务中的 Run revision 仲裁。取消先提交则拒绝交付；完成先提交则取消返回已终态。无论从任务页还是会话停止，都经同一业务路径对齐 Run 与 Turn，不能只停止 SSE 展示。

首版不自动删除运行记录或代码 worktree。归档只改变可见性；有关联执行记录的 Session 不能通过普通删除操作留下悬空引用。清理必须明确列出关联产物并保持未完成运行可用，自动保留策略后续单独实现。

dev 使用 `~/.pudding-dev`，release 使用 `~/.pudding`，任务数据库、调度器、Bridge 配置和 worktree 均隔离。测试只使用临时目录，不能向用户数据库写入测试任务或向真实 Agent 发测试 callback。

## 11. Token、时间和预算

- 等待调度、审批、用户答复和审查时，不产生进度轮询型模型调用。
- 初始输入只包含本次目标、必要约束、验收条件和明确资源引用，不复制委派方整段会话。
- 结果摘要有界，必要证据按需读取；证据不足不能靠截断标成验收通过。
- 执行模型和审查方分别配置。首版不做模型自动升级、失败后偷偷换模型或增加常驻规划 Agent。
- 每个 Run 默认最多两轮审查返工；任务可设置总工具轮次、模型请求次数、执行 token 预算和执行时间上限。等待期限单独计算，仍受持久化 wall-clock deadline 约束。
- token 用量按关联 Turn / provider 请求的真实记录汇总，复用现有计量链路。若现有记录粒度不能准确归属，实施时扩展原计量来源，不能在 TaskService 自行估算并冒充真实用量。
- 每次新模型请求前检查余额，在支持时约束单次输出。流式计量和未知输入可能导致单次请求超额，预算是停止后续工作的边界，不承诺账单精确硬上限。
- 达到执行时间、请求次数或 token 边界后停止后续调度，必要时取消正在运行的 Turn，以 `execution_timeout` 或 `budget_exhausted` 收尾并保留实际结果；不得把预算用完标成任务完成。
- 外部 GPT-6 用量只能由调用方报告或其宿主提供，必须标明来源；未提供时显示未知，Pudding 无法替外部模型强制扣费上限。

验证节省效果时，对同类任务比较：GPT-6 输入、输出与推理用量，Pudding 执行用量，缓存用量（若可得），总费用（仅在有实际价格依据时）、耗时、返工次数和验收通过率。不能把工具正文缩短比例写成总 token 节省比例。

## 12. 实施阶段与验收

所有阶段均为待实施，不表示对应检查已经执行。

| 阶段 | 交付 | 验收重点 |
| --- | --- | --- |
| P0：接入验证 | 验证一个真实同机调用方接收事件并启动审查，确定外部身份、凭据和宿主入口 | 无模型轮询；事件处理与启动审查可观察；不能把收到 callback 当作已唤醒当前 Codex 桌面任务 |
| P1：手动任务 | Task / Run、任务入口、专用 Session、明确结果提交、取消 | 同一请求不重复执行；定义快照不漂移；Turn 结束不冒充任务完成；文件与会话复用现有界面 |
| P2：审批与恢复 | 持久化审批、必要输入衔接、原路径迁移、deadline | 批准/拒绝竞争、重复决定、断线续传、撤权、超时、重启失效、无副作用重放 |
| P3：定时执行 | 结构化 schedule、调度游标、重叠规则、worktree、预算 | 时区与 DST、错过触发、休眠恢复、任务暂停、单任务不重叠、背景等待不阻塞无关工作 |
| P4：A2A 闭环 | 受限 Bridge、Result / Review、事务投递与重投 | callback 丢失/重复/迟到、越权拒绝、结果版本冲突、两轮返工上限、实际总用量比较 |

必须覆盖以下故障与竞态：

- 同一触发重复请求、不同内容复用幂等键、接受后响应丢失。
- 事务提交前后进程退出、通知发送成功但确认记录未写入、接收方处理后响应丢失。
- 审批与取消同时发生；用户和外部 Agent 同时处理；审批后目标内容发生变化。
- 已执行命令但结果未落库即崩溃；重启后不能自动重放或声称执行成功。
- 审查旧版本、审查中追加要求、返工后旧 accept 迟到、完成与取消竞争。
- 当前模型配置缺失、Git 基线不存在、worktree 创建失败、必要检查未运行、固定产物被删除。
- 数据隔离、跨 Task / Run / Session 读取或操作、无审批授权的调用方尝试批准。

按改动选择验证入口：Go 相关包测试；SQLite schema-check 与迁移测试（旧数据保留、失败回滚、重启）；Web test / build；交互补当前源码 Electron 回归；原生通知和 Bridge 生命周期补 Electron smoke。最后运行 `git diff --check` 并检查旧审批路径残留。

源码桌面回归前核对当前 Vite URL 与实际进程，选择 `web/node_modules/electron/dist/Electron.app`；不能把已安装发布版当作开发环境。启动 `make desktop-dev` 前确认不会中断现有任务。

## 13. 尚待验证的外部依赖

- 当前 Codex 桌面宿主是否提供可供本机 Bridge 使用的可靠唤醒入口；若不能接入，不能对外宣称支持自动回到该桌面任务。
- 被选择执行模型对结果提交工具、必要输入和任务约束的遵循程度，以及真实任务的返工率。
- callback 接收方的持久化去重与审查调用能力；标准 A2A 兼容性不在首版承诺内。

定时任务的产品参考为 OpenAI 的 [Scheduled tasks](https://developers.openai.com/codex/app/automations)：任务定义与运行分离，以及本地项目依赖机器和应用运行。Pudding 的具体实现与上述默认规则由本设计独立定义。
