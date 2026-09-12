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
| [provider-presets.md](provider-presets.md) | 模型预设更新依据、能力边界与 DeepSeek Responses 接入 |
| [user-input-flow.md](user-input-flow.md) | 用户问题收集、模型等待、独立面板计时和事后补答 |
| [transcript-stream-performance.md](transcript-stream-performance.md) | 流式回复卡顿优化、性能对比与隔离桌面验收 |
| [design.md](design.md) | Electron Agent Shell 的 Web UI 设计底座 |
| [apps.md](apps.md) | App 包、连接字段与 MCP App 配置 |
| [builtin-apps-design.md](builtin-apps-design.md) | 内置 App、动态加载和 runtime-provided App |
| [agent-modes-design.md](agent-modes-design.md) | Chat / Work / Code 能力边界 |
| [code-cli-sandbox-design.md](code-cli-sandbox-design.md) | Code CLI 沙箱与审批规则 |
| [context-working-set.md](context-working-set.md) | 模型工具结果去重、受限预览与 canonical 分页回读 |
| [releasing.md](releasing.md) | Desktop 构建、签名、发布和更新恢复 |
| [agent-eval.md](agent-eval.md) | Agent Eval 使用方法 |
| [tool-usage-report.md](tool-usage-report.md) | 本地工具使用率报告 |
| [code-agent-tooling-report.md](code-agent-tooling-report.md) | Code 模式工具与终端摩擦记录、优化建议与验收标准（按会话累积） |

## 仍在收尾

| 文档 | 当前状态 |
| --- | --- |
| [release-report-0.3.2.md](release-report-0.3.2.md) | 已公开为最新稳定版；双架构签名公证、九资产和更新清单校验通过；用户暂不升级本机，新签名包交互验收未执行，Worker 开发依赖告警待处理 |
| [workspace-redesign-plan.md](workspace-redesign-plan.md) | 工作区统一内容标签、项目内部文件、资源库与专注改造；保留分阶段验收记录 |
| [computer-use-design.md](computer-use-design.md) | macOS Computer Use 已实现，含输入/实例定位/独立截图及真实 Electron 回归 |
| [computer-use-background-input-probe.md](computer-use-background-input-probe.md) | 后台定向输入实验、五种鼠标手势接入及异常恢复；通用兼容与本次扩展的正式签名包验收未完成 |
| [transcript-virtualization-plan.md](transcript-virtualization-plan.md) | Transcript 会话窗口虚拟化已实现，持续回归 |
| [workspace-resize-performance-plan.md](workspace-resize-performance-plan.md) | Workspace 拖拽性能与 WebView 保活层改造 |
| [attachments-multimodal-plan.md](attachments-multimodal-plan.md) | 图片主链路已完成；文本/PDF、音频与 GC 仍未完成 |
| [voice-migration-plan.md](voice-migration-plan.md) | macOS runtime 发布链路仍在收尾 |
| [electron-migration-plan.md](electron-migration-plan.md) | 主迁移完成；保留多会话生命周期的手动验收记录 |
| [mascot-scene-lab-plan.md](mascot-scene-lab-plan.md) | 隔离的吉祥物 2.5D Scene Lab 实施计划，待按阶段验证；已记录运行时空载 CPU 归因，待优化 |

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
- [computer-use-codex-comparison-2026-09-08.md](computer-use-codex-comparison-2026-09-08.md)：Codex 京东/镜像实测、后台输入差距与优化建议；非已实现能力清单。

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
[0.1.16](release-report-0.1.16.md) ·
[0.1.17](release-report-0.1.17.md) ·
[0.1.18](release-report-0.1.18.md) ·
[0.1.19](release-report-0.1.19.md) ·
[0.1.20](release-report-0.1.20.md) ·
[0.1.21](release-report-0.1.21.md) ·
[0.1.22](release-report-0.1.22.md) ·
[0.1.23](release-report-0.1.23.md) ·
[0.1.24](release-report-0.1.24.md)
