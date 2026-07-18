# Pudding 0.1.11 发版报告

日期：2026-07-19

对比基线：`v0.1.10`（`44c9d4b1`）

审查版本：`cbbea40a` 的 App 连接、浏览器 favicon 与工作区项目标签改造及 `0.1.11` 版本提交

## 发版结论

**可以按 App 集成与工作区体验功能版发布。** 本次扩展 App 的动态连接字段、可配置 endpoint URL 和
token exchange 鉴权，完善浏览器 favicon 与后台自动化，并将项目作为工作区正式标签呈现。SQLite schema
保持 v4，不新增迁移，不修改会话、消息、画布、浏览器标签或浏览器历史的持久化结构。

## 改动摘要

- App 连接支持将自定义字段按规则注入请求 header、query 或 JSON body。
- REST / GraphQL endpoint 可按连接配置独立基础地址，并校验协议、userinfo、query 与 fragment。
- 新增 token exchange 鉴权，可从连接字段换取并短期缓存访问令牌。
- App 列表支持稳定版与预览版的版本选择、升级判断及包哈希对账。
- 浏览器 favicon 通过 Electron 同源解析、尺寸限制与本地 data URL 缓存，减少失效图标。
- 浏览器点击、输入和滚动改为目标页面内执行，不需要抢占当前 WebView 焦点。
- 项目页面成为可选择、排序和关闭的工作区标签，并持久化本地关闭偏好。
- 调整空工作区、画布库和工作区控制区域的布局与显隐逻辑。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| App 连接 | 高 | 新增动态字段注入、endpoint URL 覆盖和 token exchange。 |
| App 配置文件 | 中 | YAML 连接可选新增 `endpoint_urls`，旧配置无需修改。 |
| App 商店 | 中 | 增加版本比较、预览版选择和升级状态判断。 |
| 浏览器 favicon | 中 | Electron 解析同源图标并限制下载与输出大小。 |
| 浏览器自动化 | 高 | 点击、输入、滚动不再依赖前台焦点，保持 session/tab 定向。 |
| 工作区 | 中 | 项目成为正式资源标签，空状态与画布入口重新组织。 |
| SQLite | 无 | schema 和 release fingerprint 均保持 v4。 |
| 会话、消息与画布数据 | 无 | 不修改既有事实源、数据结构或迁移语义。 |

## 数据库分析

- `currentSchemaVersion` 与 `PRAGMA user_version` 均保持 `4`。
- `schema.sql`、`schemaMigrations` 与 v4 release fingerprint 未修改。
- 本次 App 连接扩展继续由 `<home>/config/apps.yaml` 承载，不写入 SQLite。
- 新字段均为可选字段；既有 App 连接和 endpoint 定义可直接继续使用。
- 不需要数据库备份、重建、回填或兼容迁移。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过，覆盖 App 连接、endpoint URL、
  token exchange、API、浏览器与存储。
- `npm run test:electron`：101 项 Electron 测试通过，覆盖 favicon、浏览器自动化、发布和更新链路。
- `npm run smoke:electron-browser`：17 项真实 WebView 检查通过，包括 favicon、多标签、多会话、历史、
  焦点隔离、弹窗、截图和授权撤销。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- `git diff --check` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.11` 版本和本报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes

### App Connections

- Configure connection-specific REST and GraphQL endpoint URLs for self-hosted services.
- Inject custom connection fields into request headers, query parameters, or JSON bodies.
- Exchange App credentials for cached access tokens when an integration requires token-based authentication.
- Select stable or preview App releases with version-aware upgrade checks.

### Browser Experience

- Resolve and cache same-origin favicons through the Electron bridge with strict size limits.
- Run targeted click, typing, and scrolling actions without stealing focus from the active workspace.

### Workspace

- Open projects as sortable workspace tabs that can be selected or closed like other resources.
- Refine empty workspace, canvas library, and surface control layouts.

### Data Compatibility

- Keep the local database at schema v4 with no migration required.
- Preserve all existing sessions, messages, canvas items, browser tabs, and browser history.
