# Pudding 0.2.6 发版报告

日期：2026-08-26

对比基线：`v0.2.5`（`e06e551d`）

候选提交：`6337192e`

## 发版结论

**可以发布 0.2.6 正式版。** 本版压缩 OpenAI 兼容接口产生的碎片化 reasoning continuation，
修正上下文 token 估算，并统一转录列表的动态高度测量和界面交互样式。改动不涉及 SQLite
schema、系统权限、签名、公证或自动更新链路。

## 改动摘要

- 合并流式 reasoning details 的连续文本和摘要片段，避免长推理产生过大的 continuation payload。
- 回放已有 OpenAI continuation 时执行同样的规范化，保持流式写入与后续请求行为一致。
- 上下文 token 估算只计算 canonical message parts，不再受 provider-native continuation envelope 体积影响。
- 为虚拟化转录项提供统一测量入口，折叠、展开和动态内容变化后及时校正列表高度。
- 统一侧边栏、popover、菜单、composer、设置页和工作区控件的 hover、active 与 focus 样式。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| OpenAI 兼容接口 | 中 | continuation 写入与回放会合并 reasoning 文本片段，但保留结构化字段。 |
| 上下文估算 | 低 | 改为只估算 canonical message 内容，避免 provider envelope 导致虚高。 |
| 对话列表 | 低 | 动态内容变化后统一触发虚拟列表重新测量。 |
| 界面交互 | 低 | 统一 hover、active、focus 和 selected 视觉状态。 |
| SQLite | 无 | schema 仍为 v13，本版没有迁移。 |
| 自动更新 | 无 | 更新检查、下载、安装和通道逻辑未变化。 |
| 权限与签名 | 无 | Bundle identity、权限归属、签名和公证要求未变化。 |

## 数据与兼容性

- SQLite schema 保持 v13，本版不创建数据库备份，也不执行迁移或清理。
- canonical sessions、messages、turns、projects、usage 和 provider 配置均不重写。
- reasoning continuation 仅在新写入或后续回放时进行等价压缩，不迁移既有数据库记录。
- 稳定版继续使用 `~/.pudding`，更新通道和数据目录规则不变。
- 正式数据库只读检查结果为 `PRAGMA user_version = 13`、`PRAGMA quick_check = ok`。

## 已完成验证

- `make test` 通过，包含超长 reasoning 流压缩与上下文估算测试。
- `PUDDING_RELEASE_CHANNEL=stable npm run test:electron`：180 项通过。
- `make schema-check` 通过，schema v13 release contract 有效。
- `web/` 生产构建通过；仅有既有的大 chunk 提示。
- 正式数据库只读检查：`PRAGMA user_version = 13`，`PRAGMA quick_check = ok`。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到 Draft Release 后再显式发布。

## Release Notes

### Provider Reliability

- Compact fragmented OpenAI-compatible reasoning details before storing or replaying continuation data.
- Keep long reasoning streams bounded while preserving encrypted and structured continuation fields.

### Usage and Context

- Estimate context usage from canonical message parts instead of provider-native continuation envelopes.
- Prevent fragmented or opaque continuation payloads from inflating token estimates.

### Conversation Experience

- Recalculate virtualized transcript item heights after disclosures and dynamic content changes.
- Improve scroll stability when expanding or collapsing transcript details.

### Interface Polish

- Standardize hover, active, focus, and selected states across sidebars, popovers, menus, settings, and the composer.
- Refine workspace controls and responsive layout behavior.

### Data and Compatibility

- Keep SQLite schema v13 unchanged with no migration or canonical-data rewrite.
- Preserve the existing permissions, signing, notarization, and automatic-update behavior.
