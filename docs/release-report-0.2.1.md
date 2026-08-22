# Pudding 0.2.1 发版报告

日期：2026-08-22

对比基线：`v0.2.0`（`34a78530`）

候选版本：`v0.2.1-beta.1`（`f77f58e1`）

## 发版结论

**可以发布 0.2.1 正式版。** `0.2.1-beta.1` 已完成 arm64 与 x64 构建、Developer ID 签名、
Apple 公证、Gatekeeper 检查及九个更新资产验证。正式版沿用相同运行时实现，仅追加项目操作菜单
打开时保持新建按钮可见的界面修正，不新增数据库、权限、签名或自动更新风险。

## 改动摘要

- 新增无线音频输入路由预热，在录音开始前通过短提示音激活目标路由。
- 将录音启动信号验证改为异步恢复，减少等待设备信号时对交互的阻塞。
- 收紧播放流取消条件，避免无关会话停止不属于自己的播放。
- 浏览器自动化截图期间展示正在截取状态，结束后短暂显示降采样截图预览。
- 统一 session workspace 解析，让项目目录和既有临时代码目录使用同一事实源。
- 无项目会话可继续浏览和编辑临时 workspace，并按 session 保留工作区开合状态。
- 调整项目操作、会话操作、文件变更、composer 和侧边栏的布局与交互。
- 项目操作菜单打开时保持新建项目按钮可见。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 音频 | 高 | 录音启动、设备路由预热、信号恢复和播放取消行为变化。 |
| 浏览器自动化 | 中 | 截图生命周期增加可见状态和降采样预览。 |
| Session workspace | 中 | 项目根目录和临时目录改由共享服务解析。 |
| 桌面界面 | 中 | 项目菜单、会话项、文件变更和 composer 交互调整。 |
| 自动更新 | 无 | 更新协议、通道和安装流程未变化。 |
| SQLite | 无 | schema、迁移和持久化结构未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12，指纹仍为
  `8f28c4af75aeafedf3fe75f9d0dd4b064acbc6040330260e74836d57469c5b80`。
- 本版不重写 canonical data，不重建 derived indexes，不创建迁移备份，也不改变降级行为。
- session workspace 只解析既有 project 和本地临时代码目录，不新增数据库事实源。
- 工作区开合状态仍只保存在 localStorage，不写入 canonical messages 或后端 runtime 状态。
- 主应用、Computer Use Helper 的 bundle identity、签名要求和 `~/.pudding` 数据目录均未变化。
- 正式通道用户可从 `0.2.0` 自动升级；preview 用户可从 `0.2.1-beta.1` 升级到正式版。

## 已完成验证

- `0.2.1-beta.1` 的 arm64 与 x64 应用、ZIP、DMG 均通过签名、公证和 Gatekeeper 验证。
- `0.2.1-beta.1` 的九个 GitHub release assets 已完整上传并校验摘要。
- `make test` 通过。
- `npm run test:electron`：180 项通过。
- `make computer-use-helper-test`：55 项通过。
- `make schema-check` 通过。
- `web/` 下执行 `npm run build` 通过。
- 只读检查正式数据库：`PRAGMA user_version=12`、`PRAGMA quick_check=ok`。

## Release Notes

### Audio Reliability

- Prime wireless input routes with a short local cue before capture starts.
- Verify capture startup asynchronously and recover silent device streams without blocking the interaction.
- Restrict playback cancellation so unrelated sessions do not stop another active stream.

### Browser Automation

- Show a visible capture state while browser screenshots are prepared.
- Display a bounded, downsampled screenshot preview after a successful automation capture.

### Session Workspaces

- Resolve project roots and existing temporary code directories through one session-scoped workspace service.
- Let sessions without a project browse and edit their temporary workspace with an explicit temporary label.
- Preserve workspace visibility independently for each session.

### Desktop Experience

- Refine project actions, session menus, file-change presentation, composer controls, and sidebar spacing.
- Keep project creation controls visible while their action menu is open.
- Standardize compact icon actions and interaction states across the session rail.

### Compatibility

- Keep schema v12, canonical data, signing identities, update behavior, and downgrade boundaries unchanged.
