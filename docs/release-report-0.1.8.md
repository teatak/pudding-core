# Pudding 0.1.8 发版报告

日期：2026-07-17  
对比基线：`v0.1.7`（`2b715adb`）  
审查版本：`fc8052f0` 的界面样式调整与 `0.1.8` 版本提交

## 发版结论

**可以按常规前端更新发布。** 本次统一桌面端灰阶主题、侧边栏与工作区标签的间距和状态，
并简化画布网格的展示协议。SQLite schema 与迁移代码均未改变，不新增数据库迁移。

## 改动摘要

- 将基础背景、侧边栏、工作区和浮层色彩统一为中性灰阶，减少不同灰色体系之间的偏色。
- 调整侧边栏分组、会话缩进、移动端弹层尺寸和阴影，使导航层级和对齐更稳定。
- 移除工作区标签和小组件图标的多余底色与描边，统一选中、悬停和普通状态。
- 统一画布网格卡片的内边距、边框、背景与阴影，网格画布使用独立工作区背景。
- 简化 `canvas_grid` 工具 schema，移除 `variant` 和 `surface` 参数，由产品统一控制网格视觉。
- 微调项目编辑器行号、目录层级线和 Git 不可用状态的对比度。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 全局主题 | 中 | 明暗主题的基础灰阶和工作区颜色变量调整。 |
| 侧边栏 | 低 | 分组、会话缩进、间距和移动端弹层尺寸调整。 |
| 工作区标签 | 低 | 移除图标底色和标签描边，交互逻辑不变。 |
| 画布网格 | 中 | 卡片外观统一，工具输入不再接受 `variant/surface`。 |
| 项目区 | 低 | 编辑器、目录树和 Git 状态样式调整。 |
| SQLite | 无 | schema、迁移和持久化语义均未变化。 |
| 自动更新与打包 | 无 | 双架构签名、公证和资产格式不变。 |

## 数据库分析

- `currentSchemaVersion` 保持为 `3`。
- `internal/store/schema.sql` 与 `internal/store/sqlitestore/migrations.go` 相对 `v0.1.7` 无差异。
- 本次不新增表、字段、索引，不重写现有运行数据。
- 从 `v0.1.7` 升级不执行新迁移；从更早版本升级仍按既有流程迁移到 v3，并在迁移前生成备份。
- 本次版本可直接读取 `v0.1.7` 数据目录；数据库层面不增加新的回退限制。

## 已完成验证

- `go test -tags "sqlite_fts5 webrtcaec" ./...`
- `npm run test:electron`
- `web/` 下执行 `npm run build`
- `git diff --check`
- schema 版本契约检查

## 剩余发版门槛

1. 将版本升级到 `0.1.8`，提交并推送源码。
2. 执行 `make desktop-publish`，完成 arm64、x64 签名、公证、验证和 Draft 上传。
3. 核验 Draft 的英文功能清单、9 个资产和公开版本清单后正式发布。

## Release Notes 草案

### Improvements

- Refine light and dark themes with a consistent neutral grayscale palette across the desktop interface.
- Improve sidebar group alignment, session spacing, mobile navigation sizing, and popover presentation.
- Simplify workspace tabs and widget icons by removing unnecessary backgrounds, outlines, and visual noise.
- Standardize canvas grid spacing, card surfaces, borders, shadows, and workspace backgrounds.
- Simplify the `canvas_grid` tool contract so visual styling remains consistent and product-controlled.
- Improve project editor line numbers, directory hierarchy guides, and unavailable Git states.

### Data Safety

- This update does not add a new database migration or rewrite existing sessions, messages, canvas items, or search data.
