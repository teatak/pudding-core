# Pudding 0.1.12 发版报告

日期：2026-07-19

对比基线：`v0.1.11`（`b41cfb36`）

审查版本：`f19c72fc` 的响应式工作区、设置页和会话 App 入口改造及 `0.1.12` 版本提交

## 发版结论

**可以按桌面界面与交互优化版本发布。** 本次为窄窗口增加工作区抽屉模式，重新组织设置页的信息密度，
并改进会话 App 状态和图标表现。SQLite schema 保持 v4，不新增迁移，不修改会话、消息、画布、
浏览器标签或浏览器历史的持久化结构。

## 改动摘要

- 窗口最小宽度降至 560px，窄窗口中的工作区改为覆盖式抽屉，主会话内容不再被过度压缩。
- 设置窗口缩小并统一卡片、分组、控件尺寸和间距，提高信息密度。
- 常规与音频设置支持乐观更新，保存中的开关显示独立状态并避免旧查询覆盖新值。
- 会话已加载 App 改为头像堆叠展示，可直接卸载，并对超过 5 个的项目显示剩余数量。
- App 图标增加亮色背景检测，优化深浅主题下的边框和底色。
- App 列表调整尺寸、间距和自适应列宽，改善较窄窗口中的展示。
- 浏览器与 App 的通用入口统一使用 Globe 图标。
- 主色和侧边栏选中颜色改为中性色，以匹配整体桌面界面。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 桌面窗口 | 中 | 最小宽度降低，窄窗口启用工作区抽屉。 |
| 工作区布局 | 中 | 抽屉模式覆盖主内容，并提供背景区域关闭交互。 |
| 设置页面 | 中 | 布局、控件密度和保存反馈重新组织。 |
| 音频配置 | 中 | 保存交互改为串行乐观更新，配置结构不变。 |
| App 展示 | 中 | 会话 App 入口、图标处理和列表布局调整。 |
| 主题 | 低 | 主色与侧边栏强调色改为中性色。 |
| SQLite | 无 | schema 和 release fingerprint 均保持 v4。 |
| 会话、消息与画布数据 | 无 | 不修改既有事实源、数据结构或迁移语义。 |

## 数据库分析

- `currentSchemaVersion` 与 `PRAGMA user_version` 均保持 `4`。
- `schema.sql`、`schemaMigrations` 与 v4 release fingerprint 未修改。
- 本次设置改造继续使用既有配置 API 与 YAML 文件，不新增 SQLite 字段。
- 不需要数据库备份、重建、回填或兼容迁移。
- 既有会话、消息、画布、浏览器标签和浏览器历史可直接继续使用。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器检查通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- `git diff --check` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.12` 版本和本报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes

### Responsive Workspace

- Use an overlay workspace drawer on narrow windows so the main conversation remains readable.
- Reduce the minimum desktop window width while preserving the full split layout on larger screens.

### Settings

- Refine the settings dialog with denser groups, consistent controls, and clearer visual hierarchy.
- Apply settings optimistically with per-control saving feedback and serialized audio configuration updates.

### Apps

- Show loaded session Apps as a compact avatar stack with direct unload controls.
- Improve App icon contrast across light and dark themes and refine responsive catalog layouts.

### Interface

- Adopt neutral primary colors and use a consistent Globe icon for browser-related actions.
- Preserve the local database at schema v4 with no migration required.
