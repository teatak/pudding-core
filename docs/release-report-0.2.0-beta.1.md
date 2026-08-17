# Pudding 0.2.0-beta.1 发版报告

日期：2026-08-17

对比基线：`v0.1.26`（`8fda9e14`）

审查范围：`v0.1.26..HEAD` 及本次版本提交

## 发版结论

**应先作为 0.2.0 preview 发布，不直接进入稳定通道。** 本次首次引入签名的 macOS Computer Use
Helper、系统辅助功能和录屏权限管理，并增加会话克隆、文件变更撤销/重做和 transcript 交互重构。
SQLite schema 从 v10 升至 v12。改动跨越原生进程、Electron bridge、daemon、工具策略、数据库和发布
签名链，适合通过 opt-in preview 验证真实权限、升级和应用操作后再发布 0.2.0 稳定版。

## 改动摘要

- 新增 macOS Computer Use Helper、Electron bridge 和 daemon controller，支持应用发现、启动或接管、
  窗口观察、Accessibility 语义操作、截图坐标操作和会话所有权内的退出。
- 新增 Computer Use 权限设置和 session-owned App 授权，明确区分 Pudding 启动的应用与已运行应用。
- 新增会话克隆，可从指定 canonical message 创建独立会话并复制关联附件。
- 为 turn 文件变更保存完整文本、二进制、类型、权限和摘要快照，支持带冲突检测的撤销与重做。
- 统一 transcript 展开组件，将提交错误移入 composer，并调整审批栏、会话栏和吉祥物交互。
- 扩展双架构打包与更新校验，验证嵌套 Helper 的版本、架构、Team ID、designated requirement 和权限声明。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Computer Use | 高 | 新增原生 Swift Helper、独立 bridge、系统权限和高风险输入动作。 |
| 打包与更新 | 高 | arm64/x64 包新增嵌套 App，签名、公证和更新身份必须保持一致。 |
| SQLite | 高 | schema 从 v10 升到 v12，新增授权和文件回放持久化结构。 |
| 文件变更 | 高 | 新 turn 可保存完整快照并执行带冲突检测的撤销/重做。 |
| 会话 | 中 | 新增 canonical history 克隆和附件复制。 |
| Transcript/UI | 中 | 展开、错误、审批、会话键和吉祥物布局调整。 |

## 数据库分析

- `currentSchemaVersion` 从 `10` 升到 `12`；当前 schema SHA-256 为
  `8f28c4af75aeafedf3fe75f9d0dd4b064acbc6040330260e74836d57469c5b80`。
- v11 新增 `computer_app_grants`，以 `(session_id, app_id)` 保存会话范围内的 Computer Use App 授权。
- v12 为 `turn_file_changes` 增加 snapshot version、digest、mode、type、binary 和 BLOB 字段，并新增
  `turn_file_change_states`；现有 turn file changes 只补记 `applied` 状态，不伪造可逆快照。
- 两个迁移按目标版本分别在事务中执行；任一步失败都不会提交对应 schema version。
- 正式 v10 数据库副本迁移到 v12 的演练通过：`PRAGMA quick_check=ok`，2 个 sessions 和 122 条
  messages 保持不变。
- 本版不改写 canonical messages、sessions、turns、projects、canvas 或 browser history，不重建 FTS 或
  其他派生索引。
- 迁移前创建一份完整数据库备份；迁移全部成功后仅保留本次最新备份并清理更旧迁移备份。
- 降级边界改变：preview 与稳定版共用 `~/.pudding`，数据库升到 v12 后，旧 `0.1.26` 会以 schema
  过新拒绝启动。关闭 preview 只影响后续更新，不会降级应用或数据库。

## 兼容性

- `0.1.26` 已包含“接收 Pudding 预览版”设置；未 opt-in 的稳定用户保持 `allowPrerelease=false`，不会
  收到 `0.2.0-beta.1`。
- Preview 继续使用 `Pudding.app`、`com.teatak.pudding` 和 `~/.pudding`，未来 0.2.0 稳定版必须保留
  v11/v12 的向前迁移和所有新 schema。
- Computer Use 仅在 Electron macOS 产品面启用；daemon 仍通过 loopback token 连接 Electron bridge。
- Helper 与外层 App 使用同一 Developer ID Team，更新前后完整 designated requirement 必须一致。
- 会话克隆和文件回放 API 均显式携带 session ID，没有新增后端 focus 状态或无 session scope 主路径。
- 旧文件变更仍可展示，但没有完整快照的记录不可撤销，避免根据截断内容进行不安全恢复。

## 已完成验证

- 审查 `v0.1.26..HEAD` 的 15 个提交、152 个文件和全部持久化调用点。
- `go test -tags "sqlite_fts5 webrtcaec" ./...`。
- `npm run test:electron`，166 项通过。
- `make computer-use-helper-test`，55 项 Swift/XCTest 测试通过。
- Computer Use fixture、完整产品链路、Calculator session-owned 和既有 Calculator 非所有权 smoke 全部通过。
- `web/` 下执行 `npm run build`。
- `git diff --check v0.1.26..HEAD`。
- 只读检查正式数据库：`PRAGMA user_version=10`、`PRAGMA quick_check=ok`。
- 在 `/tmp` 的正式数据库副本上完成 v10→v12 迁移演练并验证数据数量与迁移备份。

## 剩余发版门槛

1. 构建并验证 arm64/x64 的 preview 签名公证包及嵌套 Helper 身份。
2. 验证已安装稳定版升级到 preview，并确认更新后 Helper designated requirement。
3. 核验 Draft 的英文功能清单与 9 个标准资产后发布为 GitHub Prerelease。

## Release Notes

### Computer Use

- Add a signed native macOS Computer Use helper with session-scoped app approval, app ownership, semantic Accessibility actions, screenshots, and guarded pointer input.
- Add explicit Accessibility, Screen Recording, camera, and microphone permission management in desktop settings.

### Session Workflows

- Clone a conversation through any canonical message into an independent session with copied attachment ownership.
- Keep transcript identity scoped to its session and refine clone, navigation, approval, and error interactions.

### File Change Recovery

- Capture complete text, binary, file type, mode, and digest snapshots for new turn file changes.
- Support transactional undo and redo with state validation, conflict detection, and safe handling of legacy non-reversible records.

### Desktop Packaging

- Bundle the Computer Use helper for both Apple Silicon and Intel Macs with architecture, privacy, version, Team ID, and designated requirement verification.
- Preserve the helper identity across preview updates and stop the helper cleanly before installing an update.

### Data Migration

- Migrate SQLite from schema v10 to v12 with session-owned Computer Use grants and file replay state while preserving canonical conversations.
- Create one pre-migration database backup and retain only the newest successful migration backup.
