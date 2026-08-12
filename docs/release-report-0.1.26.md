# Pudding 0.1.26 发版报告

日期：2026-08-13

对比基线：`v0.1.25`（`441a3415`）

审查范围：`v0.1.25..HEAD` 及本次版本提交

## 发版结论

**可以作为会话栏、长对话渲染和工作区布局优化版本发布。** 本次将会话栏拆分为职责明确的组件，
引入虚拟列表以改善长对话稳定性，改用动态分栏比例保存工作区布局，并移除内置终端运行链路，统一从
项目目录打开系统终端。SQLite schema 保持 v10，不执行数据迁移。

## 改动摘要

- 重构会话栏组件和状态模型，统一项目、会话、运行进度、折叠与悬浮菜单交互。
- 为 transcript 引入虚拟列表，修复长对话测量、展开错误详情和滚动定位的稳定性。
- 将工作区绝对宽度持久化改为动态分栏比例，使不同窗口尺寸下的布局恢复更自然。
- 移除 daemon 内置终端 API、进程管理和 xterm 前端，改为从项目目录打开系统终端。
- 简化空工作区、资源标签页和关联布局逻辑，清理不再使用的终端与插画代码。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 会话栏 | 高 | 组件结构、状态推导、项目和会话交互整体重构。 |
| 对话渲染 | 中 | 长对话改用虚拟列表，调整测量、展开和滚动行为。 |
| 工作区布局 | 中 | 分栏状态由绝对宽度改为比例，响应窗口尺寸变化。 |
| 终端入口 | 中 | 删除内置终端，项目操作改为打开系统终端。 |
| SQLite | 无 | schema、迁移和持久化事实源均未改变。 |

## 数据库分析

- `currentSchemaVersion` 保持 `10`。
- `internal/store/schema.sql` 与 `internal/store/sqlitestore/migrations.go` 相对 `v0.1.25` 的 SHA-256 完全一致。
- 正式库只读检查结果为 `PRAGMA user_version=10`、`PRAGMA quick_check=ok`。
- 本版不改写 canonical messages、sessions、turns、projects、canvas、browser history、FTS 或 App 配置。
- 本版不重建任何派生索引，不创建迁移备份，也不触发迁移备份清理。
- 数据库降级边界不变：v0.1.25 与 v0.1.26 均使用 schema v10。

## 兼容性

- stable/preview 更新通道、bundle identifier、Developer ID 签名、公证和 `~/.pudding` 数据目录保持不变。
- 会话 API 继续显式携带 session ID；后端没有引入 focus 状态或无 session scope 主路径。
- 删除内置终端不会影响 canonical conversation 或项目文件；终端会话本来就是临时资源，不落 SQLite。
- 系统终端入口在 macOS 使用 Terminal，在 Windows 和 Linux 使用各平台可用终端，并以项目目录作为工作目录。

## 已完成验证

- 审查 `v0.1.25..HEAD` 的 5 个提交、完整文件变更和持久化调用点。
- 确认 schema 与迁移文件相对 `v0.1.25` 未改变。
- 只读检查正式数据库：`PRAGMA user_version=10`、`PRAGMA quick_check=ok`。
- `go test -tags "sqlite_fts5 webrtcaec" ./...`。
- `npm run test:electron`，131 项通过。
- `web/` 下执行 `npm run build`。
- `workers/oauth/` 下执行 `npm run typecheck`。
- `npm run smoke:electron-browser`，覆盖文件、favicon、多标签、多会话、历史、焦点隔离、输入、弹窗、截图与撤销。
- `make schema-check`。
- `git diff --check v0.1.25`。

## 剩余发版门槛

1. 提交并推送 `0.1.26` 版本号、格式修复与发版报告。
2. 构建并验证 arm64/x64 签名公证包。
3. 核验 Draft 的英文功能清单与 9 个标准资产后 finalize。

## Release Notes

### Session Navigation

- Rebuild the session rail with clearer component boundaries and more reliable project, session, progress, collapse, and menu interactions.
- Keep active session state and sidebar overlays stable while navigating complex project lists.

### Conversation Performance

- Virtualize long transcripts to improve rendering performance and scrolling stability.
- Improve dynamic row measurement, expandable error details, and transcript positioning.

### Workspace Layout

- Preserve workspace sizing as a responsive split ratio instead of a fixed pixel width.
- Simplify empty workspace, resource tab, and pane layout behavior across window sizes.

### Terminal Experience

- Replace the embedded terminal runtime with a direct action that opens the project directory in the system terminal.
- Remove obsolete terminal APIs, process management, xterm dependencies, and persisted UI paths.

### Data Safety

- Keep SQLite at schema v10 with no migration, canonical data rewrite, index rebuild, or migration backup.
