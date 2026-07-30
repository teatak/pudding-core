# Pudding 0.1.19 发版报告

日期：2026-07-30

对比基线：`v0.1.18`（`f6d3e650`）

审查范围：`v0.1.18..1b4ae53a` 的 15 个功能提交，以及后续 `0.1.19` 发版准备提交

## 发版结论

**可以按 provider 原生续写与桌面体验整合版发布。** 本次为 Anthropic、Google 和 OpenAI Responses
增加跨模型调用的原生 continuation 保存与回放，改善工具循环中的 reasoning、签名和 tool-call 连续性；
同时重构命令环境、文件变更追踪、App/OAuth 流程、项目管理和桌面界面，并统一 Pudding 品牌资源。
SQLite schema 从 v5 升级到 v6，仅给文件变更记录新增来源字段；正式数据库副本迁移和备份恢复均通过。

## 改动摘要

- 为 Anthropic、Google 和 OpenAI Responses 接入 provider-native continuation，保留原生 reasoning、
  signature 和 tool-call 状态，减少多轮工具调用中的上下文损失。
- 将 continuation 作为隐藏协议状态写入 canonical message metadata，对 UI 和公开 API 保持不可见。
- 优化 provider 消息分组、上下文估算和 tool loop 提交边界，并覆盖 state-only 响应和中断场景。
- 启动时安全读取用户 login shell 环境，过滤后用于命令工具，补齐开发工具 PATH 和常用环境变量，
  同时继续使用固定非 login shell 执行命令。
- 将结构化文件工具的变更追踪收窄到明确目标，避免扫描整个项目；命令观测到的修改与结构化修改分开标记。
- 重构 App 页面，分开展示内置 App 与已安装 App，改进安装状态、图标加载和包哈希缓存失效。
- 重构 OAuth Worker 的事务、provider、路由和兼容层，统一 OAuth 流程并支持新用户安装引导。
- 合并项目创建、重命名和目录编辑为统一表单，支持多目录、拖放和顺序调整。
- 更新 reasoning 选择器、转录折叠、Session Rail、文件变更、Voice 加载状态和 Monaco 语言支持。
- 建立集中式图标组件与统一 hover/active 样式，减少页面间交互和尺寸差异。
- 统一 Pudding Mark、macOS App/Tray、Web、OpenGraph 和 DMG 品牌资源，并加入可重复生成脚本。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Provider 协议 | 高 | 三类 provider 支持原生 continuation 保存与回放。 |
| Canonical context | 高 | 新消息可在 metadata 中保存隐藏 provider 协议状态。 |
| 命令工具 | 高 | 命令继承过滤后的用户开发环境与补全后的可执行路径。 |
| 文件变更 | 高 | 改为目标级结构化追踪，并区分 structured 与 command-observed。 |
| App/OAuth | 中 | App 分类、安装状态、图标缓存和 OAuth 路由重构。 |
| 项目管理 | 中 | 创建与编辑表单统一，支持多目录拖放与调整。 |
| 桌面界面 | 中 | 图标、交互状态、转录、reasoning、Session Rail 和品牌资源更新。 |
| SQLite | 中 | schema v5 升级到 v6，新增 `turn_file_changes.origin`。 |
| 既有运行数据 | 低 | 不改写会话、消息或 turn，不重建派生索引。 |

## 数据库分析

- `currentSchemaVersion` 从 `5` 升级到 `6`，release fingerprint 为
  `ba7c7608f0c8e450cf193174b80fd7701800309bee57a35b25979d0529eaa7e3`。
- v6 迁移仅向 `turn_file_changes` 增加 `origin TEXT NOT NULL DEFAULT 'structured'`。
- 既有文件变更记录会得到默认来源 `structured`；新记录可进一步区分结构化工具修改和命令观测修改。
- provider continuation 使用现有 `messages.metadata` 保存隐藏协议状态，不新增表或数据库列；
  只影响升级后新产生的 assistant 输出，既有消息不改写。
- 迁移前会 checkpoint WAL，并创建权限为 `0600` 的 `pudding.db.backup-v5-<UTC timestamp>` 备份。
- 使用正式 `~/.pudding` 数据库的在线副本完成 v5→v6 演练：`quick_check=ok`，
  sessions `11`、messages `2362`、turns `181`、file changes `19`，迁移前后数量完全一致。
- 演练生成的 v5 备份可正常打开，`quick_check=ok`，且不包含 `origin` 列。
- 本次不重写 canonical messages、turns、sessions、画布或浏览器状态，也不重建 FTS 或其他派生索引。
- v6 是前向迁移；`0.1.18` 无法直接打开已升级为 v6 的数据库。需要降级时，应先退出 Pudding，
  再使用迁移自动生成的 v5 备份恢复数据库。

## 兼容性

- 桌面自动更新、bundle identifier、release/preview 通道和 `~/.pudding` 数据目录保持不变。
- 已保存会话、项目、App、连接、Skill、画布和浏览器地址可继续使用。
- provider 原生状态只在 provider 与 model 匹配时回放，并保持现有公开消息 JSON 形状。
- login shell 仅用于一次性采集受控环境；实际命令仍由固定 shell 执行，不引入每次调用的 shell 配置副作用。
- 文件变更来源字段有默认值，现有转录和历史 turn 无需重算。
- OAuth Worker 保留旧回调兼容路径；远端 Worker 仍需按其独立部署流程发布。
- 数据库迁移保持已有数据不变，但升级后降级必须恢复 v5 备份。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：113 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `workers/oauth/` 下执行 `npm run typecheck`：Cloudflare Worker 类型检查通过。
- schema v6 release fingerprint 测试通过。
- 正式数据库副本 v5→v6 迁移、备份恢复和数据计数检查通过。
- `git diff --check v0.1.18` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.19` 版本号、发版报告和归档文档格式清理。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Provider Continuity

- Preserve native reasoning, signatures, and tool-call state across Anthropic, Google, and OpenAI Responses calls.
- Keep provider continuation data hidden from public message payloads while retaining canonical context continuity.
- Improve provider message grouping, request estimation, and state-only tool-loop handling.

### Development Workflows

- Capture a filtered login-shell environment once so command tools can find the user's development toolchain reliably.
- Track structured file mutations by exact target instead of scanning entire project roots.
- Distinguish structured file edits from command-observed changes in turn file summaries.

### Apps and OAuth

- Separate built-in and installed Apps with clearer installation state and package-aware icon caching.
- Refine OAuth transactions, provider routing, legacy callback compatibility, and installation handoff.
- Unify project creation, renaming, and multi-directory editing in one drag-and-drop form.

### Interface and Brand

- Refresh reasoning controls, transcript folding, session navigation, file changes, voice loading, and editor language support.
- Centralize icons and interaction states for more consistent hover, active, and sizing behavior.
- Unify Pudding branding across the app icon, tray, web assets, OpenGraph imagery, and DMG.

### Reliability and Compatibility

- Upgrade the local database from schema v5 to v6 with an automatic pre-migration backup.
- Preserve existing sessions, messages, turns, projects, and indexes without canonical rewrites or rebuilds.
