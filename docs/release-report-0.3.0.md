# Pudding 0.3.0 发版报告

日期：2026-09-06；2026-09-09 增补 Computer Use 验收

> 历史候选记录：0.3.0 标签和草稿保留不动。2026-09-09 后续主线（schema v18、五种后台手势及新画中画行为）改按 [0.3.1 候选报告](release-report-0.3.1.md)准备；下文 v17 和有限单击的验收不适用于新候选。

对比基线：`v0.2.11`（`9e9efd87`）

候选范围：`codex/workspace-redesign` 的完整工作区改动；最终源码以不可变 tag `v0.3.0` 为准。

## 发版结论

本版按 **0.3.0 正式版**准备。主要变化是统一工作区、资源库、专注模式和 Computer Use 操作预览。SQLite 从 v13 迁移到 v17，属于有持久数据变更的功能版本。源码验收已完成，公开发布须通过双架构签名、公证、产物校验及隔离升级检查。

## 功能摘要

- 项目、网页和画布共用可拖拽、可关闭的顶部标签；项目内部保留文件标签和编辑状态。
- 新资源库提供网页及画布收藏、真实最近打开记录、搜索、类型筛选和跨会话来源定位。
- 关闭画布保留内容，取消收藏保留保存版本；批量关闭沿用未保存检查和冲突保护。
- 专注模式扩大工作区并保留左侧完整对话，恢复正常布局时保留原分栏偏好。
- 改进浏览器起始页、地址栏、历史搜索、加载错误展示、标签溢出和活动标签定位。
- 浏览器容量统一为每会话 20 个、全局 40 个标签，Electron 和 daemon 读取同一配置。
- 改进项目文件定位、编辑器状态连续性、批量保存重试，以及对话阅读位置恢复。
- Computer Use 增加会话隔离的操作画中画；完善原生窗口采集、应用生命周期、元素遍历和键盘输入。
- 优化文件工具和诊断展示、会话导航、紧凑布局与工具提示。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 工作区与编辑器 | 中 | 顶部资源组织、专注布局、关闭及恢复行为发生变化。 |
| 资源库 | 中 | 新增全局收藏和最近打开引用；所有业务请求明确携带会话。 |
| SQLite | 高 | v13 → v17 单事务迁移，旧程序无法直接打开升级后的库。 |
| Computer Use | 中 | 原生 Helper、Electron bridge 与操作预览均有变化。 |
| 浏览器 | 中 | 容量、起始页、地址输入、工具栏及标签交互改变。 |
| 自动更新 | 低 | 沿用既有协议和 stable 通道；打包新增共享标签容量配置，排除 smoke 文件。 |

## 数据迁移与降级

- 已发布 v13 直接在一个事务中升级到 v17，后段失败会整体回滚至 v13。
- 迁移前创建 v13 备份，沿用仅在发生迁移时执行的备份保留与清理规则。
- 旧的关闭画布内容迁入持久画布并保持关闭；已有保存版本建立收藏关系，保留正文和 revision。
- 新增文件及画布最近打开引用，不把创建或修改时间伪造成打开时间。
- 项目、会话、canonical messages、turns、应用授权、浏览历史和浏览器标签保留。
- 现有 v17 开发库不重复迁移；未发布的 v14–v16 中间版本明确拒绝并保留原库。
- 降级到 0.2.11 或更早版本需要恢复迁移前备份，不能直接复用 v17 数据库；恢复会舍弃备份之后的数据。
- 用户正式数据仍位于 `~/.pudding`，本轮升级验证仅使用临时目录及虚构数据。

## 已完成的源码验收

详见 [2026-09-06 发布就绪评估](release-readiness-2026-09-06.md)。已核对当日测试日志：

- Go 全量测试及 SQLite race 检查通过。
- Electron 207 项、Web 19 项、Swift Helper 69 项测试通过。
- TypeScript 与生产构建通过，仍有非阻断的大 chunk 提示。
- 56 项真实桌面回归通过，覆盖工作区、编辑连续性、搜索、对话恢复、标题栏、文件定位和保存失败重试。
- 原生画中画和浏览器自动操作展示已有本次开发周期的通过记录，未计入上述 56 项。
- 正式版 v13 schema 永久夹具覆盖数据保留、单事务失败回滚、备份、完整性和重复启动。
- 当日依赖审计为 0 个已知漏洞；凭据扫描和空白检查通过。

## 发布验收

- 固定流水线重新执行 Go、Electron、schema 和生产 Web 构建。
- 生成 arm64/x64 的 DMG、ZIP、blockmap 和 `latest-mac.yml`，保持 9 个正式资产。
- 完成 Developer ID 签名、Apple 公证、Gatekeeper 和归档内容校验。
- 公开发布前，用隔离的 0.2.11 签名包和临时数据验证候选升级，并比较 Helper 签名身份。
- TCC 已授权状态的延续需在相同安装路径、相同用户环境下验证；签名身份比较本身不等于用户环境的授权验收。

### 2026-09-09 本地候选包增补

用户确认 0.3.0 尚未发布，授权继续构建 0.3.0 并上传 Apple 公证。本轮不创建 Git tag、不上传 GitHub Release、不替换已安装的 0.2.11；之前 9 月 6 日的本地打包产物保留。

- 新增显式 `delivery=background` 的有限普通左键单击，仅开放系统 Calculator、Mail、Calendar；不自动切前台、不回退到全局输入。飞书抢前台场景未解决，未开放。
- 真实原生链路已通过日历单会话批量往返、双会话串行、排队取消及在途取消。取消保留已完成前缀与未知效果，不派发剩余步骤、不重放；这不承诺撤销已在途动作。
- 修复 release Swift 测试包的 Helper/Runner 双入口冲突，独立薄 CLI target，保持产品名、Bundle ID、参数协议和点击实现不变；debug/release 各 92 项通过，零输入 CLI smoke 4 项通过。
- 本轮 Electron 213 项、schema release contract、Web 生产构建及双架构 release runtime 编译通过；此前本任务 Go 全量及 Engine 竞态回归记录见[后台输入验收文档](computer-use-background-input-probe.md#第十三轮engine-会话链路与取消记录)。本次新增改动不改变 SQLite schema 或迁移。
- 官方 `make desktop-bundle` 已退出 0：arm64/x64 Developer ID 签名、Apple 公证、stapled ticket、Gatekeeper、DMG 校验和、ZIP/DMG 解包内容及嵌套原生代码检查通过。9 个发布资产齐全，更新元数据 SHA-512/大小匹配。两架构 Helper 与安装版 0.2.11 的 Bundle ID、Team ID 和 designated requirement 一致。
- 新产物位于 `dist/verified-0.3.0-20260909/`，包含完整 `bundle-0.3.0.log` 和 `SHA256SUMS`；详见[第十五轮产物记录](computer-use-background-input-probe.md#第十五轮签名候选包准备与公证边界)。原 `dist/release` 未覆盖。
- 仍需当前签名包的 TCC 归属、真实升级及持续真人并行回归，不能把签名通过直接当作通用后台操作或公开发布验收完成；本轮未验证 Intel 真机运行。

## Release Notes

### Redesigned Workspace

- Organize projects, web pages, and canvases in one draggable tab bar while keeping project files in their own editor tabs.
- Add a unified resource library with web and canvas favorites, recent opens, search, filters, and clear source sessions.
- Preserve canvas content when closing tabs and saved versions when removing favorites.
- Keep the full conversation visible beside the workspace in focus mode and restore the previous layout when leaving it.

### Browsing and Editing

- Refresh the browser start page, address field, history search, loading errors, and tab overflow behavior.
- Support up to 20 browser tabs per session and 40 across the app with a shared capacity policy.
- Improve file navigation, editor state retention, bulk-save retries, and unsaved-change protection.
- Restore conversation reading positions across session and layout changes.

### Computer Use

- Show session-scoped picture-in-picture previews of native app and browser automation.
- Improve native window capture, application lifecycle handling, accessibility element traversal, and keyboard input.
- Keep automation previews aligned with the active operation and stop capture when the operation ends or is cancelled.
- Add opt-in background single-click delivery for system Calculator, Mail, and Calendar, preserving partial results when a batch is cancelled.

### Interface and Tools

- Refine session navigation, compact layouts, resource counts, tooltips, and approval controls.
- Improve file-tool behavior and command diagnostics.

### Data and Upgrades

- Upgrade SQLite schema v13 to v17 in a single transaction with a pre-migration backup and complete rollback on failure.
- Preserve conversations, projects, app grants, browser history, saved canvas versions, and previously closed canvas content.
- Add persistent favorites and recent-open references without inventing historical access times.
- Downgrading to 0.2.11 or earlier requires restoring the pre-migration database backup; older versions cannot open schema v17.
- Distribute signed and notarized macOS arm64 and x64 builds through the stable automatic-update channel.
