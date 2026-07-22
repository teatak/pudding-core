# Pudding 文档索引

本文是 `docs/` 的统一入口。判断当前行为时，优先级依次为:

1. `AGENTS.md` 的架构硬约束。
2. 当前代码、契约和测试。
3. 下方“当前参考”文档。
4. 已完成设计记录与历史文档。

## 当前参考

| 文档 | 用途 |
| --- | --- |
| [technology-decisions.md](technology-decisions.md) | 产品定位、后端边界、状态所有权和通信架构 |
| [contracts-checklist.md](contracts-checklist.md) | REST、SSE、消息和工具契约对照 |
| [design.md](design.md) | Electron Agent Shell 的 Web UI 设计底座 |
| [apps.md](apps.md) | App 包、连接字段与 MCP App 配置 |
| [builtin-apps-design.md](builtin-apps-design.md) | 内置 App、动态加载和 runtime-provided App |
| [agent-modes-design.md](agent-modes-design.md) | Chat / Work / Code 能力边界 |
| [code-cli-sandbox-design.md](code-cli-sandbox-design.md) | Code CLI 沙箱与审批规则 |
| [releasing.md](releasing.md) | Desktop 构建、签名、发布和更新恢复 |
| [agent-eval.md](agent-eval.md) | Agent Eval 使用方法 |
| [tool-usage-report.md](tool-usage-report.md) | 本地工具使用率报告 |

## 仍在收尾

| 文档 | 当前状态 |
| --- | --- |
| [attachments-multimodal-plan.md](attachments-multimodal-plan.md) | 图片主链路已完成；文本/PDF、音频与 GC 仍未完成 |
| [voice-migration-plan.md](voice-migration-plan.md) | macOS runtime 发布链路仍在收尾 |
| [electron-migration-plan.md](electron-migration-plan.md) | 主迁移完成；保留多会话生命周期的手动验收记录 |

## 已完成设计记录

这些文档用于解释决策过程，不作为待办列表:

- [phase-1-plan.md](phase-1-plan.md)
- [code-capabilities-plan.md](code-capabilities-plan.md)
- [code-refactor-design.md](code-refactor-design.md)
- [code-lsp-design.md](code-lsp-design.md)
- [code-cli-first-consolidation-plan.md](code-cli-first-consolidation-plan.md)
- [code-cli-eval-report.md](code-cli-eval-report.md)
- [browser-cdp-unification-plan.md](browser-cdp-unification-plan.md)
- [turn-file-changes-design.md](turn-file-changes-design.md)
- [transcript-scroll-plan.md](transcript-scroll-plan.md)

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

## 发布记录

`release-report-*.md` 是对应版本的历史发版报告，不描述当前主线状态。

[0.1.4](release-report-0.1.4.md) ·
[0.1.5](release-report-0.1.5.md) ·
[0.1.6](release-report-0.1.6.md) ·
[0.1.7](release-report-0.1.7.md) ·
[0.1.8](release-report-0.1.8.md) ·
[0.1.9](release-report-0.1.9.md) ·
[0.1.10](release-report-0.1.10.md) ·
[0.1.11](release-report-0.1.11.md) ·
[0.1.12](release-report-0.1.12.md) ·
[0.1.13](release-report-0.1.13.md) ·
[0.1.14](release-report-0.1.14.md) ·
[0.1.15](release-report-0.1.15.md) ·
[0.1.16](release-report-0.1.16.md)
