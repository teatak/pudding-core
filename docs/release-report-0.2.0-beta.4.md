# Pudding 0.2.0-beta.4 发版报告

日期：2026-08-17

对比基线：`v0.2.0-beta.3`

审查范围：`v0.2.0-beta.3..HEAD` 及本次版本提交

## 发版结论

**应作为第四个 0.2.0 preview 发布。** 本版改善 macOS 辅助功能和屏幕录制权限的首次授权流程：
Pudding 通过 Computer Use Helper 请求系统原生授权，让用户直接响应 macOS 提示，而无需先手动定位系统设置。

## 改动摘要

- 辅助功能和屏幕录制改为通过 Computer Use Helper 请求 macOS 原生授权。
- Computer Use 权限引导按钮改为直接发起权限请求。
- 设置页恢复辅助功能和屏幕录制的可请求状态。
- 未授权的相机和麦克风仍按原逻辑打开对应系统设置，权限事实源保持不变。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| macOS 权限 | 中 | 首次辅助功能和屏幕录制授权交互改变。 |
| Electron | 低 | 复用现有 Computer Use Helper 权限请求接口。 |
| 设置页 | 中 | 权限按钮由系统设置入口改为原生授权请求。 |
| SQLite | 无 | schema 和持久化数据结构均未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12，本版没有迁移、数据库备份或数据重写。
- 主应用 bundle identifier 仍为 `com.teatak.pudding`。
- Computer Use Helper 的 bundle identifier、Team ID 和 designated requirement 均未变化。
- beta.3 用户可直接通过 preview 更新通道升级；稳定通道用户不会收到该版本。

## 发布前验证

- `PUDDING_RELEASE_CHANNEL=preview npm run test:electron`。
- `web/` 下执行 `npm run build`。
- `go test ./...`。
- `git diff --check`。

## Release Notes

### macOS Permissions

- Request Accessibility and Screen & System Audio Recording through the native Computer Use permission flow.
- Let users respond to the macOS authorization prompt without navigating to System Settings first.
- Keep Camera and Microphone permission behavior unchanged.

### Reliability

- Preserve the existing Pudding and Computer Use Helper signing identities.
- Keep the permission state source and update compatibility unchanged.
