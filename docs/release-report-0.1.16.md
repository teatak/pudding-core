# Pudding 0.1.16 发版报告

日期：2026-07-23

对比基线：`v0.1.15`（`b980ed9b`）

审查版本：`24384e87`、`ee05c5bf`，以及后续 `0.1.16` 发版准备提交

## 发版结论

**可以按任务进度、App 生命周期与项目工作流增强版发布。** 本次增加结构化任务计划和 Composer 进度展示，
允许模型卸载当前任务不再需要的 App，收敛项目文件与指令读取路径，并改善项目搜索、转录展示和录音状态视觉。
SQLite schema 保持 v4，不新增迁移，不修改 canonical messages、turn、画布、浏览器状态或项目数据的持久化结构。

## 改动摘要

- 新增结构化计划工具，支持 2 至 12 个有序步骤，并在 Composer 上方显示当前步骤、总进度和完整计划。
- 新增会话级 App 卸载能力；任务完成后可移除不再相关的 App 工具，不影响安装状态和连接配置。
- 已加载 App 会显式进入提示上下文，避免重复加载，并指导模型在任务阶段结束后主动释放无关工具。
- Code 模式在构建上下文时读取每个授权项目根目录的 `AGENTS.md`，同时保持嵌套规则按具体路径生效。
- 收敛项目文件工具，统一文件读取、项目根目录枚举和补丁入口，移除重复的项目检查与指令工具。
- 改进项目搜索、项目列表和空状态布局，增强搜索结果信息密度与交互反馈。
- 在转录中简化代码工具详情和文件变更展示，减少重复状态并保持工具名称本地化。
- 调整录音激活态的边框、遮罩和渐变，让 Composer 状态更清晰。
- 增强发布脚本对 GitHub `untagged-*` Draft 占位标签的自动恢复，避免资产上传完成后流程中断。
- 固定真实 WebView smoke 的窗口激活时序，使可信指针点击验证不受当前前台应用影响。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 任务执行 | 高 | 新增计划工具和运行中计划状态，模型可持续更新步骤进度。 |
| App 生命周期 | 高 | 已加载 App 可在会话中卸载，其工具从后续模型步骤移除。 |
| Code 模式 | 高 | 项目指令改由上下文构建器读取，文件能力和工具集合有所收敛。 |
| 项目界面 | 中 | 搜索、列表、空状态和项目操作布局调整。 |
| 转录与 Composer | 中 | 新增计划进度，简化代码工具详情并更新录音状态视觉。 |
| 发布流程 | 低 | Draft 占位标签可自动恢复，WebView smoke 激活时序更确定。 |
| SQLite | 无 | schema 和 release fingerprint 均保持 v4。 |
| 运行数据 | 无 | 不改写 canonical 数据，不重建派生索引，不创建迁移备份。 |

## 数据库分析

- `currentSchemaVersion` 与正式数据库 `PRAGMA user_version` 均保持 `4`。
- `internal/store/schema.sql`、`schemaMigrations`、迁移测试和 v4 release fingerprint 未修改。
- 正式数据库以只读方式执行 `PRAGMA quick_check`，结果为 `ok`。
- 本次不重写 canonical messages、turns、sessions、画布、浏览器状态或历史搜索数据。
- 本次不重建 FTS 或其他派生索引，不创建迁移备份，也不改变降级行为。
- 既有 `~/.pudding` 数据目录可由 `0.1.15` 直接继续使用。

## 兼容性

- 桌面自动更新、bundle identifier、数据目录和 release/preview 通道保持不变。
- 已保存会话、项目、App、连接和 Skill 无需迁移。
- 已加载 App 的会话状态仍由现有会话数据管理；升级后模型可使用新的卸载工具释放无关 App。
- 项目根目录 `AGENTS.md` 会自动进入 Code 模式上下文；嵌套指令继续由具体文件路径决定作用域。
- 旧的项目检查与项目指令工具从模型工具集合移除，能力由统一文件工具和上下文构建器承接。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：107 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- 正式数据库只读检查：`user_version=4`，`quick_check=ok`。
- `git diff --check` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.16` 版本号、发版报告和发布流程修复。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Task Progress

- Let long-running tasks publish an ordered plan with explicit pending, active, and completed steps.
- Show the current step and overall progress above the Composer, with the full plan available on hover or focus.

### Apps and Context

- Unload Apps that are no longer relevant without uninstalling them or removing their connections.
- Track loaded Apps in the model context to avoid redundant loads and keep later steps focused.
- Include authorized project-root instructions automatically in Code mode while preserving path-scoped nested rules.

### Project Workflows

- Consolidate project file reading, root discovery, and patch workflows into a smaller, clearer tool set.
- Improve project search, project lists, empty states, and result interactions.

### Transcript and Composer

- Simplify code tool details and file-change presentation in the transcript.
- Refine the Composer recording state with clearer borders, masks, and gradients.

### Release Reliability

- Recover GitHub draft releases that temporarily receive an `untagged-*` placeholder after asset upload.
- Make real WebView pointer-click smoke tests deterministic by activating the test window explicitly.

### Compatibility

- Preserve the local database at schema v4 with no migration, data rewrite, index rebuild, or backup required.
