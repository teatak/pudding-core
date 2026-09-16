# 本地 HTML 预览与 Markdown 链接打开方案

> 日期：2026-09-16。状态：待实施设计，尚未实现。
> 代码核对基线：[`f2366e9`](https://github.com/teatak/pudding-core/commit/f2366e9116cce1c8e1d953ef780ecd45a84adce7)。
> 范围：Pudding Electron 桌面端中的聊天 Markdown、Markdown 文件、浏览器地址栏与本地 HTML 预览。
> 本文区分当前代码事实、拟实施行为和验收要求；不把源码推导当作用户桌面的实际复现。

## 1. 目标与交付范围

用户应能从聊天、Markdown 文档或地址栏打开本地 HTML，在 Pudding 内看到页面。文件可以位于桌面、下载目录、临时目录或其他应用有权读取的位置，无须创建项目、绑定项目或把文件移动到项目目录。

本次交付包含：

- 统一聊天、Markdown 文件和地址栏的链接目标解析与打开规则。
- 任意可读本地 `.html`、`.htm` 的内置浏览器预览，以及配套 CSS、JavaScript、图片、字体和页面请求的本地数据资源。
- 绝对 `file://` 链接、具有明确来源的相对链接、查询参数和锚点。
- 聊天链接来源上下文的保存、流式展示和历史恢复。
- 明确的内部打开、外部打开、错误提示与预览目录操作。

本次不扩展项目外 Markdown、PDF、代码等文件类型的完整预览能力，不新增通用文件管理器、文件上传或任意 Node.js 程序运行能力。现有项目文件查看器的读取接口仍有目录范围，不能仅凭 UI 路由复用就宣称任意本地文件均已支持。

项目负责组织文件和提供默认工作位置；会话负责标签页与资源归属。项目归属不再是用户主动打开本地 HTML 的准入条件。

## 2. 当前代码事实

| 位置 | 当前行为与实施含义 |
| --- | --- |
| [聊天 Markdown](../web/src/components/transcript/TurnParts.tsx) | `markdownUrlTransform` 只保留 HTTP、HTTPS、mailto 链接；`file://` 返回空字符串，相对链接以 `window.location.origin` 解析。默认点击调用 `openExternalURL` |
| [桌面外部打开](../web/src/lib/desktopBridge.ts)、[Electron 主进程](../electron/main.cjs) | 默认外部打开最终进入 `shell.openExternal`；当前通用外部 URL 白名单不包含 `file:`。不能将“弹出了浏览器”等同于“正确预览了本地 HTML” |
| [项目 Markdown 链接解析](../web/src/components/project/projectMarkdownLinks.ts) | 相对链接已有以 Markdown 原文件目录解析的基础，但本地目标限制在当前 roots 内；文件目标会丢弃 query |
| [项目文件查看器](../web/src/components/project/ProjectFileViewer.tsx) | 本地 HTML 被当作普通文件定位到源码；只有网页目标进入 `onOpenBrowserURL`。文件行号与 Markdown 标题定位不能直接复用于 HTML hash |
| [Markdown 编辑器](../web/src/components/project/ProjectMarkdownEditor.tsx) | 已提供原始 href 的点击回调，可复用，不必再做一套 Markdown 点击检测 |
| [地址栏](../web/src/browser/BrowserToolbar.tsx)、[地址规范化](../web/src/browser/helpers.ts) | 已接受带 scheme 的地址，包括 `file://`；地址栏还负责搜索词、裸域名等输入语义 |
| [本地 URL 授权器](../internal/browser/file_url.go)、[生产装配](../internal/daemon/daemon.go) | 生产链路已接入授权器，支持会话绑定项目 roots 中的普通文件；未绑定项目或位于项目外的文件被拒绝 |
| [会话工作目录](../internal/sessionworkspace/workspace.go) | 文件面板使用项目 roots 加本会话已有的 Code scratch；浏览器授权器未采用这一完整范围，存在可见文件与可预览文件不一致 |
| [浏览器宿主](../electron/browser-host.cjs)、[WebView 安全配置](../electron/browser-webview-security.cjs) | 已有标签页、导航、受信任文件目录、撤销与隔离配置；现有文件检查主要围绕导航 URL，完整预览还需统一管控本地子资源读取 |
| [项目资源接口](../internal/api/project_files.go)、[前端资源 URL](../web/src/api/client.ts) | 当前资源接口只允许图片和 PDF，前端 URL 会携带 daemon token；不能简单加入 HTML/JS MIME 后作为活动 HTML 页面入口 |
| [消息和 turn 模型](../internal/store/store.go) | 尚无统一持久化的聊天链接基目录。当前 session 项目目录、某次命令 cwd 都不能直接充当历史消息的固定来源 |

已运行的 URL 转换逻辑核对表明：绝对 file 链接被转成空串，相对链接被拼到假定的应用 HTTP origin 下。这是函数级验证，不是用户本机交互复现。

若最终 DOM 保留 `<a href="">`，读取元素的 `.href` 仍可能得到文档 base URL，随后触发外部打开。这是源码与 DOM 语义推导；用户观察到“点击会弹出浏览器”时，必须同时核对最终地址和内容，不能直接判定该次打开失败或成功。

## 3. 用户可见的打开规则

| 输入或操作 | 拟实施行为 |
| --- | --- |
| 聊天、Markdown 中的 HTTP/HTTPS 链接 | 默认在所属会话的内置浏览器打开；提供显式“在系统浏览器打开” |
| 用户点击绝对 file HTML 链接 | 直接进入内置预览，文件不必属于项目；不为同一打开动作重复弹确认 |
| Markdown 中的相对 HTML 链接 | 先按来源解析，再进入同一内置预览入口 |
| 地址栏提交 HTTP/HTTPS 或 file HTML 地址 | 导航当前标签页；继续支持已有搜索词和裸域名处理 |
| Markdown 中的纯 hash 链接 | 在原 Markdown 文档内定位；聊天消息没有对应标题时明确提示 |
| HTML 链接中的 query/hash | 完整交给浏览器，不能改成源码定位或丢弃 |
| mailto 链接 | 调用系统邮件应用 |
| 已支持的非 HTML 项目文件链接 | 按现有文件类型进入文档或源码查看器，保留行号和标题定位 |
| 暂未支持的项目外非 HTML 文件 | 显示明确的类型或预览范围提示，并提供复制路径、在 Finder 中显示等明确动作 |
| 无效链接、路径上下文缺失、文件不存在、系统拒绝读取 | 显示对应原因；不回退成应用页面、网页搜索或系统外部打开 |

Markdown 默认点击复用同一会话内已打开的同一完整目标 URL，否则创建新标签页。地址栏提交则导航当前页。新标签页、复制链接和外部打开均为明确动作，不依赖隐式 fallback。

网页链接的“在系统浏览器打开”沿用系统 URL 打开能力；本地 HTML 的外部动作应明确标为“用系统默认应用打开”，由受信任桌面入口对解析后的文件执行。不能通过放宽全局 `openExternalURL` 的 scheme 白名单，让所有调用方获得本地文件打开权限。

内置预览显示可理解的原文件路径与文件名，复制链接默认得到规范的 `file://` 地址。内部预览协议地址不是长期分享或消息持久化地址。

## 4. 路径解析规则

### 4.1 绝对链接

下列链接已经指定唯一的本地文件，不依赖项目或链接基目录：

```markdown
[pelican-bicycle.html](file:///Users/yanggang/Desktop/workspace/pelican-bicycle.html)
```

解析时保留 query/hash，将 URL 与文件路径分别处理。使用已有 URL、文件路径和真实路径工具，不手写字符串前缀判定。首版按 macOS 本地文件语义处理空 host 或 localhost；远程 file host 不作为通用网络文件协议开放，已挂载目录仍按本地路径处理。

文件名中的空格、中文、百分号及编码后的井号必须正确处理。不能先对整个 URL 解码，再用井号截断文件名。

### 4.2 Markdown 文件中的相对链接

```markdown
[pelican-bicycle.html](./pelican-bicycle.html)
[另一个页面](../demo.html?theme=dark#scene)
```

基准是 Markdown 源文件所在目录。例如：

| Markdown 源文件 | 相对链接 | 解析目标 |
| --- | --- | --- |
| `/work/site/README.md` | `./pelican-bicycle.html` | `/work/site/pelican-bicycle.html` |
| `/work/site/docs/README.md` | `./pelican-bicycle.html` | `/work/site/docs/pelican-bicycle.html` |
| `/work/site/docs/README.md` | `../pelican-bicycle.html` | `/work/site/pelican-bicycle.html` |

正常解析 `..`，不因解析目标越过项目 root 就判定用户主动打开的 HTML 无效。路径解析只确定目标；实际读取由预览入口处理。

解析必须在 DOM 相对地址补全之前进行。不要用 Electron 页面地址、Vite 地址、daemon origin 或全局当前项目作为本地 Markdown 的 base。

### 4.3 聊天中的相对链接

为新消息增加可选的来源元数据，例如 `metadata.localLinkBaseDir`：

- 由生成消息的执行上下文确定，在本轮开始生成回复前固定；不能由点击时的当前项目反推。
- 默认使用本轮明确的主要工作目录。没有唯一、明确的目录时不生成虚假的 base，并优先输出绝对 file 链接。
- 首次展示、流式展示、最终 canonical message 和历史恢复使用同一个值。流式携带的是本轮固定值的投影，不成为另一份长期事实源。
- 元数据与消息一起保存；从当前 renderer、localStorage 或最后一次工具调用 cwd 恢复 base 均不成立。
- 该字段只是路径解释上下文，不是文件读取授权，也不允许模型通过写入一个目录值授予权限。
- 涉及多个目录的输出优先使用各自的绝对 file 链接，不建立逐链接数据库记录。

历史消息缺少来源时，绝对链接仍能使用；相对链接提示“无法确定文件位置”，提供选择目标文件的操作。用户这次选择的结果不能反向当作所有旧链接的默认目录。

切换会话、修改项目 roots 或重新打开应用，不得改变同一条已具备来源上下文的消息链接的解析结果。

### 4.4 Markdown 语法与无效目标

标准行内链接写成 `[名称](目标)`。`]` 后的左括号如果在实际 Markdown 源码中被反斜杠转义，就不再构成该行内链接；展示示例与实际源码应区分。

原始 href 进入共享解析器。空值、非法 URL、禁止的执行型 scheme 返回明确的无效结果，不生成 `href=""`。只扩展本次需要支持的目标种类，不关闭 Markdown URL 过滤或放行任意 scheme。

## 5. 本地 HTML 资源通道

### 5.1 预览地址与资源根

本地页面通过独立协议加载，例如：

```text
pudding-preview://<预览标识>/<原文件路径的编码片段>
```

该地址是运行期资源定位符。业务层保存原文件 URL 和用户选择的预览目录；内部地址从有效预览授权派生，不能成为第二份可独立修改的文件目标事实源。

协议路径保留原文件的目录层级，URL 定位与资源根授权分别处理。例如，`/work/site/pages/demo.html` 可映射为 `pudding-preview://<预览标识>/work/site/pages/demo.html`，其中 `../shared/style.css` 解析到 `/work/site/shared/style.css`，`shared/style.css` 则解析到 `/work/site/pages/shared/style.css`。不能把入口压缩成协议根下的 `/demo.html`，否则浏览器规范化后会丢失这两种路径的区别。

处理器先从规范化 URL 还原唯一的本地目标，再检查该目标是否位于本次资源根内。选择新的预览目录只改变读取范围，不改变入口和相对资源的定位结果。路径编码、根绝对路径引用和符号链接的具体处理遵循本地 file URL 语义，并纳入解析验收。

用户主动打开任意可读 HTML 时，默认以 HTML 所在目录为资源根。预览通道保留站点目录结构，对每一次 HTML、CSS、JavaScript、图片、字体和本地数据读取执行相同的目录与普通文件检查。

使用标准 URL 解析、正确 MIME 和相对资源语义；支持普通脚本、模块脚本和页面对预览范围内数据的读取。外部 HTTP/HTTPS 资源沿用浏览器正常的网络与跨域规则，不代理附带 daemon 凭证的请求。

### 5.2 上级目录资源

页面 `/work/site/pages/demo.html` 引用 `../shared/style.css` 是常见结构。默认资源根 `/work/site/pages` 无法提供它，必须给用户可理解的处理方式：

1. 预览菜单提供“预览目录”，默认显示当前 HTML 所在目录。
2. 根外资源加载失败时展示实际资源路径及该操作，避免只显示一个残缺页面。
3. 用户可将这次预览目录改为共同父目录 `/work/site`，然后重新加载。
4. 选择的目录必须包含入口 HTML；它只替换本次预览的资源范围，不修改项目配置或自动扩大到其他父目录。

手动点击 Markdown 链接是用户打开一个文件的动作；页面脚本、重定向、iframe 和子资源请求只能使用已有预览范围，不能自行创建或扩大授权。

### 5.3 Electron 集成与隔离

- 在实际承载预览 WebView 的 Electron session 上注册协议；不能只注册默认 session。标准 scheme 注册时机与 privileges 按当前 Electron API 验证。
- 保留 `sandbox: true`、`contextIsolation: true`、`nodeIntegration: false`、`webSecurity: true`；不通过关闭 Web 安全或 CSP 来修复资源加载。
- 协议处理器验证有效授权、请求来源及归属。每个预览使用独立的 origin 标识，其他网页或其他会话不能仅凭知道内部 URL 就借用授权；具体 frame/request 识别能力需通过真实 Electron 测试确认。
- 本地路径规范化、真实路径和符号链接检查复用现有路径工具。不能只检查入口 HTML，或只在第一次请求时校验目录。
- 预览文档与 daemon UI/API 分离 origin，不向页面 URL、脚本或请求中暴露 daemon 启动 token。
- 内置原始 `file://` 加载不能成为绕过新资源检查的后备路径。接通受控预览后，同一产品入口统一走新通道。

## 6. 用户打开、工具打开与生命周期

### 6.1 授权来源

用户从地址栏、聊天链接、Markdown 文件链接或原生文件选择器主动打开本地 HTML，可直接建立本次预览授权，不要求绑定项目或再次确认同一动作。

受信任用户入口由 Electron 主进程核验发送方窗口、frame 和具体调用路径。普通请求体中的 `source: "user"`、`fileRoot` 或 `_fileAuthorized` 字段不能作为用户授权的证明；嵌入网页和工具桥不能调用用户打开入口自行授予目录。

当前内置 `browser.open` 与 UI 使用同一浏览器服务，但没有可直接复用的本地文件审批入口。现有 Code 临时目录权限也没有自动进入浏览器授权器。实施时必须把工具自动打开与用户主动打开的授权来源分清，不能借此给模型全盘读取能力。

本轮不新建一套通用审批系统。工具打开只使用可信执行上下文中已有、仍有效的文件授权；没有有效授权时返回明确的权限原因。若要进一步让 Work 模式主动申请任意本地文件访问，应单独定义审批接入，不能仅标记为 read 就宣称已有审批保障。

### 6.2 数据归属与恢复

| 数据 | 归属及处理 |
| --- | --- |
| 原文件 URL、用户选择的预览目录、tab/session 归属 | 复用 canonical 浏览器标签页状态；预览目录作为打开意图保存，不直接等同于有效授权 |
| 聊天相对链接基目录 | canonical message metadata；本轮流式展示引用同一固定值 |
| 有效本地读取授权与内部预览 origin | Electron 原生能力层运行期状态，绑定 tab、session 和当前预览；由受信任打开操作或有效工具权限派生 |
| 标签页选择、菜单展开状态 | 现有前端 UI 状态，不保存第二套浏览器文件归属 |

关闭预览、删除会话或释放其浏览器资源时销毁相应授权。手动打开的文件与项目变更解耦；撤销项目自动授权时只影响依赖该授权的资源，不能误关用户独立打开的文件。

标签页离开本地预览导航到普通网页时，不能把前一个页面的本地读取能力带给新网页。用户显式打开的 HTML 内部导航可在现有预览范围内继续工作，页面不能自动扩权。

应用重启后先恢复原目标及标签页元数据，不复用过期内部 URL。用户首次激活待恢复的本地预览时，按用户打开入口重新建立运行期授权，不再额外弹确认；后台自动恢复或 AI 访问不能冒充这次用户操作。授权尚未建立时展示明确的待打开状态，不把它记成文件不存在。

## 7. 共享解析与打开职责

共享的是目标判定和打开动作，不是把所有输入当作相同的裸字符串处理。

| 环节 | 输入与职责 |
| --- | --- |
| 入口适配 | 地址栏保留搜索词与裸域名处理；Markdown 提供原始 href、源文档 URL 或消息 base，并显式带 sessionID |
| 链接解析 | 产生网页、本地 HTML、文档文件、文档锚点、邮件或无效目标；保留 HTML 的 query/hash |
| 打开分派 | 接收解析目标和“当前页／复用目标页／新页／外部打开”的明确意图，复用现有标签页与文件查看动作 |
| 本地预览 | 接收受信任打开请求，建立资源授权并加载预览协议地址，维护生命周期与错误状态 |

目标解析结果应是可区分的类型，不再让一个“看起来像 URL”的字符串同时承担网页地址、项目文件标识和授权凭证。

聊天点击携带原消息所属 sessionID；后台没有 focus 业务状态。跨会话不复用标签页，不能从全局当前会话 store 隐式取目标。

## 8. 代码实施范围

以下为拟修改位置，不代表代码已经完成：

| 模块 | 拟实施内容 |
| --- | --- |
| 新增共享链接目标模块与打开 hook | 原始 href 加显式来源解析；调用已有浏览器、文件和 native 动作，不建立另一套标签页状态 |
| [TurnParts.tsx](../web/src/components/transcript/TurnParts.tsx) | 接入现有 `resolveLinkURL`、`onResolvedLinkClick` 扩展点；传递 session 与来源；无效链接不生成空 href |
| [projectMarkdownLinks.ts](../web/src/components/project/projectMarkdownLinks.ts)、[ProjectFileViewer.tsx](../web/src/components/project/ProjectFileViewer.tsx) | 本地 HTML 分流到预览；解除该目标的项目根限制；保留普通文件的行号与文档锚点语义 |
| [BrowserToolbar.tsx](../web/src/browser/BrowserToolbar.tsx)、[useWorkspaceBrowserSurface.ts](../web/src/components/workspace/useWorkspaceBrowserSurface.ts)、[BrowserOptionsMenu.tsx](../web/src/browser/BrowserOptionsMenu.tsx) | 统一导航动作、复用标签页、提供外部打开与预览目录操作 |
| [main.cjs](../electron/main.cjs)、[preload.cjs](../electron/preload.cjs)、[browser-host.cjs](../electron/browser-host.cjs) 与新增协议处理模块 | 受信任打开入口、协议注册、资源读取、错误报告与授权销毁 |
| [browser bridge](../internal/browser/electron_bridge.go)、[browser API](../internal/api/browser.go)、[file URL 授权](../internal/browser/file_url.go) | 明确用户与工具授权来源；支持原文件目标与内部加载地址映射；更新只识别 file scheme 的恢复与撤销逻辑 |
| [store 模型](../internal/store/store.go)、[engine](../internal/engine/engine.go)、[前端契约](../web/contracts/api.ts) 与相关消息事件 | 保存固定链接 base，贯通流式与历史；具体序列化契约在实现时同步 |
| 前端/Electron i18n、相关单测与 smoke | 完整覆盖打开行为、来源解析、资源范围、错误和真实页面显示 |

项目外非 HTML 文件若要进入现有查看器，需要额外的资源读取适配，不能伪造项目 root 以绕过现有接口。

实现前核对新增 metadata 和标签页意图所需的存储变化；若涉及持久化结构，遵循仓库的 schema、迁移和版本指纹要求，不修改已发布迁移。只增加可选 JSON 字段时，也需覆盖旧数据读取和字段往返。

## 9. 验收标准

| 场景 | 必须观察到的结果 |
| --- | --- |
| 无项目会话打开桌面或下载目录 HTML | 内置浏览器显示正确文件，无须创建项目或重复确认 |
| 点击绝对 file 链接 | 文件名、地址与页面内容匹配；不打开 daemon 页面 |
| Markdown 同目录及 `../` 链接 | 按源文档位置正确解析；项目外 HTML 可由用户主动打开 |
| 聊天相对链接 | 流式、最终消息、重新打开历史使用同一固定 base |
| 旧消息没有 base | 明确提示无法定位，可选择目标；不猜当前项目或命令 cwd |
| query、hash、中文、空格及编码后的井号 | 文件路径和 URL 参数均正确，HTML hash 不变成源码行号 |
| CSS、普通/模块 JavaScript、图片、字体、本地数据 | 页面真实加载并执行所需内容；不能仅以顶层导航成功作为通过 |
| `pages/demo.html` 引用 `../shared` | 提示根外资源；调整本次预览目录后正常加载，不修改项目 |
| `../shared/style.css` 与 `shared/style.css` | 保留不同的本地目标；调整资源根前后定位不变，不能被压缩成相同的协议 URL |
| 目录外、符号链接越界、其他预览来源请求 | 资源检查生效；脚本、iframe、重定向不能自行扩大授权 |
| 普通网页试图借用内部预览 URL | 不获得另一预览或其他会话的本地文件访问 |
| 刷新、前进后退、重复点击、跨会话点击 | 目标稳定，按打开意图复用或创建标签页，无串会话 |
| 关闭标签页、释放会话、切换到普通网页 | 相应本地读取授权失效 |
| 修改项目、重启后激活手动预览 | 原文件目标和用户选择的预览目录不漂移；有效授权按生命周期重新建立 |
| 无效链接及空 href | 无误导航，无系统浏览器弹出应用页面 |
| 显式外部打开 | 网页进入系统浏览器；本地文件通过明确的系统默认应用动作打开 |
| 已有项目文档链接 | Markdown 标题与源码行号定位正常，已有文件读取范围未被伪造扩展 |

验证分层：

- 链接解析单测覆盖源上下文、URL 编码、无效目标和 query/hash。
- Go 测试覆盖消息元数据往返、旧数据、用户/工具入口及授权生命周期契约。
- Electron 单测覆盖请求来源、资源根检查、授权销毁与无 fallback 行为。
- 真实 Electron smoke 使用临时目录和 HTML/CSS/JS/图片 fixture，检查页面内容、交互、实际资源请求和最终打开位置。
- 桌面回归先确认运行的是当前源码开发版，避免误用已安装的正式版；测试文件与数据只放临时目录。

按实际改动运行 `npm --prefix web test`、`npm --prefix web run build`、`npm run test:electron`，以及相关 Go 包的 `go test -tags 'sqlite_fts5 webrtcaec'`；跨模块与 schema 变更按 [AGENTS.md](../AGENTS.md) 补齐对应检查。此列表是实施验收要求，不是本次文档交付已运行的结果。

## 10. 实施顺序与工作量

按一名熟悉项目的开发者、具备可运行当前源码的 macOS/Electron 环境估算：

| 阶段 | 交付内容 | 估算 |
| --- | --- | --- |
| 1 | 共享解析与打开动作，修正空 href、外部打开和 HTML 分流 | 1 人日 |
| 2 | 任意位置 HTML、受控资源协议、预览目录与授权生命周期 | 2–3 人日 |
| 3 | 聊天固定 base 的生成、流式传递、持久化与恢复 | 1 人日 |
| 4 | 真实桌面回归、边界修正、清理旧打开路径 | 1–2 人日 |
| 合计 | 本文首版范围完整交付 | 5–7 人日 |

这些阶段属于同一交付范围。完整接通后删除被替代的产品加载路径，不保留绕过目录或来源检查的第二条 fallback。

估算不包含项目外所有文件类型的预览适配、新的通用工具审批系统、目录热更新或复杂前端项目的构建服务。需要 dev server 的项目仍使用其 HTTP 地址；本功能负责静态文件与浏览器可直接运行的页面。

## 11. 参考

- [Electron：本地页面优先使用自定义协议](https://www.electronjs.org/docs/latest/tutorial/security#18-avoid-usage-of-the-file-protocol-and-prefer-usage-of-custom-protocols)
- [Electron protocol：session、scheme 与资源处理](https://www.electronjs.org/docs/latest/api/protocol)
- [Electron shell：系统外部打开](https://www.electronjs.org/docs/latest/api/shell)
- [HTML：链接元素及 href 解析](https://html.spec.whatwg.org/multipage/links.html#dom-hyperlink-href)
- [CommonMark：行内链接语法](https://spec.commonmark.org/0.31.2/#links)
