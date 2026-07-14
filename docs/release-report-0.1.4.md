# Pudding 0.1.4 发版报告

日期：2026-07-14  
对比基线：`v0.1.3`（`ee6cfa09`）  
审查版本：`91f25b90` 加打包符号链接修复

## 发版结论

**数据库改动可以无迁移发布。** 整体达到有条件发版状态：需要先将版本升级到 `0.1.4`、提交当前
UI 改动，并让双架构签名包通过现有发版流水线。

## 改动摘要

- 用统一工作区替换原画布外壳，集中管理小组件、浏览器、终端、项目文件和文件预览；底层继续复用
  现有画布数据与 API。
- 会话提交新增项目引用和当前 UI 上下文。
- 会话 API 返回已加载 App，并支持从单个会话卸载 App。
- macOS 新增 Apple Silicon 与 Intel 两套独立安装包。
- 打包时将 runtime 内部绝对符号链接转换为 App 内相对链接，避免签名越出 bundle。
- 简化 DMG 背景，调整会话操作和删除操作样式。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 工作区 UI | 高 | 核心组件和布局状态重构，是主要回归风险区。 |
| 对话上下文 | 中 | 新增 `project_reference`、`ui_context` 两种内容类型。 |
| 会话 App | 中 | 现有 `loaded_app_ids` 开放给客户端并增加卸载写入。 |
| SQLite 结构 | 低 | 表、列、索引、触发器和 schema 版本均未改变。 |
| 现有用户数据 | 低 | 不重写、不删除、不批量重建索引。 |
| 桌面打包 | 高 | 同时生成 arm64、x64 两套独立产物。 |
| 自动更新 | 中 | 同一更新清单包含双架构产物，仍需最终签名包验证。 |

## 数据库分析

### Schema 与迁移

- `internal/store/schema.sql` 与 `v0.1.2`、`v0.1.3` 完全一致。
- Schema SHA-256 仍为 `8c5dc7392f4b5bdc77a1edb38c193789a24a1292defa9bf99b1effd96fbaea3d`。
- `PRAGMA user_version` 仍为 `1`。
- `currentSchemaVersion` 仍为 `1`，`schemaMigrations` 仍为空。
- 启动时只校验 v1 契约，不创建迁移备份，也不执行迁移 SQL。

### 数据语义

- 工作区小组件继续使用 `canvas_items`、`canvas_closed_items`，没有改表或搬迁数据。
- 浏览器状态继续使用 `session_browser_tabs`。
- 卸载 App 只更新已有的 `sessions.loaded_app_ids` JSON 列。
- 项目引用与 UI 上下文存入已有的 `messages.parts` JSON 列，旧消息无需转换。
- FTS5 逻辑未改变；新增的非文本内容不需要重建 `messages_fts` 或 `messages_terms_fts`。

### 版本兼容

- 从 `v0.1.3` 升级：无数据库迁移。
- 从 `v0.1.2` 升级：同为 v1 schema，无数据库迁移。
- 从未标版本的正式 `0.1.1` 升级：现有逻辑会校验并标记为 v1。
- 数据库结构允许退回 `v0.1.3`，但旧客户端可能忽略新内容类型；创建过项目引用消息后不建议降级。

### 真实 release 数据库只读检查

- `PRAGMA user_version`：`1`
- `PRAGMA quick_check`：`ok`
- v1 契约要求的 7 个索引均存在。
- 检查过程没有写入、迁移、备份或修复原数据库。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`
- `npm run test:electron`：76 项通过
- `web/` 下执行 `npm run build`
- `git diff --check`
- 对比 `v0.1.2`、`v0.1.3` 的 schema 指纹

前端构建仍有既存的大 chunk 警告，不阻塞本次发版。

## 剩余发版门槛

1. 将 `package.json`、`package-lock.json` 从 `0.1.3` 升级到 `0.1.4`。
2. 提交或明确放弃当前 4 个未提交 UI 文件。
3. 执行唯一支持的 `make desktop-bundle`，验证签名、公证、staple、Gatekeeper 和 9 个发版产物。
4. 使用旧数据冒烟测试工作区小组件与浏览器标签恢复。
5. 分别测试 arm64、x64 安装包。当前 x64 最低要求 macOS 15.5，arm64 最低要求 macOS 14.0。

## Release Notes 草案

### 新增

- 统一工作区现在可以集中管理小组件、浏览器、终端、项目文件和预览。
- 项目文件和目录可以直接引用到会话。
- 会话 App 管理更加清晰。
- 新增 Intel Mac 独立安装包。

### 改进

- 自动继承旧画布的开合状态和标签顺序。
- 长工具输出的展示更稳定。
- 简化 macOS 安装界面。

### 数据安全

本次更新不修改 SQLite schema，也不执行数据库迁移。现有会话、消息、小组件、浏览器标签和搜索索引
都会原样保留。
