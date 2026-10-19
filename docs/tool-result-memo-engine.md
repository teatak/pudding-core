# 工具结果摘要：Core 实验接入

更新：2026-10-19。状态：**已接入真实 Engine／SQLite、公开历史／SSE 和 Desktop note 展示；长任务及提示精简对照均未证明净收益，继续默认关闭。** 详见 [首批长任务报告](tool-result-memo-benchmark.md) 和 [v4 三组对照](tool-result-memo-optimization.md)。单协议隔离原型见 [原型记录](tool-result-memo-prototype.md)，跨仓方案见 `pudding-desktop/docs/tool-result-memo-plan.md`。

## 当前模型参数

只提供 `summarize_results`，当前 schema 版本为 `summarize-results-v2-brief`：

提示与时机诊断已完成：六次运行只有一次超长控制尝试，没有实际释放；增加提醒的候选已撤回。随后按用户要求仅补充摘要简短说明：通常 1–3 个短句，只保留选中结果的必要事实、标识与来源，不复述过程或整段原文；必要证据放不下时选择更少结果。共享提示及 `summary` 参数说明同步，800 字符硬上限和可选性保持。新提示四组冻结任务复测仍为 0/4 采用、0 次释放，没有摘要长度样本；事实、所需引用和追问检查全部通过，仍未证明采用或净收益改善。详见 [简短约束与复测](tool-result-summaries-brief-notes.md)；此前数据见 [历史诊断记录](tool-result-summaries-trigger-diagnosis.md)。

```json
{
  "scope": "project",
  "path": "src/auth.ts",
  "summarize_results": {
    "result_refs": [{"turn_id": "turn_123", "call_id": "call_read_1"}],
    "summary": "已定位登录逻辑及校验顺序；接下来核对错误处理。"
  }
}
```

整个对象可省略；提供时 `result_refs` 数组和非空 `summary` 均必填。模型未使用时正常继续；格式无效时只忽略控制，不追加纠错请求。共享提示明确它搭载必要的业务调用，只概括此前已见事实，且不要求额外工具或模型请求。

功能尚未发布，不接收旧 `memo/release/note` 参数，不增加别名或记录迁移。Core 将 `summary` 映射到现有内部 `Note`／公开 `note` 字段，持久化、事件、CLI 开关及报告标签仍使用原内部名称；这不构成旧模型参数兼容。更名前实测保留原样；新命名的受控全流程已通过，见下节。更名后的真实源码自然采用／成本对照也已完成，见 [真实仓库长任务报告](tool-result-summaries-repository-benchmark.md)。

## 2026-10-19 新命名受控实测

官方 DeepSeek Flash、Chat Completions、`reasoning_effort: high`、输出上限 4096。使用当时的 `summarize-results-v1`，在临时项目及 SQLite 中明确要求必要的第二次文件读取携带 `summarize_results`；没有修改用户 profile、会话或默认开关。原始报告及本次源码哈希见 [受控验证记录](validation/summarize-results-controlled-deepseek-2026-10-19.json)。

- 主任务 **3 次模型请求、2 次业务读取**，正确回答过期 token 返回 HTTP 401；接受并应用 1 个释放，后续实际请求发送 1 个 `released` 占位，无新增整理调用或请求。
- 原生调用／思考回放检查 3 次通过。SQLite 关闭重开后，释放状态、摘要公开历史及持久化事件回放一致，没有重复摘要。
- 修改原夹具文件后，历史回读仍取得已保存的原始结果，确认释放只改变模型视图。
- 另发 **1 次独立用户追问**验证重启及换链后的摘要恢复：实际请求携带恢复摘记，模型无需工具便正确回答 `auth_reset.go`。这次请求单独计数，不属于主任务的整理开销。
- 主任务实际输入 27,509 token（cached 21,632，uncached 5,877），内容／推理输出 379／262；独立追问输入 8,333，内容／推理输出 14／33。没有本次基线对照，不能据此判断净节省。

这是明确引导的单样例，验证 Core、公开历史及事件完整流程；未进行 Desktop 界面操作验收，也不代表自然采用率或成本已达标。功能继续默认关闭。

## 入口与范围

Core 内部 settings 的 `experimental_tool_result_memo` 必须为字符串 `true`；缺省关闭，没有新增 Desktop 设置入口。新会话或真实 reset 后，且配置满足以下条件才接纳新 memo：

- Chat Completions（`openai-compatible`），官方 `https://api.deepseek.com`。
- 模型 `deepseek-flash`；`reasoning_effort: high`，配置的输出上限 4096，不含其他 OpenAI 请求选项。
- 仅装饰可安全扩展的 `builtin_file_slice`、`builtin_history_get_message`，继续服从既有权限／工具筛选。

Engine 不自动切换配置协议，也不在已有历史上中途打开能力。工具定义、模型／端点／凭据身份或配置变化后，当前 epoch 永久停止接纳新 memo；切回原模型不会重开。已应用的占位和已接受的 note 保持。compact 只重建请求链；reset 才建立新 epoch。

本次真正实测的是上述 DeepSeek 路径。跨模型切换的状态机有本地回归，但其他供应商／协议的固定历史移交尚未实测；非 Chat 客户端在已有 memo 状态时会在本地停止发送，提示新建会话或 reset，不把这条路径冒充已兼容。

## 已接通的链路

1. 原型和 Engine 共用 `internal/toolmemo` 的 schema 和参数提取。保留原始调用及 native continuation，执行前先移除请求快照中声明过的保留字段，覆盖所有路由分支。
2. 正常批次关闭时注册完整纯文本结果的稳定外层引用。只接受本次最终 wire 已发送的结果；局部来源、正文哈希和唯一性共同确认可见性。重复调用 ID 在业务执行前拒绝，避免旧释放引用指向新结果。
3. SQLite v47 新增私有 `tool_memo_contexts`：epoch、链身份、投影 revision、结果登记、note、最终 applied／skipped 一并保存。`AppendTurnOutput.ToolMemo` 在同一事务中进行 revision 比较和提交；并发或过期批次失败即回滚，不存在 pending 队列。
4. 已有业务结果由原执行流程保存。批次取消只提交合法 note，所有新释放跳过；半截模型流不接纳 memo。崩溃后读取已提交记录，不从原始 arguments 自动重新执行释放意图。
5. 当前 turn、历史重建、输入预算／用量估算和 compact 摘要输入使用同一释放状态。原文始终留在 canonical，历史工具继续读取原始快照。
6. 正常 note 由未改写的 native arguments 承载。载体被过滤／压缩／换链时，在模型输入中使用有明确助手摘记归属的 host context（user 消息），不伪造没有 reasoning 的 assistant 响应。模型侧恢复不改变 UI 应当在批次后显示正文的设计。
7. 新链将旧原生交换投影为标明来源的历史记录，保留已释放占位与 note，不按相同模型名复用旧 reasoning。供应商原生数据仍保留在数据库。
8. 最终请求冻结后发送，不隐藏重试；预算若修改输出上限，会在发送前重新冻结相同输入。释放门槛本地比较已有预览与释放后的请求，扣除 schema／共享提示／封装及本次 memo 输出的估算开销；至少净减少 64 token。真实全任务成本另行统计。

原型测试的二进制检查点仅用于独立 harness；Engine 使用 SQLite，不把原型 journal 作为生产数据源。

## 公开 note 与 Desktop

工具批次关闭时，Core 将非空、已接纳的 note 挂在本批最后一条 canonical 消息的 `metadata.toolMemos`，字段为 `{id,batchID,callIDs,note}`。`id` 来自已提交决策，`batchID` 来自冻结请求；取消且无结果时允许挂在已保存的调用消息。调用／结果正文和 native continuation 均不改写。

此元数据、私有裁剪决策及 `turn.memo` 事件在同一个 SQLite 事务提交；事件带 `seq/sessionID/turnID/messageID/memoID`，只在提交成功后发布，可通过既有 SSE 游标重放。过期 revision 或找不到本批载体会整体回滚，不能出现“展示已成功但裁剪未提交”。空 note 不生成展示事件；原始 arguments 中未被接纳的 memo 不展示。

Desktop 收到事件后刷新 turn，从同一份历史快照读 note，按批次位置复用普通 Markdown 正文组件。它不把 note 加进流式 text，也不从工具参数猜测 note；稳定 ID 去重，已提交批次替换对应 overlay 前缀。刷新、重连、取消及 compact 使用同一路径。旧快照即使与新快照的毫秒时间戳相同，也不能覆盖已提交的 note。

历史 note 不随私有 epoch reset 删除；会话克隆保留局部 ID，展示按会话隔离。不会新建 UI 草稿数据库或额外模型请求。该扩展为可选协议 31 字段，未新增设置入口；Desktop 本地联调通过 `PUDDING_CORE_DIR=../pudding-core` 生成 contracts，发布前仍需先提交 Core 再更新 Desktop `core.lock.json`。

## 复现

命令读取指定 profile，只在临时目录建立项目、会话和 SQLite；不修改原 profile、用户数据库或 Desktop 锁。

```bash
go run ./cmd/memo-prototype -engine \
  -provider DeepSeek -model deepseek-flash -protocol openai-compatible \
  -compare -output /tmp/pudding-memo-engine-deepseek.json

go run ./cmd/memo-prototype -engine -engine-recovery \
  -provider DeepSeek -model deepseek-flash -protocol openai-compatible \
  -output /tmp/pudding-memo-engine-deepseek-recovery.json
```

`-engine-recovery` 在主任务完成、SQLite 重开后关闭新 memo 接纳，再发一个独立的用户后续问题，验证固定投影和 note 恢复；主任务、恢复验证的请求数及用量分别记录。它不是为整理上下文新增的模型轮次。默认夹具明确要求搭载 `summarize_results`；`-natural` 才观察自发采用，不能把受控夹具当作自然任务采用率证据。

## 本次实测

真实 Engine 对照的基线和 memo 都是 **3 次请求／2 次业务读取**。memo 应用 1 个释放，下一次请求确实发送 released 占位；原生调用／思考 HTTP 边界校验、SQLite 重启及修改源文件后的原快照回读通过。

| 主任务指标 | 基线 | memo |
| --- | ---: | ---: |
| 完整输入 token（cached + uncached） | 27329 | 27439 |
| 其中 uncached | 5057 | 6063 |
| 内容输出／推理 token | 278／66 | 433／87 |
| 最后一次实际输入 token | 10019 | 8926 |

这是短受控样本，完整任务 token 未下降，不能声称成本节省。恢复验证另跑：主任务 3 次请求，加 1 次独立用户后续请求；恢复请求无需工具，正确回答摘记中的 `auth_reset.go`。其他模型及真实长任务成本尚未验收。

公开契约接入后再次用同一 DeepSeek Flash 入口验证：`publicNotesOK`、`memoReplayOK`、重启／原文回读及恢复均通过；仍为主任务 3 次请求、2 次业务调用，另 1 次独立用户后续请求。公开 note 的读写与 UI 通知没有增加模型轮次。

## 回归与后续

```bash
go test -race ./internal/toolmemo ./internal/memoprototype \
  ./internal/provider/openai ./internal/store/sqlitestore \
  ./internal/engine ./internal/contextbuilder ./cmd/memo-prototype
```

覆盖原型既有场景，以及 Engine／SQLite 闭环、CAS 回滚、取消只留 note、当前／历史／compact 投影一致、重启、配置切换后不可重开、reset、最终 wire 的 note 恢复和复杂 schema 跳过。公开历史增加消息／turn 读取、跨会话隔离、native 隐藏、批次位置、原子事件及重启回放验证；Desktop 增加普通 Markdown 渲染、可选缺失、并行批次、重连、取消、compact 与旧快照覆盖回归。

首批真实端点、合成长任务对照自然采用为 0/4，开启组费用高 14.45%，见 [首批数据](tool-result-memo-benchmark.md)。随后 `memo-v4` 精简提示和重复封装：同批关闭／v3／v4 的采用率为 0／4、1／4、0／4，费用为 $0.052987／$0.068455／$0.056551；旧版出现自然释放和后续快照回读，优化版尚未证明这一任务中的净收益，见 [完整对照](tool-result-memo-optimization.md)。相关 race 回归通过。下一阶段需要更有代表性的真实工作负载，更多供应商／协议另行验收；实验入口继续默认关闭。


## 新命名真实仓库任务结果

4 个真实源码审查任务各跑开／关一次，全部原始结果保留。开启组自然采用 0/4、实际释放 0。3 对完整用量任务事实与所需引用均为 27/27，输入增加 4.59%，统一费率费用从 $0.087182832 到 $0.085030116（-2.47%）；变化伴随读取分批和推理输出差异，不能归因于裁剪。另一个关闭组最终流超时且缺 usage，未并入成对费用，未重跑。

完整数据、任务范围、解释复核和未知费用边界见 [真实仓库长任务对照](tool-result-summaries-repository-benchmark.md)。实验仍默认关闭。
