# Pudding 0.2.0-beta.5 发版报告

日期：2026-08-17

对比基线：`v0.2.0-beta.4`

审查范围：`v0.2.0-beta.4..HEAD` 及本次版本提交

## 发版结论

**应作为第五个 0.2.0 preview 发布。** 本版完善 macOS 桌面权限引导：首次请求仍使用系统原生授权流程，重复请求屏幕录制权限时直接打开对应系统设置，并在刷新权限前重启 Computer Use Helper，避免继续读取旧进程中的权限状态。

## 改动摘要

- 首次请求屏幕录制权限时继续使用 macOS 原生权限请求。
- 再次请求仍未授予的屏幕录制权限时，直接打开系统设置的屏幕录制页面。
- 用户从系统设置返回并刷新权限时，先重启 Computer Use Helper，再读取最新权限状态。
- 增加权限请求、重复请求和 Helper 重启顺序的 Electron 测试。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| macOS 权限 | 中 | 屏幕录制重复请求与授权后刷新流程改变。 |
| Computer Use | 中 | 刷新权限前会停止 Helper，后续调用按现有机制重新启动。 |
| Electron | 低 | 权限控制器增加系统设置入口和显式刷新行为。 |
| SQLite | 无 | schema 和持久化数据结构均未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12，本版没有迁移、数据库备份或数据重写。
- 主应用 bundle identifier 仍为 `com.teatak.pudding`。
- Computer Use Helper 的 bundle identifier、Team ID 和 designated requirement 均未变化。
- beta.4 用户可直接通过 preview 更新通道升级；稳定通道用户不会收到该版本。

## 发布前验证

- `PUDDING_RELEASE_CHANNEL=preview npm run test:electron`。
- `web/` 下执行 `npm run build`。
- `go test ./...`。
- `git diff --check`。

## Release Notes

### macOS Permissions

- Open the Screen & System Audio Recording settings page when a repeated native permission request still needs user action.
- Restart Computer Use before refreshing permission state after users return from System Settings.
- Preserve the native first-request flow for Screen & System Audio Recording.

### Reliability

- Add coverage for native permission requests, repeated requests, and Computer Use restart ordering.
- Keep the database schema, signing identities, and update compatibility unchanged.
