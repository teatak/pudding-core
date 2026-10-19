# 工具结果摘要：单协议隔离原型

更新日期：2026-10-19。入口为 `cmd/memo-prototype`，用于验证 Chat Completions 协议可行性。原型复用现有 OpenAI-compatible 客户端、结果预览、文件片段读取和历史结果分页；本文记录独立 harness。默认关闭的 Engine／SQLite／compact、公开事件和 Desktop 展示后续实现见 [Core 实验接入](tool-result-memo-engine.md)。

当前模型侧仅使用可选 `summarize_results: {result_refs, summary}`，`summary` 必填且非空；无旧格式别名或迁移。内部 note／memo 记录与命令名称暂保留，详见 [当前模型参数](tool-result-memo-engine.md#当前模型参数)。末尾真实端点数据为更名前的历史验证；新格式已通过 Engine 受控全流程，见 [实测记录](tool-result-memo-engine.md#2026-10-19-新命名受控实测)。

## 运行

从指定 Pudding home **只读**加载一个明确指定的 profile 和模型。原型只支持 `openai-compatible`；profile 使用其他协议时必须通过 `-protocol openai-compatible` 显式指定隔离验证路径，报告同时记录配置协议和实际协议，不修改 profile。模型调用使用本机已有凭据；报告不含凭据和原始思考正文。

```bash
go run ./cmd/memo-prototype \
  -home "$HOME/.pudding-dev" \
  -provider DeepSeek -model deepseek-flash \
  -protocol openai-compatible \
  -compare -output /tmp/pudding-memo-prototype.json
```

每次运行在系统临时目录生成只读业务夹具：先读目录清单，再按清单读取处理函数，回答过期 token 的 HTTP 状态。默认夹具明确要求在必要的第二次读取中附带一份 `summarize_results`，以验证调用后的协议链路；不增加专门的清理调用。`-compare` 增加一次独立基线任务，基线不开放 memo；两者的业务问题相同，memo 版本多了上述明确的控制验证要求，因此不是自然使用率或严格成本实验。

传入 `-natural` 可取消夹具中的明确要求，观察模型是否自发使用可选 memo。没有有效释放时任务仍可正常完成，但原型的释放验收会返回非零状态；报告区分任务成功与原型验收成功。没有 memo 不会触发重试、追问或额外整理请求。

`-negative-probe` 是独立协议验收：只发送一次工具调用／结果已配对、但缺少 `reasoning_content` 的无状态历史夹具，不进入任务循环或执行工具。它的请求数与正常任务分开记录；只有 400 且错误明确指出缺失该字段才算负例通过，不把鉴权失败或任意请求失败记为通过。

```bash
go run ./cmd/memo-prototype \
  -provider DeepSeek -model deepseek-flash -protocol openai-compatible \
  -negative-probe -output /tmp/pudding-memo-deepseek-negative.json
```

`-max-requests` 限制每个任务的正常请求数，默认 8，允许 3–12；每个任务总超时三分钟。结束后删除临时目录和私有检查点，保留指定位置的报告。不创建或修改用户会话、profile、项目或生产数据库。

## 已实现的边界

- 模型侧只装饰 `builtin_file_slice`、`builtin_history_get_message` 的普通对象 schema；已有业务 `summarize_results`、重名定义、插件归属及复杂根 schema 不装饰。共享提示词解释可选性、事实时序、外层引用、立即应用或跳过、无需确认和禁止额外整理调用。
- 依据产生调用的那次请求的定义快照，从执行副本移除 `summarize_results`；原始参数和原生 continuation 原样保留。无效 memo 不影响能够合法提取的业务参数；调用审计区分 `absent`、`null`、`invalid`、`valid` 和 `undeclared`。
- 执行范围只允许临时项目根目录中的文件片段，以及当前隔离会话已保存的纯文本结果分页。原型不会执行写文件、命令、网络、第三方工具或整条历史消息读取。
- 每个可释放结果从第一次进入模型视图起拥有外层 `{result_ref, ok, view, content}`。回读页的外层引用指向页自身；正文内的引用仍指向原快照。释放后保留相同身份和真实执行状态，`view` 变为 `released`，省略 `content`。
- `PrepareChatRequest` 冻结最终 wire 请求。可见清单在原生重放、媒体处理和工具配对过滤后由同一份冻结输入生成，并与 Core 来源映射及正文哈希匹配；工具正文不能自行授予释放资格。发送时直接使用冻结字节，不重新投影，也不自动重试 `stream_options` 兼容错误。
- 每批最多接受第一份合法 memo；最多 32 个引用、800 字符 note。只处理产生 memo 的请求已发送的结果；未知、未来、歧义、不可回读和跨边界目标跳过。
- 原型保守要求**新增释放必须有合法 note**；`result_refs: []` 的纯 summary 仍允许。正常路径由未经改写的原生调用参数承载 note；载体被过滤时从同一逻辑记录在批次边界投影一次，不恢复已释放正文。
- 关闭批次时进行本地试算，将已有预览与实际释放视图对比，计入结果封装、共享提示、schema 和 memo 输出的估算开销；估算净减少至少 64 token 才应用。门槛不预测后续缓存、回读或真实收费，完整任务用量另计。
- 原始调用／结果、note 和最终 `applied`／`skipped` 决策在同一私有二进制检查点中通过临时文件、同步和原子替换保存。二进制编码保留原生 JSON 及参数字节，避免 JSON 格式化重排原生数据；该格式不是 daemon 公共持久化契约。
- 已提交结果在重启后恢复；跳过项没有 pending 队列，也不会因门槛变化补应用。半截模型流不执行工具或释放；中断工具批次最多提交合法 note，释放跳过。带有在途标记的检查点拒绝恢复执行，避免重跑半批工具。
- 固定绑定协议、端点、模型、推理／输出配置、凭据指纹、schema、工具定义、项目根和功能开关。原型不做配置迁移；不兼容变更必须新建隔离会话，不能在旧链中注入 memo 或切回旧原生状态。

检查点仅验证进程重启，不作为断电耐久性或生产 SQLite 事务实现的验收。

## 验证与报告

```bash
go test -race ./internal/memoprototype ./internal/provider/openai ./cmd/memo-prototype
```

本地测试覆盖：三次正常请求内完成释放、业务参数剥离、原生调用和思考保留、memo 缺失／无效、收益不足后不补应用、流截断、可见清单受最终配对过滤约束、正文伪造引用、独立回读页身份、调用 ID 歧义、载体过滤后的 note 保留、配置变化拒绝及重启后的原文回读。冻结请求测试还覆盖输入副本不可变和没有隐藏 HTTP 重试。

真实端点报告分别记录 HTTP 请求数、工具调用、memo 状态与最终决策、估算输入、实际 cached／uncached／输出／推理用量、后续已发送的释放占位、重启投影和快照回读。HTTP 发送边界逐项比较原始 `tool_calls` 与 `reasoning*`，报告只保留校验数量以及端点返回的模型／响应标识。

`prototypePassed` 要求任务正确、检查点恢复一致；启用 memo 的任务还必须发生实际释放、在后续正常请求发送该占位、确实产生思考并回放原生状态，以及回读仍取得原始证据。快照验证在改写夹具文件后执行，排除“回读其实重新执行了文件工具”的假阳性。

端点返回成功不单独构成兼容证据；没有带思考的真实释放也不算通过。此原型未实现 Anthropic 前缀绑定强制检查、其他协议签名负例或跨模型移交，不能据此启用这些路径。报告中的模型名如果仍是别名，不代表已经固定了供应商内部不可变版本。

## Engine 后续接入

独立 harness 不修改用户配置或 Desktop `core.lock.json`。后续已完成默认关闭的 Engine／SQLite、公开历史／事件和 Desktop note 展示实验接入，见 [Core 实验接入](tool-result-memo-engine.md)。完整任务成本与质量验证仍待完成。

跨仓设计见 `pudding-desktop/docs/tool-result-memo-plan.md`。协议依据：[OpenAI function calling](https://developers.openai.com/api/docs/guides/function-calling)、[DeepSeek 思考回传说明](https://api-docs.deepseek.com/guides/thinking_mode/)。Chat Completions 原型沿用现有客户端显式 `strict: false`，使用普通可选对象 schema；并未声称通过严格 schema 模式的验收。验证配置显式指定 `reasoning_effort: high`、输出上限 4096；DeepSeek 思考模式按该端点文档默认开启。

## 2026-10-19 DeepSeek Flash 实测

用户指定模型 `deepseek-flash`，官方端点 `https://api.deepseek.com/chat/completions`。本机 profile 配置为 Responses，本次通过显式参数运行 Chat Completions，未修改 profile。三个响应均返回模型名 `deepseek-flash` 和相同 fingerprint `e72d3c7f663d3e1383b00b3884cb4501`；保留响应 ID，不将别名当作不可变模型版本。

| 指标 | 基线 | memo 明确控制夹具 |
| --- | --- | --- |
| 正常 HTTP 请求／业务读取 | 3／2 | 3／2 |
| 已应用 memo／后续已发送释放占位 | 0／0 | 1／1 |
| 最后一次实际输入 token，含 cached | 3833 | 2760 |
| 完整任务实际输入 token | 8763 | 8870 |
| 其中 cached／uncached | 4992／3771 | 4096／4774 |
| 内容输出／推理 token | 253／32 | 375／82 |
| HTTP 边界原生调用校验次数 | 3 | 3 |
| 任务与检查点投影 | 通过 | 通过 |
| 释放后原始快照回读 | 不适用，无释放 | 通过 |

该样本证明指定结果能在下一次正常请求退出正文，同时保留真实思考与调用参数、note 和原始回读；正常路径没有增加请求。**短任务的完整输入和输出均未下降，不能据此宣称成本降低。** 两组提示词因协议夹具的明确控制要求而不同；还需真实任务、重复运行与质量对照，评估 schema、note、缓存失效和回读的总影响。

独立负例实际发送 1 次请求，端点返回 400，并明确要求回传 `reasoning_content`；未执行工具、未重试，返回用量为 0。它验证必需字段约束，不能代替签名／前缀绑定的验证；这里的 DeepSeek 路径仍按明文 reasoning 完整回放处理。负例不计入上表的正常任务请求数。

2026-10-19 复查修正：共享 schema 拒绝条件／否定／属性数量约束的根对象；缺失 note 载体时使用有归属的 host context，避免合成缺少原生思考字段的 assistant 响应。恢复投影已通过 DeepSeek Flash 独立后续请求验证。
