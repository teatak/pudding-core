# Pudding 0.1.24 发版报告

日期：2026-08-11

对比基线：`v0.1.23`（`54bf3005`）

审查范围：`v0.1.23..HEAD`

## 发版结论

**可以作为浏览器运行时、桌面更新体验和工作区性能优化版本发布。** 本次让受管浏览器在会话与
工作区界面切换时保持运行，补齐页面元数据同步和自动化焦点协调；桌面更新改为先提示可用版本、
由用户启动下载，并在没有活动 turn 时自动重启安装。SQLite schema 保持 v9，不执行数据库迁移，
也不会生成或清理迁移备份。

## 改动摘要

- 新增浏览器运行时提供层，在界面切换和会话切换时保留 WebView 与页面状态。
- 协调浏览器可见、待机和自动化焦点租约，并同步首屏标题与 favicon 元数据。
- 重构浏览器选项和凭据交互，优化标签、页面状态及工作区呈现。
- 后台发现更新后显示版本入口，由用户开始下载并展示进度；下载完成后在安全时机自动安装。
- 修复更新在活动 turn 中下载完成后，turn 结束时未继续安装的问题。
- 使用定向 ResizeObserver、像素化 dock 尺寸和 memoization 减少布局与工作区重绘。
- 缩小 Query 失效与 UI 状态写入范围，提取资源菜单并优化标签、画布、transcript 和执行面板布局。
- 简化模型推理强度选择逻辑，以真实 `auto` 值作为显示与选中状态，并调整菜单尺寸和对齐。
- 清理不再使用的 DOMPurify、Mermaid 和 semver 依赖。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 浏览器运行时 | 高 | WebView 生命周期改由统一 provider 管理，并增加页面元数据与焦点协调。 |
| 桌面更新 | 高 | 更新检查、下载、进度和安装时机经过重构。 |
| 工作区性能 | 中 | 布局观察、尺寸同步、Query 失效和组件渲染路径收敛。 |
| 浏览器交互 | 中 | 选项菜单、凭据提示、标签与资源呈现调整。 |
| Transcript 与画布 | 低 | 执行面板、活动摘要、画布操作和局部样式优化。 |
| 模型选择 | 低 | 推理强度菜单统一使用实际选择值，并微调紧凑布局。 |
| SQLite | 低 | schema 仍为 v9，没有迁移或运行数据改写。 |

## 数据库分析

- `currentSchemaVersion` 保持 `9`，`internal/store` 相对 `v0.1.23` 没有代码变化。
- 本次不改写 canonical messages、sessions、projects、turns、canvas、browser history、FTS 或 App 配置。
- 不重建派生索引，不生成迁移备份，也不触发历史迁移备份清理。
- 降级边界不变；正式版与预览版继续共享 `~/.pudding`，本次不存在跨 schema 降级风险。

## 兼容性

- stable/preview 更新通道、bundle identifier、Developer ID 签名、公证和 `~/.pudding` 数据目录保持不变。
- 更新仍通过整包 ZIP 安装，安装前显式停止 daemon 和浏览器桥接。
- 浏览器运行时只改变前端与 Electron 生命周期，不改变 session-scoped 后端协议或 canonical message 边界。
- Provider profile YAML、模型元数据、显式 session 路由和硬件所有权保持不变。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`。
- `npm run test:electron`，127 项通过。
- `web/` 下执行 `npm run build`。
- `workers/oauth/` 下执行 `npm run typecheck`。
- `npm run smoke:electron-browser`，覆盖多标签、多会话、历史、焦点隔离、弹窗、截图与撤销。
- `make schema-check`。
- 自动更新代码审查确认检查、下载、进度、活动 turn 等待、安装前资源停止和 stable/preview 通道路径。
- SQLite schema 对比确认 `internal/store` 无变化，schema 仍为 v9。
- `git diff --check v0.1.23`。

## 剩余发版门槛

1. 提交并推送 `0.1.24` 版本号与发版报告。
2. 构建并验证签名公证包。
3. 执行正式发布，核验 Draft 的英文功能清单与 9 个标准资产后 finalize。

## Release Notes

### Browser Runtime

- Keep managed browser runtimes alive across workspace and session presentation changes.
- Coordinate visible, standby, and automation focus while synchronizing initial page titles and favicons.
- Refine browser options, credential prompts, tabs, and workspace presentation.

### Desktop Updates

- Surface available versions before downloading and show progress after the user starts an update.
- Install downloaded updates automatically when no turn is active, or immediately after the active turn finishes.
- Preserve signed stable and preview update channels while stopping the daemon and browser bridge before installation.

### Workspace Performance

- Reduce layout work with targeted resize observation, pixel-based dock sizing, and memoized workspace surfaces.
- Replace broad query invalidation and state synchronization with focused updates.
- Refine resource menus, tabs, canvas controls, transcript activity, and the floating turn console.
- Simplify reasoning-effort selection around the actual automatic value and refine the compact picker layout.

### Data and Compatibility

- Keep SQLite schema v9 and leave all existing runtime data untouched without migration or backup churn.
- Preserve the existing app identity, data directory, session routing, and provider configuration contracts.
