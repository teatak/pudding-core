# Pudding 0.1.23 发版报告

日期：2026-08-04

对比基线：`v0.1.22`（`06528266`）

审查范围：`v0.1.22..b173528c`

## 发版结论

**可以作为浏览器凭据、媒体理解与执行过程交互增强版本发布。** 本次为内置浏览器增加基于
Electron `safeStorage` 的本地加密密码库、保存与自动填充流程及 Chrome CSV 导入；将图片专用工具
替换为统一媒体读取工具，并新增浮动执行过程面板、Mermaid 渲染和审批状态同步。SQLite schema
保持 v9，不执行数据库迁移，也不会在本次升级中清理已有迁移备份。

## 改动摘要

- 为内置浏览器增加密码检测、保存、更新、填充、删除、清空、站点排除和 Chrome CSV 导入。
- 使用 Electron `safeStorage` 加密独立凭据文件；渲染进程只通过受控 IPC 获取当前 origin 的元数据。
- 合并浏览器 preload，强化 session、tab、origin 和 live WebContents 校验，并补充浏览器查找与选项菜单。
- 用 `builtin_media_read` 替换图片专用读取工具，统一读取会话附件及 Code 模式授权文件中的图片和音频。
- 新增底部居中的浮动执行过程面板，归纳当前 turn 的分析、工具进度和文件变更。
- 审批模式更新返回权威 session 状态，前端同步项目审批状态，减少界面与后端状态偏差。
- Markdown 增加 Mermaid 渲染，优化工作区标签关闭、编辑器布局和会话音频控制。
- 数据库迁移成功后自动清理旧迁移备份，仅保留本次迁移生成的备份；普通启动不执行清理。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 浏览器凭据 | 高 | 新增本地加密密码库、页面检测、填充和管理入口。 |
| 浏览器隔离 | 高 | 合并 preload，并加强 session、tab、origin 与 WebContents 路由校验。 |
| LLM 工具协议 | 高 | 图片专用工具由统一媒体读取工具替代，新增音频和授权文件来源。 |
| Turn 交互 | 中 | 新增浮动执行面板并重构 transcript 活动摘要。 |
| 审批状态 | 中 | API 返回 session 快照，前端据此同步项目状态。 |
| Markdown 与工作区 | 中 | 新增 Mermaid，并调整标签、编辑器和音频控制。 |
| SQLite | 低 | schema 仍为 v9；仅新增未来迁移完成后的备份清理行为。 |

## 数据库分析

- `currentSchemaVersion` 保持 `9`，schema SQL 与 fingerprint 均未改变。
- 本次没有 migration，不改写消息、项目、turn、画布、浏览器历史、FTS 或 App 配置。
- 备份清理只在 `version < currentSchemaVersion` 且迁移完成并通过 schema 校验后执行。
- 清理前确认本次迁移生成的备份仍存在；若不存在则不删除任何旧备份。
- 无 schema 升级和迁移失败时均不清理；清理成功后只保留本次迁移备份。
- 当前正式数据库为 schema v9，`PRAGMA quick_check` 为 `ok`，包含 11 个会话、2362 条消息和
  181 个 turn。
- 当前 6 份历史迁移备份约占 95 MiB；因本次没有数据库迁移，它们会保持不变，直到未来真实迁移。

## 兼容性

- stable/preview 更新通道、bundle identifier、签名、公证和 `~/.pudding` 数据目录保持不变。
- 浏览器密码保存在独立加密 vault，不进入 SQLite、canonical messages 或前端持久化状态。
- `builtin_media_read` 是新的媒体读取事实源；旧图片专用工具不再注册给模型。
- Provider profile YAML、session 路由和 canonical message 边界保持不变。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`。
- `npm run test:electron`。
- `npm run smoke:electron-browser`。
- `web/` 下执行 `npm run build`。
- `workers/oauth/` 下执行 `npm run typecheck`。
- `make schema-check`。
- 数据库迁移备份清理覆盖有迁移、无迁移和保留目标丢失三种路径。
- `git diff --check v0.1.22`。

## 剩余发版门槛

1. 提交并推送 `0.1.23` 版本号与发版报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单与 9 个标准资产后执行 `make desktop-release-finalize`。

## Release Notes

### Browser and Passwords

- Add encrypted browser password saving, updating, autofill, management, site exclusions, and Chrome CSV import.
- Consolidate browser preload behavior and strengthen session, tab, origin, and live WebContents routing checks.
- Add in-page find and browser options while refining workspace tab interactions.

### Media and Agent Activity

- Replace the image-only attachment reader with a unified media tool for supported images and audio.
- Route session attachments and authorized Code mode files through one explicit media-reading contract.
- Add a floating turn console that summarizes active analysis, tool progress, and file changes.

### Sessions and Workspace

- Synchronize project approval state from the authoritative session returned by the API.
- Add Mermaid rendering and refine workspace tab closing, editor layout, and session audio controls.
- Improve transcript activity grouping and active-turn presentation.

### Data and Maintenance

- Keep SQLite schema v9 and preserve all existing runtime data without migration in this release.
- Remove older migration backups only after a future successful schema migration, retaining its verified backup.
