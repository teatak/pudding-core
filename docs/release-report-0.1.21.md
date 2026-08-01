# Pudding 0.1.21 发版报告

日期：2026-08-01

对比基线：`v0.1.20`（`bef395f3`）

审查范围：`v0.1.20..bd9fca91`

## 发版结论

**可以按项目工作流与桌面交互增强版本发布。** 本次加入项目与 GitHub 仓库绑定、GitHub App
授权、会话内搜索、项目排序和空状态，并增强 transcript 附件、照片展示、内容揭示动画、输入法布局
与自动贴底稳定性。SQLite schema 从 v6 升级到 v7，仅新增项目 App 绑定表及索引；迁移前自动备份，
不改写现有消息、会话、项目、turn、画布或搜索索引数据。

## 改动摘要

- 支持将项目绑定到 GitHub App 连接及具体仓库，并为项目会话自动解析首选连接。
- 接入 GitHub App OAuth broker，保存账户信息，支持 token 刷新、重新授权和仓库列表读取。
- 新增会话内搜索，通过 `Cmd/Ctrl+F` 打开，按完整文本查询当前可见对话且不限制匹配数量。
- 支持项目创建、按名称排序、拖拽重排、空目录项目和项目空状态，并以会话活动时间排序项目。
- 在工具结果中持久化附件并统一去重，相机拍照完成后可直接显示照片。
- 为 transcript 增加 turn 展示动画，改进长内容揭示、AI 文本间距和组件对齐。
- 重构工作区 pane 布局，改善终端、项目文件和画布组件的切换与持久展示。
- 修复输入法组合期间的布局跳动，并收紧自动贴底恢复阈值，避免滚动位置误判。
- 缓存提前到达的 OAuth 回调，支持 release 与 dev 协议返回，并在退出信号下优雅关闭 Electron。
- 将 BuzzHive OpenAI 预设名称统一为 BuzzHive。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 项目与 GitHub | 高 | 新增 GitHub App 授权、仓库绑定和项目级连接路由。 |
| 会话搜索 | 中 | 新增当前会话完整文本搜索，不改变全局历史搜索行为。 |
| Transcript | 中 | 新增附件持久化、去重、turn 动画、输入法间距和自动贴底修复。 |
| 工作区与侧边栏 | 中 | 项目排序、拖拽、空状态与 pane 管理有较大 UI 调整。 |
| OAuth 配置 | 中 | 连接 YAML 增加账户、授权变体和 refresh 过期时间字段。 |
| SQLite | 中 | schema 从 v6 升到 v7，新增一张表和两个索引。 |
| 既有运行数据 | 低 | 迁移不改写既有业务表，但旧版本不能直接打开 v7 数据库。 |

## 数据库分析

- `currentSchemaVersion` 从 `6` 升级到 `7`，release fingerprint 为
  `70e57320b3c594f4d1f6db147c78dfc21c3c2b7fda37ddc970c999c8d3a9dd07`。
- v7 migration 在一个事务内创建 `project_app_bindings`、主连接唯一索引和 connection 索引。
- migration 不更新、删除或重排既有消息、会话、项目、turn、文件变更、画布、浏览器历史或 FTS 数据。
- 本次不重建任何既有索引；新增索引只覆盖新建且初始为空的 `project_app_bindings` 表。
- 正式数据库副本在升级前确认为 schema v6，`PRAGMA quick_check` 返回 `ok`。
- 从 v6 首次升级前自动执行 WAL checkpoint，并生成 `pudding.db.backup-v6-<timestamp>` 备份。
- 新增迁移回归测试确认 v6 项目与会话升级后仍存在、绑定表可用且备份文件已生成。
- 新安装直接创建完整 v7 schema，不额外执行历史迁移。
- 直接回退到 `0.1.20` 不受支持：旧版本最高识别 schema v6，会拒绝打开 v7 数据库。需要回退时应先
  备份当前 `~/.pudding`，再恢复自动生成的 v6 数据库备份；恢复会舍弃升级后新增的项目绑定数据。

## 兼容性

- 桌面自动更新、bundle identifier、stable/preview 通道和 `~/.pudding` 数据目录保持不变。
- 既有会话、项目、消息、画布、浏览器地址、App 和连接无需人工迁移。
- 工具结果附件字段属于 canonical message JSON 的向后兼容扩展，不修改已有附件内容。
- GitHub 旧 OAuth 连接会标记为需要重新授权；其他 App 连接流程保持不变。
- GitHub App 流程依赖 `https://x-t.top` OAuth 服务；发布前已验证 GitHub provider 接口在线。
- dev 使用独立的 `pudding-dev://` 返回协议，release 继续使用 `pudding://`。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：113 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过全部 20 项检查。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `workers/oauth/` 下执行 `npm run typecheck`：Cloudflare Worker 类型检查通过。
- schema v7 release fingerprint、v6 到 v7 migration、备份和旧数据保留测试通过。
- 线上 GitHub OAuth provider 接口返回有效安装地址。
- `git diff --check v0.1.20` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.21` 版本号、迁移测试与发版报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Projects and GitHub

- Connect projects to GitHub App accounts and select a repository for project-scoped work.
- Route project sessions through their preferred GitHub connection automatically.
- Add project creation, name sorting, drag-to-reorder, empty projects, and activity-aware project lists.

### Search and Transcript

- Search the complete visible conversation with `Cmd/Ctrl+F` and navigate every matching message.
- Persist and deduplicate attachments embedded in tool results, including automatically displayed camera photos.
- Improve turn reveal transitions, IME layout stability, transcript spacing, and reliable bottom anchoring.

### Workspace and OAuth

- Refine workspace pane persistence, project empty states, terminal presentation, and sidebar interactions.
- Add account-aware GitHub App authorization with token refresh, reauthorization, and repository discovery.
- Preserve early OAuth callbacks and support separate release and development return protocols.

### Reliability and Compatibility

- Shut Electron down gracefully on termination signals and keep the BuzzHive preset name consistent.
- Upgrade SQLite to schema v7 with an automatic pre-migration backup and no rewrite of existing user data.
- Preserve existing sessions, projects, messages, canvas items, browser addresses, Apps, and connections.
