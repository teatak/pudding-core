# Pudding 0.2.11 发版报告

日期：2026-09-04

对比基线：`v0.2.10`（`062ca8e1`）

候选提交：`2bb82239`

## 发版结论

**可以发布 0.2.11 正式版。** 本版统一工作区标签状态，完善浏览器导航、历史记录和停止加载行为，并调整工作区空状态、活动展示与审批界面。SQLite schema、自动更新协议、系统权限归属、签名身份和稳定通道规则均未变化。

## 改动摘要

- 将工作区标签顺序、选中状态和资源显示统一到 session-scoped workspace store，删除旧的项目标签与标签顺序双轨 store。
- 统一项目、浏览器、画布和文件预览的标签 key 与 reveal 路径，减少打开、切换和关闭标签时的重复同步。
- 浏览器历史只记录成功完成后的最终 URL，不再将跳转中间页或单纯元数据更新记为新访问。
- 浏览器历史搜索按用户可见 URL 和标题匹配，不让 `http/https` scheme 干扰短关键词结果。
- 增加停止页面加载能力，完善 reload 完成时机、临时 favicon、关闭后迟到 webview 注册和导航状态同步。
- 优化空工作区、画布条目数量、活动状态、审批栏和应用图标展示。
- 将传递依赖 `qs` 更新到 6.16.0，清除发布前发现的中危依赖漏洞。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 工作区标签 | 中 | 状态事实源收敛到统一 workspace store，旧的两个专用 store 已删除。 |
| 内置浏览器 | 中 | 导航、历史、favicon、reload、停止加载和 webview 生命周期均有调整。 |
| 浏览历史数据 | 低 | 保留已有记录，仅改变新增记录时机和查询匹配逻辑。 |
| 桌面界面 | 低 | 调整空状态、活动卡片、审批栏、画布计数和图标形态。 |
| 构建依赖 | 低 | `qs` 安全补丁只影响构建工具依赖链。 |
| SQLite | 无 | schema 仍为 v13，没有迁移、备份或数据重写。 |
| 自动更新 | 无 | 更新检查、下载、安装、架构选择和通道逻辑未变化。 |
| 权限与签名 | 无 | Bundle identity、TCC 权限、Developer ID 和公证配置未变化。 |

## 数据与兼容性

- SQLite schema 保持 v13，本版不创建数据库迁移备份，也不执行迁移或备份清理。
- `browser_history` 表结构和已有数据保持不变；成功完成的最终导航才新增访问记录，后续标题和 favicon 更新只修改元数据。
- canonical sessions、messages、turns、projects、usage 和 provider 配置均不重写。
- 稳定版继续使用 `~/.pudding`，数据目录、Bundle ID 和更新通道规则不变。

## 已完成验证

- Go 全量测试通过，包含浏览器 API、历史记录、SQLite store 和 session 状态。
- Electron 全量测试：197 项通过，包含导航完成、重定向历史、停止加载和迟到 webview 注册。
- `web/` TypeScript 检查和生产构建通过。
- `web/` npm audit 通过：0 个已知漏洞。
- `make schema-check` 通过，schema v13 release contract 有效。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线重新运行全量 Go、Electron、schema 和 Web build。
- 完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到唯一 Draft Release 后再显式发布。

## Release Notes

### Workspace Tabs

- Consolidate workspace tab order, selection, and resource visibility into one session-scoped state source.
- Remove the superseded project-tab and tab-order stores to keep opening, switching, closing, and revealing resources consistent.
- Align project, browser, canvas, and file-preview tab identities across the workspace.

### Browser Navigation and History

- Record browser history only after a successful final navigation, avoiding redirect intermediates and duplicate metadata visits.
- Match history searches against visible URLs and titles without protocol noise.
- Add stop-loading support and refine reload completion, provisional favicons, and navigation state updates.
- Prevent late webview registration from recreating a tab that has already been closed.

### Interface Refinements

- Refine the empty workspace, canvas item counts, activity presentation, approval bar, and application icon shapes.

### Security and Compatibility

- Update the transitive `qs` dependency to 6.16.0 and clear the release dependency audit.
- Keep SQLite schema v13 unchanged with no migration, backup, cleanup, or canonical-data rewrite.

### Updates and Distribution

- Preserve the existing automatic update protocol, app identity, permissions, signing, notarization, and stable-channel behavior.
