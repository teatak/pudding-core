# Core 文档索引

本仓维护 daemon、公共协议、数据和工具执行。桌面 UI、Electron、Swift Helper 与应用发布见 [desktop 文档](https://github.com/teatak/pudding-desktop/blob/main/docs/README.md)。

当前行为以源码、公共契约和测试为准；当前参考用于说明，归档用于追溯。[AGENTS.md](../AGENTS.md) 提供简短的协作说明和文档入口。

## 当前参考

| 文档 | 内容 |
| --- | --- |
| [技术选型与当前架构](technology-decisions.md) | 仓库边界、状态所有权、Provider、存储、API 与 SSE |
| [API 快速开始](api-quickstart.md) | 临时 daemon 的创建会话、提交、取消与续传 |
| [公共契约](../contracts/README.md)／[字段对照](contracts-checklist.md) | runtime、REST、SSE、消息与工具协议 |
| [模型预设](provider-presets.md) | 模板与运行配置的边界、provider 适配记录 |
| [插件](plugins.md)／[内置插件](builtin-plugins-design.md) | 包、连接、动态加载与 runtime-provided 插件 |
| [Chat / Work / Code](agent-modes-design.md) | 模式与工具能力边界 |
| [CLI 沙箱](code-cli-sandbox-design.md) | Ask / Auto / Full、host 和授权复用 |
| [Agent 工具契约](agent-tool-contracts.md) | 路径、临时附件、后台命令、搜索与参数诊断 |
| [工具结果按需读取](context-working-set.md)／[上下文压缩](context-compaction.md) | canonical 历史、结果投影与上下文预算 |
| [用户问题收集](user-input-flow.md) | 等待、补答、canonical 数据与桌面面板 |
| [会话协作](session-collaboration-plan.md) | 父子调度、结果回收、任务卡片与统一审批 |
| [浏览器远程访问](remote-access.md) | 配对、设备凭证、LAN / relay 与授权通知 |
| [定时任务](scheduled-tasks-plan.md) | 规则、持久化、调度、执行记录与通知 |
| [Agent Eval](agent-eval.md)／[工具使用率报告](tool-usage-report.md) | 开发验证和本地统计入口 |

协作与定时任务文档保留原文件名供现有引用使用；首版已有实现和发布记录，其历史实施段落不作为新的任务清单。

## 文档范围

本仓公开文档描述 core 的当前行为、架构、契约与验证依据。桌面产品内部待办和跨仓交付清单由 desktop 仓库维护。

## 历史归档

[归档索引](archive/README.md) 收录已完成／被取代的计划和问题样本：

- `archive/plans/`：第一阶段、Code/LSP、浏览器、拆仓与审批等原方案。
- `archive/reports/`：工具问题样本、CLI Eval 等阶段报告。
- `archive/` 原有历史文件：旧架构、移动端概念和早期进度，不恢复主线。

### summarize_results 实验（已停止并撤回）

2026-10-19 撤回 Core／Desktop 功能接入；以下文件保留当时的设计与评测结论，不代表当前可用功能。入口、撤回范围及证据边界见 [失败总结](tool-result-summaries-withdrawal.md)。原始报告位于 `docs/validation/`，离线分析入口见 [说明](../cmd/memo-prototype/README.md)。

| 历史文档 | 记录范围 |
| --- | --- |
| [工具结果摘要原型](tool-result-memo-prototype.md) | 隔离 Chat Completions 验证入口、最终可见清单、即时释放、原生续接及快照回读；保留独立验证入口 |
| [工具结果摘要：Core 实验接入](tool-result-memo-engine.md) | 默认关闭的 Engine／SQLite、公开历史／事件、Desktop note 展示与 DeepSeek Flash 验证 |
| [工具结果 memo：长任务对照](tool-result-memo-benchmark.md) | DeepSeek Flash 自然采用、任务质量、缓存与完整成本；保留 pilot 和逐请求实测记录 |
| [工具结果 memo：触发与开销优化](tool-result-memo-optimization.md) | 可选参数示例、精简封装与关闭／旧版／优化版三组对照 |
| [summarize_results 真实仓库长任务对照](tool-result-summaries-repository-benchmark.md) | 冻结真实源码、自然采用、事实与引用质量、完整请求用量及失败记录 |
| [summarize_results 提示与触发诊断](tool-result-summaries-trigger-diagnosis.md) | 2×2 对照及结果提醒探针；区分未采用、超长拒绝与实际释放，候选已撤回 |
| [summarize_results 简短约束与复测](tool-result-summaries-brief-notes.md) | 新提示四组任务仍 0/4 采用；无摘要长度样本，附模型复核、质量与完整用量 |

## 跨仓路径

`internal/`、`cmd/`、`contracts/` 相对 core；`web/`、`electron/`、`native/` 相对 desktop。历史文件中的路径、行号、版本和临时证据目录对应记录时基线。公共契约由 core 维护，desktop 的 `web/contracts/` 为生成文件，不手改、不提交。

## Studio

[Studio 内容契约](studio.md)：原生文档与小组件、正文并发和版本作者、Chat 工具、v25 及 v26→v30 合并升级、插件查询与确认操作；产品操作与验收由 Desktop 文档维护。

[内容命名约定](content-naming.md)：Core 的 `note` / `document` 文件、类型、执行路径与协议／历史兼容边界。
