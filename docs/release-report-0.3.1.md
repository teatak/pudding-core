# Pudding 0.3.1 发布候选报告

日期：2026-09-09。

## 基线与候选范围

- 公开发布基线：`v0.2.11`，源码 `9e9efd87a653e50872faa96c144215bb8a7a4e31`。
- 2026-09-09 GitHub 只读核对：0.3.0 是未发布草稿；其源码标签固定在 `afbb3a7c2fc6b1f4cd62a8e4acfbf724cb5014d7`，不包含当前主线。
- 用户已确认当前候选使用 **0.3.1**。`package.json`、lockfile 及 lockfile 根包同步；不移动 `v0.3.0`，不复用旧草稿资产。
- 候选包括基线至 `2f035169` 的完整主线，以及归档导航修复、画中画回归补强和本报告，由 `95549c42bc62ea9a295ade7ab7fb8685e69aad1e` 冻结。准备阶段只授权本地提交；后续用户明确授权 Apple 公证、源码标签、GitHub `teatak/pudding` 上传及验收后的公开发布，执行状态见下方补记。
- [0.3.0 报告](release-report-0.3.0.md)和 `dist/verified-0.3.0-20260909/` 仅作为历史证据，不能证明本候选已签名或完成安装包验收。

## 当前结论

源码收尾、双架构签名公证、签名版功能和正常安装位置的升级验收通过，GitHub 0.3.1 草稿九资产已齐。公开发布命令在执行前被审批拒绝，要求用户重新明确确认目标仓库与公开范围，尚未公开。`/Applications/Pudding.app` 已从 0.3.0 正常升级为 0.3.1，当前用户可写、签名/公证/Helper 身份及数据延续通过。临时路径升级的 root 所有权失败保留为历史测试异常，不将其改写为通过，也不声称已定位该临时路径问题的根因。没有移动 `v0.3.0` 或 `v0.3.1` 标签。

画中画历史 `computer_window_raise_failed` 不能凭 App 已激活就判定成功：原测试没有等待原生置前请求完成。补强测试逐次等待最终结果，记录目标和失败，检查额外请求；不增加产品重试、不降低窗口可见性校验。诊断轮一次点击、严格回归四次点击均成功，未复现历史错误，因而不声称已定位或修复历史产品故障，也不把“激活即成功”的旧断言继续作为依据。历史异常保留为签名包复验项。

## 功能与影响

| 范围 | 影响 | 当前候选变化 |
| --- | --- | --- |
| 工作区、项目和资源库 | 中 | 项目/网页/画布共用标签，专注模式保留对话；收藏、历史、来源和搜索展示优化；文件定位、Markdown 链接和编辑连续性改善。 |
| 对话和排队输入 | 中 | 支持队列调整顺序与编辑；附件引导按消息位置呈现图片；改善折叠滚动及授权展示。 |
| 会话归档 | 中 | 标题栏和侧栏共用一条 mutation；当前会话归档后进入对应项目/全局草稿；列表先刷新、用户已切走、分屏及失败场景不错误跳转。 |
| Computer Use | 高 | 显式后台路径扩展五种鼠标手势并移除兼容 App 白名单；保持受保护 App、权限、实例/窗口身份和取消边界。 |
| 画中画 | 中 | 无标题、无鼠标采集；失效目标不创建卡片；多 App 层叠及各自 30 秒到期；有产物在产物下方，无产物在对话右上角；展开工作区不停止采集或重建图片节点。 |
| 数据库 | 高 | 公开版 v13 经 v17 升至 v18；新增队列顺序，迁移关闭画布，建立收藏关系。 |
| 发布和更新 | 中 | 新版本走既有双架构签名、公证、九资产和更新验证流程，不能沿用旧包的验收结论。 |

## 数据迁移与兼容

- v13 schema SHA-256：`19ae784ea197314f139a5836cd172a6d3c2ca2e3c0f717d69eccf595d25fb99f`。
- v18 schema SHA-256：`ebf8a68ae4799a117f114a3c86e3471c64eb5b2fe1096efbadb6771592c004af`。
- v13 → v17：关闭画布转入 `canvas_items` 且 `visible=0`，保留正文；已有保存版本建立收藏关系。原 `canvas_closed_items` 删除。
- v17 → v18：为 `queued_inputs` 增加 `sort_order`，按每个会话的 `created_at,rowid` 回填，并重建队列索引。正文、附件和队列状态保留。
- **迁移按版本分别提交事务**，不是 v13 → v18 整段单事务：v17 步骤失败回滚至 v13；v17 已成功后若 v18 失败，保留 v17，重启继续迁移。已有 v18 不重复迁移；v14–v16 未发布中间版本仍明确拒绝。
- 迁移前创建备份，只有迁移成功后才按既有规则清理旧备份。v13 永久夹具覆盖正文、项目/会话、turns、应用授权、浏览历史/标签、画布正文与 revision、备份、完整性和重复启动；v17 队列夹具覆盖失败回滚、原顺序及状态保留、重复启动。
- 不重写 canonical messages 或 turns。迁移改变画布存储位置、收藏关系和队列顺序；只重建队列索引，不新增消息全文索引重建步骤。沿用启动时既有搜索索引检查，不把它计为新的迁移。
- `library_recent_opens` 是 v17 已登记的持久结构，当前接口和 UI 已删除独立最近打开记录路径；不修改已登记的 v17 迁移/指纹，也不宣称仍在记录文件最近打开。
- 降级到 0.2.11 或旧 v17 候选包不能直接使用 v18 数据库，需要恢复相应迁移前备份；恢复会舍弃备份之后的新数据。不要在真实数据上做降级演练。
- 当日正式库只读检查：v18、`quick_check=ok`，关键表/队列列符合当前结构；读取前后 SHA-256 一致。迁移测试只使用临时数据，不在 `~/.pudding` 执行迁移。

## 已完成验证

2026-09-09 发布评估已运行（本轮版本及文档准备不改这些原生/Go 实现）：

- `make test`、`make schema-check` 通过；首次沙箱监听失败，在允许临时 loopback 端口后重跑通过。
- Electron 216 项、Web 36 项、Swift debug/release 各 101 项通过。
- Web TypeScript/生产构建通过，仅有既有大 chunk 提示；跟踪文件密钥扫描通过。
- 归档 8 项隔离源码桌面回归再次通过：`/private/tmp/pudding-release-archive-8VoLgs/result.json`；覆盖两个入口、项目/全局草稿、列表刷新先于响应、用户切走、分屏和失败恢复。保留原共享 mutation 修复，不新增第二条归档路径。
- 新后台实现隔离 AppKit/Electron 五手势、异常恢复和窗口生命周期记录见[后台输入验收](computer-use-background-input-probe.md)。它们不是任意 App 或签名安装包兼容性承诺。
- 画中画诊断轮 15 项通过：`/private/tmp/pudding-release-preview-6DqZcw/result.json`，唯一显式预览点击返回成功。补强轮 16 项通过：`/private/tmp/pudding-release-preview-strict-BACtYF/result.json`；同 bundle 两个进程及第二个 App 的四次点击均等待最终成功，零额外请求、零置前错误、零 renderer errors。通过后又显式固定“四次覆盖”计数断言，保留结果中的四条完整请求作为依据。
- Electron 216 项、Web 36 项及 Web 生产构建在版本更新后再次通过；版本一致性与 Release Notes 提取通过。
- 桌面测试使用源码 Electron 43.2.0、当前 Vite、临时 daemon/home；原生 Helper 为已测试源码 debug 构建（UUID 与 debug 产物一致），不是 Developer ID 签名安装包。测试结束释放自己的进程，不重启现有开发 daemon。

## 公开发布前的门槛

1. 本地候选经授权推送后，确认工作树干净且与上游一致。再使用官方流水线建立不可变 `v0.3.1`，不得移动旧标签。
2. 经用户授权后运行 `make desktop-bundle`：生成当前源码的 arm64/x64 九资产，验证 Developer ID、Apple 公证、Gatekeeper、归档内容和更新元数据。
3. 在最新签名候选包完成五手势、画中画、权限申请后恢复，以及已有授权延续/Helper 身份检查。旧包授权不必无条件再次重置；画中画需等待完整置前结果，历史异常如再出现必须保留诊断、定性后再公开发布。
4. 隔离数据验证 0.2.11 → 0.3.1 的更新与数据库迁移；已有本地 0.3.0 安装也需检查到 0.3.1 的版本增长。安装路径/TCC 与数据迁移验证分别记录。

已知适用边界：飞书历史自行激活问题、镜像输入兼容、多屏、持续真人并行和 Intel 真机覆盖尚未全部验收；后台路径显式选择，不自动替换前台路径。目标 App 自行激活时停止后续输入且不反向抢焦点，不保证任意 App 均可后台操作。

## 2026-09-09 公证与安装包验收补记

- 用户明确批准后执行 `make desktop-publish`。`make test`、Electron 216 项、Web 生产构建、双架构打包通过；arm64/x64 App、ZIP、DMG 均通过 Developer ID、公证票据和 Gatekeeper 验证。源码标签 `v0.3.1` 固定在 `95549c42`，已推送。
- 首轮上传最后阶段报告 `fetch failed`，远端实际已有九资产。通过 `PUDDING_RELEASE_CHANNEL=stable make desktop-publish-upload-resume` 再次完整验包并恢复草稿成功，没有重建或更换资产。草稿 ID `385378285`；未执行 `desktop-release-finalize`。日志：`/private/tmp/pudding-031-publish.log`、`/private/tmp/pudding-031-upload-resume.log`。
- 验收目录：`/private/tmp/pudding-031-acceptance-1JB0tX/`。从 GitHub 下载公开 0.2.11 到临时路径，用独立 home、userData、端口及 mock provider 建立消息/画布夹具，经真实 Squirrel.Mac 更新为 0.3.1；没有覆盖 `/Applications/Pudding.app`（仍为 0.3.0）。本地简易 feed 不支持 multipart ranges，旧 updater 回退为完整 ZIP 下载成功；不将其记为生产增量下载通过。
- **临时路径安装检查失败**：用户通过系统触控 ID 确认后，ShipIt 以提权进程将临时 App 替换为 `root:wheel`，目录 0755、`app.asar` 0644；`verifyInstalledApp` 的当前用户可写检查失败。保留原失败，未使用 chown/chmod 消除证据或降低断言。单独检查更新包 codesign、stapler、Gatekeeper 及 Helper 身份均通过（`installed-signature.json`）。重新解压的原版 0.2.11 本身可写；原版和新版 Squirrel 的 Mach-O UUID 相同。此时尚不能确定为何选择提权，因此保留草稿，随后单独验证正常安装位置，见下一节。
- **测试隔离偏差**：自动重启未继承 `PUDDING_HOME`、`PUDDING_DAEMON_ADDR`、`PUDDING_ELECTRON_USER_DATA_DIR`。17:26 自动重启的临时 App 曾按默认配置启动正式目录/9669；发现后关闭 PID 93287，17:28 用显式隔离配置重新启动。没有在正式会话提交测试操作，也没有恢复/覆盖正式数据库；不能把这轮声称为从未访问正式目录的全隔离自动重启测试。
- 显式隔离重启后，数据库 v13 → v18 的 13 项检查通过：canonical 消息、项目/会话、保存/关闭画布、收藏、浏览历史、应用授权、队列排序字段及完整性符合预期，并生成 v13 备份。`upgrade-result.json` 同时保留 `installationVerificationPassed=false` 和所有权错误。
- 从**升级后的真实签名 Pudding 主进程**经正式 bridge/Helper 测试 AppKit、Electron 两种隔离接收窗口：五种手势各一次，共 10/10，通过 1518 次监测，鼠标最大位移 0，前台/遮挡顺序稳定，guard 无误收输入。证据 `signed-native-results.json`；不再仅以独立启动 Helper 的结果替代签名主进程验收。
- 该签名主进程可读取已有辅助功能、录屏权限；Helper 的 bundle ID、Team ID、完整 designated requirement 与公开 0.2.11、本机 0.3.0 一致。未清理或重新申请系统授权；本轮验证已有授权延续，不新增“首次申请后恢复”实测结论。
- 签名版画中画：真实帧、展开工作区保持卡片和图片节点、完整原生 reveal 返回 true、取消后卡片清除，补测通过（`signed-preview-results.json`、`signed-preview.png`）。临时 CDP 脚本先后遇到 DOM 返回值序列化及选择器引号错误，均修正测试脚本后仅补测画中画；没有改动产品代码、跳过原生返回值断言或把脚本失败算成产品通过。

本阶段结论：公证/上传完成，暂时保留草稿，等待正常安装位置的升级可写性验收。后续结果见下节；Intel 真机、持续真人并行、多屏及特定 App 兼容边界仍同上，不扩大发布承诺。

## 2026-09-09 正常安装位置升级复核

- 用户明确允许升级已安装的 `/Applications/Pudding.app`，重启并使用现有正式数据。升级前确认正式版已停止、9669 无监听；正式库 v18、`quick_check=ok`，保存关闭状态下的一致性备份。没有提交测试消息或修改正式会话。
- 首次运行官方升级测试被**升级前**的可写性检查拒绝（`EPERM`）；此时 0.3.0 的目录与文件仍属于当前用户。核对当前 Codex 宿主为 `/Applications/ChatGPT.app`、bundle ID `com.openai.codex`，系统「App 管理」中的对应授权关闭。用户明确批准后开启该项，应用重启后同一可写性检查通过。此项是测试宿主的 macOS 授权限制，不是安装包所有权故障；没有修改 Pudding 或其他 App 的文件权限。
- 执行 `PUDDING_RELEASE_CHANNEL=stable PUDDING_UPDATE_TEST_REQUIRE_COMPUTER_USE_IDENTITY=1 PUDDING_UPDATE_TEST_PORT=19733 node scripts/run-update-test.cjs`，使用本地九资产中的已公证候选，复用已校验的下载缓存，通过真实 Squirrel.Mac 完成 0.3.0 → 0.3.1。18:00 官方脚本输出 `Local update verified: 0.3.1`，退出码 0。
- 升级后 App 为 `yanggang:staff`，目录 0755、`app.asar` 0644，当前用户可写；递归可写性、codesign、stapler、Gatekeeper 和 Helper 完整身份比对均通过。原生「关于 Pudding」显示 `版本0.3.1 (0.3.1)`；正式 daemon 使用 `/Applications/Pudding.app` 内二进制及正式目录/9669。
- 升级后权限页显示辅助功能、屏幕录制、摄像头、麦克风均已授权；本轮没有重置或重新申请 Pudding 权限。既有授权延续通过，不新增首次申请的测试结论。
- 升级后的在线 SQLite 备份与升级前静态备份比较：10 个业务表逐行规范化散列一致，包含 canonical 消息、会话、项目、turns、队列、App 授权、画布、保存版本、收藏和历史；两份快照均为 v18、`quick_check=ok`、无外键错误。具体计数/散列只保存在本地验收文件，不上传用户数据库。
- 本轮证据：`/private/tmp/pudding-031-installed-upgrade-3teHA3/update.log`（授权前拒绝）、`update-authorized.log`（正常升级通过）、`data-comparison.json`（数据核对）及升级前后数据库快照。开发 daemon 未重启；没有使用 chmod/chown、移除扩展属性或绕过签名/可写性断言。

结论：正常安装位置的升级阻断已解除。随后尝试 `make desktop-release-finalize RELEASE_TAG=v0.3.1`，执行审批认为当前授权尚未明确具体公开仓库，因此在命令启动前拒绝。未绕过审批或改用其他发布接口；只读核对草稿仍为 `draft=true`、九资产完整。待用户明确确认将 GitHub `teatak/pudding` 的 `v0.3.1` 公开并设为最新稳定版后继续。临时路径升级异常与未覆盖的兼容边界仍保留，不能推断任意路径、任意 App 或 Intel 真机都已通过。

## Release Notes 草案

### Workspace and Library

- Organize projects, browser pages, and canvases in a shared tab bar while preserving project editor state.
- Keep the conversation visible in focus mode; improve resource favorites, browsing history, search, source navigation, and Markdown links.
- Preserve canvas content when closing tabs and saved versions when removing favorites.

### Conversations and Input

- Reorder queued inputs with persistent per-session ordering and preserve attachments while editing.
- Improve inline guidance, image attachments, disclosure scrolling, and approval controls.
- Open the appropriate project or global draft after archiving the current conversation; preserve other navigation when an archive request finishes later.

### Computer Use

- Extend explicit background pointer delivery to left clicks, right clicks, left double-clicks, left-button drags, and two-axis pixel scrolling without an application compatibility allowlist.
- Preserve permissions, protected-application rules, exact target identity, partial outcomes, and interrupted-input cleanup; never automatically replay uncertain input or switch to foreground delivery.
- Localize application names and show titleless, cursor-free previews with per-app stacking and expiry. Invalid targets show no preview.
- Keep capture and image identity continuous while expanding or collapsing the workspace; float previews below artifacts or at the conversation's top-right when artifacts are absent.
- Background compatibility remains application-dependent; an application may activate itself, and remaining input stops when foreground changes.

### Data and Upgrades

- Upgrade schema v13 through v17 to v18 using backed-up, per-version transactional migrations.
- Preserve conversations, app grants, browser history, saved canvas content, and queued input state; persist queue ordering.
- Downgrades require the appropriate pre-migration backup; older applications cannot directly open schema v18.
