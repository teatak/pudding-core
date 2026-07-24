# Pudding 0.1.18 发版报告

日期：2026-07-25

对比基线：`v0.1.17`（`bb10260b`）

审查版本：`ea71abe8`、`ff6ef814`、`85531aa5`、`78b3a6c4`、`bc2fab56`、`7ceda8bb`、`90dac356`，
以及后续 `0.1.18` 发版准备提交

## 发版结论

**可以按 Agent Console 与交互式工作流增强版发布。** 本次新增可浮动或停靠的 Agent Console、运行中消息引导、
项目结构化文档预览和多目录管理，并增强工作区资源、文件变更、浏览器选区与 Composer 交互。
SQLite schema 从 v4 升级到 v5，仅新增模型用量校准表；正式数据库副本迁移演练通过，既有会话、消息和 turn 数量保持不变。

## 改动摘要

- 新增 Agent Console，可在浮动、左侧停靠和右侧停靠模式间切换，并支持拖拽、缩放和边缘吸附。
- 将浏览器、画布和工作区资源统一接入 Console 与活动卡片，改善多任务操作和状态恢复。
- 支持将排队消息引导到正在运行的 turn，并在安全采样边界继续执行。
- 新增项目根目录编辑和多目录管理，完善项目操作菜单与工作区切换。
- 新增 CSV、TSV、JSON 和 YAML 项目文档预览，并对行列、节点和深度设置渲染上限。
- 改进 turn 文件变更展示，支持资源分组和单文件预览。
- 新增附件导出到已授权项目目录的能力，并强化代码沙箱缓存目录和命令路径策略。
- 将浏览器选中文字纳入上下文，改进代码复制、上下文用量提示和工作区交互。
- 新增按 provider/model 保存的 bounded EWMA 用量校准，提高上下文 token 估算稳定性。
- 增强 Composer 吉祥物视线跟随和 mention 菜单交互。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Agent Console | 高 | 新增浮动、停靠、拖拽、缩放及工作区资源整合。 |
| Turn 执行 | 高 | 排队消息可在运行中的安全边界引导当前 turn。 |
| 项目文件 | 高 | 新增多根目录管理和结构化文档预览。 |
| 工作区 | 中 | 浏览器、画布、选区、附件和文件变更展示得到整合。 |
| 上下文估算 | 中 | 新增按 provider/model 持久化的 bounded EWMA 校准。 |
| 界面 | 中 | Composer、mention、复制按钮和用量提示等交互调整。 |
| SQLite | 中 | schema v4 升级到 v5，新增 `usage_calibrations` 表。 |
| 既有运行数据 | 低 | 不改写 canonical 数据，不重建派生索引。 |

## 数据库分析

- `currentSchemaVersion` 从 `4` 升级到 `5`，release fingerprint 为
  `76313b2ba7212b51e772206fa1877c4471a084f787b244096108f242e856ca3f`。
- v5 迁移仅新增 `usage_calibrations` 表，以 `(provider, model)` 为主键保存样本数、输入 token 比率 EWMA
  和最近一次估算/实际值。
- 迁移前会 checkpoint WAL，并创建权限为 `0600` 的 `pudding.db.backup-v4-<UTC timestamp>` 备份。
- 使用正式 `~/.pudding` 数据库的在线副本完成 v4→v5 演练：`quick_check=ok`，
  sessions `7`、messages `2137`、turns `147`，迁移前后数量完全一致。
- 演练生成的 v4 备份可正常打开，`quick_check=ok`，且保持相同的 sessions、messages 和 turns 数量。
- 本次不重写 canonical messages、turns、sessions、画布或浏览器状态，也不重建 FTS 或其他派生索引。
- v5 是前向迁移；`0.1.17` 无法直接打开已升级为 v5 的数据库。需要降级时，应先退出 Pudding，
  再使用迁移自动生成的 v4 备份恢复数据库。

## 兼容性

- 桌面自动更新、bundle identifier、release/preview 通道和 `~/.pudding` 数据目录保持不变。
- 已保存会话、项目、App、连接、Skill、画布和浏览器地址可继续使用。
- 项目结构化文档预览为只读展示，不改变源文件格式。
- 新增 steering API 与事件由同一版本的 Electron 和 daemon 一起交付，不引入跨版本组件组合。
- 浮动 Console 和工作区布局状态属于本地 UI 偏好，不写入 canonical messages。
- 数据库迁移保持已有数据不变，但升级后降级必须恢复 v4 备份。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：113 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v5 release fingerprint 通过。
- 正式数据库副本 v4→v5 迁移、备份恢复和数据计数检查通过。
- `git diff --check v0.1.17..HEAD` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.18` 版本号与发版报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Agent Console

- Use the Agent Console as a floating surface or dock it to either side of the workspace.
- Drag, resize, and snap the console while keeping browser and canvas resources close at hand.

### Live Steering

- Steer an active turn with queued messages at safe model sampling boundaries.
- Preserve explicit session routing and canonical turn completion while incorporating new guidance.

### Project Files

- Manage multiple project roots and edit project directories from the project actions menu.
- Preview CSV, TSV, JSON, and YAML files with bounded table and structured-data renderers.
- Review grouped file changes, open focused single-file previews, and export attachments into authorized project paths.

### Workspace and Context

- Reuse selected browser text as context and surface browser, canvas, and workspace activity more clearly.
- Improve code copying, context usage feedback, Composer mascot tracking, and mention interactions.
- Calibrate context token estimates per provider and model with bounded usage history.

### Reliability and Compatibility

- Upgrade the local database from schema v4 to v5 with an automatic pre-migration backup.
- Preserve existing sessions, messages, turns, projects, and workspace data without canonical rewrites or index rebuilds.
