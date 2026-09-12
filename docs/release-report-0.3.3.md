# Pudding 0.3.3 发布报告

日期：2026-09-12。

## 基线与范围

- 公开基线：`v0.3.2`，源码 `46e3814c750cacde9912ae3f48087642ca0eb8a4`，仍为最新稳定版。
- 功能范围：`v0.3.2..e296fc490d446341a913fa5d39356145f419cbce`，104 个文件、4782 行新增、909 行删除，含历史发布记录和测试文档。
- `v0.3.3-beta.1` 仅存在旧 Draft，其源码不包含后续改动。本次创建独立的 `v0.3.3` 正式版，不复用旧包、不移动旧标签、不公开旧草稿。
- 用户已明确批准发布 `0.3.3` 正式版。根 package 与 lockfile 两处根版本同步；不使用版本覆盖变量，不改打包脚本，不新增既定九资产之外的附件。
- 公开版本清单仍由官方流程提交到 `teatak/pudding/releases/v0.3.3.json`，Release 标题为 `v0.3.3`，功能清单为英文。

## 功能与影响

| 范围 | 风险 | 变化与边界 |
| --- | --- | --- |
| 流式渲染 | 中低 | Shiki 在独立 Worker 执行，取消过期排队任务；折叠详情不挂载；连续文本 delta 按 50ms 合并，工具、引导、终态等边界立即刷新。canonical 历史仍是唯一事实源。 |
| 问答、队列与压缩 | 中低 | 多次引导保留已流出的片段和附件边界；队列保持相邻顺序；模型等待改为固定截止时间，交互不再续期，仍支持超时补答；压缩按 clientMessageID 对账并先取得 canonical turn 再清除 pending 行。 |
| 工具结果与模型上下文 | 中 | 统一当前 turn 与历史请求的结果投影，去重文件结果，对大结果提供带引用的受限预览；复用历史读取工具按字符、行、数组记录精准回读。仅在回读工具可用且结果有标识时作有损预览，原始 canonical 结果不改写。 |
| Code 工具 | 中低 | 补丁失配返回有界上下文和精确候选位置，不自动移动补丁；沙箱允许 trustd 证书验证，不开放私钥读取；自动审批模式减少结构化 Git 写入提示，保留仓库范围、准备索引和漂移检查，Ask 仍审批写入及执行。 |
| 模型配置 | 低至中 | 新增显式选择的 DeepSeek Responses，修复 Responses 并行工具流索引；更新模型模板与能力/限额元数据，新增模型默认使用供应商温度。已保存 YAML 配置不自动迁移或覆盖。 |
| 桌面界面 | 低 | 调整最小窗口与面板宽度、窄窗口抽屉、项目 Git 列表滚动、步骤和浮层并存、模型设置布局、听写/原始音频图标；修复应用图标折叠、计数和点击层级。失焦时暂停装饰动画。 |
| 数据库与发布 | 低 | schema 仍为 v18，无新迁移；产品自动更新、签名、公证及发布流程未变。更新测试脚本仅延迟 CLI 配置初始化，修复 beta 版本被测试导入时错误校验 stable 的问题。 |

## 数据与兼容

- `internal/store/schema.sql`、已发布迁移与指纹相对 `v0.3.2` 未改动。schema SHA-256：`ebf8a68ae4799a117f114a3c86e3471c64eb5b2fe1096efbadb6771592c004af`，版本 v18。
- 从 0.3.2 升级不批量重写 canonical 数据、不重建派生索引、不创建迁移备份，也不触发仅在成功迁移后执行的备份清理。
- 存储调用变化限于新事件保存可选 canonical user parts、压缩 turn 的 clientMessageID 及克隆时的压缩身份处理；历史事件不回填。工具结果裁剪只发生在模型请求投影中。
- 本机正式数据库只读检查为 v18、quick_check=ok、无外键错误。普通只读连接报 SQLite 14 后，确认无打开进程及 WAL/SHM，使用 immutable 只读连接完成检查；检查前后原库 SHA-256 一致。未重启应用、写入测试消息、迁移或恢复数据库。
- 同为 v18 的旧版本不会因 schema 版本拒绝打开，但不承诺新版问答、事件与模型行为的无损降级；自动更新不执行降级。更早 schema 的版本继续受既有迁移限制。
- preview 与 stable 仍共用应用标识和正式数据目录；本次不触碰本机 `/Applications/Pudding.app`，只读确认已安装版本为 0.3.2。

## 已完成验证

核心源码在 `d93fd360` 上评估，最终 `e296fc49` 只新增应用图标布局修复及其 smoke；该增量已补 Web 与对应桌面检查。

- `make test`、`make schema-check` 通过。Go/Electron 首轮因沙箱禁止监听 loopback 失败，允许本地监听后完整重跑通过，没有放宽断言。
- `internal/engine`、`internal/contextbuilder`、`internal/store/sqlitestore` race 测试通过。
- Electron 218 项通过；Web 61 项通过。最终界面提交后 Web 61 项再次通过，TypeScript 与生产构建通过，仅有既有大 chunk 警告。
- `compact-delivery-smoke.cjs`：HTTP/SSE 两种顺序、拒绝和快照失败 4 个场景通过。
- `input-flow-delivery-smoke.cjs`：问答等待、固定截止时间、超时补答、数字输入、撤回重答及无重复投递等 11 个场景通过。
- `transcript-stream-smoke.cjs`：长思考、Markdown、500 行代码、持续增长代码及大量工具结果共 6 个场景通过；覆盖输入不丢失、Worker 生产产物、代码 DOM 保留及折叠状态。该夹具性能数据不等于整应用性能保证。
- `transcript-guides-smoke.cjs`：引导附件预览、多次引导顺序、结束对账及 canonical 重载通过。
- `session-apps-smoke.cjs`：400/800px，0/1/2/3/6/7 个应用、图标与数量叠放以及原生点击溢出列表通过；退出时出现 macOS task_policy_set 警告，全部断言和进程退出码通过。
- Worker TypeScript 检查通过。GitHub Dependabot #14 状态为 fixed，修复时间 2026-09-12 13:53:58 UTC；当前开放告警列表为空，不将此描述为不存在未知漏洞。
- 密钥扫描 1110 个跟踪文件通过；`git diff --check` 通过；`pudding-notary` 凭据有效。
- 所有桌面回归使用仓库 Electron 可执行文件、临时 Vite/userData 和模拟 API；没有使用生产会话或真实 provider。

证据位于 `/tmp/pudding-033-assess-*.log`，包括 go-host、electron-host、race、schema、web-final、build-final、compact、input、stream、guides、session-apps、worker、secrets、notary。

## 验收边界与发布门槛

- 当前未发现必须先发 beta 的阻断问题，用户知悉评估后批准正式版。主要剩余风险是模型如何使用受限预览和精准回读；DeepSeek Responses 有离线协议及并行工具回归，未执行真实付费 API 任务。网络沙箱的显式联网下载测试本轮未开启。
- 本轮不执行本机 0.3.2 到 0.3.3 的实际自动更新、不启动 dist 中的 App、不做新签名包或 Intel 真机交互验收；源码 smoke、签名和更新清单检查不替代这些项目。
- 发布前必须提交并推送版本及报告，工作树干净且与上游一致；通过官方 `make desktop-publish` 的测试、双架构构建、签名、公证、归档解包及更新元数据校验。
- 发布时核对唯一 v0.3.3 草稿、英文功能清单、源码/公开标签锚点、公开版本清单与九资产大小及 SHA-256，再使用官方 finalize 公开为 Latest；随后核对公开下载和更新清单。
- 本报告准备时尚未构建、签名、公证或上传 0.3.3；实际结果在后续补记。

## Release Notes

### Conversation and Performance

- Keep long streaming replies responsive with background syntax highlighting, batched text updates, and on-demand rendering of collapsed tool details.
- Preserve streamed reply segments and attachments across multiple steering inputs, and keep queued inputs in a stable order.
- Use a fixed deadline for requested user input while retaining late answers, draft reopening, and duplicate-safe delivery.
- Keep the compacting row in place as the saved summary arrives, avoiding jumps during conversation compaction.

### Tools and Model Context

- Reduce oversized tool results in model requests while preserving the original results in conversation history. Read back specific fields, matching lines, or complete array records when needed.
- Provide precise context and candidate locations when a patch does not match, without automatically relocating or applying it.
- Improve certificate validation for sandboxed commands and reduce routine Git approval prompts while retaining repository, index, and drift checks.

### Models and Workspace

- Add an optional DeepSeek Responses connection and fix pairing of parallel Responses tool calls and results.
- Refresh provider model presets, capability metadata, and output limits. Let new model configurations use provider-default temperature without overwriting saved profiles.
- Improve narrow-window layouts, project change-list scrolling, model settings, and app-icon overflow counts and menus.
- Distinguish dictation from raw audio input and pause decorative animation while the window is unfocused.

### Data and Updates

- Keep database schema v18 unchanged; upgrading from 0.3.2 requires no new migration or migration-backup cleanup.
- Preserve the established signed macOS update flow and fix preview-version validation in the update test runner.
