# Pudding 0.1.6 发版报告

日期：2026-07-15  
对比基线：`v0.1.5`（`2894f8f4`）  
审查版本：`94afb92b` 加 `0.1.6` 版本提交

## 发版结论

**可以无数据库迁移发布。** 本次主要改进 macOS 音频路由、应用位置恢复、Code 模式项目访问、
工作区布局和发版可追溯性，并新增隔离的 Agent Eval 框架。SQLite schema、迁移版本、索引和
持久化路径均未改变。完整 Go、Electron、前端构建及真实 release 数据库只读检查均已通过。

## 改动摘要

- macOS 开始录音时参与系统音频路由仲裁，并在默认设备变化后刷新 PortAudio 设备。
- 重新打开应用时恢复上次的会话、草稿、项目或应用页面位置。
- Code 模式已有授权项目时不再重复请求能力，项目路径解析进一步约束符号链接与缺失路径。
- 统一画布、项目和浏览器工作区的标签及操作区布局，收紧侧边栏菜单和会话分组间距。
- 新增隔离 fixture 的 Agent Eval 命令与 10 个回归案例，不接触用户项目或正式数据库。
- 发版时向公开仓库提交版本清单，并在 Release 中生成带 `v` 的标题和功能清单。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| macOS 音频 | 高 | 录音启动和停止增加系统路由仲裁；仲裁失败或超时会记录警告并继续原有录音路径。 |
| 应用路由 | 中 | localStorage 只保存页面位置，不保存消息；无有效当前路由时才恢复。 |
| Code 模式与项目路径 | 中 | 减少重复审批，并修正符号链接根目录和待创建路径的解析。 |
| 工作区 UI | 中 | 标签、工具栏和 surface controls 样式统一，不改变后端协议。 |
| Agent Eval | 低 | 仅开发命令；使用临时 home 和 fixture，不进入桌面产品主路径。 |
| 发版流程 | 中 | public tag 指向版本清单 commit，Release 带功能列表和资产哈希。 |
| SQLite 结构 | 低 | 表、列、索引、触发器、schema 指纹和版本均未改变。 |

## 数据库分析

### Schema 与迁移

- `internal/store/schema.sql` 与 `v0.1.5` 完全一致。
- Schema SHA-256 仍为 `8c5dc7392f4b5bdc77a1edb38c193789a24a1292defa9bf99b1effd96fbaea3d`。
- `internal/store/sqlitestore/migrations.go` 与 `v0.1.5` 完全一致。
- `PRAGMA user_version` 和 `currentSchemaVersion` 仍为 `1`，`schemaMigrations` 仍为空。
- 本次不创建迁移备份、不执行迁移 SQL，也不修改 schema 契约。

### 数据语义

- 上次页面位置仅写入前端 localStorage，不进入 SQLite，不保存 canonical messages。
- Agent Eval 使用系统临时目录中的独立 home、数据库和 Git fixture，结束后默认删除。
- 音频路由、项目路径和 UI 改动不重写 sessions、messages、turns、events 或 canvas 数据。
- 不重建或修改 FTS5 派生索引。

### 版本兼容

- 从 `v0.1.5` 升级不需要数据库迁移，现有会话、消息、画布、浏览器标签和搜索索引原样保留。
- 数据库层面可退回 `v0.1.5`；旧版本会忽略新写入的页面位置偏好。
- 新版不会在后台改写用户项目；项目工具仍受显式项目授权和路径边界约束。

### 真实 release 数据库只读检查

- `PRAGMA user_version`：`1`
- `PRAGMA quick_check`：`ok`
- v1 契约要求的 7 个索引均存在。
- 使用 immutable 只读连接检查，没有写入、迁移、备份或修复原数据库。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`
- `npm run test:electron`：83 项通过
- `web/` 下执行 `npm run build`
- `make agent-eval RUNARGS="--mock --case pagination-boundary"`：runner、daemon 和隔离环境链路完成
- `git diff --check v0.1.5..HEAD`
- 对比 `v0.1.5` 的 schema 与迁移文件
- 正式数据库 immutable 只读健康检查

前端构建仍有大 chunk 警告，不阻塞本次发版。Agent Eval 使用 mock provider，因此案例验证失败是
预期结果；该检查只验证 runner 链路，不产生外部模型费用。

## 剩余发版门槛

1. 执行 `make desktop-publish`。
2. 确认 arm64、x64 的签名、公证、Gatekeeper、DMG、ZIP 和 9 个更新资产全部通过。
3. 检查公开版本清单、Draft Release 功能清单和资产后再发布。

## Release Notes 草案

### Added

- Restore the last active session, project, or app page when reopening Pudding.

### Improvements

- Improve audio device routing and switching reliability while recording on macOS.
- Reduce repeated project access confirmations in Code mode.
- Unify tabs and controls across canvas, project, and browser workspaces, with a denser sidebar layout.
- Strengthen project path boundary checks and release asset traceability.

### Data Safety

This update does not change the SQLite schema, run database migrations, or rebuild search indexes. Existing
sessions, messages, canvases, and project content are not rewritten in the background.
