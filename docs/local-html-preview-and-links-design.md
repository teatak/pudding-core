# 本地 HTML 预览与 Markdown 链接打开方案

> 日期：2026-09-16。状态：待实施设计，尚未实现。
> 代码核对基线：[`06c06d9`](https://github.com/teatak/pudding-core/commit/06c06d9e00ec08add5177d4c7df770afd744060e)。
> 范围：Pudding Electron 桌面端中的聊天 Markdown、Markdown 文件、侧边栏文件查看、浏览器地址栏与本地 HTML 预览。
> 本文区分当前代码事实、拟实施行为和验收要求；不把源码推导当作用户桌面的实际复现。

## 1. 目标与交付范围

用户应能从聊天、Markdown 文档或地址栏打开本地 HTML，在 Pudding 内看到页面。文件可以位于桌面、下载目录、临时目录或其他应用有权读取的位置，无须创建项目、绑定项目或把文件移动到项目目录。

本次交付包含：

- 统一聊天、Markdown 文件和地址栏的链接目标解析；分别保留文件位置和打开意图。
- 任意可读本地 `.html`、`.htm` 的内置浏览器预览，以及配套 CSS、JavaScript、图片、字体和页面请求的本地数据资源。
- 普通绝对路径默认打开侧边栏文件查看器，显式 `file://` 链接默认请求内置浏览器预览；包含任意可读本地 HTML 的只读源码查看。
- 聊天本地文件链接必须绝对定位，按用途选择普通路径或 `file://`；Markdown 文件支持有明确来源的相对链接，查询参数和锚点按打开意图解释。
- 多目录下不猜测聊天相对路径；链接保留原始表示，随既有消息正文保存。
- 明确的内部打开、外部打开、错误提示与预览目录操作。

本次不扩展项目外非 HTML 文件（Markdown、PDF、其他代码等）的完整读取/编辑能力，不新增通用文件管理器、文件上传或任意 Node.js 程序运行能力。现有项目文件查看器的读取接口仍有目录范围，不能仅凭 UI 路由复用就宣称任意本地文件均已支持。

项目负责组织文件和提供默认工作位置；会话负责标签页与资源归属。项目归属不再是用户主动打开本地 HTML 的准入条件。

## 2. 当前代码事实

| 位置 | 当前行为与实施含义 |
| --- | --- |
| [聊天 Markdown](../web/src/components/transcript/TurnParts.tsx) | `markdownUrlTransform` 只保留 HTTP、HTTPS、mailto 链接；`file://` 返回空字符串，相对链接以 `window.location.origin` 解析。默认点击调用 `openExternalURL` |
| [桌面外部打开](../web/src/lib/desktopBridge.ts)、[Electron 主进程](../electron/main.cjs) | 默认外部打开最终进入 `shell.openExternal`；当前通用外部 URL 白名单不包含 `file:`。不能将“弹出了浏览器”等同于“正确预览了本地 HTML” |
| [项目 Markdown 链接解析](../web/src/components/project/projectMarkdownLinks.ts) | 相对链接已有源文档基准，但 file URL 与普通路径合并为同一种 file 结果；裸 `/...` 按当前项目 root 处理，query 会丢弃。这些行为需随打开意图契约调整 |
| [项目文件查看器](../web/src/components/project/ProjectFileViewer.tsx)、[文件预览分类](../web/src/components/project/projectPreviewKinds.ts) | HTML/HTM 已按文本进入 ProjectEditor；普通路径可复用源码入口。项目外 HTML 还需要单文件读取适配，文件行号不能直接复用于页面 hash |
| [Markdown 编辑器](../web/src/components/project/ProjectMarkdownEditor.tsx) | 已提供原始 href 的点击回调，可复用，不必再做一套 Markdown 点击检测 |
| [地址栏](../web/src/browser/BrowserToolbar.tsx)、[地址规范化](../web/src/browser/helpers.ts) | 已接受带 scheme 的地址，包括 `file://`；地址栏还负责搜索词、裸域名等输入语义 |
| [本地 URL 授权器](../internal/browser/file_url.go)、[生产装配](../internal/daemon/daemon.go) | 生产链路已接入授权器，支持会话绑定项目 roots 中的普通文件；未绑定项目或位于项目外的文件被拒绝 |
| [会话工作目录](../internal/sessionworkspace/workspace.go) | 文件面板使用项目 roots 加本会话已有的 Code scratch；浏览器授权器未采用这一完整范围，存在可见文件与可预览文件不一致 |
| [浏览器宿主](../electron/browser-host.cjs)、[WebView 安全配置](../electron/browser-webview-security.cjs) | 已有标签页、导航、受信任文件目录、撤销与隔离配置；现有文件检查主要围绕导航 URL，完整预览还需统一管控本地子资源读取 |
| [项目文件与资源接口](../internal/api/project_files.go)、[前端资源 URL](../web/src/api/client.ts) | 文本读取要求有效 rootID 与相对 path，最多 2 MiB 的 UTF-8 无 NUL 文本；资源接口只允许图片/PDF，URL 携带 daemon token。不能伪造 root 来读取项目外源码，也不能把该接口直接变成活动 HTML 页面入口 |
| [消息和 turn 模型](../internal/store/store.go) | 尚无统一持久化的聊天链接基目录；本方案在消息正文保留绝对目标及原始链接形式，不为聊天新增基目录状态 |
| [项目模型](../internal/store/store.go)、[通用路径解析](../internal/projectpath/projectpath.go) | roots 首项定义为 primary；通用相对路径解析会依 roots 顺序尝试候选。该行为不能作为聊天链接定位规则 |
| [文件工具](../internal/tool/file.go)、[命令结果](../internal/tool/command.go)、[模型上下文](../internal/contextbuilder/builder.go) | 文件结果已有真实绝对 path，命令结果有启动 cwd，模型提示已要求多 roots 使用绝对路径；需要进一步明确聊天链接绝对定位，并按文件查看/浏览器预览选择表示 |

已运行的 URL 转换逻辑核对表明：绝对 file 链接被转成空串，相对链接被拼到假定的应用 HTTP origin 下。这是函数级验证，不是用户本机交互复现。

若最终 DOM 保留 `<a href="">`，读取元素的 `.href` 仍可能得到文档 base URL，随后触发外部打开。这是源码与 DOM 语义推导；用户观察到“点击会弹出浏览器”时，必须同时核对最终地址和内容，不能直接判定该次打开失败或成功。

## 3. 用户可见的打开规则

**文件位置与打开方式分别确定。** 以下是 Pudding 在聊天和 Markdown 文档中的产品约定：普通文件路径默认打开侧边栏文件查看器，显式 `file://` 表达浏览器预览意图。不能因为文件扩展名是 HTML 就覆盖用户选择的打开方式，也不把这项约定描述成 URL 协议本身的强制行为。

| 输入或操作 | 拟实施行为 |
| --- | --- |
| `[查看源码](/work/site/demo.html)` | 在侧边栏文件查看器显示该 HTML 的源码 |
| `[预览页面](file:///work/site/demo.html)` | 在所属会话的内置浏览器渲染该 HTML |
| 普通绝对路径指向已支持的 Markdown、代码、图片或 PDF | 进入侧边栏文件查看器，按现有类型展示，不承诺所有文件都显示为源码 |
| Markdown 文件中的 `./demo.html`、`../demo.html` | 按源文档目录定位，默认打开侧边栏源码；可显式选择浏览器预览 |
| 聊天中的相对文件链接 | 没有明确文件来源时提示无法定位，可选择本次目标；不按项目 roots 搜索 |
| 聊天、Markdown 中的 HTTP/HTTPS 链接 | 默认进入内置浏览器；提供显式“在系统浏览器打开” |
| 浏览器地址栏提交本地绝对路径或 file URL | 地址栏已明确表达浏览器导航意图，在当前标签页预览；不因缺少 file scheme 跳到侧边栏 |
| Markdown 中的纯 hash 链接 | 在原 Markdown 文档内定位；聊天消息没有对应标题时明确提示 |
| 普通路径后的源码行号或 Markdown 标题片段 | 交给文件查看器的既有定位规则 |
| file URL 的 query/hash | 原样交给浏览器，不能改成源码定位或丢弃 |
| mailto 链接 | 调用系统邮件应用 |
| 目标类型尚未支持所选打开方式 | 明确提示，并提供用户可主动选择的其他方式；不静默切换 |
| 无效链接、缺少来源、文件不存在、系统拒绝读取 | 显示对应原因，不回退到应用页面、网页搜索或外部打开 |

同一文件可以同时有“源码”和“页面”两个视图。复用时先区分现有文件标签页与浏览器标签页，再在各自视图中按规范化目标定位；仅 URL 相同不能把两个视图合并。跨会话仍不复用标签页。

菜单提供“打开文件”“浏览器预览”“复制文件路径”和“复制预览链接”等明确动作。显式操作优先于链接的默认打开意图，文件目标保持不变；源码行号与页面锚点不互相冒充。用户不必手工增删 scheme 才能切换视图。

浏览器里的 HTML 自身链接、脚本、iframe 和子资源请求继续遵循浏览器导航与资源规则，不按聊天链接约定跳到源码查看器，也不能借此调用受信任文件打开入口。

网页的外部动作沿用“在系统浏览器打开”；本地 HTML 的外部动作标为“用系统默认应用打开”，由受信任桌面入口执行。不能通过放宽全局 `openExternalURL` 白名单给所有调用方增加本地文件权限。

内置预览与文件查看器均显示可理解的真实路径。复制文件路径得到本机绝对路径，复制预览链接得到规范的 `file://` URL；内部预览协议地址不作为消息或分享地址保存。

## 4. 路径解析规则

### 4.1 绝对定位与打开意图

下列链接指向同一文件，默认打开方式不同，均不依赖项目 roots 或链接基目录：

```markdown
[查看源码](/Users/yanggang/Desktop/workspace/pelican-bicycle.html)
[预览页面](file:///Users/yanggang/Desktop/workspace/pelican-bicycle.html)
```

先从入口和原始 href 确定打开意图，再规范化文件位置。可以在内部把两者转换成同一个规范 file URL，但必须单独保留 `openIntent: file | browser`。不能先统一加上 `file://` 再根据 scheme 决定打开方式，也不能把普通绝对路径交给应用 HTTP origin 补全。

本地聊天和 Markdown 文件中的裸 `/...` 按 macOS 文件系统绝对路径处理，替换当前解析器把它视作项目 root 相对路径的行为。需要相对源文档定位时使用 `./`、`../`。HTTP 页面中的根相对链接仍按网页 origin 解析，不解释为用户电脑文件。

file URL 使用标准 URL 解析，分别处理 pathname、query 和 hash；首版只接受本地空 host 或 localhost。远程 file host 不作为网络文件协议开放，已挂载目录仍按本地路径处理。

原生文件路径与 Markdown href 是不同表示：路径中的空格、中文、百分号、井号和问号应使用标准工具转换和编码；Markdown 目标中的字面 `#`、`?` 用 `%23`、`%3F` 表示。不能先对整个 href 解码再按井号截断，也不能给已编码内容重复编码。短文件名只用于显示，不参与目标定位。

源码模式只应用文件查看器支持的行号、范围或 Markdown 标题定位。`/work/demo.html#L12` 是源码行号，`file:///work/demo.html#L12` 则是页面原始 hash。query 不属于文件名，页面参数只有浏览器模式才执行；不支持的源码定位参数要明确提示，不自动切换到浏览器。

### 4.2 Markdown 文件中的相对链接

```markdown
[pelican-bicycle.html](./pelican-bicycle.html)
[查看源码行](../demo.html#L12)
[带参数预览](file:///work/site/demo.html?theme=dark#scene)
```

基准是 Markdown 源文件所在目录。例如：

| Markdown 源文件 | 相对链接 | 解析目标 |
| --- | --- | --- |
| `/work/site/README.md` | `./pelican-bicycle.html` | `/work/site/pelican-bicycle.html` |
| `/work/site/docs/README.md` | `./pelican-bicycle.html` | `/work/site/docs/pelican-bicycle.html` |
| `/work/site/docs/README.md` | `../pelican-bicycle.html` | `/work/site/pelican-bicycle.html` |

正常解析 `..`，不因目标越过项目 root 就判定用户主动打开的 HTML 无效。没有 scheme 的相对文件链接继承文件查看意图；是否有已知来源与用什么视图打开是两项独立判断。实际读取使用对应视图的授权入口。

解析必须在 DOM 相对地址补全之前进行。不要用 Electron 页面地址、Vite 地址、daemon origin 或全局当前项目作为本地 Markdown 的 base。

### 4.3 聊天中的本地链接使用绝对地址

**要求 LLM 在聊天中提供本地文件链接时使用确认过的绝对位置，并按用途选择打开方式。** 引用、检查或修改文件时提供普通绝对路径；交付可渲染页面、明确要求预览时提供完整 `file://` URL。该规则适用于单目录和多目录项目，显示文字可用短文件名。

```markdown
[查看前端源码](/work/frontend/preview.html)
[预览前端页面](file:///work/frontend/preview.html)
[查看后端源码](/work/backend/preview.html)
```

不同目录的同名文件各自具有唯一目标，同一文件又可明确选择源码或预览。用户点击后进入对应的受信任文件或浏览器入口；首版 HTML 可位于项目外，其他文件类型仍以实际读取和展示能力为准。HTTP/HTTPS 链接和 Markdown 文件内部相对定位规则保持独立。

模型输出规范应明确：

- 根据已经确认的实际文件路径构造链接；不能为了满足格式要求，把一个猜测的目录补在文件名前。
- 优先使用文件工具返回的绝对 `path`，以及已有文件引用中的真实来源。命令结果的 `cwd` 只是进程启动目录，脚本内部 `cd` 或自定义输出目录后，不能机械地与文件名拼接。
- 跨目录结果逐个输出绝对链接，不要求用户先选一个全局根目录。
- 聊天本地文件链接不用 `./`、`../` 或裸文件名。普通绝对路径有效且默认打开文件；只有需要浏览器预览时添加 `file://`。
- 不知道真实文件位置时，先按任务需要确认位置或说明无法确定，不生成貌似完整的虚假链接。

不能只依靠 prompt 保证正确。工具卡片与文件引用组件应直接从结构化的真实路径构造目标，默认文件查看，并提供明确的 HTML 预览动作；自然语言 Markdown 中的本地 href 由共享解析器校验表示形式，打开时检查目标和读取条件。不能按链接显示文字或同名文件去匹配前面的工具结果，也不能把“格式合法”当成“文件位置真实”。

将这条交付规范加入所有模式共享的 `core_system.md`，并把 `mode_code.md` 中临时工作区的“使用相对路径”明确限定为工具入参，避免与聊天链接要求冲突。工具结果被摘要或截断后，若真实路径不在可见上下文中，按既有 canonical 结果回读机制取得所需字段；不能从截断文本重新猜位置，也不额外维护一份链接来源表。

**聊天不新增 `metadata.localLinkBaseDir`、逐链接数据库或隐式根目录映射。** 普通绝对路径或 file URL 按原始表示随 Markdown 正文进入 canonical message，流式展示与历史恢复从同一正文派生目标和打开意图，不在保存时统一重写成 file URL。切换会话、重排 roots 或改变工作目录都不会重新解释已保存的绝对地址。

对于模型未遵守规范或历史消息中的相对链接，保留显示文字，返回明确的“无法确定文件位置”，可让用户针对这一次打开选择目标文件。不要输出空 href、跳网页搜索，或自动猜测项目根。用户本次选中的文件也不反向改写该消息中其他相对链接的含义。

“缺少来源”必须作为已处理的点击结果由 UI 消费，不能因为没有解析出 URL 就落回现有 `openExternalURL` 分支。用户粘贴的相对 Markdown 同样遵守；已有结构化文件引用只为它自己的链接提供来源，不能替同一消息的其他裸链接设置基目录。

### 4.4 多目录项目的确定性规则

项目 roots 是文件组织与授权范围，不是聊天链接的搜索路径。现有 `RootDirs[0]` 的 primary 定义可继续服务项目工作流，但不能据此认定某条聊天链接属于它；最后一次工具 cwd、前端当前选中目录和临时授权目录也不构成聊天链接来源。

假设项目有 `/work/frontend` 和 `/work/backend`，两边都存在 `preview.html`：

| 链接来源与目标 | 应解析到的位置 |
| --- | --- |
| 聊天：`/work/frontend/preview.html` | 前端文件的源码，不参考 roots 顺序 |
| 聊天：`file:///work/frontend/preview.html` | 同一个前端文件的浏览器预览 |
| 聊天：`/work/backend/preview.html` | 后端文件的源码，与前端同名文件无关 |
| `/work/frontend/README.md` 中的 `./preview.html` | `/work/frontend/preview.html` |
| `/work/backend/README.md` 中的 `./preview.html` | `/work/backend/preview.html` |
| `/work/frontend/docs/README.md` 中的 `../../backend/preview.html` | `/work/backend/preview.html`，按真实目录关系解析 |
| 聊天中无文件来源的 `./preview.html` | 无法定位，由用户明确选择本次目标 |

如果按已知来源解析的目标不存在，即使另一 root 有同名文件，也只报告原目标不存在。无来源时，即使搜索只得到一个结果，也不能把“当前恰好存在”当作链接含义；新增文件或调整目录顺序不应让旧链接换一个目标。

因此，预览解析器不能直接调用“相对路径在所有 roots 中逐个尝试”的 `projectpath.Resolve` 逻辑。先通过绝对链接或明确的源文档位置得到唯一绝对目标，再复用真实路径与访问范围校验；不修改其他文件工具原有的多目录查找契约。

同一文件可能同时属于外层 root 和嵌套 root。rootID、目录显示名和 roots 顺序只用于已有项目界面的归属展示，不作为聊天链接长期身份；打开依据规范化绝对目标和已保留的打开意图。预览标题、地址详情或悬停信息应显示可区分的目录路径，避免两个同名文件在界面上无法辨认。

### 4.5 Markdown 语法与无效目标

标准行内链接写成 `[名称](目标)`。`]` 后的左括号如果在实际 Markdown 源码中被反斜杠转义，就不再构成该行内链接；展示示例与实际源码应区分。

原始 href 进入共享解析器。绝对文件路径与 file URL 都是合法的本地目标表示，不能把所有无 scheme 的路径都当成网页相对地址。空值、非法目标、禁止的执行型 scheme 返回明确的无效结果，不生成 `href=""`。只扩展本次需要支持的目标种类，不关闭 Markdown URL 过滤或放行任意 scheme。

## 5. 本地 HTML 资源通道

### 5.1 预览地址与资源根

本地页面通过独立协议加载，例如：

```text
pudding-preview://<预览标识>/<原文件路径的编码片段>
```

该地址是运行期资源定位符。业务层保存原文件 URL 和用户选择的预览目录；内部地址从有效预览授权派生，不能成为第二份可独立修改的文件目标事实源。

协议路径保留原文件的目录层级，URL 定位与资源根授权分别处理。例如，`/work/site/pages/demo.html` 可映射为 `pudding-preview://<预览标识>/work/site/pages/demo.html`，其中 `../shared/style.css` 解析到 `/work/site/shared/style.css`，`shared/style.css` 则解析到 `/work/site/pages/shared/style.css`。不能把入口压缩成协议根下的 `/demo.html`，否则浏览器规范化后会丢失这两种路径的区别。

处理器先从规范化 URL 还原唯一的本地目标，再检查该目标是否位于本次资源根内。选择新的预览目录只改变读取范围，不改变入口和相对资源的定位结果。路径编码、根绝对路径引用和符号链接的具体处理遵循本地 file URL 语义，并纳入解析验收。

用户以浏览器预览意图打开任意可读 HTML 时，默认以 HTML 所在目录为资源根。预览通道保留站点目录结构，对每一次 HTML、CSS、JavaScript、图片、字体和本地数据读取执行相同的目录与普通文件检查。

多目录项目不自动把所有 roots 授予这个页面，也不把它们合成一个虚拟目录。HTML 中的相对资源仍跟随页面真实地址；需要上级目录资源时按下节调整本次预览范围，不到其他 root 中查找同名 CSS、脚本或图片。

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

用户从地址栏、聊天链接、Markdown 文件链接或原生文件选择器主动打开本地 HTML，按打开意图建立本次浏览器预览或只读源码访问，不要求绑定项目或再次确认同一动作。

受信任用户入口由 Electron 主进程核验发送方窗口、frame 和具体调用路径。普通请求体中的 `source: "user"`、`fileRoot` 或 `_fileAuthorized` 字段不能作为用户授权的证明；嵌入网页和工具桥不能调用用户打开入口自行授予目录。

当前内置 `browser.open` 与 UI 使用同一浏览器服务，但没有可直接复用的本地文件审批入口。现有 Code 临时目录权限也没有自动进入浏览器授权器。实施时必须把工具自动打开与用户主动打开的授权来源分清，不能借此给模型全盘读取能力。

本轮不新建一套通用审批系统。工具打开只使用可信执行上下文中已有、仍有效的文件授权；没有有效授权时返回明确的权限原因。若要进一步让 Work 模式主动申请任意本地文件访问，应单独定义审批接入，不能仅标记为 read 就宣称已有审批保障。

### 6.2 数据归属与恢复

| 数据 | 归属及处理 |
| --- | --- |
| 原文件 URL、用户选择的预览目录、tab/session 归属 | 复用 canonical 浏览器标签页状态；预览目录作为打开意图保存，不直接等同于有效授权 |
| 聊天本地链接目标与默认意图 | 既有 canonical 消息正文中的普通绝对路径或 file URL；保留原始表示，流式展示解析同一正文，不保存第二份基目录或映射 |
| 有效本地读取授权与内部预览 origin | Electron 原生能力层运行期状态，绑定 tab、session 和当前预览；由受信任打开操作或有效工具权限派生 |
| 标签页选择、菜单展开状态 | 现有前端 UI 状态，不保存第二套浏览器文件归属 |

关闭预览、删除会话或释放其浏览器资源时销毁相应授权。手动打开的文件与项目变更解耦；撤销项目自动授权时只影响依赖该授权的资源，不能误关用户独立打开的文件。

标签页离开本地预览导航到普通网页时，不能把前一个页面的本地读取能力带给新网页。用户显式打开的 HTML 内部导航可在现有预览范围内继续工作，页面不能自动扩权。

应用重启后先恢复原目标及标签页元数据，不复用过期内部 URL。用户首次激活待恢复的本地预览时，按用户打开入口重新建立运行期授权，不再额外弹确认；后台自动恢复或 AI 访问不能冒充这次用户操作。授权尚未建立时展示明确的待打开状态，不把它记成文件不存在。

### 6.3 项目外 HTML 的只读源码查看

项目内或已有 workspace root 中的 HTML 复用现有文件查看路径。项目外 HTML 目前无法直接通过要求 rootID 的项目文件接口读取；首版需增加受信任用户入口下的单文件只读适配，复用现有文本/源码展示组件。该工作属于本方案交付范围，不能只改变链接路由就声称已支持。

- 只读取用户此次指定的普通 HTML/HTM 文件，规范化路径并核验真实目标，不伪造项目 root 或把父目录加入 Project。
- 沿用现有文本读取边界：最多 2 MiB、有效 UTF-8、无 NUL；超限或非文本明确提示，不把未读取部分当作完整源码。
- 此入口不赋予保存、编辑、列目录或模型读取权。已有项目编辑器保持原有权限；浏览器的资源目录授权也不能被转换成源码编辑授权。
- 源码作为文本显示，不加载其中的脚本、iframe 或 CSS。可复用现有 [FilePreviewSurface](../web/src/components/canvas/FilePreviewSurface.tsx) 展示，但该组件本身不是读取或授权服务。
- 文件视图保存打开意图和文件目标，内容按已有读取/刷新流程获取。有效访问属于运行期可信入口；重启后用户重新激活视图时重新核验，不把持久化路径当作授权。

项目外 Markdown、PDF、其他代码文件的完整读取/编辑仍不由这项 HTML 适配自动覆盖。file URL 请求浏览器打开不支持的类型时也应说明原因，用户可显式选择已有文件查看方式，不能静默改写意图。

## 7. 共享解析与打开职责

共享的是目标判定和打开动作，不是把所有输入当作相同的裸字符串处理。

| 环节 | 输入与职责 |
| --- | --- |
| 入口适配 | 地址栏保留搜索词与裸域名处理；聊天提供原始 href，Markdown 文件额外提供源文档 URL，均显式带 sessionID |
| 链接解析 | 先确定本地目标及 file/browser 意图，再规范化路径；网页、锚点、邮件、缺少来源与无效目标分别返回，不遍历 roots 猜目标 |
| 打开分派 | 接收规范化目标、file/browser 意图及当前页/复用/新页/外部动作；复用现有文件查看器与浏览器，扩展名不覆盖已选意图 |
| 本地浏览器预览 | 接收受信任打开请求，建立资源授权并加载预览协议地址，维护生命周期与错误状态 |
| 本地文件查看 | 复用已有文件读取；项目外 HTML 经单文件只读适配取得源码，不执行页面内容 |

目标解析结果应是可区分的类型。本地结果至少区分规范化绝对目标、打开意图与定位参数；不能只返回一个 file URL 字符串，让调用方再次按 scheme 或扩展名猜意图。新结果由所有入口一次性接入，不保留旧字符串分派作为 fallback。

聊天点击携带原消息所属 sessionID；后台没有 focus 业务状态。跨会话不复用标签页，不能从全局当前会话 store 隐式取目标。

## 8. 代码实施范围

以下为拟修改位置，不代表代码已经完成：

| 模块 | 拟实施内容 |
| --- | --- |
| 新增共享链接目标模块与打开 hook | 原始 href、来源及显式 UI 动作生成 target + openIntent；复用浏览器、文件和 native 动作，不建立另一套标签页状态 |
| [TurnParts.tsx](../web/src/components/transcript/TurnParts.tsx) | 接入现有 `resolveLinkURL`、`onResolvedLinkClick` 扩展点；传递 session 与来源；无效链接不生成空 href |
| [projectMarkdownLinks.ts](../web/src/components/project/projectMarkdownLinks.ts)、[ProjectFileViewer.tsx](../web/src/components/project/ProjectFileViewer.tsx) | 普通路径保留文件查看，file URL 进入预览；裸 `/...` 按绝对文件路径；行号与页面 hash 按意图区分 |
| [BrowserToolbar.tsx](../web/src/browser/BrowserToolbar.tsx)、[useWorkspaceBrowserSurface.ts](../web/src/components/workspace/useWorkspaceBrowserSurface.ts)、[BrowserOptionsMenu.tsx](../web/src/browser/BrowserOptionsMenu.tsx) | 统一导航动作、复用标签页、提供外部打开与预览目录操作 |
| [main.cjs](../electron/main.cjs)、[preload.cjs](../electron/preload.cjs)、[browser-host.cjs](../electron/browser-host.cjs) 与新增协议处理模块 | 受信任打开入口、协议注册、资源读取、错误报告与授权销毁 |
| [browser bridge](../internal/browser/electron_bridge.go)、[browser API](../internal/api/browser.go)、[file URL 授权](../internal/browser/file_url.go) | 明确用户与工具授权来源；支持原文件目标与内部加载地址映射；更新只识别 file scheme 的恢复与撤销逻辑 |
| [共享提示词](../internal/prompt/assets/core_system.md)、[Code 提示词](../internal/prompt/assets/mode_code.md)、[模型上下文](../internal/contextbuilder/builder.go) | 聊天链接绝对定位，文件查看用普通路径、页面预览用 file URL；工具相对入参与交付链接明确区分 |
| [文件工具结果](../internal/tool/file.go)、[上下文结果摘要](../internal/tool/context_result.go)、[消息渲染](../web/src/components/transcript/TurnParts.tsx) 与文件引用组件 | 复用真实 path 及既有结果回读，消费缺少来源的链接状态，不新增消息基目录契约 |
| [桌面 bridge](../web/src/lib/desktopBridge.ts)、[preload](../electron/preload.cjs)、[FilePreviewSurface](../web/src/components/canvas/FilePreviewSurface.tsx) 与既有文件入口 | 适配项目外 HTML 的受信任单文件只读读取，复用源码展示与打开/刷新生命周期 |
| 前端/Electron i18n、相关单测与 smoke | 覆盖源码/预览分派、显式切换、来源、资源范围、错误和页面实际显示 |

项目外非 HTML 文件若要进入现有查看器，需要额外的资源读取适配，不能伪造项目 root 以绕过现有接口。

实现前核对标签页意图所需的存储变化；若涉及持久化结构，遵循仓库的 schema、迁移和版本指纹要求，不修改已发布迁移。只增加可选 JSON 字段时，也需覆盖旧数据读取和字段往返。聊天本地链接复用既有消息正文，不为相对路径基目录增加字段或迁移。

## 9. 验收标准

| 场景 | 必须观察到的结果 |
| --- | --- |
| 无项目会话打开桌面或下载目录 HTML | file URL 在内置浏览器显示页面；普通绝对路径经只读适配显示源码，均不要求创建项目或重复确认 |
| 同一 HTML 的普通路径与 file URL | 分别打开源码和页面，可同时存在；规范化目标相同不会丢失意图或错误合并 |
| file URL 与扩展名分派 | 不因 HTML 扩展名将普通路径送进浏览器；不支持的浏览器类型明确提示，不自动切文件视图 |
| 点击绝对 file 链接 | 文件名、地址与页面内容匹配；不打开 daemon 页面 |
| Markdown 同目录及 `../` 链接 | 按源文档位置定位并默认查看文件；用户可显式选择浏览器预览 |
| 裸 `/...`、地址栏和浏览器内部链接 | 本地聊天/Markdown 按绝对路径打开文件；地址栏按浏览器意图；HTML 内部导航仍留在浏览器规则内 |
| 聊天输出本地文件链接 | 引用/检查文件用普通绝对路径，页面预览用 file URL；显示名可短，结构化引用保留实际目标与动作 |
| 多 roots 同名文件 | 两个绝对链接分别打开正确文件；源 Markdown 的相对链接只按各自文档位置解析 |
| 已知目标缺失、另一 root 有同名文件 | 报原目标不存在，不换目标 |
| 模型或旧消息给出无来源的相对链接 | 明确提示无法定位，可选择本次目标；不猜当前项目、primary root 或命令 cwd |
| 多模式、无项目 scratch 与结果摘要 | 工具可用相对入参，聊天交付绝对定位并表达意图；缺少真实路径时回读结果，不能编造 |
| 流式、历史恢复及 roots 重排/增删 | 消息正文中的绝对目标和打开意图保持，不因项目配置或文件存在性重新解释 |
| query、hash、中文、空格及编码后的井号 | 编码不改变真实文件名；源码 `#L12` 与浏览器 hash 分开，页面参数不进入文件名 |
| 项目外 HTML 源码读取 | 普通文件、大小/编码边界生效；内容只作为文本，不执行 HTML，也不能写入或列目录 |
| 显式切换与复制 | 查看源码、浏览器预览保持同一文件；复制路径与复制预览链接产生各自正确表示 |
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

- 链接解析单测覆盖同目标不同意图、源文档、无来源聊天、多目录同名/缺失、URL 编码和 query/hash。
- Go 测试覆盖聊天输出规范、真实路径结果、用户/工具入口及授权生命周期契约；旧聊天正文读取不依赖新增元数据。
- Electron 单测覆盖请求来源、浏览器资源范围、项目外 HTML 单文件只读、授权销毁与无 fallback 行为。
- 真实 Electron smoke 使用临时目录和 HTML/CSS/JS/图片 fixture，检查页面内容、交互、实际资源请求和最终打开位置。
- 桌面回归先确认运行的是当前源码开发版，避免误用已安装的正式版；测试文件与数据只放临时目录。

按实际改动运行 `npm --prefix web test`、`npm --prefix web run build`、`npm run test:electron`，以及相关 Go 包的 `go test -tags 'sqlite_fts5 webrtcaec'`；跨模块与 schema 变更按 [AGENTS.md](../AGENTS.md) 补齐对应检查。此列表是实施验收要求，不是本次文档交付已运行的结果。

## 10. 实施顺序与工作量

按一名熟悉项目的开发者、具备可运行当前源码的 macOS/Electron 环境估算：

| 阶段 | 交付内容 | 估算 |
| --- | --- | --- |
| 1 | 共享解析与打开动作，修正空 href、外部打开和 HTML 分流 | 1 人日 |
| 2 | 任意位置 HTML、受控资源协议、预览目录与授权生命周期 | 2–3 人日 |
| 3 | 项目外 HTML 单文件只读适配与侧边栏源码展示 | 1–2 人日 |
| 4 | 聊天绝对定位和打开意图规范、真实引用、多目录歧义与历史展示 | 1 人日 |
| 5 | 真实桌面回归、边界修正、清理旧打开路径 | 1–2 人日 |
| 合计 | 本文首版范围完整交付，含项目外 HTML 源码读取 | 6–9 人日 |

这些阶段属于同一交付范围。完整接通后删除被替代的产品加载路径，不保留绕过目录或来源检查的第二条 fallback。

估算不包含项目外所有文件类型的预览适配、新的通用工具审批系统、目录热更新或复杂前端项目的构建服务。需要 dev server 的项目仍使用其 HTTP 地址；本功能负责静态文件与浏览器可直接运行的页面。

## 11. 参考

- [Electron：本地页面优先使用自定义协议](https://www.electronjs.org/docs/latest/tutorial/security#18-avoid-usage-of-the-file-protocol-and-prefer-usage-of-custom-protocols)
- [Electron protocol：session、scheme 与资源处理](https://www.electronjs.org/docs/latest/api/protocol)
- [Electron shell：系统外部打开](https://www.electronjs.org/docs/latest/api/shell)
- [HTML：链接元素及 href 解析](https://html.spec.whatwg.org/multipage/links.html#dom-hyperlink-href)
- [CommonMark：行内链接语法](https://spec.commonmark.org/0.31.2/#links)
