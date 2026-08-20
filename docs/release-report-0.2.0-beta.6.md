# Pudding 0.2.0-beta.6 发版报告

日期：2026-08-20

对比基线：`v0.2.0-beta.5`

审查范围：`v0.2.0-beta.5..HEAD`、本次测试修复及版本提交

## 发版结论

**应作为第六个 0.2.0 preview 发布。** 本版引入局域网移动访问桥接，重构 Computer Use 的归一化坐标和连续语义动作，并完善权限刷新、应用身份与设置界面。改动范围较大，需要先通过 preview 更新通道完成真实升级和功能验证，不直接晋升稳定版。

## 改动摘要

- 新增由 Electron 托管的局域网访问桥接和移动端配对设置，daemon 继续只监听 loopback。
- Computer Use 指针协议改用窗口内归一化坐标，删除截图尺寸与缩放倍率的重复参数。
- Computer Use 支持连续语义动作，并加强目标窗口变化、前台应用和中断拖拽检查。
- 稳定元素身份不再依赖位置变化，优先使用 identifier、标签或结构路径。
- 新增 Computer Use 应用身份展示，统一桌面权限刷新请求并修正设置页状态处理。
- 调整通用设置、语音设置、侧边栏、会话项和 transcript 的交互与间距。
- 修正 PointerAction 测试仍使用旧像素坐标的问题，使测试与归一化协议一致。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Computer Use | 高 | 指针输入协议、连续动作、元素身份和错误分类均有变化。 |
| 局域网访问 | 高 | 新增按需启动的 Electron 代理和设备配对入口。 |
| macOS 权限 | 中 | 权限刷新请求合并，设置页状态同步方式调整。 |
| 桌面界面 | 中 | 设置、侧边栏、会话项和 transcript 样式调整。 |
| 自动更新 | 低 | 更新实现与通道规则未变化。 |
| SQLite | 无 | schema、迁移和持久化调用均未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12，没有新增迁移。
- 本版不重写 canonical data，不重建 derived indexes，不创建迁移备份，也不改变降级行为。
- 本地 release 数据库 `PRAGMA user_version` 为 12，`PRAGMA quick_check` 返回 `ok`。
- 主应用 bundle identifier 仍为 `com.teatak.pudding`；Computer Use Helper 的身份与签名要求未变化。
- beta.5 用户可通过 preview 更新通道直接升级；稳定通道用户不会收到该版本。

## 发布前验证

- `make test` 通过。
- `PUDDING_RELEASE_CHANNEL=preview npm run test:electron`：179 项通过。
- `make computer-use-helper-test`：55 项通过。
- `make schema-check` 通过。
- `web/` 下执行 `npm run build` 通过。
- `git diff --check` 通过。

## 剩余发布门槛

- 提交并推送版本与报告。
- 运行唯一支持的 `make desktop-preview-publish` 流程。
- 验证 arm64 与 x64 包的签名、公证、Gatekeeper、权限声明和更新元数据。
- 核对九个 release assets 后发布 GitHub Prerelease。

## Release Notes

### Computer Use

- Use normalized window coordinates for pointer actions and remove screenshot-geometry coupling.
- Support sequential semantic actions with stricter foreground, target-window, and interrupted-drag checks.
- Keep element identities stable across layout movement and expose the active application identity in the interface.

### Local Network Access

- Add on-demand local-network access through an Electron-owned bridge while keeping the daemon loopback-only.
- Add device pairing controls and a QR-based mobile access flow in Settings.

### Permissions and Interface

- Coalesce concurrent desktop permission refreshes and keep permission settings state synchronized.
- Refine settings controls, sidebar states, session items, and transcript spacing.

### Reliability

- Align Computer Use pointer tests with the normalized coordinate protocol.
- Keep schema v12, persisted data, signing identities, and preview update compatibility unchanged.
