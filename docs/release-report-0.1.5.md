# Pudding 0.1.5 发版报告

日期：2026-07-15  
对比基线：`v0.1.4`（`7ab3571c`）  
审查版本：`9afdf2c5` 加 `0.1.5` 版本提交

## 发版结论

**可以无数据库迁移发布。** 本次主要增加项目文件管理和 Git 写操作；SQLite schema、迁移版本、
索引与持久化路径均未改变。版本、完整测试和生产构建已通过，剩余门槛是执行标准双架构签名、
公证、打包和 Draft Release 流程。

## 改动摘要

- 项目工作区支持复制、移动、重命名和删除文件或目录，并改进目录级文件监听。
- Git 面板支持初始化、暂存、取消暂存、放弃修改、提交、分支管理、同步和发布。
- 切换 Git 分支前会检查未保存的编辑内容，避免无提示丢失改动。
- 项目引用支持精确到代码起止行列，并继续写入已有消息 JSON。
- 编辑器右键菜单改由桌面端统一提供，简化多处菜单和工具栏图标。
- 更新 Monaco 与相关前端依赖，补充文件类型图标和项目错误提示。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 项目文件系统 | 高 | 新增跨目录及跨根目录复制、移动；拒绝符号链接、特殊文件、覆盖和目录自包含。 |
| Git 操作 | 高 | 新增 stage、discard、commit、branch、sync、publish 等显式写操作。 |
| 项目编辑器 | 高 | 编辑、预览、标签、文件树、上下文菜单和文件监听均有调整。 |
| 对话上下文 | 中 | 项目引用新增起止行列，保存在已有 `messages.parts` JSON 中。 |
| Electron | 中 | 新增编辑器菜单委托和目录递归监听。 |
| SQLite 结构 | 低 | 表、列、索引、触发器、schema 指纹和版本均未改变。 |
| 自动更新与打包 | 低 | 发版脚本、签名、公证和更新元数据逻辑未改变。 |

## 数据库分析

### Schema 与迁移

- `internal/store/schema.sql` 与 `v0.1.4` 完全一致。
- Schema SHA-256 仍为 `8c5dc7392f4b5bdc77a1edb38c193789a24a1292defa9bf99b1effd96fbaea3d`。
- `internal/store/sqlitestore/migrations.go` 与 `v0.1.4` 完全一致。
- `PRAGMA user_version` 和 `currentSchemaVersion` 仍为 `1`，`schemaMigrations` 仍为空。
- 本次不创建迁移备份、不执行迁移 SQL，也不修改 schema 契约。

### 数据语义

- 项目文件和 Git 写操作直接作用于用户明确选择的项目目录，不经过 SQLite，也不会后台自动执行。
- 项目引用的起止行列作为可选字段写入已有 `messages.parts` JSON；旧消息无需转换。
- 不重写 canonical sessions、messages、turns 或 events。
- 不重建或修改 FTS5 派生索引。

### 版本兼容

- 从 `v0.1.4` 升级不需要数据库迁移，现有会话、消息、画布、浏览器标签和搜索索引原样保留。
- 数据库层面可退回 `v0.1.4`；旧版本会忽略项目引用中的新增行列字段。
- 新版本执行过的项目文件或 Git 操作属于外部工作区变更，降级不会自动撤销这些操作。

### 真实 release 数据库只读检查

- `PRAGMA user_version`：`1`
- `PRAGMA quick_check`：`ok`
- v1 契约要求的 7 个索引均存在。
- 使用 immutable 只读连接检查，没有写入、迁移、备份或修复原数据库。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`
- `npm run test:electron`：78 项通过
- `web/` 下执行 `npm run build`
- `git diff --check v0.1.4..HEAD`
- 对比 `v0.1.4` 的 schema 与迁移文件指纹
- 正式数据库 immutable 只读健康检查

前端构建仍有大 chunk 警告，不阻塞本次发版。

## 剩余发版门槛

1. 执行 `make desktop-publish`。
2. 确认 arm64、x64 的签名、公证、Gatekeeper、DMG、ZIP 和 9 个更新资产全部通过。
3. 检查 Draft Release 后再执行 `make desktop-release-finalize`。

## Release Notes 草案

### 新增

- 在项目工作区中复制、移动和整理文件或目录。
- 直接暂存、提交、切换分支、同步和发布 Git 仓库。
- 将选中的代码范围精确引用到会话。

### 改进

- 切换分支前保护未保存的编辑内容。
- 改进项目文件监听、文件类型图标和编辑器右键菜单。
- 优化项目工作区错误提示和多标签操作。

### 数据安全

本次更新不修改 SQLite schema，不执行数据库迁移，也不重建搜索索引。项目文件和 Git 写入只在用户
明确操作时发生。
