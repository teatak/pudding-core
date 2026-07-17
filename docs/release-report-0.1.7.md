# Pudding 0.1.7 发版报告

日期：2026-07-17  
对比基线：`v0.1.6`（`977ce442`）  
审查版本：`07350e55` 加 daemon token 并发恢复修复与 `0.1.7` 版本提交

## 发版结论

**可以通过 v1→v3 数据库迁移发布。** 本次新增 Code Turn 文件变更记录、会话级画布与全局
已保存小组件，并统一 Apps、MCP 与 Skills 管理。迁移会先生成数据库备份，再在事务中升级
schema；完整测试和真实 release 数据库副本迁移均已通过。

## 改动摘要

- Code Turn 结束后记录新增、修改、删除和重命名文件，在对话中显示摘要，并可打开持久化多文件 Diff。
- 画布窗口改为会话级数据；小组件可保存到全局库，在其他会话中复用，并跟踪版本和未保存修改。
- MCP server 统一作为 App 管理，支持导入标准 MCP 配置；新增 App 创建能力和原子校验写入。
- Skill 改为直接编辑后校验，移除旧 Draft 与 Review 中间流程。
- Code 模式可按需创建临时工作区，并改进常见命令行工具 PATH 与单次审批执行路径。
- 改进 daemon 复用校验、文件日志、音频设备切换、表单交互和工作区布局。
- 修正并发恢复空 daemon token 时可能误删新 token 的竞态。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Turn 文件变化 | 高 | Code Turn 首次工具调用前快照，收尾时与 canonical message 同事务落库。 |
| 画布与小组件 | 高 | 活跃和最近关闭窗口按 session 隔离，新增全局已保存小组件。 |
| Apps / MCP / Skills | 高 | MCP 并入 Apps；App 保存和 Skill 校验采用新的直接流程。 |
| Code 模式 | 中 | 无项目时使用 session 临时工作区，并补充本地工具链 PATH。 |
| daemon / Electron | 中 | 增加身份校验、日志轮转、启动恢复和音频设备刷新。 |
| 工作区 UI | 中 | Diff、表单、标签、侧边栏和画布操作有较多调整。 |
| SQLite 结构 | 高 | schema 从 v1 升级到 v3，新增表并重建画布相关表。 |
| 自动更新与打包 | 低 | 双架构签名、公证和资产格式不变；修正公开 tag 的发布后校验。 |

## 数据库分析

### Schema 与迁移

- `currentSchemaVersion` 从 `1` 升级到 `3`。
- v2 新增 `turn_file_changes` 表及 `(session_id, turn_id, path)` 索引。
- v3 将 `canvas_items`、`canvas_closed_items` 改为以 `session_id` 为作用域的复合主键。
- v3 新增 `canvas_saved_items`，并增加保存来源、revision 和 dirty 状态字段及索引。
- 每次从旧版本升级前执行 WAL checkpoint，并生成 `pudding.db.backup-v<version>-<timestamp>`。
- v2、v3 分别在独立事务中执行；失败会回滚当前迁移并保留升级前备份。

### 数据迁移语义

- 仍归属于有效 session 的当前画布窗口和最近关闭窗口保留在对应 session。
- 无 session 或 session 已不存在的旧画布记录迁入全局已保存小组件，避免静默丢失。
- sessions、messages、turns、events、浏览器标签和 FTS5 搜索索引不重写。
- 已保存的小组件使用 revision 做冲突检测；会话副本保留基线 revision 和 dirty 状态。

### 真实 release 数据库副本验证

- 原数据库：`PRAGMA user_version = 1`，`PRAGMA quick_check = ok`；全程只读且升级后仍为 v1。
- 副本升级：`PRAGMA user_version = 3`，`quick_check = ok`，`foreign_key_check` 无错误。
- 2 个 session 和 2 个有效当前画布窗口完整保留。
- 3 个引用已删除 session 的最近关闭窗口迁入全局已保存小组件。
- 升级前 v1 备份文件已正确生成并可读。

### 兼容与回退

- 支持正式 v1 数据库直接升级到 v3，也支持中间 v2 数据库继续升级。
- 新版本首次成功启动后，旧版客户端不认识 v3 画布结构，不支持直接降级运行。
- 需要回退时应先退出 Pudding，再恢复自动生成的 v1 备份并安装旧版本。

## 已完成验证

- `go test -tags sqlite_fts5 ./...`
- daemon token 原子初始化与空文件并发恢复测试连续 100 轮通过
- `npm run test:electron`：91 项通过
- `web/` 下执行 `npm run build`
- `git diff --check`
- v1→v2→v3、迁移回滚、迁移前备份和 future schema 拒绝测试
- 正式数据库只读检查及真实副本产品路径迁移

前端构建仍有既存的大 chunk 警告，不阻塞本次发版。

## 剩余发版门槛

1. 将版本升级到 `0.1.7`，提交并推送源码。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes 草案

### Added

- Review files added, modified, deleted, or renamed by a Code turn, with persistent multi-file diffs.
- Save canvas widgets to a reusable library and open them in other sessions.
- Import standard MCP server configurations as Apps and create validated local Apps directly from Pudding.

### Improvements

- Keep active and recently closed canvas windows isolated per session while preserving saved widget revisions and edits.
- Simplify Skill creation with direct validation and unify MCP server management under Apps.
- Improve Code mode temporary workspaces, local command discovery, forms, workspace controls, and audio device switching.
- Improve daemon startup recovery, identity validation, and privacy-safe rotating logs.

### Data Migration

- Upgrade the SQLite schema from v1 to v3 to store turn file changes and session-scoped canvas state.
- Create a database backup before migration and preserve legacy orphaned canvas items in Saved Widgets.
- Existing sessions, messages, browser tabs, and search indexes are not rewritten.
