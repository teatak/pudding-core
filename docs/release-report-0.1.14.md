# Pudding 0.1.14 发版报告

日期：2026-07-20

对比基线：`v0.1.13`（`b62e71bf`）

审查版本：`90e06c88`、`09e678e6` 的项目搜索、编辑器设置、更新可靠性与界面优化，以及 `0.1.14` 版本提交

## 发版结论

**可以按项目工作区与桌面体验增强版发布。** 本次新增项目内容搜索和编辑器字体设置，
改进待安装更新的版本确认逻辑，并优化吉祥物、菜单与即时交互。SQLite schema 保持 v4，
不新增迁移，不修改会话、消息、画布、浏览器或项目文件数据的持久化结构。

## 改动摘要

- 在项目侧边栏搜索文件内容，支持结果高亮、行号展示和点击跳转到对应编辑器行。
- 项目树和文件监听支持常用隐藏配置文件，同时继续忽略 `.git`、`node_modules` 等生成目录。
- 新增编辑器字体、字号和行高设置，并统一应用于项目文件、diff 与独立文件预览。
- 安装已下载更新前重新检查最新版本；如果存在更新版本则先下载，失败时保留原有可安装版本。
- 防止重复触发安装，并补充待安装更新刷新场景的 Electron 测试。
- 吉祥物新增响应鼠标位置的视线与更平滑的分层动画，同时保留错误和思考状态。
- 移除桌面端大范围颜色过渡，使 hover 和焦点反馈即时生效，并统一浮层描边与阴影。
- 修复输入框失焦后再次输入 `@` 无法正常打开提及菜单的问题。
- Electron WebView 测试运行时升级到 `43.1.1`。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 项目工作区 | 高 | 新增 session-scoped 项目内容搜索、结果定位和隐藏配置文件展示。 |
| 编辑器与 diff | 中 | 字体、字号和行高由全局设置统一控制。 |
| 桌面更新 | 中 | 已下载版本在安装前重新确认最新 release，并保持失败回退。 |
| 吉祥物与交互 | 中 | 视线、分层动画、菜单阴影和 hover 响应调整。 |
| Composer | 低 | 修复 `@` 提及菜单在焦点切换后的重新触发。 |
| SQLite | 无 | schema 和 release fingerprint 均保持 v4。 |
| 运行数据 | 无 | 不修改会话、消息、turn、画布、浏览器或项目数据结构。 |

## 数据库分析

- `currentSchemaVersion` 与 `PRAGMA user_version` 均保持 `4`。
- `schema.sql`、`schemaMigrations` 与 v4 release fingerprint 未修改。
- 编辑器排版设置继续存储在 `<home>/config/settings.yaml`，不进入 SQLite。
- 项目搜索为即时文件系统读取，不创建搜索索引或持久化搜索结果。
- 不需要数据库备份、重建、回填或兼容迁移。
- 既有 release 数据目录可直接由 `0.1.14` 继续使用。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：105 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：17 项真实 WebView 浏览器检查通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- `git diff --check` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.14` 版本和本报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes

### Project Workspace

- Search project file contents from the sidebar and jump directly to matching editor lines.
- Include common hidden project configuration files while continuing to ignore generated and vendor directories.

### Editor

- Configure the editor font family, font size, and line height across files, diffs, and standalone previews.
- Restore mention suggestions after the composer loses and regains focus.

### Update Reliability

- Recheck a downloaded update before installation and fetch a newer release when one is available.
- Keep the previous downloaded update usable when the refresh fails and prevent duplicate install attempts.

### Interface

- Add pointer-responsive gaze and refined layered motion to the Pudding mascot.
- Make desktop hover and focus feedback immediate while unifying menu outlines and shadows.

### Compatibility

- Preserve the local database at schema v4 with no migration required.
