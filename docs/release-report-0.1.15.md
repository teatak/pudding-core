# Pudding 0.1.15 发版报告

日期：2026-07-22

对比基线：`v0.1.14`（`2466ada1`）

审查版本：`f444faa7`、`1e2900de`、`ba83a676`、`17a3eae8`、`509ab301`、`8a05b962`，以及后续 `0.1.15` 版本提交

## 发版结论

**可以按代码工作流、Composer 与 App 体验增强版发布。** 本次重构内置工具与 App 的归属，统一 shell 命令入口，
支持后台和交互式命令会话，拆分 Composer 组件，并改进应用升级、会话搜索、浏览器点击和桌面窗口行为。
SQLite schema 保持 v4，不新增迁移，不修改 canonical messages、turn、画布、浏览器状态或项目数据的持久化结构。

## 改动摘要

- 将文件、Git、代码智能、截图、Skill Authoring 和 App Authoring 能力组织为明确的内置 App，移除运行时 toolkit 注入。
- 统一 shell 命令参数，支持受控的前台执行、后台进程、TTY 与运行中输入，并通过 shell AST 加强命令和重定向分析。
- 改进文件写入、补丁事务、代码重命名、审批策略和后台进程清理，减少跨会话残留状态。
- 将 Composer 拆分为输入区、附件、审批栏和工具栏组件，补充 IME 组合输入保护并优化附件与审批交互。
- 为已安装 App 增加批量升级，区分稳定版和预览版，并改善会话 App 控件布局。
- 会话搜索结果显示 Chat、Work、Code 模式图标，项目分组与会话列表交互更清晰。
- 浏览器自动点击在派发指针事件前后与目标 WebView 协调焦点生命周期，提升受控点击稳定性。
- 修复 macOS 全屏窗口关闭后无法恢复的问题，并继续保持关闭按钮隐藏主窗口的行为。
- 更新 Qwen、Moonshot 和 Zhipu 模型版本定义，统一中文界面中的 Pudding 品牌名称。
- 补充 OAuth 公共页面与品牌资源，并整理当前设计文档和历史归档。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 代码工具 | 高 | 命令输入契约、后台进程、TTY、文件与补丁能力均有调整。 |
| 内置 App | 高 | 工具按 Project Files、Source Control、Code Intelligence、Capture 与 Authoring App 重新归属。 |
| Composer | 高 | 输入、附件、审批和工具栏拆分，IME 与工作区自动收起逻辑调整。 |
| 应用管理 | 中 | 新增批量升级和稳定版/预览版选择。 |
| 浏览器 | 中 | 自动点击增加 WebView 生命周期确认，浏览器宿主与 smoke 覆盖同步更新。 |
| 会话导航 | 中 | 搜索结果增加模式标识，项目和会话 App 交互调整。 |
| 桌面窗口 | 低 | macOS 全屏关闭时先退出全屏再隐藏窗口。 |
| 模型配置 | 低 | 更新部分内置 provider 的模型版本定义。 |
| SQLite | 无 | schema 和 release fingerprint 均保持 v4。 |
| 运行数据 | 无 | 不改写 canonical 数据，不重建派生索引，不创建迁移备份。 |

## 数据库分析

- `currentSchemaVersion` 与正式数据库 `PRAGMA user_version` 均保持 `4`。
- `internal/store/schema.sql`、`schemaMigrations`、迁移测试和 v4 release fingerprint 未修改。
- 正式数据库以只读方式执行 `PRAGMA quick_check`，结果为 `ok`。
- 本次不重写 canonical messages、turns、sessions、画布、浏览器状态或历史搜索数据。
- 本次不重建 FTS 或其他派生索引，不创建迁移备份，也不改变降级行为。
- 既有 `~/.pudding` 数据目录可由 `0.1.14` 直接继续使用。

## 兼容性

- 桌面自动更新、bundle identifier、数据目录和 release/preview 通道保持不变。
- 已保存会话和项目无需迁移；升级后由新版本重新发布当前工具定义。
- shell 工具参数已收敛为 `command` 字符串和显式 `background` / `tty` 选项；这是模型运行时工具契约变化，
  不影响已落库消息，但升级前正在执行的命令会随应用重启结束。
- 内置 App 的工具归属发生变化，用户安装的第三方 App 与 Skill 数据结构保持兼容。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`：全量 Go 测试通过。
- `npm run test:electron`：106 项 Electron 单元与集成测试通过。
- `npm run smoke:electron-browser`：真实 WebView 浏览器 smoke 通过。
- `web/` 下执行 `npm run build`：TypeScript 与 Vite 生产构建通过。
- `make schema-check`：schema v4 release fingerprint 通过。
- 正式数据库只读检查：`user_version=4`，`quick_check=ok`。
- `git diff --check` 通过。

## 剩余发版门槛

1. 将 `package.json` 与 `package-lock.json` 更新为 `0.1.15`，提交并推送本报告和版本提交。
2. 执行 `make desktop-publish`，完成 arm64、x64 构建、签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后执行 `make desktop-release-finalize`。

## Release Notes

### Code Workflows

- Organize project files, source control, code intelligence, capture, and authoring tools as explicit built-in Apps.
- Run guarded shell commands through one consistent interface with foreground, background, TTY, and live-input support.
- Strengthen file edits, patch transactions, code rename, approvals, and background-process cleanup.

### Composer

- Refine message composition with dedicated text, attachment, approval, and toolbar surfaces.
- Preserve IME composition behavior and improve workspace auto-close, attachment, and approval interactions.

### Apps and Sessions

- Upgrade installed Apps in bulk with stable and preview release awareness.
- Show Chat, Work, and Code mode icons in session search and refine session App controls.

### Browser and Desktop

- Coordinate WebView focus around automated clicks for more reliable browser interaction.
- Restore macOS windows correctly after closing Pudding from full-screen mode.

### Models and Branding

- Refresh bundled Qwen, Moonshot, and Zhipu model definitions.
- Use the Pudding brand consistently across localized desktop and interface copy.

### Compatibility

- Preserve the local database at schema v4 with no migration, data rewrite, index rebuild, or backup required.
