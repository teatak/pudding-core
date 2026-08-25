# Pudding 0.2.5 发版报告

日期：2026-08-25

对比基线：`v0.2.4`（`4fdf8e52`）

候选提交：`6c3bbd67`

## 发版结论

**可以发布 0.2.5 正式版。** 本版集中优化对话、侧边栏、浮层控件和吉祥物的交互与视觉，
并为较长的用户消息增加可折叠展示。改动仅涉及 Web 前端，不改变 SQLite schema、daemon、
系统权限、签名、公证或自动更新链路。

## 改动摘要

- 长用户消息超过固定高度后自动折叠，可展开或收起，并在收起时保持当前阅读位置。
- 简化上下文用量环和详情面板，以当前上下文估算作为统一展示事实源。
- 将“跳到最新消息”按钮居中放置，避免与 composer 吉祥物和边缘控件互相影响。
- 统一 popover、侧边栏项目、分支列表和各类选择器的 hover、active、selected 状态。
- 优化后台进程列表、模型推理选择器、会话音频控件和项目操作控件的交互反馈。
- 调整文件改动图标、侧边栏动作定位、tooltip 行为和吉祥物金属层次。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 对话界面 | 低 | 新增长用户消息折叠，并调整跳到最新消息按钮位置。 |
| 上下文用量 | 低 | 统一以当前上下文估算值呈现环形进度和详情。 |
| 浮层与菜单 | 低 | 统一 hover、active、selected 视觉状态，不改变业务请求。 |
| 侧边栏 | 低 | 优化项目选中态、动作按钮定位和 tooltip。 |
| 吉祥物与图标 | 低 | 仅视觉层次和图标语义调整。 |
| SQLite | 无 | schema 仍为 v13，本版没有迁移。 |
| daemon 与 API | 无 | 后端协议、session routing 和工具行为未变化。 |
| 自动更新 | 无 | 更新检查、下载、安装和通道逻辑未变化。 |
| 权限与签名 | 无 | Bundle identity、权限归属、签名和公证要求未变化。 |

## 数据与兼容性

- SQLite schema 保持 v13，本版不创建数据库备份，也不执行迁移或清理。
- canonical sessions、messages、turns、projects、usage 和 provider 配置均不重写。
- 稳定版继续使用 `~/.pudding`，更新通道和数据目录规则不变。
- 正式数据库只读检查结果为 `PRAGMA user_version = 12`、`PRAGMA quick_check = ok`；
  该数据库会由既有 v12 到 v13 迁移在首次启动新版时升级，本版没有新增迁移步骤。

## 已完成验证

- `make test` 通过。
- `PUDDING_RELEASE_CHANNEL=stable npm run test:electron`：180 项通过。
- `make schema-check` 通过，schema v13 release contract 有效。
- `web/` 生产构建通过；仅有既有的大 chunk 提示。
- 正式数据库只读检查：`PRAGMA user_version = 12`，`PRAGMA quick_check = ok`。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到 Draft Release 后再显式发布。

## Release Notes

### Conversation Experience

- Collapse long user messages into a compact preview with explicit expand and collapse controls.
- Preserve the reader's viewport position when a long message is collapsed.
- Center the jump-to-latest control so it remains clear of composer decorations and edge controls.

### Usage and Context

- Present context usage from one consistent estimate across the ring and detail panel.
- Simplify usage progress styling while retaining warning, danger, and compaction thresholds.

### Interface Polish

- Standardize hover, active, and selected states across popovers, sidebars, selectors, and branch menus.
- Refine background-process controls, reasoning selection, session audio controls, and project actions.
- Improve file-change icon semantics, sidebar action alignment, tooltip behavior, and mascot rendering.

### Data and Compatibility

- Keep SQLite schema v13 unchanged with no migration or canonical-data rewrite.
- Preserve the existing daemon protocol, permissions, signing, and automatic-update behavior.
