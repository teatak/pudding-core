# Pudding 文档索引

本文是 `docs/` 的统一入口。判断当前行为时，优先级依次为:

1. `AGENTS.md` 的架构硬约束。
2. 当前代码、契约和测试。
3. 下方“当前参考”文档。
4. 已完成设计记录与历史文档。

## 文档归属与路径

本仓维护 daemon、公共协议、数据与工具执行的文档。纯桌面 UI、原生交互和应用发布文档归属
`pudding-desktop`。涉及前后端协作的文档保留一个版本，并在文首标明路径归属。

跨仓文档中的 `internal/`、`cmd/`、`contracts/` 相对 core 根目录；`web/`、`electron/`、
`native/` 相对 desktop 根目录。历史文档中的文件名和行号对应记录时的版本，不保证仍存在。

## 当前参考

| 文档 | 用途 |
| --- | --- |
| [api-quickstart.md](api-quickstart.md) | 独立 daemon 的会话、提交、取消与 SSE 示例 |
| [technology-decisions.md](technology-decisions.md) | 产品定位、后端边界、状态所有权和通信架构 |
| [contracts-checklist.md](contracts-checklist.md) | REST、SSE、消息和工具契约对照 |
| [provider-presets.md](provider-presets.md) | 模型预设更新依据、能力边界与 DeepSeek Responses 接入 |
| [user-input-flow.md](user-input-flow.md) | 跨仓契约：core 的模型等待与答复路由，以及 desktop 的提问面板 |
| [apps.md](apps.md) | App 包、连接字段与 MCP App 配置 |
| [builtin-apps-design.md](builtin-apps-design.md) | 内置 App、动态加载和 runtime-provided App |
| [agent-modes-design.md](agent-modes-design.md) | Chat / Work / Code 能力边界 |
| [code-cli-sandbox-design.md](code-cli-sandbox-design.md) | Code CLI 沙箱与审批规则 |
| [context-working-set.md](context-working-set.md) | 模型工具结果去重、受限预览与 canonical 分页回读 |
| [context-compaction.md](context-compaction.md) | 压缩预算、工具循环内触发、并发边界和回归入口 |
| [agent-eval.md](agent-eval.md) | Agent Eval 使用方法 |
| [tool-usage-report.md](tool-usage-report.md) | 本地工具使用率报告 |
| [code-agent-tooling-report.md](code-agent-tooling-report.md) | core 工具与沙箱改进记录；桌面问题样本按文首跨仓路径约定读取 |

## 待实施设计

| 文档 | 当前状态 |
| --- | --- |
| [tasks-design.md](tasks-design.md) | 跨仓任务设计：core 的调度、持久化与接口，以及 desktop 的任务视图；尚未实现 |
| [approval-optimization-design.md](approval-optimization-design.md) | 减少无谓审批：低风险豁免、首期授权复用、合并确认与临时目录归属；保留项目沙箱现有便利，重新规划，尚未实现 |

## 已完成设计记录

这些文档用于解释决策过程，不作为待办列表:

- [desktop-repository-split-plan.md](desktop-repository-split-plan.md)：工程拆分和 preview 发布已完成；core 采用 Apache-2.0，保留完整 Git 历史。
- [phase-1-plan.md](phase-1-plan.md)
- [code-capabilities-plan.md](code-capabilities-plan.md)
- [code-refactor-design.md](code-refactor-design.md)
- [code-lsp-design.md](code-lsp-design.md)
- [code-cli-first-consolidation-plan.md](code-cli-first-consolidation-plan.md)
- [code-cli-eval-report.md](code-cli-eval-report.md)
- [browser-cdp-unification-plan.md](browser-cdp-unification-plan.md)
- [turn-file-changes-design.md](turn-file-changes-design.md)
- [transcript-scroll-plan.md](https://github.com/teatak/pudding-desktop/blob/main/docs/transcript-scroll-plan.md)
- [computer-use-codex-comparison-2026-09-08.md](https://github.com/teatak/pudding-desktop/blob/main/docs/computer-use-codex-comparison-2026-09-08.md)：Codex 京东/镜像实测、后台输入差距与优化建议；非已实现能力清单。

## [历史与已取代文档](archive/README.md)

以下文档保留用于追溯，不代表当前实现:

| 文档 | 取代原因 |
| --- | --- |
| [progress.md](archive/progress.md) | 2026-07-09 的项目快照，已不再维护 |
| [tool-migration-status.md](archive/tool-migration-status.md) | 仍使用旧 workspace 模式和旧工具归属 |
| [design-tools.md](archive/design-tools.md) | 早期工具/MCP 分阶段草案，已由当前契约和 App 架构取代 |
| [discuss-model-config.md](archive/discuss-model-config.md) | 模型配置讨论已落地为 YAML 配置体系 |
| [browser-automation-plan.md](archive/browser-automation-plan.md) | 旧可见 Chrome MVP 路线 |
| [browser-lifecycle-refactor.md](archive/browser-lifecycle-refactor.md) | 旧 screencast/WebSocket 路线 |
| [pudding-mobile-v1-scope.md](archive/pudding-mobile-v1-scope.md) | Pudding 当前只支持 Electron Desktop |
| [unicorn-ai-mobile-modular-design.md](archive/unicorn-ai-mobile-modular-design.md) | 外部 Mobile 概念稿，不属于当前 Pudding 主线 |
| [ui-improvements-and-tooling-feedback.md](archive/ui-improvements-and-tooling-feedback.md) | 单次体验反馈快照，事项可能已经实现或被后续设计取代 |

## 桌面与发布记录

桌面设计、Computer Use 原生实现及历史发布记录已迁入
[pudding-desktop 文档](https://github.com/teatak/pudding-desktop/tree/main/docs)。core 的完整 Git 历史保留原文件。
