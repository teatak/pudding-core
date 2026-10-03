# Core 文档索引

本仓维护 daemon、公共协议、数据和工具执行。桌面 UI、Electron、Swift Helper 与应用发布见 [desktop 文档](https://github.com/teatak/pudding-desktop/blob/main/docs/README.md)。

判断当前行为以 [AGENTS.md](../AGENTS.md)、当前源码／契约／测试为准；当前参考用于说明，归档用于追溯。源码与硬约束冲突时需明确记录，不能以历史方案覆盖。

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

## 跨仓路径

`internal/`、`cmd/`、`contracts/` 相对 core；`web/`、`electron/`、`native/` 相对 desktop。历史文件中的路径、行号、版本和临时证据目录对应记录时基线。公共契约由 core 维护，desktop 的 `web/contracts/` 为生成文件，不手改、不提交。

## Studio

[Studio 内容契约](studio.md)：原生文档与小组件、正文并发和版本作者、Chat 工具、v25／v27／v28 升级、插件查询与确认操作；产品操作与验收由 Desktop 文档维护。
