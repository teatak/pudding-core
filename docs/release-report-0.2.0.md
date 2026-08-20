# Pudding 0.2.0 发版报告

日期：2026-08-20

稳定版对比基线：`v0.1.26`（`8fda9e14`）

稳定候选基线：`v0.2.0-beta.6`（`d067eb82`）

审查范围：`v0.1.26..HEAD`、六个 preview 版本及本次正式版元数据

## 发版结论

**具备发布 0.2.0 稳定版的条件。** 0.2.0 的功能、数据库迁移、Computer Use Helper、权限流程、
双架构签名公证包和自动更新已通过六个 preview 版本逐步验证。`beta.6` 发布后没有功能代码变化，
本次只提升稳定版本号并生成正式版清单，不引入新的运行时行为。

## 改动摘要

- 新增签名的 macOS Computer Use Helper，支持应用发现、启动、窗口观察、语义操作、截图、指针输入和连续动作。
- 新增会话范围内的应用授权与所有权，统一辅助功能、屏幕录制、相机和麦克风权限状态及用户引导。
- 新增会话克隆，可从指定 canonical message 创建独立会话并复制关联附件。
- 为新 turn 文件变更保存完整快照，支持带冲突检测和状态校验的撤销与重做。
- 新增 Electron 托管的局域网访问桥接和设备配对，daemon 继续只监听 loopback。
- Computer Use 使用窗口内归一化坐标、稳定元素身份和更严格的目标窗口、前台应用及拖拽检查。
- 完善设置、侧边栏、会话项、审批、错误提示和 transcript 等桌面交互。
- 扩展 arm64/x64 打包与更新验证，覆盖嵌套 Helper、权限声明、签名、公证和 designated requirement。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Computer Use | 高 | 新增原生 Helper、系统权限、应用授权、语义动作和指针操作。 |
| SQLite | 高 | 稳定用户首次从 schema v10 迁移到 v12。 |
| 文件变更 | 高 | 新 turn 支持完整快照及安全的撤销/重做。 |
| 打包与更新 | 高 | 双架构包新增嵌套 Helper，并强化签名、公证和更新身份检查。 |
| 局域网访问 | 中 | 新增按需启动的 Electron 桥接和设备配对入口。 |
| 会话与界面 | 中 | 新增会话克隆并调整桌面交互。 |

## 数据库分析

- `currentSchemaVersion` 从稳定版 v10 升到 v12；当前 schema SHA-256 为
  `8f28c4af75aeafedf3fe75f9d0dd4b064acbc6040330260e74836d57469c5b80`。
- v11 新增 `computer_app_grants`，保存 session-scoped Computer Use 应用授权。
- v12 扩展 `turn_file_changes` 并新增 `turn_file_change_states`，保存文件回放所需的完整状态。
- 两个迁移均按目标版本在独立事务中执行；失败不会提交对应 schema version。
- 本版不改写 canonical messages、sessions、turns、projects、canvas 或 browser history，也不重建派生索引。
- 从 v10 升级时创建一份迁移前数据库备份；迁移成功后只保留最新迁移备份并清理更旧备份。
- 本地 release 数据库已是 v12，`PRAGMA quick_check` 返回 `ok`，从 `beta.6` 升级不会再次迁移或创建备份。
- 降级边界改变：数据库升到 v12 后，旧 `0.1.26` 会因 schema 过新而拒绝启动。

## 兼容性

- 正式版继续使用 `Pudding.app`、`com.teatak.pudding` 和 `~/.pudding`。
- 主应用和 Computer Use Helper 的 bundle identifier、Team ID 与 designated requirement 未变化。
- `beta.6` 用户可直接升级到正式版；稳定版用户通过稳定更新通道从 `0.1.26` 升级。
- 未开启 preview 的稳定用户不会收到 beta；0.2.0 发布后会重新进入稳定通道。
- daemon 继续只监听 loopback；局域网访问由 Electron 按需代理并通过配对令牌授权。
- 旧文件变更仍可展示，但没有完整快照的记录不可撤销，避免不安全恢复。

## 已完成验证

- 审查 `v0.1.26..v0.2.0-beta.6` 的完整改动和全部持久化影响。
- `make test` 通过。
- `PUDDING_RELEASE_CHANNEL=preview npm run test:electron`：179 项通过。
- `make computer-use-helper-test`：55 项通过。
- `make schema-check` 通过。
- `web/` 下执行 `npm run build` 通过。
- `git diff --check` 通过。
- beta.6 双架构包的签名、公证、Gatekeeper、权限声明和九个更新资产验证通过。
- beta.6 已完成真实升级与核心功能测试，用户确认没有问题。

## 剩余发布门槛

- 提交并推送正式版本号与本报告。
- 运行唯一支持的 `make desktop-publish` 流程。
- 重新验证 arm64 与 x64 正式包的签名、公证、Gatekeeper、权限声明和更新元数据。
- 核对九个 release assets 后发布 GitHub Stable Release。

## Release Notes

### Computer Use

- Add a signed native macOS Computer Use helper with session-scoped app approval, ownership, semantic actions, screenshots, and guarded pointer input.
- Use normalized window coordinates, stable element identities, and stricter foreground, target-window, and interrupted-drag validation.
- Support sequential semantic actions and expose the active application identity in the interface.

### Permissions

- Add just-in-time guidance for Accessibility and Screen & System Audio Recording permissions.
- Keep desktop settings and runtime permission state synchronized, and resume blocked Computer Use operations after authorization.
- Preserve native Camera and Microphone authorization flows.

### Sessions and File Recovery

- Clone a conversation through any canonical message into an independent session with copied attachment ownership.
- Capture complete text, binary, file type, mode, and digest snapshots for new turn file changes.
- Support transactional undo and redo with conflict detection and safe handling of legacy non-reversible records.

### Local Network Access

- Add on-demand local-network access through an Electron-owned bridge while keeping the daemon loopback-only.
- Add device pairing controls and a QR-based access flow in Settings.

### Desktop Experience

- Refine settings controls, sidebar and session states, approvals, errors, transcript layout, and desktop interactions.
- Bundle and verify native components independently for Apple Silicon and Intel Macs.

### Data Migration

- Migrate SQLite from schema v10 to v12 while preserving canonical conversations, projects, canvas state, and browser history.
- Create one pre-migration database backup and retain only the newest successful migration backup.
