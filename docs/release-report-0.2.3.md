# Pudding 0.2.3 发版报告

日期：2026-08-24

对比基线：`v0.2.2`（`20d64034`）

候选提交：`e3263746`

## 发版结论

**可以发布 0.2.3 正式版。** 本版集中优化桌面界面、项目工作区导航和文件查看体验，
并为 SVG 文件新增源码编辑与预览切换。改动全部位于 Web 前端，不涉及数据库迁移、权限、
签名、自动更新协议或 Electron 主进程。

## 改动摘要

- 项目侧栏改为文件、搜索和版本管理分段切换器。
- SVG 文件支持源码编辑、保存、外部冲突检测及预览刷新。
- 改进图片缩放控件和文件预览工具栏。
- 分离主操作色与信息提示色，统一深浅色主题下的状态表达。
- 统一会话栏、composer、附件、确认弹窗和项目视图的间距、圆角与交互状态。
- 精简移动端配对设置和项目工作区边框层级。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 项目工作区 | 中 | 导航样式、图片缩放及 SVG 编辑/预览交互调整。 |
| 对话界面 | 低 | composer、附件、会话栏和确认弹窗样式统一。 |
| 浏览器界面 | 低 | 加载进度和自动化指示改用信息语义色。 |
| 自动更新 | 无 | 更新协议、通道和安装流程未变化。 |
| 权限与签名 | 无 | Bundle identity、权限归属和签名要求未变化。 |
| SQLite | 无 | schema、迁移和持久化结构未变化。 |

## 数据与兼容性

- SQLite schema 仍为 v12；本版不运行迁移，不创建迁移备份。
- 不重写 canonical data，不重建派生索引，不改变降级行为。
- provider 配置、session routing、本地数据目录和自动更新通道均未变化。
- SVG 编辑复用现有项目文件保存、revision 冲突检测和文件监听路径。
- 正式通道用户可从 `0.2.2` 自动升级到 `0.2.3`。

## 已完成验证

- `make test` 通过。
- `npm run test:electron`：180 项通过。
- `make schema-check` 通过。
- `web/` 生产构建通过。
- 发布数据库只读检查：`PRAGMA user_version = 12`，`PRAGMA quick_check = ok`。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到 Draft Release 后再显式发布。

## Release Notes

### Project Workspace

- Replace compact project activity icons with a clearer Files, Search, and Git switcher.
- Add source editing, save conflict handling, and live preview refresh for SVG files.
- Refine image zoom controls and project file preview actions.

### Interface Polish

- Separate primary action colors from informational status colors across light and dark themes.
- Standardize spacing, borders, corners, and active states across sessions, composer controls, attachments, and dialogs.
- Simplify project surfaces and settings sections for a quieter desktop layout.

### Compatibility

- Keep schema v12, canonical data, permissions, signing identities, and update behavior unchanged.
