# Pudding 0.1.22 发版报告

日期：2026-08-03

对比基线：`v0.1.21`（`e4bf1c2f`）

审查范围：`v0.1.21..fc851323`

## 发版结论

**可以作为 Code 工具可靠性、模型设置与桌面交互增强版本发布。** 本次将项目文件、Git 和代码
智能工具改为 Code 模式固定核心能力，增强前台命令文件变更追踪和实时工具状态对账，并重构模型、
推理强度和语音设置。SQLite 从 v7 依次迁移到 v8、v9：删除已撤销的项目 App 绑定表，并从会话
配置中清理 3 个已转为核心能力的旧 App ID；迁移前自动备份，不改写消息、项目、turn、画布、
浏览器历史或 FTS 数据。

## 改动摘要

- 将项目文件、Git 和代码智能工具从可加载内置 App 改为 Code 模式固定核心工具，减少工具状态漂移。
- 修复 tool turn 实时 overlay 对账，并通过静态命令分析追踪可确认的前台命令文件变更。
- 重构 Provider 创建与编辑流程，完善预设、协议字段、模型配置和通用校验。
- 统一 OpenAI、Anthropic 与 Google 的推理强度处理，并持久化模型级推理偏好。
- 为语音设置增加“通用 / 高级”分段视图，并为设置加载失败状态增加手动刷新入口。
- 改善会话侧栏渐隐提示、Composer 焦点恢复、浏览器历史菜单和空状态布局。
- 优化工作区标签宽度、图标与关闭按钮布局，浏览器刷新改为直接重载当前 WebContents。
- 更新工作区空状态插图与桌面细节样式。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Code 工具 | 高 | 文件、Git 与代码智能工具改为固定核心能力，工具名和协议保持不变。 |
| 工具执行与文件追踪 | 高 | 调整实时工具状态对账，并扩展前台命令文件变更归因。 |
| Provider 与模型设置 | 中 | 创建、编辑、预设、推理强度和校验流程有较大调整。 |
| 语音设置 | 中 | 新增通用/高级分组和错误恢复入口，不改变已有配置格式。 |
| 浏览器与工作区 | 中 | 调整刷新语义、标签布局、空状态和侧栏滚动提示。 |
| SQLite | 中 | schema 从 v7 升到 v9，删除废弃表并清理会话中的旧内置 App ID。 |
| 既有核心数据 | 低 | 不改写消息、项目、turn、画布、浏览器历史或搜索索引。 |

## 数据库分析

- `currentSchemaVersion` 从 `7` 升到 `9`；当前 schema fingerprint 为
  `ba7c7608f0c8e450cf193174b80fd7701800309bee57a35b25979d0529eaa7e3`。
- v8 migration 在事务内执行 `DROP TABLE IF EXISTS project_app_bindings`，移除已撤销的项目 App
  绑定功能及其索引。该表中的绑定数据会被删除，不影响 `projects` 或 `sessions`。
- v9 是仅数据迁移：从 `sessions.loaded_app_ids` 中移除 `project-files`、`source-control` 和
  `code-intelligence`，其他 App ID 保持原顺序并规范化保存。
- 新安装直接创建 v9 schema；对无版本号但符合当前布局的数据库先识别为 v8，再执行 v9 数据清理。
- 正式数据库副本在升级前确认为 schema v7，`PRAGMA quick_check` 返回 `ok`；包含 11 个会话、
  2362 条消息和 181 个 turn，项目绑定及受旧 App ID 影响的会话均为 0。
- 从 v7 首次升级前自动执行 WAL checkpoint，并生成 `pudding.db.backup-v7-<timestamp>` 备份。
- 迁移回归测试覆盖 v7 表删除、项目与会话保留、v8 旧 App ID 清理和备份生成。
- 直接回退到 `0.1.21` 不受支持：旧版本最高识别 schema v7，会拒绝打开 v9 数据库。需要回退时应
  先备份当前 `~/.pudding`，再恢复自动生成的 v7 数据库备份。

## 兼容性

- 桌面自动更新、bundle identifier、stable/preview 通道和 `~/.pudding` 数据目录保持不变。
- 文件、Git 与代码智能工具的 LLM 工具名保持不变，Code 模式不再要求加载对应内置 App。
- 已保存的 3 个旧内置 App ID 会自动清理；其他已加载 App 与 App 连接不受影响。
- Provider profile YAML 仍是模型配置事实源，SQLite 不承载 Provider profile 元数据。
- 浏览器刷新会重载当前 WebContents，保留当前标签身份与浏览器组件生命周期。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：113 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过全部 20 项检查。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `workers/oauth/` 下执行 `npm run typecheck`：Cloudflare Worker 类型检查通过。
- schema v7 到 v9 migration、备份、旧数据保留和废弃 App ID 清理测试已覆盖。
- 正式数据库副本完成版本、数据量和 `PRAGMA quick_check` 审计。
- `git diff --check v0.1.21` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.22` 版本号与发版报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Code Tools and Reliability

- Make project files, Git, and code intelligence tools always available as core Code mode capabilities.
- Reconcile live tool-call overlays more reliably and preserve authoritative tool execution across runtime refreshes.
- Track statically identifiable file mutations from foreground commands in turn file changes.

### Models and Settings

- Redesign provider creation and editing with clearer presets, protocol fields, model options, and validation.
- Standardize reasoning effort across OpenAI, Anthropic, and Google models and remember model-level preferences.
- Add General and Advanced voice settings, manual error refresh actions, and clearer settings organization.

### Browser and Workspace

- Reload the active browser WebContents directly while preserving its tab and component lifecycle.
- Refine workspace tabs, empty states, browser history, session rail fades, and composer focus restoration.
- Improve icon alignment, close-button visibility, responsive tab widths, and workspace illustration layout.

### Data and Compatibility

- Upgrade SQLite from schema v7 to v9 with an automatic pre-migration backup.
- Remove the retired project App binding table and clean obsolete built-in App IDs from session settings.
- Preserve existing messages, projects, turns, canvas items, browser history, search indexes, and other App selections.
