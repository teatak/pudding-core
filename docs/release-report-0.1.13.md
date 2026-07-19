# Pudding 0.1.13 发版报告

日期：2026-07-19

对比基线：`v0.1.12`（`dcf1ae93`）

审查版本：`98fc221f` 的 turn 文件变更与项目工作区整合及 `0.1.13` 版本提交

## 发版结论

**可以按项目文件变更体验功能版发布。** 本次将每轮 AI 产生的文件变更直接整合进项目工作区，
支持以项目标签查看历史 diff，并改进 transcript 中的变更摘要。SQLite schema 保持 v4，不新增迁移，
不修改会话、消息、turn 文件快照、画布或浏览器数据的持久化结构。

## 改动摘要

- transcript 中的文件变更改为摘要卡片，显示文件数量、总新增行和总删除行。
- 超过 6 个文件时默认显示前 5 个，可在原位置展开或收起完整列表。
- 有项目的会话会在项目工作区中打开 turn diff，并作为独立的虚拟文件标签展示。
- turn diff 标签支持关闭、关闭其他和关闭右侧，与普通项目文件标签统一交互。
- 点击项目树、Git diff 或普通文件标签时会自然退出当前 turn diff，但保留已打开标签。
- 项目树会定位当前变更文件，并将所选 diff 作为可见 UI 上下文提供给会话。
- 无项目会话继续使用原有独立文件预览，不改变既有工作流。
- diff 查看器改用应用主题状态，并修复空行背景与可选中文本表现。
- 消息元信息移到文件变更摘要之后，使时间、模型和操作区保持在整个回复末尾。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Transcript | 中 | 文件变更以摘要卡片展示，并调整消息元信息位置。 |
| 项目工作区 | 高 | 历史 turn diff 成为项目文件标签的一部分。 |
| 文件标签 | 中 | 普通文件、Git diff 与 turn diff 共享关闭和切换逻辑。 |
| UI 上下文 | 中 | 当前 turn diff 以 `project_diff` 资源暴露给会话。 |
| 无项目会话 | 低 | 继续使用既有独立 diff 预览路径。 |
| SQLite | 无 | schema 和 release fingerprint 均保持 v4。 |
| 会话、消息与文件快照 | 无 | 不修改既有事实源、数据结构或迁移语义。 |

## 数据库分析

- `currentSchemaVersion` 与 `PRAGMA user_version` 均保持 `4`。
- `schema.sql`、`schemaMigrations` 与 v4 release fingerprint 未修改。
- turn 文件内容继续通过既有 API 读取，前端虚拟标签仅保存在内存状态中。
- 不需要数据库备份、重建、回填或兼容迁移。
- 既有会话、消息、turn 文件快照、画布和浏览器数据可直接继续使用。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器检查通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- `git diff --check` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.13` 版本和本报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes

### File Changes

- Summarize each turn's edited files with aggregate additions and deletions directly in the transcript.
- Expand or collapse longer file-change lists without leaving the conversation.

### Project Workspace

- Open historical turn diffs as first-class tabs inside the associated project workspace.
- Locate the changed file in the project tree and expose the selected diff as visible session context.
- Manage turn diff tabs with the same close, close others, and close right actions as project files.

### Compatibility

- Keep the existing standalone diff preview for sessions without an associated project.
- Preserve the local database at schema v4 with no migration required.
