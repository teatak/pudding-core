# Pudding 0.1.25 发版报告

日期：2026-08-12

对比基线：`v0.1.24`（`06d91f8e`）

审查范围：`v0.1.24..HEAD`

## 发版结论

**可以作为会话归档、搜索体验和输入区布局优化版本发布。** 本次增加可恢复的会话归档与
30 天自动清理机制，重构会话搜索结果排序和归档管理界面，并统一浮动输入区及相关布局控件。
SQLite schema 从 v9 升级到 v10；迁移只为现有 session 写入默认未归档状态并创建索引，不改写
canonical messages 或其他业务数据。

## 改动摘要

- 将会话删除改为归档，支持从设置中搜索、恢复、单独永久删除或按项目批量清理。
- 归档会话保留 30 天，daemon 定时永久清理过期内容；归档时同步取消 turn、语音和排队输入并释放运行资源。
- 活跃会话、归档会话和内部维护查询使用显式 scope，避免归档数据重新进入常规会话列表和项目活跃排序。
- 重构会话搜索，将标题、项目、模型和消息命中统一排序，并仅在存在消息匹配时显示正文摘要。
- 统一浮动输入区、工具栏、推理强度选择和工作区布局控件，删除不再使用的独立音频控制组件。
- 增加 Electron Builder 依赖版本一致性校验，防止打包配置与实际依赖漂移。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 会话生命周期 | 高 | 删除主路径改为归档，新增恢复、永久删除和 30 天自动清理。 |
| SQLite | 高 | schema v9 升级到 v10，新增归档字段和索引。 |
| 会话搜索 | 中 | 搜索聚合、匹配排序、摘要与结果布局重构。 |
| 会话侧边栏 | 中 | 菜单动作、归档状态和列表刷新行为调整。 |
| 输入区与布局 | 中 | 浮动输入区和关联工具栏组件重新组织。 |
| 桌面打包 | 低 | 新增 Electron Builder 版本一致性守卫。 |

## 数据库分析

- `currentSchemaVersion` 从 `9` 升级到 `10`。
- v10 迁移在 `sessions` 新增 `archived_at INTEGER NOT NULL DEFAULT 0`，并创建
  `sessions_archived_at` 索引；迁移事务提交后才更新 `PRAGMA user_version`。
- 迁移不会改写 canonical messages、turns、projects、canvas、browser history、FTS 或 App 配置；现有
  session 仅通过列默认值保持活跃状态。
- 本次创建一个新的派生索引，不重建 FTS 或其他派生数据。
- 正式库检查结果为 schema v9、`PRAGMA quick_check=ok`，当前 `sessions` 表符合 v9 基线。
- 升级前会创建数据库备份；迁移和 schema 校验成功后清理旧迁移备份，只保留本次最新备份。没有迁移时
  不执行备份清理。
- v10 是单向前进迁移；v0.1.24 及更早版本会拒绝打开 v10 数据库。回退前必须恢复迁移备份或继续使用
  v0.1.25 及更新版本。
- 归档动作会更新目标 session 的归档时间并取消待处理输入；用户永久删除或超过 30 天后，才按现有外键
  关系清除该 session 的 canonical 数据。

## 兼容性

- stable/preview 更新通道、bundle identifier、Developer ID 签名、公证和 `~/.pudding` 数据目录保持不变。
- 所有新增业务 API 均显式使用 session ID；后端没有引入 focus 状态或无 session scope 主路径。
- 活跃列表默认排除归档会话，内部需要完整数据的调用方显式请求 `all` scope。
- Provider profile YAML、模型元数据、硬件所有权、transport 和 canonical message 边界保持不变。
- 正式版与预览版共享数据库，因此后续 preview/stable 版本必须继续包含 schema v10 迁移。

## 已完成验证

- 审查 `v0.1.24..HEAD` 的提交、持久化调用点、schema 和迁移实现。
- 只读检查正式数据库：`PRAGMA user_version=9`、`PRAGMA quick_check=ok`。
- 确认 v10 released fingerprint 已追加，旧 fingerprint 未修改。
- 使用正式 v9 数据库文件的临时副本演练升级：迁移后 `PRAGMA user_version=10`、
  `PRAGMA quick_check=ok`，原有 1 个 session 和 73 条 message 保持不变，归档索引存在且生成 1 份 v9 备份。
- `go test -tags "sqlite_fts5 webrtcaec" ./...`。
- `npm run test:electron`，128 项通过。
- `web/` 下执行 `npm run build`。
- `workers/oauth/` 下执行 `npm run typecheck`。
- `npm run smoke:electron-browser`，覆盖文件、favicon、多标签、多会话、历史、焦点隔离、输入、弹窗、截图与撤销。
- `make schema-check`。
- `git diff --check v0.1.24`。

## 剩余发版门槛

1. 提交并推送 `0.1.25` 版本号与发版报告。
2. 构建并验证 arm64/x64 签名公证包。
3. 核验 Draft 的英文功能清单与 9 个标准资产后 finalize。

## Release Notes

### Session Archive

- Archive sessions instead of deleting them immediately, with restore and permanent-delete controls in Settings.
- Keep archived sessions for 30 days and automatically purge expired data while releasing active runtime resources promptly.
- Keep archived sessions out of active lists, project activity ordering, and queued-work recovery.

### Search and Navigation

- Rank session search results across titles, projects, models, and matching messages.
- Show message excerpts only when message content matches the query.
- Add focused archived-session search and project-grouped management.

### Composer and Workspace

- Unify the floating composer layout, toolbar controls, reasoning picker, and workspace layout actions.
- Remove redundant audio-control UI and simplify the surrounding component structure.

### Packaging and Data

- Guard Electron Builder configuration against dependency-version drift.
- Migrate SQLite from schema v9 to v10 with a transactional archive column and index.
- Preserve existing sessions and canonical content while retaining one migration backup for rollback recovery.
