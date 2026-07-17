# Pudding 0.1.9 发版报告

日期：2026-07-17  
对比基线：`v0.1.8`（`4f02e8e1`）  
审查版本：`11cda53f` 的 WebView 输入与焦点修复及 `0.1.9` 版本提交

## 发版结论

**可以按浏览器稳定性修复发布。** 本次修复浏览器自动化输入可能落入错误窗口、并发操作相互干扰，
以及自动化结束后编辑器焦点恢复不稳定的问题。SQLite schema 与迁移代码均未改变，不新增数据库迁移。

## 改动摘要

- 将浏览器点击、输入和滚动放入统一串行队列，避免多个自动化输入操作并发交错。
- 输入前由 Electron 主进程请求渲染层聚焦目标 WebView，并在确认失败时停止发送键盘输入。
- 自动化开始时暂存并释放会话输入框焦点，结束后恢复原焦点和选区。
- 使用 CDP `Page.bringToFront` 与 `Input.insertText` 输入文本，替代逐字符模拟按键。
- 清空输入框时直接设置选区，兼容普通输入框和 `contenteditable` 元素。
- 补充焦点门控、输入完成通知、串行输入和开发态浏览器 smoke 覆盖。
- 简化 daemon 浏览器输入实现，使 Electron WebView 与 headless 浏览器采用一致的 CDP 文本输入路径。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 浏览器自动化 | 高 | 点击、输入和滚动改为串行执行，避免竞争。 |
| WebView 焦点 | 高 | 文本输入前必须确认目标 WebView 已聚焦。 |
| 会话输入框 | 中 | 自动化期间临时释放焦点，结束后恢复焦点与选区。 |
| headless 浏览器 | 中 | 文本输入改用 `Input.insertText`，清空语义保持不变。 |
| Electron IPC | 中 | 新增焦点请求完成与自动化结束事件。 |
| SQLite | 无 | schema、迁移和持久化语义均未变化。 |
| 自动更新与打包 | 无 | 双架构签名、公证和资产格式不变。 |

## 数据库分析

- `currentSchemaVersion` 保持为 `3`。
- `internal/store/schema.sql` 与 `internal/store/sqlitestore/migrations.go` 相对 `v0.1.8` 无差异。
- 本次不新增表、字段、索引，也不重写会话、消息、画布、浏览器标签或搜索数据。
- 从 `v0.1.8` 升级不执行新迁移；从更早版本升级仍按既有流程迁移到 v3。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`
- `npm run test:electron`
- `npm run smoke:electron-browser`：文件、多标签、多会话、历史、焦点隔离、截图和授权撤销通过
- `web/` 下执行 `npm run build`
- `git diff --check`
- schema 版本契约检查

## 剩余发版门槛

1. 将版本升级到 `0.1.9`，提交并推送源码。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes 草案

### Fixes

- Prevent browser automation input from being delivered to the wrong window or composer.
- Serialize browser click, type, and scroll actions to avoid overlapping input operations.
- Restore the previous composer focus and selection after browser automation completes.
- Stop keyboard input safely when the target WebView cannot be focused.

### Improvements

- Use the browser's native CDP text insertion path for more reliable input across standard and rich-text fields.
- Improve WebView focus coordination between Electron and the renderer.

### Data Safety

- This update does not add a database migration or rewrite existing sessions, messages, browser tabs, canvas items, or search data.
