# Pudding 0.2.7 发版报告

日期：2026-08-26

对比基线：`v0.2.6`（`f27f7d9c`）

候选提交：`c6917a18`

## 发版结论

**可以发布 0.2.7 正式版。** 本版细化吉祥物机身轮廓、面部屏幕层次和明暗主题配色。
改动仅涉及前端视觉，不涉及 SQLite schema、系统权限、签名、公证或自动更新链路。

## 改动摘要

- 将机身外轮廓与后层填充分离，使吉祥物边缘更清晰、结构更稳定。
- 调整面部凹槽与玻璃边缘的描边比例，改善屏幕层次感。
- 重新平衡浅色和深色主题中的机身、金属阴影、轮廓与屏幕颜色。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 吉祥物视觉 | 低 | 调整 SVG 图层、描边和主题颜色，不改变交互与布局。 |
| 前端主题 | 低 | 仅修改吉祥物相关 CSS 变量。 |
| SQLite | 无 | schema 仍为 v13，本版没有迁移。 |
| 自动更新 | 无 | 更新检查、下载、安装和通道逻辑未变化。 |
| 权限与签名 | 无 | Bundle identity、权限归属、签名和公证要求未变化。 |

## 数据与兼容性

- SQLite schema 保持 v13，本版不创建数据库备份，也不执行迁移或清理。
- canonical sessions、messages、turns、projects、usage 和 provider 配置均不重写。
- 稳定版继续使用 `~/.pudding`，更新通道和数据目录规则不变。
- 正式数据库只读检查结果为 `PRAGMA user_version = 13`、`PRAGMA quick_check = ok`。

## 已完成验证

- `make test` 通过。
- `PUDDING_RELEASE_CHANNEL=stable npm run test:electron`：180 项通过。
- `make schema-check` 通过，schema v13 release contract 有效。
- `web/` 生产构建通过；仅有既有的大 chunk 提示。
- 正式数据库只读检查：`PRAGMA user_version = 13`，`PRAGMA quick_check = ok`。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到 Draft Release 后再显式发布。

## Release Notes

### Mascot Design

- Add a dedicated body outline layer for a cleaner mascot silhouette.
- Refine the face groove and screen edge for more consistent visual depth.

### Theme Polish

- Rebalance mascot body, metal shading, shell outline, and screen colors in light and dark themes.

### Data and Compatibility

- Keep SQLite schema v13 unchanged with no migration or canonical-data rewrite.
- Preserve the existing permissions, signing, notarization, and automatic-update behavior.
