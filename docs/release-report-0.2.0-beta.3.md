# Pudding 0.2.0-beta.3 发版报告

日期：2026-08-17

对比基线：`v0.2.0-beta.2`

审查范围：`v0.2.0-beta.2..HEAD` 及本次版本提交

## 发版结论

**应作为第三个 0.2.0 preview 发布。** 本版修正 macOS 权限设置页重复展示屏幕录制权限的问题。
运行时检查确认 Pudding daemon 与 Computer Use Helper 的 responsible process 均为主应用，因此辅助功能和
屏幕录制最终归属 `com.teatak.pudding`。设置页现在只保留一条屏幕录制权限事实源。

## 改动摘要

- 合并 Computer Use 窗口截图与桌面截图重复展示的屏幕录制权限。
- 删除 `desktopScreenRecording` 独立状态、请求分支和前端类型。
- 统一屏幕录制说明，使其同时覆盖指定 App 窗口和桌面画面。
- 更新简体中文、繁体中文和英文权限文案。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| macOS 权限 | 中 | 屏幕录制权限只保留一条状态和系统设置入口。 |
| Electron | 低 | 删除重复权限状态及请求分支。 |
| 设置页 | 中 | 移除“桌面截图”重复权限项。 |
| SQLite | 无 | schema 和持久化数据结构均未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12，本版没有迁移、数据库备份或数据重写。
- 主应用 bundle identifier 仍为 `com.teatak.pudding`。
- Computer Use Helper 的 bundle identifier 与 designated requirement 均未变化。
- beta.2 用户可直接通过 preview 更新通道升级；稳定通道用户不会收到该版本。

## 已完成验证

- `PUDDING_RELEASE_CHANNEL=preview npm run test:electron`，172 项通过。
- `web/` 下执行 `npm run build`。
- `git diff --check`。

## 剩余发版门槛

1. 构建并验证 arm64/x64 preview 签名公证包。
2. 验证嵌套 Computer Use Helper 的版本、架构、Team ID 和 designated requirement。
3. 发布 GitHub Prerelease 后确认稳定通道仍指向最新正式版。

## Release Notes

### macOS Permissions

- Show Screen & System Audio Recording as one permission across Computer Use and desktop screenshots.
- Remove the duplicate desktop screenshot permission state and settings row.
- Clarify that screen access is used only for explicitly requested app-window or desktop captures.

### Reliability

- Use the main Pudding application as the single macOS permission owner.
- Keep the Computer Use Helper identity and update compatibility unchanged.
