# Pudding 0.3.4 发布报告

日期：2026-09-14。

## 基线与范围

- 公开基线：`v0.3.3`，源码 `97c9d24b5bf4018c78091a2134c078389cc9bbd5`；评估时是 `teatak/pudding` 的 Latest 稳定版，非 Draft、非预发布。
- 功能范围：`v0.3.3..4e61a3f9f5a9dd997a80b51c944cd520a3ee64b9`，6 个提交、69 个文件、3352 行新增、412 行删除；完整差异证据为 `/tmp/pudding-034-sol-complete-diff.log`。
- 版本定为下一稳定补丁版 `0.3.4`。初始核对时源码与分发仓库均无 `v0.3.4` Release、Draft 或 tag，源码 `main` 与上游一致。
- 发版执行只修改根 `package.json`、`package-lock.json`、本报告和文档索引；不修改功能源码或打包/发布脚本，不使用 `PUDDING_APP_VERSION`。

## 功能与影响

| 范围 | 风险 | 变化与边界 |
| --- | --- | --- |
| 上下文预算与自动压缩 | 中高 | 每次模型请求前按 context window、输出额度和 5% 估算余量校验输入；已知窗口超限时在安全边界压缩或明确失败。运行中仅在完整工具交换落 canonical 后压缩，保留当前输入、最新完整工具交换、并行调用、附件和 provider continuation。 |
| 压缩生成与收尾 | 中 | 超长历史按实际剩余预算分批滚动摘要；只有完整、非空且实际缩短请求的最终摘要才落库。普通回复到达 provider 长度上限时保留已生成内容并正常收尾；压缩摘要被截断则拒绝落库。 |
| canonical 数据与事件 | 中 | 压缩写入在同一 SQLite 事务中校验 snapshot 末消息和 running turn 所有者。运行中摘要属于原 turn，按 `turn_index` 插入并发出带 seq 的 `turn.compacted`；不结束 turn。原始 canonical messages 不删除、不改写，UI 仍由 messages query 与实时 overlay 合成。 |
| 压缩界面 | 中低 | 手动和运行中压缩使用稳定的 24px 折叠行，显示估算节省、超预算边界及具体失败原因。canonical 快照到达前不伪造空 assistant 行，刷新后从持久化摘要恢复。 |
| Provider 模型候选 | 中低 | OpenRouter / BuzzHive / Anthropic / Gemini 模型目录返回结构化候选，包括显示名、上下文、输出上限与图像/音频/工具能力。端点明示字段优先，缺失字段只用完整 ID 精确命中预设补齐；未知值不按品牌猜测。候选只是导入来源，选中保存后 YAML profile 才是事实源。 |
| OpenRouter 归属与预设 | 低 | Chat Completions、Responses、Anthropic 和 Gemini 模型请求都带 Pudding 应用标识，两组分类由进程内原子计数轮换，不持久化、不额外发请求。OpenRouter 预设保留 Free 路由并增加六个常用付费模型，旧配置不自动迁移。 |
| 工具与文档 | 低 | 文件搜索明确 `context_lines` 为 0–5 并指向分片读取恢复路径；新增任务/定时/A2A 设计文档，明确标注为尚未实现。 |

## 数据、迁移与兼容

- `internal/store/schema.sql`、`internal/store/sqlitestore/migrations.go`、已发布指纹及迁移测试相对 `v0.3.3` 未改动。schema SHA-256 为 `ebf8a68ae4799a117f114a3c86e3471c64eb5b2fe1096efbadb6771592c004af`，版本仍为 v18。
- 从 0.3.3 升级不批量改写 canonical 数据、不删除历史、不重建派生索引、不创建迁移备份，也不触发迁移备份清理；本次没有新增迁移或数据改写步骤，正常启动与业务运行仍可写入数据库。实际使用压缩时才追加 summary message 和 lifecycle event。
- `metadata.compact` 新增的压缩前/后估算值与输入预算均为可选 JSON 字段；旧 summary 无该字段仍正常读取。结构化候选不进 SQLite，已保存的 `<home>/config/*.yaml` 不会被预设或目录刷新覆盖。
- 数据库降级门槛不变：0.3.3 仍能打开同为 v18 的数据库。但 0.3.4 新生的 turn 内 summary 和 `turn.compacted` 展示/续传行为不承诺在旧客户端完全一致；自动更新不执行降级。
- 本机正式数据库仅用 `mode=ro` 打开：`PRAGMA user_version=18`、`quick_check=ok`、无外键错误；检查前后主库 SHA-256 均为 `b3692eda8282e2e1326e0a8ab4b90f7a18b28f89654706ad1f50eef9d8b25048`。未迁移、复制、启动或重启正式应用。

## 已完成验证

- 完整审阅 69 个变更文件，追踪 provider request 预算检查、工具交换落库、压缩分区/分批、原子 summary 写入、`EffectiveMessages`、SSE 和 transcript 对账链路。
- `make test` 主机重跑通过：42 个有测试包、11 个无测试文件包。首次沙箱运行仅因 `httptest` 被禁止监听 `::1` 失败，主机端同一测试不放宽断言后通过。
- `internal/engine`、`internal/contextbuilder`、`internal/store/sqlitestore` 的 race 测试通过；`make schema-check` 通过。
- Web 70 项测试通过，0 失败、0 跳过；TypeScript/生产构建通过，仅有既有大 chunk 警告。Electron 218 项通过，0 失败、0 跳过。
- 源码 Electron 压缩 smoke 通过 9 项断言组：运行中 7 次工具交换后从 28,585 降至 8,056 估算输入 token，当前输入、最新结果、Stop、取消、刷新恢复和 24px 行高均保持；手动成功、无收益、空摘要、截断、固定开销超预算与滚动稳定性通过。该测试只使用仓库 Electron、随机端口、本地模拟 provider 和临时 home。
- OpenRouter 公开目录中 7/7 个新预设 ID 存在，上下文、输出上限和 input modalities 与预设相符；官方文档确认 `HTTP-Referer`、`X-OpenRouter-Title` 和 `X-OpenRouter-Categories` 协议及四个所用分类名。四类别在四种 provider 协议的请求头回归通过。
- 密钥扫描 1131 个跟踪文件通过；`pudding-notary` 凭据有效。证据日志：`/tmp/pudding-034-sol-{go-full-host,race,schema,web-test,web-build,electron-test,context-smoke,secrets,notary}.log`。

## 验收边界与发布门槛

- 未执行真实付费 provider 的长会话摘要，确定性回归不代表摘要的实际信息保留质量；多批滚动摘要仍可能累计遗漏。未对 Anthropic / Gemini 官方目录做带付费凭据的在线请求。
- 未启动、替换或重启 `/Applications/Pudding.app`，未启动 `dist` 中 App，未向 `~/.pudding` 写测试数据；不执行本机实际自动升级、新签名包交互或 Intel 真机交互验收，不记为通过。
- 本报告准备时尚未创建 `v0.3.4` 源码 tag，也未构建、签名、公证或上传 0.3.4。后续通过官方 `make desktop-publish` 完成测试、双架构构建、Developer ID 签名、Apple 公证、成品校验、源码 tag、公开仓库版本清单和九资产 Draft；官方 status 通过后停止，不 finalize、不公开、不设置 Latest。公开仓库 tag 仅在未来批准 finalize 后创建，Draft 阶段不创建。

## Draft 结果与独立审查

- 2026-09-14，已完成 [v0.3.4 Draft](https://github.com/teatak/pudding/releases/tag/untagged-49291a732912b5858b01)，Release ID `388298223`，标题 `v0.3.4`，`draft=true`、`prerelease=false`。Latest 仍为 `v0.3.3`；公开仓库尚无 `v0.3.4` tag，未执行 finalize。
- 源码 tag 固定在 `de13085ca4401992d218fbad8dd1c7704c690c65`。公开仓库版本清单提交为 `f4b6970a2340ffcf326999a6ca6b74a12d53c6a7`，记录源码锚点、英文功能清单和九资产 SHA-256/大小。本节为事后文档补记，不移动源码 tag 或修改安装包。
- Sol 完成源码评估、回归与版本报告准备；执行中出现多余一级委派，已关闭该协调层。共享主分支推送和官方发布命令被执行任务的权限审查拒绝，用户在原始主任务明确授权后，由主任务推送并执行官方发布流水线。此次不是子 agent 独立完成 Draft 的成功样本。
- 官方流程完成 Go 全量、Electron 218 项、Web TypeScript/生产构建和 schema 检查；arm64/x64 均完成 Developer ID 签名和 Apple 公证。staged App 与 ZIP/DMG 解包后的签名、公证票据、Gatekeeper、嵌套二进制、权限与更新元数据全部通过。
- 九资产已上传后，最后的 GitHub 状态读取发生 `fetch failed`。官方 `desktop-publish-upload-resume` 完成复核及状态恢复，未重新构建、公证、增加附件或创建重复 Draft。
- 主任务独立核对唯一 Draft、本地与 GitHub asset digest、清单中的九个文件大小及 SHA-256、英文说明和源码锚点，全部一致。更新清单为 0.3.4，双架构 DMG/ZIP 的大小和 SHA-512 均与实际文件一致；清单 SHA-256 为 `8d6648401661d2840b52006364a4f6236f7940494591caa312d6bffd3af076f5`。
- 两个新包的 Computer Use Helper bundle ID、Team ID 和完整 designated requirement 与本机 0.3.3 一致。只读确认 `/Applications/Pudding.app` 仍为 0.3.3，没有替换、启动、重启或做本机升级验收。
- 独立审查复核了原子压缩写入、上下文预算边界和测试证据，未发现新增发布阻断；修正了本报告中对正常数据库写入及 Draft/public tag 时机的两处表述。真实 provider 长会话摘要质量、实际自动升级、新包交互和 Intel 真机测试仍未验收。

执行证据：`/tmp/pudding-034-main-publish.log`、`/tmp/pudding-034-main-upload-resume.log`。独立审查证据：`/tmp/pudding-034-main-draft-review.log`、`/tmp/pudding-034-main-helper-review.log`。Draft 下载尚不对普通用户公开，未将本轮校验描述为公开下载或实际升级通过。

## Release Notes 草案

### Long Conversations

- Compact long conversations safely before oversized model requests and between complete tool exchanges, while retaining the current task, recent inputs, the latest tool results, attachments, and provider continuation state.
- Enforce model-aware input budgets that reserve output space and estimation headroom, and preserve generated replies that end at a provider length limit while rejecting incomplete compact summaries.
- Keep compaction progress, savings, failures, cancellation, and refresh recovery stable inside the transcript without duplicating streamed tool output or creating empty assistant rows.

### Providers and Models

- Import structured model candidates from OpenRouter, BuzzHive, Anthropic, and Gemini, including display names, context windows, output limits, and explicitly reported image, audio, and tool capabilities.
- Prefer endpoint metadata, fill only missing fields from exact model presets, and leave saved provider profiles unchanged until a candidate is explicitly added.
- Refresh the OpenRouter presets with its free router and six commonly used paid models, with model-specific capabilities and limits.

### Integration and Reliability

- Attribute model traffic to Pudding across OpenAI Chat Completions, OpenAI Responses, Anthropic, and Gemini-compatible gateways using the standard OpenRouter application headers.
- Preserve canonical history during compaction with atomic snapshot and active-turn ownership checks, monotonic lifecycle events, and no database migration.
- Clarify the supported file-search context range and guide larger-context reads to precise file slices.
