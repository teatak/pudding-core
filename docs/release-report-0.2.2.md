# Pudding 0.2.2 发版报告

日期：2026-08-24

对比基线：`v0.2.1`（`cff896fd`）

候选提交：`ff57871f`

## 发版结论

**可以发布 0.2.2 正式版。** 本版以桌面界面和项目工作区交互优化为主，项目侧栏改为文件、
搜索和 Git 标签导航，并统一会话栏、对话、文件预览、附件和工具栏操作样式。唯一后端变化是
项目树过滤 `.DS_Store`；不涉及数据库迁移、权限、签名或自动更新链路。

## 改动摘要

- 项目侧栏改为文件、搜索和 Git 标签导航，并显示 Git 变更数量。
- 统一项目树布局、文件类型图标、文件查看器和文档预览行为。
- 改进会话切换时工作区开合状态，避免残留的关闭动画影响新会话。
- 调整浏览器查找栏层级、浏览器工具栏和工作区菜单交互。
- 新增共享 `ShellActionButton`，统一桌面工具栏和菜单按钮状态。
- 优化会话栏、composer、transcript、附件缩略图和文件变更视图的间距与状态。
- 项目文件树忽略 macOS `.DS_Store` 文件。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 项目工作区 | 中 | 侧栏导航、文件树、搜索、Git 和文件预览交互调整。 |
| Session workspace | 低 | 修正跨会话切换时工作区动画状态。 |
| 浏览器界面 | 低 | 查找栏层级、工具栏和菜单交互调整。 |
| 对话界面 | 低 | 会话栏、composer、transcript 和附件展示调整。 |
| 自动更新 | 无 | 更新协议、通道和安装流程未变化。 |
| SQLite | 无 | schema、迁移和持久化结构未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12，不创建迁移备份，也不改变降级行为。
- canonical messages、session routing、provider 配置和本地数据目录均未变化。
- 项目侧栏状态属于本地 UI 状态，不写入 SQLite 或后端 runtime。
- 主应用及 Computer Use Helper 的 bundle identity、权限归属和签名要求均未变化。
- 正式通道用户可从 `0.2.1` 自动升级到 `0.2.2`。

## 已完成验证

- `make test` 通过。
- `npm run test:electron`：180 项通过。
- `make schema-check` 通过。
- `web/` 下执行 `npm run build` 通过。
- `git diff --check` 通过。

## Release Notes

### Project Workspace

- Replace the split project sidebar with focused Files, Search, and Git navigation.
- Show Git change counts directly in the project navigation.
- Standardize project tree spacing, file type icons, document previews, and file viewer behavior.
- Hide macOS `.DS_Store` metadata from project trees.

### Session Experience

- Keep workspace visibility transitions scoped to the active session.
- Refine session rail controls, active states, menus, and project approval controls.
- Improve transcript spacing, assistant metadata transitions, attachments, and file-change presentation.

### Browser and Desktop UI

- Keep the browser find bar correctly layered with the persistent browser surface.
- Refine browser toolbar, workspace menus, composer controls, and shared shell actions.
- Standardize compact desktop action buttons and hover states across the application.

### Compatibility

- Keep schema v12, canonical data, permissions, signing identities, and update behavior unchanged.
