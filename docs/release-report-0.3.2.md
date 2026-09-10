# Pudding 0.3.2 发布报告

日期：2026-09-10。

## 基线与范围

- 公开基线：`v0.3.1`，源码 `95549c42bc62ea9a295ade7ab7fb8685e69aad1e`，2026-09-09 已正式发布；本轮只读确认仍为最新稳定版，双架构九资产齐全。
- 功能范围：基线至 `ff3badaaacab2fb13ba9d9b08a1d077ec26e998e`，包括两个功能/修复提交及上一版发布记录收尾。
- 用户已批准发布 `0.3.2` 正式版。版本来自根 `package.json`，lockfile 两处根版本同步；不使用版本覆盖变量，不移动旧标签。
- 本次仅准备版本与报告，不修改发布脚本，不增加既定九资产之外的附件。公开版本清单继续由官方发布流程提交到 `teatak/pudding`。

## 功能与影响

| 范围 | 影响 | 变化与边界 |
| --- | --- | --- |
| 用户问答 | 中 | 模型等待与面板空闲收起分别计时；交互只延长有效等待；支持超时补答、草稿重开、撤回后主动重答。 |
| 答复投递与消息顺序 | 中 | 答案只进入 canonical user message 或现有队列；按原问题身份幂等投递，不引导无关 turn；撤回项保持 cancelled，不虚假标记 promoted；快速续写保持工具结果、答案、后续输出顺序。 |
| 模型上下文 | 中 | 工具图片保留结果批次顺序和来源；App/Skill 正文从当前注册来源解析，历史仅保存引用，压缩后仍可恢复引用；不可用来源不回退旧正文。 |
| Computer Use | 中 | 鼠标操作默认后台投递；前台投递必须显式选择。保留权限、目标身份、部分结果及取消边界；不自动切换前台或重放不确定操作。 |
| 数据库与更新 | 低 | schema 仍为 v18，无新迁移；签名、公证、更新配置和发布脚本未改动。 |

## 数据与兼容

- `internal/store/schema.sql`、已发布迁移和 schema 指纹与 `v0.3.1` 一致，当前版本为 v18，SHA-256 为 `ebf8a68ae4799a117f114a3c86e3471c64eb5b2fe1096efbadb6771592c004af`。
- 从 0.3.1 升级不批量重写 canonical 数据、不新增索引重建、不创建迁移备份，也不执行仅在成功迁移后触发的备份清理。
- 队列状态和新消息时间排序是正常业务写入变化，不是 schema 迁移。历史 App/Skill 正文在模型请求构建时投影为引用，不回写旧消息。
- 本机正式数据库仅执行只读 `user_version`、`quick_check`、`foreign_key_check`：v18、ok、无外键错误；未执行测试消息、迁移或恢复。
- 同为 v18 的 0.3.1 不会因 schema 版本拒绝打开数据，但不将此描述为新版问答行为可无损降级；自动更新不提供降级。更早版本仍受既有 schema 兼容限制。
- 后台鼠标操作是否产生预期界面效果仍取决于目标 App。投递成功不等同于业务成功；不声称任意 App、多屏或 Intel 真机已验收。

## 已完成源码验证

针对上述功能提交，本轮评估已运行：

- `make test`、`make schema-check` 通过。
- `internal/engine`、`internal/contextbuilder`、`internal/store/sqlitestore` 的 race 测试通过。
- Web 39 项、Electron 216 项、Swift debug/release 各 99 项测试通过；Web TypeScript 与生产构建通过，仅保留既有大 chunk 提示。
- 隔离源码 Electron 问答投递 11 个场景通过，覆盖等待、超时、异步、长等待、失败重试、重启恢复、关闭、本地/SSE 撤回重答及真实数字按键。
- 扩展问答 UI 回归独立复跑完整通过，覆盖四组焦点环、键盘选择、输入、重复记录、布局及审批。首轮 `:focus-visible` 断言失败保留；未改产品代码或降低断言，独立复跑未复现，不声称已定位首轮根因。签名包仍需复查对应键盘行为。
- 密钥扫描 1079 个跟踪文件通过；`git diff --check` 通过；`pudding-notary` 凭据验证有效。
- 测试使用源码 Electron、临时 Vite/userData 和模拟接口，不访问真实 provider，不操作已安装的 Pudding。上述源码测试不替代新安装包验收。

证据：`/tmp/pudding-032-assessment-{go,race,electron,swift,web-test,web-build,delivery,input-ui,input-ui-repeat,secrets}.log`。

## 发布门槛

1. 提交并推送版本和报告，工作树干净且与上游一致。
2. 通过官方 `make desktop-publish` 生成 arm64/x64 九资产，完成签名、公证、归档、权限和更新元数据验证后创建不可变源码标签及公开仓库草稿。
3. 新包复查问答键盘行为、默认后台操作和 Helper 身份；本机升级需用户允许重启后，在正常安装位置验证 0.3.1 → 0.3.2。不得用临时路径自动重启测试冒充完全隔离，或无授权重启正在工作的正式 App。
4. 验收通过后通过官方 finalize 公开 `teatak/pudding` 的 `v0.3.2` 并设为最新稳定版，核对清单、九资产及更新元数据。

本报告准备时尚未打包、签名、公证、上传或执行新版本升级；完成情况在后续补记，不提前记为通过。

## Release Notes

### Questions and Answers

- Wait for requested user input with independent model-wait and panel-inactivity timers; renew active waits during interaction without restarting expired waits.
- Reopen unanswered questions and restore in-memory drafts after the panel closes. Submit late answers to the original question without interrupting an unrelated task.
- Keep answers as canonical user messages, prevent duplicate delivery, and allow withdrawn queued answers to be explicitly replaced and submitted again.
- Improve numeric keyboard input, countdowns, completion states, and chronological ordering between tool results, user answers, and assistant continuations.

### Computer Use and Model Context

- Default pointer actions to background delivery while keeping foreground delivery explicit. Preserve permission checks, exact targets, cancellation, and partial-result handling without automatic foreground fallback.
- Keep tool images beside their result batches and before later actions, with source-tool attribution for model inspection.
- Resolve App and Skill instructions from their current registered sources, retain references across conversation compaction, and stop reusing stale instruction bodies.

### Data and Updates

- Keep database schema v18 unchanged; upgrading from 0.3.1 requires no new database migration or migration-backup cleanup.
- Preserve existing conversations, queued inputs, saved content, and the established signed macOS update flow.
