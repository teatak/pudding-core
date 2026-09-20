# Pudding Core / Desktop 拆仓准备方案

> 状态：工程拆分及 `0.3.5-beta.1` 公开预览版发布已完成；core 已采用 Apache-2.0 并公开仓库。
> 日期：2026-09-20；实际迁移来源：`846ed125`。
> core 拆分分支已合并并推送到 `main`，desktop 使用已有 `main`；两仓历史均保持原样。

## 1. 目标与已确认决定

- `pudding-core` 开源，保留可独立运行的本地 Agent daemon。
- 新建私有 `pudding-desktop`，承载完整桌面产品，包括 Web 界面、Electron 和桌面原生组件。
- **保留 `pudding-core` 的完整 Git 历史，不过滤、不重写，不以新建干净历史作为开源前提。**
- 用户明确接受：转为 public 后，历史提交和 tag 中的桌面源码也会公开。
- 桌面闭源目标针对拆分后在私有仓库持续开发的版本；历史桌面源码可继续被查看。已有授权的适用范围与后续许可证另行核对，不能把迁移目录当作收回已有授权。
- 桌面源码迁出后，core 主线不再同步桌面的实现代码；允许桌面通过公开协议使用 core。

已确认官网与 OAuth Worker 随 desktop 迁移，部署配置与 URL 保持不变。工程拆分时保留原 AGPL 声明；随后用户明确选择 core 改用 Apache-2.0 并公开，desktop 自身许可证另行处理。

## 2. 仓库与目录边界

| 当前内容 | 建议归属 | 处理方式 |
| --- | --- | --- |
| `cmd/puddingd`、`go.mod`、`go.sum` | core | 保留 Go module 路径和独立 daemon 入口 |
| `internal/` 中的 engine、provider、store、tool、API 等 | core | 保留业务与数据事实源，不复制到 desktop |
| `internal/webui` | 删除原内嵌路径 | core 不再内嵌或构建产品前端，desktop 自行打包、加载前端资源 |
| `web/` | desktop | 整体迁移 UI、状态管理、i18n、资源、前端工具与测试；公共协议定义按下文处理 |
| `electron/` | desktop | 迁移主进程、preload、BrowserHost、权限、更新和测试 |
| `native/macos/computer-use-helper` | desktop | 迁移 Swift helper、fixture 和测试 |
| `assets/` 中的品牌、图标和安装器资源 | desktop | 保留各资源的权利说明 |
| `evals/`、Go 测试和 schema 检查 | core | 独立于私有仓库运行；产品集成 smoke 随对应执行端迁移 |
| `packaging/`、`scripts/`、根 Makefile | 按职责拆分 | Go runtime 的构建依赖归 core；Electron 打包、签名、公证、更新归 desktop |
| 语言服务版本清单与准备脚本 | core | 保留版本权威来源；desktop 将指定版本产物装入应用 |
| CI、法律声明生成、开发文档 | 各仓维护自身部分 | desktop 汇总实际随包交付的 core、前端及原生依赖声明 |
| `workers/oauth` | desktop | 官网与 OAuth 服务整体迁移，保持原部署配置与 URL |
| `teatak/pudding` 发布仓库 | 保持现用途 | 继续公开安装包、更新元数据和发布说明 |

不能按文件名批量搬走所有带 `desktop` 的 Go 文件。例如健康握手、相机采集、截图与附件保存分别涉及进程、hardware 和 session。按职责逐项处理；纯窗口或系统 UI 操作归 Electron，hardware 仍归 daemon。

## 3. Core 保留的功能

| 功能 | 保留范围 |
| --- | --- |
| 模型接入 | Provider、模型元数据、配置解析、流式输出与推理参数 |
| 会话与消息 | 多会话、canonical messages、输入队列、取消、生命周期事件与 SSE 续传 |
| Agent 执行 | 工具循环、上下文构建与压缩、系统提示词、Chat / Work / Code 能力边界 |
| 项目与编程 | 项目、文件读写与搜索、CLI、后台进程、Git、LSP、turn 文件变更记录 |
| 权限与审批 | 会话及项目权限、操作审批、命令沙箱与授权范围 |
| 扩展 | Skills、Apps、MCP 的加载、配置、连接与调用路由 |
| 数据与资源 | SQLite、附件、资源库、画布内容与 API、用量统计；配置继续由 YAML 管理 |
| 音频与相机 | 当前 Go 侧音频处理、语音识别、采集与相关配置，保留现有平台及依赖限制 |

以下能力存在明确的执行端依赖：

- 浏览器：core 保留工具、会话归属和现有无头 Chrome 实现；可视标签页、Electron BrowserHost 和桌面交互归 desktop。独立运行 core 使用浏览器能力仍需满足现有 Chrome 运行条件。
- 电脑操作：core 保留工具定义、审批、协议客户端和 session 管理；macOS 执行端在 desktop。没有执行端时按现有能力状态处理，不伪装可用，也不新增替代实现。
- 画布：存储与 API 留在 core；界面、交互以及当前由前端注册的 runtime-provided App / tools 归 desktop。单独运行 core 不等于拥有完整画布工具。
- OAuth：通用客户端逻辑可以保留在 core；官方 OAuth 服务的使用、部署与凭据不因 core 开源而自动具备。

Core 的独立使用入口是 API。提供最小请求示例用于创建会话、提交、订阅事件和取消；本次不新增一套开源 Web UI 或交互式 CLI。

## 4. 实际拆分方式

| 耦合点 | 已实现的边界 |
| --- | --- |
| core 内嵌 Web UI | 删除 `internal/webui` 与 `embed` 构建；core 默认只提供 API，可通过 `-ui-dir` 挂载绝对路径下的外置前端 |
| Electron 页面与 API origin | desktop 将 `web/dist` 放入应用的 `Resources/app/web/dist`，Electron 启动 daemon 时传入；沿用原 loopback origin、preload 信任边界和浏览器存储 |
| 浏览器限额与握手版本 | [contracts/runtime.json](../contracts/runtime.json) 是唯一来源，协议版本为 2，避免连接旧启动契约的 daemon |
| TypeScript API / SSE 契约 | [contracts/api.ts](../contracts/api.ts)、[contracts/events.ts](../contracts/events.ts) 属于 core；desktop 从选定 core 生成被忽略的 `web/contracts/` |
| Go / Swift / 语言服务构建 | core 的 [build-runtime.sh](../scripts/build-runtime.sh) 构建 Go runtime 与语言服务；desktop 负责 Swift helper、资源装配与签名 |
| core 依赖 | desktop 的 `core.lock.json` 锁定完整 SHA；默认自动检出，开发可显式指定 `PUDDING_CORE_DIR`，打包要求其提交匹配且工作区干净 |
| 发布溯源 | 新发布清单同时记录 desktop 提交和 core SHA；安装包仍发布到 `teatak/pudding`，历史发布记录不重写 |
| 移动兼容 | 删除 MobileAccessBridge、配对 API、设备 token、设置界面和 Worker 移动回调；daemon 拒绝非 loopback 地址 |

协议和状态约束继续遵循 [AGENTS.md](../AGENTS.md)：session scope、`clientMessageID`、单调事件序号、可取消流、canonical message、事务收尾、loopback 与 token 均不得因拆仓改变。无需为拆仓把 Go `internal` 改成供 desktop 直接导入的 SDK，也不新增第三个协议仓库。

## 5. 开发、依赖与发布

### 5.1 依赖管理

- desktop 用一份版本控制内的依赖记录锁定 core 的完整 commit SHA；可附可读 tag，但实际构建必须校验 SHA。
- 日常构建自动准备该版本的 core 源码并调用其构建入口，不依赖开发者手动复制 `puddingd`，不自动跟随 core `main`。
- 联调可以显式指定本地 core checkout；正式发布必须使用锁定且干净的源码，拒绝未记录的本地覆盖。
- core 可单独 checkout、构建与测试，不需要访问 desktop 私有仓库，也不要求安装产品前端依赖。可选语言服务的准备仍可需要 Node/npm。
- core 的公开 API、事件、bridge 契约与契约资产随同一个 core 修订交付；desktop 验证其支持的协议版本，版本不匹配应明确报错。

### 5.2 日常入口

desktop 保留原有使用方式：

| 操作 | desktop 入口 | 目标行为 |
| --- | --- | --- |
| 开发联调 | `make desktop-dev` | 自动准备 core，启动开发 daemon、Vite 与 Electron |
| 双架构打包 | `make desktop-bundle` | 构建锁定 core、前端和 helper，生成签名并公证的安装包 |
| 包检查 | `make desktop-verify` | 验证 bundle、依赖、签名、公证和更新产物 |
| 发布 | `make desktop-publish` 及已有 preview 入口 | 保留现有发布流程，使用两个仓库的准确源码信息 |
| 升级验证 | `make desktop-update-test` 及 Computer Use 升级检查 | 验证旧版到拆分后版本的真实更新路径 |

core 保留 `make test`、`make schema-check` 与独立 daemon 构建入口，删除依赖已迁出前端的 `embed` 流程和桌面构建目标。

### 5.3 用户与产物连续性

- 保持 `com.teatak.pudding`、`Pudding.app`、原生 helper identifier 和签名身份。
- 保持 release `~/.pudding`、dev `~/.pudding-dev`，不因拆仓迁移用户数据。
- 保持 `teatak/pudding` 更新地址、stable / preview 行为和既有产物命名规则。
- desktop `package.json` 继续作为应用版本来源，版本从当前发布序列递增；core 的修订单独记录，不要求与应用版本相同。
- 新发布清单记录两个仓库的构建来源、渠道、产物 hash 和大小；历史 tag、发布清单和资产保持原样。
- core 不需要 Apple 发布凭据才能构建和测试；应用及随包二进制的最终签名、公证由 desktop 发布流程承担。
- 拆仓本身不引入数据库结构变化；若实现发现确需变化，按原 schema、迁移与版本指纹约束单独评估。

## 6. 实施顺序与验收

### 阶段一：确定迁移清单与契约

- [x] 确认先保留 AGPL 声明，OAuth Worker 与官网随 desktop 迁移。
- [x] 逐项划分源码、测试、构建依赖、品牌资源和当前文档；保留 core 的历史提交和 tag。
- [x] 记录迁移来源提交，定义 core 依赖记录与公开契约资产出口。
- [x] 明确生产前端加载方式及信任边界，覆盖附件、App/Skill 资源与回调 URL。

### 阶段二：完成独立 core 与 desktop 开发链路

- [x] 创建私有 desktop 仓库并迁入桌面文件；core 当前树删除已迁出的实现，不保留双轨。
- [x] 去掉 core 的 Web 内嵌和私有目录依赖，实现 desktop 自动构建指定 core。
- [x] 拆分 CI、Makefile、脚本和测试，迁移各仓适用的 AGENTS、开发及发布文档。
- [x] 清理拆分触及的移动端兼容路径。已删除原有配对与移动桥接，不保留双轨。
- [x] 在临时数据目录验证 core 独立启动、模型请求、会话隔离、submit / cancel、SSE 续传及存储。
- [x] 通过 core Go 测试与 schema 检查，通过 desktop Electron 测试、Web 测试和生产构建。

### 阶段三：桌面与正式产物回归

- [ ] 确认真实桌面测试使用源码开发版 Electron 和当前 Vite，避免测试到已安装发布版。
- [ ] 验证多会话切换、流式回复、审批、项目文件、终端、Git、画布和附件。
- [ ] 验证可视浏览器、标签页恢复、Computer Use、音频/相机权限及 OAuth 回流。
- [x] 构建并验证 arm64、x64 两种完整产物，包括动态库、语言服务、Swift helper、许可证声明、签名和公证；`0.3.5-beta.1` 已发布。
- [ ] 使用隔离环境验证已有版本升级到拆分后版本，检查原数据可读、更新源可用及 helper 权限身份连续性；不直接用真实用户数据库做迁移实验。
- [x] `0.3.5-beta.1` 发布清单记录 desktop 与 core SHA，preview 续传及公开发布验证通过；既有稳定版 latest 保持 `0.3.4`。

### 阶段四：开源资料与仓库公开准备

- [x] 更新 core README / CONTRIBUTING / 文档索引，清楚说明独立能力、执行端依赖与构建要求。
- [x] 当前 521 个跟踪文件凭据检查通过。Gitleaks 8.30.1 扫描全部 refs 的历史补丁，唯一命中为已移除的 Edge TTS 客户端公开常量，与公开上游逐字匹配；未发现待处理的账户凭据。没有过滤或重写历史。
- [ ] 核对许可证、第三方声明和历史分发版本的授权安排；不将私有仓库可见性等同于许可证。
- [ ] 检查两个仓库的 diff、新文件与旧路径残留，确认完整回归结果后完成公开准备。

桌面实际验证记录见私有 desktop 的 `docs/repository-split-verification.md`；core 开源结果见下文。未勾选的实际升级与外部服务回归仍未完成。

## 7. 规模与工作量

拆分前（2026-09-20）按跟踪文件估算，行数包含注释和空行，不含依赖、构建产物、文档和图片：

| 仓库 | 源码，不含测试和构建脚本 | 含测试、构建与发布脚本 |
| --- | ---: | ---: |
| core | 约 7.5 万行 | 约 12.3 万行 |
| desktop | 约 9.6 万行 | 约 12 万行 |

desktop 约包含 Web 8.1 万行、Electron 9400 行、Swift 5800 行。OAuth Worker 另约 1900 行，迁入 desktop，未计入上表的原估算。最终数值会随公共契约、脚本归属和旧路径清理变化；大部分工作是迁移现有实现。

按一名熟悉项目的开发者估算：

| 工作 | 人日 |
| --- | ---: |
| 运行与资源边界拆分 | 2–3 |
| 独立开发与协议依赖 | 1–2 |
| 构建、CI 与发布改造 | 1–2 |
| 回归、文档及开源准备 | 2–3 |
| 合计 | 6–10 |

跑通开发版预计 2–3 天，包含在总量内；6–10 人日是可开源、可发布的完整工程估算，不含许可证处理和外部服务等待时间。保留 Git 历史省去历史过滤工作，但不减少打包和升级验证。

## 8. 决策与剩余事项

1. **Core 许可证（已确认）。** 用户选择 [Apache-2.0](../LICENSE)，当前 core 及后续默认贡献采用该协议；第三方依赖保留原条款。历史提交、tag 和已发布安装包不重写，原有 AGPL 授权不撤回。desktop 需要锁定本次变更后的 core 提交并同步第三方声明；desktop 自身的许可证不随 core 自动变更。
2. **OAuth Worker / 官网（已确认）。** 随 desktop 迁移，保持原部署与 URL；本次没有触发部署。core 本地构建不依赖这部分私有源码。
3. **core 版本命名（已采用）。** 首期用完整 SHA 锁定，不急于增加单独的二进制发布系统；之后采用独立版本 tag 时，须避免改写已有应用历史 tag。

## 9. Core 开源结果（2026-09-20）

- [teatak/pudding-core](https://github.com/teatak/pudding-core) 已设为 public，GitHub 识别许可证为 `Apache-2.0`；未认证下载的 `LICENSE` 与 Apache 官方原文逐字节一致。
- 许可证变更始于 `19813dd93fbbbc7518dfe076475a26c26b7e2f4a`。该提交只修改 LICENSE 和四份说明文档，没有修改运行代码、协议、依赖或数据库结构。README 和 CONTRIBUTING 明确当前授权、默认贡献条款和历史授权边界。
- 检查 Git 作者记录，人工提交归于同一邮箱，另有 Dependabot 依赖更新；没有改写第三方许可证和原生依赖声明。
- 扫描前全部 refs 可达 702 个提交，Gitleaks 8.30.1 报告检查 693 个提交的补丁，唯一命中已核对为 [公开上游 Edge TTS 常量](https://github.com/rany2/edge-tts/blob/master/src/edge_tts/constants.py)，不是账户凭据；当前树已无该实现。当前 521 个跟踪文件检查通过，历史文件路径检查未发现受检查的密钥或数据库文件。自动扫描结果不等同于人工审计每段历史内容。
- 原 core 基线仍是当前 main 的祖先，所有本地既有 tag 的对象 ID 前后相同；未清空、过滤、压缩或重写历史。历史桌面源码随完整 Git 历史保留。
- 已发布 `0.3.5-beta.1` 继续对应原 core SHA 和随包的 AGPL 文本；本次不替换其安装包、发布清单或 tag。desktop 后续构建应锁定新的 Apache-2.0 core 提交并生成相应声明。
- 公开后实际 Core CI 暴露了新环境漏装 Abseil 的问题；源码与原生归档确认 arm64 音频桥依赖 `absl::lts_20250512`。新增共用准备脚本，校验固定 `20250512.1` 源码并在忽略目录构建静态依赖，CI 和开发说明同步接入，避免依赖 Homebrew 的滚动最新版。
- 本地屏蔽系统 Abseil 搜索路径后，使用新准备的依赖执行完整 `make test`、`make schema-check` 和临时输出的 `make daemon`，全部通过；未启动或修改现有应用和用户数据库。新增依赖准备不修改 Go 业务代码或 schema。
