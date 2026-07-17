# Pudding 0.1.10 发版报告

日期：2026-07-18  
对比基线：`v0.1.9`（`b38ccfb3`）  
审查版本：`3e1c8310` 的浏览器历史、新标签页与弹窗安全改造及 `0.1.10` 版本提交

## 发版结论

**可以按浏览器体验功能版发布。** 本次新增持久化浏览器历史、历史搜索与管理、新标签页最近访问入口，
并完善 `window.open`、OAuth 弹窗、导航失败和 WebView 安全边界。SQLite schema 从 v3 升级到 v4，
仅新增 `browser_history` 表及索引，不重写既有会话、消息、画布或浏览器标签数据。

## 改动摘要

- 新增全局浏览器历史，按完整 URL 去重并更新最近访问时间，最多保留 1000 条。
- 地址栏聚焦时支持搜索历史，可删除单条记录或清空全部历史。
- 新标签页展示最近访问站点，按站点去重并提供快捷打开入口。
- 浏览器导航成功后记录历史，同一页面的标题和 favicon 更新不会重复计为访问。
- 普通新标签链接转为受管理的浏览器标签，并保留前台、后台与 referrer 语义。
- 需要原生 `WindowProxy` 的 OAuth 或站点弹窗使用隔离的 Electron 子窗口，并限制总数量。
- 强化 WebView 附加前的安全配置，禁止非受管 partition、预加载脚本和危险 Node 权限。
- 导航失败时保留结构化错误信息并显示可恢复的错误状态。
- 修正主窗口识别，避免弹窗影响应用激活、菜单、更新和窗口状态保存。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 浏览器历史 | 高 | 新增记录、搜索、单条删除、清空和最近站点展示。 |
| SQLite | 高 | schema v3 升级到 v4，新增 `browser_history` 表和时间索引。 |
| BrowserToolbar | 中 | 地址栏下拉加入历史结果和管理操作。 |
| 新标签页 | 中 | 空白页展示最多 8 个最近访问站点。 |
| Electron 弹窗 | 高 | 区分受管标签与原生子窗口，并隔离 OAuth 弹窗。 |
| WebView 安全 | 高 | 附加前强制 sandbox、context isolation 和受管 partition。 |
| 浏览器自动化 | 中 | 新标签激活语义、导航错误与 referrer 传递更完整。 |
| 会话、消息与画布 | 无 | 不修改既有事实源、数据结构或持久化语义。 |

## 数据库分析

- `currentSchemaVersion` 从 `3` 升级到 `4`。
- v4 迁移仅创建 `browser_history` 表和 `browser_history_visited_at` 索引。
- 新表使用 URL 唯一约束；重复访问更新标题、favicon、访问时间和更新时间。
- 迁移使用 `CREATE TABLE/INDEX IF NOT EXISTS`，并由 schema fingerprint 与 v3 升级测试保护。
- 现有数据库首次启动 `0.1.10` 时原位升级，不删除或重写任何旧表数据。
- 全新数据库直接创建 v4 schema；未标版本的 v3 数据库可识别后继续升级。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：浏览器历史 store、API、迁移和持久化测试通过。
- `npm run test:electron`：97 项 Electron 测试通过，覆盖受管标签、原生弹窗、WebView 安全和导航失败。
- `npm run smoke:electron-browser`：16 项真实 WebView 检查通过，包括历史、焦点隔离、referrer、父子窗口、
  命名窗口、blob、`noopener` / `noreferrer`、截图和授权撤销。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- `git diff --check` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.10` 版本和本报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes

### Browser History

- Keep a local browser history with search, per-entry deletion, and clear-all controls.
- Show recent sites on new tabs for faster navigation.
- Refresh page titles and favicons without creating duplicate visits.

### Browser Windows

- Open tab-like links as managed Pudding browser tabs with foreground and background activation support.
- Support isolated native child windows for OAuth and sites that require standard `WindowProxy` behavior.
- Preserve navigation referrers and expose recoverable page-load errors.

### Security

- Harden managed WebViews before attachment with sandboxing, context isolation, and a restricted persistent partition.
- Block unmanaged WebView attachments, unsafe preload scripts, and unsupported external navigation schemes.

### Data Migration

- Upgrade the local database from schema v3 to v4 by adding the browser history table and recency index.
- Preserve all existing sessions, messages, canvas items, browser tabs, and search data during migration.
