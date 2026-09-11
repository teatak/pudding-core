# Pudding 0.3.3-beta.1 发布报告

日期：2026-09-11。

## 基线与范围

- 公开基线：`v0.3.2`，源码 `46e3814c750cacde9912ae3f48087642ca0eb8a4`，2026-09-10 已正式发布并设为最新稳定版。
- 本轮范围：`v0.3.2..5e3236af`，包含 3 个功能/修复提交和 1 个 v0.3.2 发布记录补记提交。
- 发布目的：按用户要求走一次完整 preview 发布链路验证（beta 通道），停在 Draft；公开前需要用户单独确认，因此本轮不执行 `desktop-release-finalize`。
- 版本：`0.3.3-beta.1`，tag `v0.3.3-beta.1`。根 `package.json` 与 `package-lock.json` 两处根版本同步；不使用版本覆盖变量。
- 不移动任何旧标签，不新增既定九资产之外的附件。

## 功能与影响

| 范围 | 影响 | 变化与边界 |
| --- | --- | --- |
| 引导（steering）中的流式历史 | 中 | assistant overlay 保留 `previousSegments`，把已经流出的分段连同其输入边界一起展示；`turn.started` / `input.steered` 事件携带 canonical user `parts`；canonical 对账按 `clientMessageID` 而不是首个 assistant messageID，`canonicalReady` 改为以 turn 终态快照为准，避免提前删除 live overlay 或短暂重复 thought/tool 行。 |
| 结构化问答答复气泡 | 低 | 等待 canonical `form_result` 卡片期间，不再把 overlay 里的临时摘要文本渲染成答复气泡；`input-flow-{requestID}` 幂等身份解析收敛到 `inputFlowRequestID` 一处。 |
| 输入队列显示顺序 | 中 | 已引导、离开 REST 权威列表的项按上一帧相邻位置回到队列，而不是当新到达项追加到末尾；从未落库的 overlay 项才追加到末尾。新增 `mergeQueuedInputs` 与对应单元测试。 |
| 事件契约 | 低 | `turn.started` / `input.steered` 新增可选 `parts`（canonical user parts）。旧客户端忽略未知字段，新客户端对字段缺失保持兼容。 |
| 数据库与更新 | 无 | schema 仍为 v18，无新迁移；签名、公证、更新配置和发布脚本未改动。 |

## 数据与兼容

- `internal/store/schema.sql` 与 `v0.3.2` 字节一致，SHA-256 为 `ebf8a68ae4799a117f114a3c86e3471c64eb5b2fe1096efbadb6771592c004af`；当前版本仍为 v18，未新增迁移、未重建派生索引、未创建迁移备份。
- 本轮 `sqlitestore.go` / `memstore.go` 的改动只是把 `parts` 一并写入既有 `events.payload` 完整 Event JSON（`turn.started`、`input.steered`），不改变表结构；canonical message 写入路径未变。
- 历史事件不含 `parts`，读取端按可选字段处理，不迁移、不回写旧事件。
- 本机正式数据库仅执行只读 `user_version`、`quick_check`、`foreign_key_check`：v18、ok、无外键错误；未执行测试消息、迁移或恢复。
- 升级不自动作废数据，自动更新不提供降级。同为 v18 的 0.3.2 可打开同一数据目录，但不将新版引导渲染与队列顺序行为描述为可无损降级。
- preview 与 stable 共用 `Pudding.app`、bundle identifier 与 `~/.pudding`；本版本没有 schema 迁移，预览期间的数据保持对最终 stable 前向兼容。

## 已完成源码验证

在 `5e3236af`（`v0.3.2` 之后、提版本之前）上执行：

- `make test`：Go 全量包通过，含 `internal/api`、`internal/engine`、`internal/store/sqlitestore`、`internal/store/memstore`。
- `npm run test:electron`：216 项通过，0 失败。
- `npm --prefix web test`：45 项通过，0 失败（含新增的队列顺序与引导分段用例）。
- `npm --prefix web run build`：生产构建通过，仅保留既有大 chunk 提示。
- `npm run check:secrets`：1082 个跟踪文件通过。
- `git diff --check`：无输出。
- 公证凭据 `pudding-notary` 有效；`Developer ID Application: Gang Yang (7K47HJ79JA)` 是唯一可自动选中的签名身份。
- 上述测试使用源码与本地构建，不访问真实 provider、不操作已安装的 Pudding，不替代新安装包验收。

证据：`$TMPDIR/pudding-033b1-assessment.log`（host 执行）。

## 发布门槛

1. 提交并推送版本与报告，工作树干净且与上游一致。
2. 通过官方 `make desktop-preview-publish` 生成 arm64/x64 九资产，完成签名、公证、归档、权限和更新元数据验证后，创建不可变源码标签 `v0.3.3-beta.1` 及公开仓库 Draft。
3. 草稿创建后核对九资产、`beta-mac.yml` 双架构引用和公开版本清单。
4. `make desktop-release-finalize` 需要用户明确同意后再执行；本轮停在 Draft。
5. 安装包级验收（0.3.2 → 0.3.3-beta.1 升级、GUI 交互、Intel 真机）本轮不执行，不记为通过。本机已安装 0.3.2；如需验证预览升级，需要用户同意重启安装位置上的 App。

本报告准备时尚未打包、签名、公证或上传。

## 已知问题（beta 通道期间）

- 版本号为预发布形态 `x.y.z-beta.n` 期间，裸 `npm test`（即 `node --test electron/test/*.test.cjs`）会在 `electron/test/update-test-runner.test.cjs` 抛 `stable release version must match x.y.z`；`.github/workflows/ci.yml` 的 `npm test` 步骤同样失败。
- 实测：不设 `PUDDING_RELEASE_CHANNEL` 时该测试文件 `fail 1`，设为 `preview` 时 `pass 3`。
- 原因：`scripts/run-update-test.cjs` 在模块加载时调用 `resolveReleaseChannel(process.env.PUDDING_RELEASE_CHANNEL, version)`，未设环境变量时按 `stable` 校验版本；该测试文件顶层 `require` 了此模块。
- 影响范围：仅 CI 与本地裸跑测试的信号。发布链路为每个步骤注入 `PUDDING_RELEASE_CHANNEL=preview`，因此打包、签名、公证、九资产校验及本轮 Draft 均不受影响。该组合在 `v0.2.1-beta.1` 时期已存在，非本次改动引入。
- 消除条件：版本提升为 `0.3.3`（stable）后自然恢复，无需改动代码。
- 本条属于发版后的文档补记；不移动标签 `v0.3.3-beta.1`，不重新打包。

## Release Notes 草案

### Conversation steering

- Keep already streamed assistant segments visible when you steer a running turn, instead of collapsing them into a single reply.
- Show each steered input on its own boundary, in order, while the live reply keeps streaming.
- Reconcile the live reply with saved history by input identity, so a finishing turn no longer flickers or shows duplicated rows.

### Input queue and questions

- Keep the queue order stable while an answer is being delivered, so an item returns to its previous place instead of jumping to the end.
- Stop showing a temporary summary as an answer bubble while waiting for the structured answer card; the card now appears once, from saved history.

### Data and updates

- Keep database schema v18 unchanged; upgrading from 0.3.2 needs no migration and no migration backup.
- Preserve existing conversations, queued inputs, saved content, and the established signed macOS update flow.
