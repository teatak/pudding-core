# Pudding 0.2.4 发版报告

日期：2026-08-25

对比基线：`v0.2.3`（`ba856735`）

候选提交：`a5beb10f`

## 发版结论

**可以发布 0.2.4 正式版。** 本版重新收紧命令执行边界，将 host 执行改为显式请求，
把输入 token 校准从跨会话共享改为 session 最近一次同模型请求，并完善对话交互与吉祥物视觉。
SQLite schema 从 v12 升级到 v13；迁移已通过单元测试和正式数据库副本演练。

## 改动摘要

- 命令默认始终留在项目 sandbox；host 执行必须显式声明并提供原因后审批。
- 对越界目录和 host 服务需求返回结构化结果，引导申请项目目录或重新发起精确授权。
- sandbox 缓存按项目或 session 保持稳定，并补全常见系统只读路径与 Python 用户安装路径。
- 输入 token 校准改为 session 级最近一次同 provider/model 请求，避免不同会话互相影响。
- 统一 transcript key、工具交互区域和消息元信息 hover 行为，调整 mention 菜单定位。
- 在 composer 集成交互式吉祥物，并重做金属层次、屏幕反光和面部细节。
- 更新提示直接显示目标版本号并优化窄宽度布局。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 命令执行 | 中 | sandbox/host 边界与审批语义调整，host 命令需要显式原因。 |
| Token 用量 | 中 | 校准事实源从全局 provider/model 表改为 session 最近一次请求。 |
| SQLite | 中 | schema v12 升级到 v13，新增 3 个 session 用量字段并删除旧校准表。 |
| 对话界面 | 低 | transcript 元信息、文件改动、mention 菜单和审批栏交互调整。 |
| 吉祥物 | 低 | composer 集成与 SVG/CSS 视觉重构。 |
| 自动更新 | 低 | 仅更新可用提示文案和布局，下载、安装及通道逻辑未变化。 |
| 权限与签名 | 无 | Bundle identity、权限归属、签名和公证要求未变化。 |

## 数据与兼容性

- SQLite schema 从 v12 升级到 v13，迁移在单一事务中为 `session_usage` 增加
  `last_provider`、`last_model` 和 `last_estimated_input_tokens`。
- 迁移删除旧的 `usage_calibrations` 表；该表只保存派生校准状态，不属于 canonical data。
- sessions、messages、turns、projects 和已有 session usage 累计值不重写、不删除。
- 不重建派生索引；升级前按既有机制创建 v12 数据库备份并执行备份保留清理。
- 升级后的 v13 数据库不能由 0.2.3 直接打开；自动降级不受支持，需要降级时必须恢复迁移前备份。
- 正式数据库副本演练后，sessions `8`、messages `387`、turns `36`、projects `2`、
  session usage `8`，与迁移前完全一致。
- provider 配置、session routing、本地数据目录和稳定/预览更新通道均未变化。

## 已完成验证

- `make test` 通过。
- `npm run test:electron`：180 项通过。
- `make schema-check` 通过，schema v13 release contract 有效。
- `web/` 生产构建通过；仅有既有的大 chunk 提示。
- 正式数据库只读检查：`PRAGMA user_version = 12`，`PRAGMA quick_check = ok`。
- 正式数据库副本升级：`PRAGMA user_version = 13`、`quick_check = ok`、
  `foreign_key_check` 无错误，新字段齐全且旧校准表已删除。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到 Draft Release 后再显式发布。

## Release Notes

### Command Execution

- Keep commands inside the project sandbox by default and require an explicit reason before host execution.
- Return structured guidance when a command needs another project directory or host service.
- Preserve command caches per project or session and improve Python user-tool discovery.

### Usage and Context

- Calibrate input-token estimates from the latest matching provider request in each session.
- Prevent calibration data from one conversation from influencing another conversation.
- Exclude native continuation payloads from context estimates.

### Conversation Experience

- Refine transcript metadata visibility, tool interaction grouping, file-change rows, and mention menu positioning.
- Add the interactive mascot to the composer and redesign its metal depth, reflections, and facial details.
- Show the target version directly when a new update is available.

### Data and Compatibility

- Migrate SQLite schema v12 to v13 while preserving canonical conversations, projects, turns, and usage totals.
- Back up the database before migration and remove the obsolete shared calibration table.
