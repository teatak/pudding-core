# Pudding 0.2.8 发版报告

日期：2026-08-30

对比基线：`v0.2.7`（`53d32893`）

候选提交：`23175a82`

## 发版结论

**可以发布 0.2.8 正式版。** 本版集中优化浏览器标签与工作区活动栏，统一 Computer Use 操作接口和应用身份展示，并补强项目合并与自动更新状态管理。
SQLite schema、权限归属、签名、公证和稳定通道规则均未变化。

## 改动摘要

- 浏览器标签改为异步预留并拆分运行态上下文，减少打开、关闭和切换标签时的阻塞与无关重渲染。
- 关闭标签后优先选择相邻的下一个标签；无下一个时选择上一个，行为不再依赖历史父标签。
- 统一工作区标签、活动栏卡片和空白网页的标题、图标及事实源，浏览器页面与画布产物保持同步。
- 将语义操作和指针操作统一到有序的 Computer Use action 接口，并加强目标与权限校验。
- Computer Use 记录仅在明确识别到 macOS bundle ID 时显示原生应用图标，不再显示 Pudding 自身能力图标。
- 项目合并在单个事务中迁移关联会话并删除重复项目，避免跨步骤中间状态。
- 自动更新在已有可用版本时继续后台检查，可切换到更新版本；刷新失败时保留当前可用更新。
- 调整响应式工作区侧栏、项目菜单、模型选择器和相关界面细节。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 浏览器与工作区 | 中 | 标签生命周期、运行态订阅、相邻标签选择和活动栏展示均有调整，并有 Electron 与前端构建覆盖。 |
| Computer Use | 中 | 工具 action 结构和转录展示已统一，原生 Helper 身份与权限归属不变。 |
| 项目管理 | 中 | 新增事务化项目合并行为，沿用现有 projects/sessions 表结构。 |
| 自动更新 | 中 | 后台刷新可替换更新版本并保留失败前状态，稳定通道、下载和安装机制不变。 |
| SQLite | 无迁移 | schema 仍为 v13，不执行版本升级备份、迁移或清理。 |
| 权限与签名 | 无 | Bundle identity、TCC 权限归属、Developer ID 签名与公证要求未变化。 |

## 数据与兼容性

- SQLite schema 保持 v13，本版安装或启动时不创建迁移备份，也不执行迁移或备份清理。
- 项目合并只在用户明确执行时事务化更新现有 projects/sessions 数据，不改写 canonical messages 或 turns。
- provider 配置、usage、会话内容和派生索引均不重建。
- 稳定版继续使用 `~/.pudding`，更新通道和数据目录规则不变。
- 正式数据库只读检查结果为 `PRAGMA user_version = 13`、`PRAGMA quick_check = ok`。

## 已完成验证

- `make test` 通过。
- `PUDDING_RELEASE_CHANNEL=stable npm run test:electron`：192 项通过。
- `make schema-check` 通过，schema v13 release contract 有效。
- `web/` 生产构建通过。
- 正式数据库只读检查：`PRAGMA user_version = 13`，`PRAGMA quick_check = ok`。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到 Draft Release 后再显式发布。

## Release Notes

### Browser and Workspace

- Make browser tabs open and close more responsively with asynchronous reservation and isolated runtime state.
- Select the adjacent next tab after closing, or the previous tab when no next tab exists.
- Keep browser pages and canvas artifacts synchronized across workspace tabs and the responsive activity rail.
- Unify blank-tab names, browser metadata, and icons across workspace surfaces.

### Computer Use

- Unify semantic and pointer interactions under one ordered action interface with stronger target validation.
- Show native macOS app icons for recognized Computer Use activity without exposing Pudding capability icons in the transcript.

### Projects and Navigation

- Merge duplicate projects transactionally while preserving their sessions and directories.
- Improve project menus, session navigation, model controls, and responsive workspace layout.

### Updates and Reliability

- Keep background update checks active while an update is available and adopt a newer version when found.
- Preserve the current update offer when a background refresh fails.
- Reduce unnecessary browser runtime subscriptions and conversation rerenders.

### Data and Compatibility

- Keep SQLite schema v13 unchanged with no release migration, backup, cleanup, derived-index rebuild, or canonical-data rewrite.
- Preserve existing app identity, permissions, signing, notarization, and stable update-channel behavior.
