# Pudding 0.1.20 发版报告

日期：2026-07-30

对比基线：`v0.1.19`（`6ef9e7c0`）

审查范围：`v0.1.19..93a715e0` 的 3 个功能提交，以及后续 `0.1.20` 发版准备提交

## 发版结论

**可以按桌面交互稳定性更新发布。** 本次修复 transcript 折叠状态在流式更新和 canonical
消息切换时丢失的问题，稳定工具调用及原始数据详情的展开状态；同时改进模型选择、项目排序、
App 包缓存和通知展示，并更新前端 UI 依赖。SQLite schema 保持 v6，不涉及数据库迁移、
canonical 数据改写或索引重建。

## 改动摘要

- 为 transcript 折叠状态同时保存显式打开与关闭值，避免用户关闭后被流式更新再次展开。
- 使用稳定 tool-call key 关联折叠项，避免工具步骤进入或离开压缩分组时丢失展开状态。
- 将工具原始参数与结果详情纳入统一 disclosure 状态，切换渲染阶段后仍保持用户选择。
- 优化模型与 reasoning 选择器的即时更新、失败回滚、面板高度、标签提示和关闭时机。
- 抽取应用级 Select Content，统一弹层定位、拖动区域隔离和项目排序菜单结构。
- 允许创建暂未关联目录的项目，并新增 API 回归测试。
- App 包缓存改为以内容哈希作为缓存键，避免包内容更新后继续显示旧数据。
- 改进 Toast 长文本布局、菜单弹层与 Select 滚动交互，并统一部分 hover/active 样式。
- 收紧 Composer turn 进度提示宽度，强化完成态颜色并缩小处理中 spinner。
- 更新 Radix UI、Lucide 和 shadcn 依赖，移除重复的独立 Radix 包声明。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Transcript | 中 | 折叠状态和工具原始数据详情在流式更新期间保持稳定。 |
| 模型选择 | 中 | 使用乐观更新并支持失败回滚，选择器尺寸与提示更完整。 |
| 项目管理 | 低 | 支持无目录项目，并调整排序菜单。 |
| App 缓存 | 低 | 包内容变化时以哈希失效缓存。 |
| 前端依赖 | 中 | Radix UI、Lucide 与 shadcn 更新，生产构建和桌面测试已覆盖。 |
| SQLite | 无 | schema 保持 v6，无 migration。 |
| 既有运行数据 | 无 | 不改写消息、会话、turn、项目或搜索索引。 |

## 数据库分析

- `currentSchemaVersion` 保持 `6`，release fingerprint 保持
  `ba7c7608f0c8e450cf193174b80fd7701800309bee57a35b25979d0529eaa7e3`。
- 本次没有新增或修改 schema、migration、FTS、索引或数据库初始化逻辑。
- transcript disclosure 属于前端本地 UI 状态，不写入 canonical messages 或 SQLite。
- App 包缓存键、Select 组件和模型选择器更新均不改变运行数据格式。
- 从 `0.1.19` 升级到 `0.1.20` 不触发数据库备份或迁移。
- `0.1.20` 与 `0.1.19` 共用 schema v6；若仅从数据库格式考虑，可直接回退。

## 兼容性

- 桌面自动更新、bundle identifier、stable/preview 通道和 `~/.pudding` 数据目录保持不变。
- 已保存会话、项目、App、连接、Skill、画布和浏览器地址无需迁移。
- 前端依赖升级只进入打包产物，不改变 daemon API 或公开消息协议。
- 模型与 reasoning 更新仍通过显式 session API，并在请求失败时恢复原缓存。
- 无目录项目继续遵循现有 project API，可在之后补充项目目录。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：113 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `workers/oauth/` 下执行 `npm run typecheck`：Cloudflare Worker 类型检查通过。
- schema v6 release fingerprint 测试通过。
- 23 项桌面发布规则测试通过。
- `git diff --check v0.1.19` 通过。

## 剩余发版门槛

1. 提交并推送 `0.1.20` 版本号与发版报告。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Transcript and Tool Details

- Preserve explicit open and closed transcript disclosure states across streaming and canonical updates.
- Keep compacted tool steps stable with persistent tool-call keys as groups change shape.
- Remember raw tool input and output disclosure state alongside the parent tool entry.

### Models and Projects

- Apply model and reasoning changes optimistically with rollback and background reconciliation.
- Improve model picker sizing, labels, accessibility hints, and close behavior.
- Allow projects without directories and refine project sorting controls.

### Apps and Interface

- Invalidate cached App packages by content hash when package contents change.
- Standardize Select popovers, scrolling controls, toast layout, and interaction states.
- Make Composer turn progress more compact with clearer completed and in-progress states.
- Update Radix UI, Lucide, and shadcn dependencies while removing duplicate package declarations.

### Reliability and Compatibility

- Keep the SQLite schema at v6 with no migration, data rewrite, or index rebuild.
- Preserve existing sessions, projects, Apps, connections, Skills, canvas items, and browser addresses.
