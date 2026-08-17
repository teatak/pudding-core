# Pudding 0.2.0-beta.2 发版报告

日期：2026-08-17

对比基线：`v0.2.0-beta.1`

审查范围：`v0.2.0-beta.1..HEAD` 及本次版本提交

## 发版结论

**应作为第二个 0.2.0 preview 发布。** 本版将 Computer Use 的 macOS 权限处理从工具错误和
LLM 文本提示改为由 Electron 统一协调的原生权限引导，并在授权完成后恢复被中断的操作。它适合用来
验证 beta.1 到 beta.2 自动更新、Helper 权限身份保持和真实授权闭环，不进入稳定通道。

## 改动摘要

- 新增 Computer Use 权限协调器，统一管理辅助功能和屏幕录制权限状态。
- 权限不足时显示结构化引导，提供对应系统设置入口，不再依赖 LLM 生成授权说明。
- Pudding 重新获得焦点时自动检查权限，并继续之前被阻塞的 Computer Use 操作。
- 将 Helper 权限错误以结构化字段贯通 Swift、Electron bridge、Go controller 和工具结果。
- 仅在系统授权请求后仍未获得权限时打开系统设置，避免授权成功后的多余跳转。
- 设置页和运行时引导共享同一份桌面权限状态。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| Computer Use | 高 | 权限不足时工具调用会等待用户处理，并在授权后恢复执行。 |
| Electron | 中 | 新增权限协调、状态广播、Helper 重启和应用重启 IPC。 |
| 原生 Helper | 中 | 权限错误携带明确的权限类型，截图和窗口发现改为统一失败路径。 |
| Transcript/UI | 中 | 新增按需权限对话框及中英文文案。 |
| SQLite | 无 | schema 和持久化数据结构均未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12，本版没有迁移、数据库备份或数据重写。
- Preview 继续使用 `Pudding.app`、`com.teatak.pudding` 和 `~/.pudding`。
- Computer Use Helper bundle identifier 和完整 designated requirement 必须与 beta.1 保持一致。
- beta.1 用户可直接通过 beta 更新通道升级；稳定通道用户不会收到该版本。

## 已完成验证

- `go test ./...`。
- `PUDDING_RELEASE_CHANNEL=preview npm run test:electron`，172 项通过。
- `make computer-use-helper-test`，54 项 Swift/XCTest 测试通过。
- `web/` 下执行 `npm run build`。
- `git diff --check`。

## 剩余发版门槛

1. 构建并验证 arm64/x64 preview 签名公证包。
2. 验证嵌套 Computer Use Helper 的版本、架构、Team ID 和 designated requirement。
3. 发布 GitHub Prerelease 后，用 beta.1 验证 beta.2 自动更新与权限保留。
4. 重置权限后验证拒绝、部分授权、完整授权和授权后自动继续四种真实流程。

## Release Notes

### Computer Use Permissions

- Add just-in-time macOS permission guidance for Accessibility and Screen & System Audio Recording.
- Resume the interrupted Computer Use operation automatically after the required permissions are granted.
- Keep runtime guidance and desktop settings synchronized through one permission state source.

### Reliability

- Preserve structured permission ownership across the native helper, Electron bridge, daemon, and tool result.
- Restart the helper only when macOS reports permission as granted but the helper still cannot use it.
- Avoid opening System Settings after the native permission request has already succeeded.
