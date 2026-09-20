# Markdown 链接优化

> 更新：2026-09-20。实现同项目文件链接、共享聊天解析，以及内置浏览器 file URL / 地址栏本地绝对路径预览；项目外文件由用户确认，验证记录见末尾。
> 本文保留原文件名以维持引用。按用户最新要求，**file URL 进入浏览器，不进入文件编辑器**，替代上一版规划中的同视图规则。

## 1. 打开规则

假设源 Markdown 是 `/work/app/docs/README.md`：

| 链接 | 动作 |
| --- | --- |
| `guide.md`、`./guide.md` | 文件视图打开 `/work/app/docs/guide.md` |
| `../src/main.ts` | 文件视图打开 `/work/app/src/main.ts` |
| `/work/app/docs/guide.md` | 文件视图打开该绝对文件，不再补项目根 |
| `file:///work/app/page.html` | 当前会话的内置浏览器打开；项目内直接打开，项目外弹出警告确认 |
| `file://localhost/work/app/page.html?q=1#intro` | 同上，保留 query/fragment |
| `http://...`、`https://...` | 当前会话的内置浏览器；复用相同 URL 的已有页，不覆盖其他页 |
| `//example.com/path` | 按 HTTPS 网页处理 |
| `#使用方法` | 当前 Markdown 标题定位 |
| `guide.md#使用方法` | 目标 Markdown 标题定位 |
| `../src/main.ts#L12` | 源码第 12 行；`#L12-L20` 仍定位起始行，不声称选中整个范围 |
| `docs`、`./` 等实际目录目标 | 定位文件树，不按扩展名猜类型 |
| `mailto:user@example.com` | 既有系统邮件入口 |

**scheme 决定用户意图**：普通 `page.html` 路径查看源码，不执行脚本；`file:///.../page.html` 显式交给内置浏览器。浏览器自行决定展示或下载，Markdown 不保证渲染成排版文档。

不回退到系统浏览器、文件关联应用或命令执行。用户取消不创建空标签页。

## 2. 解析与文件身份

共享纯解析器 [markdownLinks.ts](../web/src/lib/markdownLinks.ts)：

- 输入原始 href；不用 DOM 的 `element.href`，不根据应用 origin 补地址。
- 原生源文件位置与 Markdown URL 分开处理。先拆 query/hash，再解码一次，支持中文、空格、括号、`%23`、`%3F`、`%25`。
- 普通绝对路径始终是文件系统绝对路径；不兼容原来“补项目根”的错误解释。
- 本地文件 query 不参与磁盘定位；fragment 只用于标题/行号。
- Markdown 文件中的相对路径先基于源文档目录得到唯一绝对目标，再映射到当前会话有效 root。不搜索其他目录中的同名文件，不因目标缺失更换解释。
- 不同 root 与嵌套 root 可映射同一绝对文件；已有标签页身份优先复用，保留其未保存内容。
- 聊天中的相对路径以当前会话所属项目的唯一根目录为基准：例如项目为 `/work/app`，`./docs/guide.md` 打开 `/work/app/docs/guide.md`。目录来自既有 session-scoped roots API，排除 `temporary` 临时工作目录；没有项目根或有多个项目根时明确提示，不猜 primary root、最近 cwd、正在查看的文件或其他会话。
- 聊天点击时刷新目录查询（仅查询 roots，不扫描项目文件），避免刚移动或解除项目时沿用旧缓存；不新增基目录状态。
- 聊天中的普通绝对路径不受根目录数量影响；裸 `#标题` / `?query` 没有源文件仍提示，不能把项目目录冒充 Markdown 文件。历史消息按点击时会话的项目归属解析；需要固定指向原文件时使用绝对路径。
- 仅接受完整本地 file URL：`file:///...`、`file://localhost/...`。远程 host、UNC 形式、不完整 `file:relative`、控制字符、畸形编码、编码分隔符和执行型 scheme 拒绝。
- 不产生空 href 或跳回应用页面。禁止协议仍保留点击反馈，不把正文改写成另一个链接。

[projectMarkdownLinks.ts](../web/src/components/project/projectMarkdownLinks.ts) 只承担项目来源与 root 映射，不再维护第二套 URL 解析规则。

## 3. 打开流程与权限

### 3.1 普通项目路径

[useMarkdownLink.ts](../web/src/components/project/useMarkdownLink.ts) 被 Markdown 文件和聊天共同使用：

1. 解析唯一目标并绑定来源 session。
2. 项目目标通过 `GET /sessions/{id}/project/entry?rootID=...&path=...` 检查存在性与实际类型。
3. 后端复用项目路径授权，检查符号链接边界；只读元数据，不创建文件、不扩权限。
4. 文件复用现有编辑器、图片/PDF 视图；目录定位树。后续实际读取继续校验权限，不将元数据检查当作永久授权。
5. 同一文件重复打开只更新定位，现有草稿不保存、不覆盖、不丢弃。

新接口解决目录可能带 `.md` 扩展名、文件可能无扩展名，以及目录列表截断/过滤不能作为单文件存在性证据的问题。普通缺失目标返回 404，未授权路径返回 403，UI 分别提示。

用户点击不调用 LLM 工具、不弹命令审批、不改变 Project roots。文件标签页仍以当前会话的既有 workspace 状态为唯一来源。

### 3.2 file URL 交给内置浏览器

受信任主 frame 的专用 IPC `pudding:browser:open-local-file`，显式带 sessionID 和 URL；地址栏导航另外传当前 tabID：

- 主进程重新校验 URL、realpath 和普通文件类型；会话及项目 roots 从现有 daemon REST 读取，不接受调用方自报 roots/confirmed。
- 检查项目 roots 时跳过已不存在或不再是目录的项，继续验证其余 roots；权限及其他文件系统错误仍报错，不静默忽略。目标文件本身不存在仍返回缺失提示。
- 当前会话关联项目内直接打开；项目外（含未关联项目的会话）显示原生警告，包含完整真实路径、脚本/网络提示、仅预览声明，默认按钮为取消。
- 确认后重新核对路径和项目归属，防止弹窗期间符号链接换目标。取消、目标变化或会话消失均不创建标签页。
- Markdown 点击仅在同意后通过现有 session-scoped REST 创建标签，BrowserHost 负责预览。已有相同规范 URL 的标签直接复用；并发相同点击合并，不重复弹窗。
- 地址栏传 tabID 时只导航该会话的现有标签，不新建、不切到另一个相同 URL 的标签。确认前后均检查目标标签仍存在，关闭或跨会话的标签不能被重新创建；失败时不释放原标签。并发合并键包含 tabID，不合并不同标签的导航。
- 项目授权沿用目录范围；项目外仅记录 `{file: canonicalPath}`，不是父目录授权。该授权只存在于 BrowserHost 的当前标签，关闭或进程重启即失效，不写入项目或模型授权。
- 地址栏导航直接查询当前标签的既有 BrowserHost 授权；同一文件修改 query/fragment 或离开后返回无需再次确认。复用时不重新添加授权，校验期间若授权被撤销则拒绝导航；不跨标签、跨会话复用，也不新增权限缓存。
- 关闭后重开需要重新确认，重启不自动恢复项目外文件授权。模型文件工具及 daemon 原有文件打开授权规则不变。
- 保留现有沙箱、上下文隔离、关闭 Node.js、设备权限拒绝和内置浏览器独立存储分区。不新增存储分区，不放宽 `openExternal` 白名单。
- 缺失、无法访问和打开失败有提示，不偷偷改用其他打开方式；打开失败清理本次新建标签。

浏览器隔离不代表任意 HTML 安全：页面仍可能执行脚本、访问网络或诱导操作。上述目录/单文件授权限制浏览器打开和导航目标，**不是操作系统级的页面子资源目录沙箱**；本轮不另加资源拦截机制。此入口只在用户明确打开时启动。

#### 浏览器地址栏

- `/Users/.../page.html`、`C:/work/page.html` 等原生绝对路径优先识别为本地文件，不补 HTTPS、不作为搜索词。地址栏打开 HTML 是浏览器导航，和 Markdown 中普通路径打开源码的规则不同。
- 中文、空格及原生文件名里的 `#`、`?`、`%` 按文件名编码；若需要网页 query/fragment，输入明确的 `file:///.../page.html?q=1#intro`。不猜测当前目录、不展开 `~` 或相对路径。
- 原生路径转换与显式 file URL 共用 [openLocalFile.ts](../web/src/browser/openLocalFile.ts) 和上述主进程校验。项目内直接预览；项目外警告确认；取消或文件缺失时保持原页面并恢复地址显示。
- 普通网址、localhost 及显式 `? 搜索词` 保持原行为，不修改 Markdown 文件编辑器的打开规则。

### 3.3 HTTP/HTTPS

[useOpenBrowserTab.ts](../web/src/browser/useOpenBrowserTab.ts) 是聊天、项目和工作区共同的网页打开入口。按目标 session 查询现有页面，命中相同 URL 则复用，否则创建新页；所有 API 都显式携带 sessionID。

## 4. 尚未实施

- **普通路径指向项目外文件**的编辑器只读适配仍未实施；明确提示不在当前可访问范围。显式 `file://` 可在确认后预览，但不是自动 fallback。
- 项目外目录的文件树/访达快捷动作未增加。
- 新的资源服务、子资源目录/网络沙箱不在本轮范围。当前只在 macOS 上完成桌面验收。
- 不新增链接数据库、历史消息基目录、项目外自动授权或第二套编辑器状态。

## 5. 验证

测试数据使用临时项目、临时 daemon 和源码 Electron/Vite；不操作正式版数据。

已覆盖的自动化入口：

- `web/test/markdown-links.test.ts`：URL 分类、源目录、编码、缺少来源、危险协议。
- `web/test/project-markdown-links.test.ts`：绝对路径不补根、跨 root、file URL 单独分派，以及聊天唯一项目根、临时目录排除、多根/无项目/裸锚点拒绝。
- `web/test/project-reveal.test.ts`：嵌套 root 标签身份复用和目录定位。
- `internal/api/project_files_test.go`：新接口文件/目录类型、缺失、跨会话 root 与符号链接逃逸。
- `internal/api/browser_test.go`：预览临时授权失效后，仅删除不可恢复的标签绑定，保留其他网页与正常列表响应。
- `electron/test/local-file-browser.test.cjs`：项目内免确认、项目外取消/确认、并发复用、单文件范围、符号链接换目标、关闭与会话隔离；同标签授权复用、撤销竞态、失效 roots 与权限错误区分。
- `web/test/browser-address.test.ts`：原生绝对路径与 URL 的区分、中文/空格/标点编码，以及普通网址/搜索回归。
- `PUDDING_SMOKE_SCENARIO=browser-local-navigation npm --prefix web run smoke:electron-workspace`：真实地址栏提交、原标签页与同一 Chromium guest 保留、项目外取消/确认、缺失文件、前进后退、同文件锚点/参数不重复授权及失效 roots；原生对话框仅自动供应用户选择。
- `electron/test/browser-host.test.cjs`：单文件授权不放行相邻文件、符号链接换目标或后来替换成的目录。
- `PUDDING_SMOKE_SCENARIO=markdown-links npm --prefix web run smoke:electron-workspace`：真实桌面点击、编码、标题、行号、目录、多 root、草稿与浏览器标签复用。

桌面 smoke 保留真实 IPC、daemon、BrowserHost 和 Chromium 页面渲染，只在原生对话框返回处供应取消/确认选择，便于无人值守运行。不声称已手动验证系统弹窗外观。

上一轮 file URL 实现已通过：Web 113 项测试与构建、Electron 226 项测试（后续单文件权限收紧再次通过 56 项定向测试）、浏览器/项目文件 API 定向测试、共享 prompt 测试，以及源码 Electron 点击回归（11 项检查、无 renderer 错误）。真实 HTML 预览已验证；原生弹窗的取消/确认由测试自动供应，未手动检查系统弹窗外观。

本轮聊天相对路径增量已通过：Web 116 项测试与构建、共享 prompt 测试，以及源码 Electron 点击回归（13 项检查、无 renderer 错误）。先以测试复现缺少源文件的拒绝，再在桌面回归中复现项目移动后旧 roots 缓存导致的错误解析，修改后通过同一场景；覆盖唯一项目根附带 scratch、查看子目录文档时点击聊天链接、HTML 源码、缺失/越界、移动及解除项目。

2026-09-20 地址栏增量已通过：Web 121 项测试与构建、Electron 231 项测试，以及隔离源码 Electron 地址栏回归（5 项检查）和原有 Markdown 链接回归（13 项检查），均无 renderer 错误。先用失败测试复现本地路径被补为 HTTPS、指定标签被忽略等问题，再验证原标签页导航、取消/缺失保持原页面与前进后退。测试使用地址栏的真实提交按钮；不声称验证了后台窗口的系统回车投递或系统警告框的外观。

2026-09-20 边界修复已通过：Electron 237 项测试和隔离源码 Electron 地址栏回归（6 项检查，无 renderer 错误）。新增测试先复现重复确认、失效 roots 误报，再验证修复；覆盖授权撤销时不重新授权、不同标签/会话与相邻文件隔离。桌面验证前置 root 删除后仍可打开有效项目文件，以及已确认的项目外文件修改锚点/参数、离开后返回均不重复弹窗。

测试环境需使用本机 Xcode 的 macOS 26.5 SDK；默认 CommandLineTools 的 macOS 27 SDK 与当前链接器不匹配。测试服务器使用临时监听端口和临时数据目录，不重启常用开发实例。
