# Pudding 0.1.17 发版报告

日期：2026-07-23

对比基线：`v0.1.16`（`d42f8749`）

审查版本：`17180a5e`、`96d8b17d`、`a00ad3f7`，以及后续 `0.1.17` 发版准备提交

## 发版结论

**可以按 Markdown 编辑与转录滚动体验增强版发布。** 本次将 Markdown 文件预览升级为所见即所得的实时编辑器，
保留保存、定位和引用选区能力，并修复窗口或 Composer 尺寸变化时转录视口意外脱离底部的问题。
SQLite schema 保持 v4，不新增迁移，不修改 canonical messages、turn、画布、浏览器状态或项目数据的持久化结构。

## 改动摘要

- Markdown 文件改用 Vditor 实时编辑，可直接编辑格式化后的正文、表格和代码内容。
- 保留 `Command/Ctrl + S` 保存、外部文件变化同步、指定位置揭示和选区引用能力。
- 编辑器支持明暗主题和中英文界面，并按项目文件查看场景懒加载。
- 修复窗口、Composer 或转录内容尺寸变化时浏览器自动调整 `scrollTop` 导致的意外脱离。
- 保持用户主动浏览历史消息时的当前位置，同时继续跟随处于底部的实时输出。
- 更新任务进度图标、会话 App 控件和项目文件界面细节。
- 统一工作区标签页颜色变量，改善明暗主题下的视觉一致性。
- 移除实现切换后残留的 CodeMirror 与 Lezer 直接依赖，避免携带无用编辑器代码。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Markdown 编辑 | 高 | 项目 Markdown 文件由只读预览升级为 Vditor 实时编辑。 |
| 项目文件 | 中 | 保存、外部同步、位置揭示和选区引用继续沿用现有项目文件协议。 |
| 转录滚动 | 中 | 视口尺寸变化时重新计算并恢复底部或历史锚点。 |
| 界面 | 低 | 更新任务进度、App 控件、工作区标签和主题颜色。 |
| 安装包 | 低 | 增加按需加载的 Vditor 编辑资源，同时清理未使用依赖。 |
| SQLite | 无 | schema 和 release fingerprint 均保持 v4。 |
| 运行数据 | 无 | 不改写 canonical 数据，不重建派生索引，不创建迁移备份。 |

## 数据库分析

- `currentSchemaVersion` 与正式数据库 `PRAGMA user_version` 均保持 `4`。
- `internal/store`、`internal/searchtext`、schema SQL、迁移和 release fingerprint 均未修改。
- 正式数据库以只读方式执行 `PRAGMA quick_check`，结果为 `ok`。
- 本次不重写 canonical messages、turns、sessions、画布、浏览器状态或历史搜索数据。
- 本次不重建 FTS 或其他派生索引，不创建迁移备份，也不改变降级行为。
- 既有 `~/.pudding` 数据目录可由 `0.1.16` 直接继续使用。

## 兼容性

- 桌面自动更新、bundle identifier、数据目录和 release/preview 通道保持不变。
- 已保存会话、项目、App、连接、Skill 和画布无需迁移。
- Markdown 编辑仍通过现有项目文件读写接口保存，不引入新的项目文件格式。
- 非 Markdown 文件继续使用现有代码编辑器和预览路径。
- Vditor 资源随应用打包并按需加载，不依赖外部 CDN。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：107 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- 正式数据库只读检查：`user_version=4`，`quick_check=ok`。
- `git diff --check` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.17` 版本号、发版报告和无用依赖清理。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Markdown Editing

- Edit Markdown files directly in a live rich-text surface powered by Vditor.
- Preserve keyboard saving, external file synchronization, location reveals, and selection references.
- Match the active language and color theme while loading editor resources only when needed.

### Transcript Scrolling

- Keep live transcripts attached to the latest output when the viewport or Composer changes size.
- Preserve the reader's position when they intentionally browse earlier messages.

### Interface Refinements

- Refresh task progress icons, session App controls, project file details, and workspace tab colors.
- Remove unused CodeMirror and Lezer dependencies left by the editor transition.

### Compatibility

- Preserve the local database at schema v4 with no migration, data rewrite, index rebuild, or backup required.
