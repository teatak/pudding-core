# Codex Computer Use 实测与 Pudding 差距分析

日期：2026-09-08。范围：Codex 桌面端当前 `mcp__cua_repl`、iPhone 镜像中的京东外卖，以及本工作区当前 Pudding 源码（包含未提交改动）。不是对 OpenAI Responses API 的比较，也不是发布版验收。

本轮只操作 Codex 工具、检查代码和编写文档，不修改 Pudding 业务代码。用户随后要求停止购买；未加购、未创建订单、未付款。

后续实施与当前验收结果见 [后台输入原型记录](computer-use-background-input-probe.md)；以下保留当时的对比结论，不把建议追记为已上线能力。

## 1. 结论

1. **Codex 可以通过 iPhone 镜像操作京东，用户现场确认了后台操作。** 本次已打开京东、进入外卖、搜索商家并进入店内菜单。不能再把“我们的简单 PID 投递没有成功”解释成“macOS 或 Codex 不支持后台点击”。
2. **Pudding 不是完全不能后台操作。** AX 语义动作已经支持后台；坐标指针走全局 HID 事件流，并主动要求目标前台、命中窗口无遮挡。镜像里的手机控件没有 AX 元素，所以主要落入受前台限制的指针路径。
3. **差距主要在原生输入后端，不在有没有 observationID。** Pudding 当前已经没有 observationID 参数、消费和过期门槛，已经支持 1–32 个动作的统一数组，也不会每个动作后自动观察。
4. **Codex 也不是所有动作都稳定。** 本次中文 `typeText` 未产生预期文字；滚轮有时只推动外围区域，未稳定推进商品列表；一次 `paste` 超时后出现了非本次提供的剪贴板内容。需要分别验收点击、滚轮、键盘和跨设备剪贴板。
5. 优先补“可靠的后台定向输入验证”和“视觉任务的输入/观察闭环”，其次减少模型往返与观察输出体积。不要通过删除全局指针的前台保护来冒充后台能力。

## 2. 证据边界

| 证据 | 本轮能证明什么 | 不能据此推断什么 |
| --- | --- | --- |
| Codex 工具返回的运行时文档 | 当前 App/Tab 方法、参数、批量编排和观察规则 | 原生服务内部具体采用哪个 macOS 事件 API |
| Codex 的实际调用和返回截图 | 点击、页面变化、输入失败及粘贴超时后的实际界面 | 每次动作都完成业务目标；任意第三方 App 都兼容 |
| 用户现场观察 | 本次存在后台操作成功 | 本轮未采样前台 PID/真实鼠标轨迹，未证明内部完全没有瞬态切换 |
| Pudding 当前源码 | 前台判断、投递方式、动作数组、观察和错误语义 | 当前源码在所有签名/系统版本上的真实表现 |
| 本机 Codex 客户端代码 | App 定向请求经过 IPC 发给原生服务 | JS 中没有 `foreground` 就等于原生实现没有任何焦点处理 |

公开说明确认 macOS 支持限定任务的后台使用，并区分系统权限与 App 授权；它没有公开后台事件实现算法。[OpenAI Computer Use 官方说明](https://learn.chatgpt.com/docs/computer-use)

本轮没有重置 TCC、修改权限、重启用户应用、切换 Pudding 开发/发布版本，也没有通过 shell、AppleScript 或 Pudding 工具代替 Codex 操作界面。对已安装 Codex 客户端/二进制的检查仅用于只读分析。

## 3. Codex 当前接口怎么用

### 3.1 入口与对象

本次入口是持久 JavaScript 会话 `mcp__cua_repl.js`。首次选择 App 会返回运行时文档和初始 AX 状态；不清楚应用身份时先用 `cua.getState()` 枚举可用 App 与浏览器。

```javascript
// 入口调用单独执行；先阅读返回的文档和状态。
var app = await cua.getApp("com.apple.ScreenContinuity");
```

`getApp` 接受名称、bundle ID 或完整 App 路径。随后复用对象；无需每次在模型参数里重复 App 和窗口。底层仍然传递 App 目标，并不意味着输入发送给任意当前前台窗口。

### 3.2 一个调用中组织多个动作与一次观察

```javascript
// 示例：elementIndex 必须来自当前实际 AX 状态，不能照抄数字。
await app.click(elementIndex);
await app.setValue(elementIndex, "example");
await app.pressKey("Return");
await app.getAXState();
```

这是 JavaScript 的顺序编排，不是“任意长的原子事务”。示例只适用于同一已知控件、后续目标不依赖中间页面判断的场景。进入新页面后还未确认输入框可用，不能仅因代码可连写就盲目立即输入。

本次实际采用了多次 `click → getAXStateAndScreenshot`、`scroll → getAXStateAndScreenshot`，不用在两者之间再经过一轮模型推理。中文 `click → typeText → observe` 的组合虽能调用，但未获得预期输入效果。

### 3.3 观察与标识规则

- `getAXState()`：默认返回相对上次 AX 树的增删改差异。
- `getScreenshot()`：只取截图。
- `getAXStateAndScreenshot()`：同时返回 AX 状态和图像；本次镜像任务主要使用它。
- `{disableDiffing: true}`：请求完整 AX 树。
- `{emit: false}`：不把观察结果直接展示到模型输出；不等于停止读取。
- AX 动作用数字索引；坐标动作用 `[x, y]`。没有让调用方提供 observationID。
- **Codex 工作流仍要求在一个或多个动作后读取 AX 状态，再决定下一步，并从最新结果重新确定索引。** 它不是“旧索引永远有效、完全不需要观察”；只截图后，再依赖 AX 索引之前需要完整树。
- 文档说明观察内部有适当等待，不要求模型添加固定 sleep。不过本轮仍捕获到启动页、骨架屏和转场，因此不能把这个等待等同于业务页面加载完成。

### 3.4 暴露的动作

| 类别 | 当前 Codex 方法 | 本轮验证情况 |
| --- | --- | --- |
| 点击 | `click(index 或 [x,y], {mouseButton, clickCount})` | 原生“连接”按钮与多个手机页面坐标点击生效；未测右键/双击 |
| 拖拽 | `drag(from, to)` | 未测试；镜像专属说明要求滚动用 scroll，不用 drag 代替 |
| 滚动 | `scroll(target, direction, pages)` | 能调用，部分区域发生位移；商品列表推进不稳定 |
| 按键 | `pressKey("Return")`、组合键等 | 文档支持；本次未单独验证 Return 的业务效果 |
| 输入 | `typeText`、`setValue`、`paste` | 镜像中文 typeText 未生效；paste 出现超时及非预期内容 |
| 文本选择 | `selectText(index, text, {prefix,suffix,selectionType})` | 文档支持消歧与光标位置；本次未测 |
| AX 次级动作 | `performSecondaryAction(index, action)` | 调用了实际暴露的 Raise；不能由返回成功推断输入已经可用 |

工具还在选择 iPhone 镜像后提供了少量应用专属说明：Cmd+1 主屏幕、Cmd+2 App 切换器、Cmd+3 Spotlight；图标点击中心；滚动用 scroll。这些是平台导航知识，不是固定业务点击脚本。

## 4. 京东实测记录

目标：在京东外卖查找香河肉饼中的韭菜牛肉饼和黄金粥。只记录任务相关结果，不把账号、地址、旧剪贴板正文或原始截图写进仓库。

| 阶段 | 实际动作 | 结果 |
| --- | --- | --- |
| 发现入口 | `cua.getState()`，选择 iPhone 镜像 | 找到运行中的镜像 |
| 恢复连接 | 点击 AX“连接”按钮，读取图像 | 从“iPhone 使用中”界面恢复到手机主屏幕 |
| 打开京东 | 根据截图点击京东图标中心 | 先看到启动画面，之后进入首页 |
| 无关权限 | 拒绝京东读取此前剪贴板内容 | 回到首页；未允许读取无关内容 |
| 进入外卖 | 点击首页外卖入口 | 进入秒送/外卖页面 |
| 搜索商家 | 打开搜索框并 typeText；随后用已显示的“香河肉饼”搜索词 | 输入未出现；点击现有相关词后商家列表出现 |
| 查看菜单 | 点击“香河肉饼北苑店”、分类和滚轮 | 看到多个肉饼＋黄金粥商品；没有确认到用户指定的韭菜口味 |
| 店内搜索 | 点击搜索框并 typeText“韭菜” | 未出现预期文字；后来 Raise 后再试仍未成功 |
| 粘贴实验 | `paste("韭菜", {format:"text"})` | 返回 `-10005: Timed out waiting for the application to read the clipboard` |
| 检查未知结果 | 不重放，先取截图 | 搜索框出现另一段剪贴板文字，并形成一条店内搜索记录；已清空输入框，未清除历史记录 |
| 停止 | 用户明确要求只完成文档 | 停止 UI 操作，没有加购/下单/付款 |

三个值得保留的失败样本：

1. **AX 无变化不是视觉无变化。** 手机页面已从首页变为商家列表和菜单，AX 经常只显示镜像外壳按钮且报告“无变化”。只观察 AX 会导致错误结论和无意义重复调用。
2. **能批量调用不等于能跨转场盲批。** 搜索框出现前后存在异步时序；本次 typeText 失败尚未完成对焦点、输入方式和镜像传输的因果隔离，不能武断归咎于模型、中文或后台状态中的某一个。
3. **粘贴超时不表示没有副作用。** Codex 文档称原生 paste 会恢复原剪贴板。截图证明本次最终输入不是所提供的文字；“镜像读取较晚，与恢复剪贴板竞态”是待验证假设，尚未证明。也不能宣称旧内容没发到京东：它已经进入搜索界面并留下记录。

## 5. 后台操作：到底差在哪里

### 5.1 Codex 已查到的链路

```text
app.click([x, y])
    → JS 客户端携带 app + click action
    → ComputerUseIPCAppPerformActionRequest
    → 本机 native pipe
    → 独立 Codex Computer Use 原生服务
    → 指定应用的实际 UI 效果
```

本机客户端中 `targets/mac/click.js` 调用 `client.click({app, elementIndex, x, y, ...})`；`client.js` 构造 App 定向的 `performAction` 请求；`native-pipe.js` 串行发送带请求 ID、截止时间和 turn metadata 的 IPC。

本次检查到的 Helper 版本：`26.831.1000926`，build `1000926`。其原生服务不是本仓库源码。二进制中能找到 `VirtualCursor` / `targetWindowID` 等名字，但**这些字符串不足以证明具体执行算法**；不把它们当作“使用某个私有 API”或“只需 CGEvent.postToPid”的依据。

因此可以确认：后台能力封装在原生服务层；模型不需要每次要求用户切到前台。可以合理推断它做了有效的目标 App/窗口输入路由，但本轮无法确认事件字段、焦点处理或底层函数的完整实现。

### 5.2 Pudding 当前指针链路：前台限制有明确代码原因

源码：[PointerAction.swift](../native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/PointerAction.swift)。

```text
act.actions[] 的归一化窗口坐标
    → frame.x/y + x/y × frame.width/height
    → 检查 foreground PID、点击位置 topmost window
    → 先发送真实 mouseMoved
    → CGEvent.post(tap: .cghidEventTap)
    → 系统全局指针事件流
```

直接证据：

- `PointerCoordinatePolicy.globalPoint` 把窗口归一化坐标换成全局点。
- `PointerService` 默认 `postEvent` 调用 `.cghidEventTap`，不是 App 定向的输入接口。
- `requireCurrentTarget` 要求 `frontmostPID == pid`，再验证每个位置命中目标 PID/windowID；`isCurrentTarget` 在动作中再次验证。
- 动作前还会发送 `.mouseMoved`。因此现实现不仅要求前台，也会参与真实桌面指针状态。

**仅删除 foreground/topmost 判断，不会把全局事件变成后台定向事件。** 被遮挡时，全局坐标对应的是另一窗口，存在误点；这是当前后端下必要的路由保护，不是 observationID 那种模型快照门槛。

### 5.3 Pudding 已经能后台执行的部分

- AX `press` / `set_value` / `select` 等直接操作目标 AX 元素，不必先激活应用。[AccessibilityService.swift](../native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/AccessibilityService.swift)
- 独立窗口截图用 `SCContentFilter(desktopIndependentWindow:)`；视觉观察与全局鼠标点击不是同一种能力。[ScreenCaptureService.swift](../native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/ScreenCaptureService.swift)
- `use_app` 默认后台；显式显示才调用前台策略。[ForegroundPolicy.swift](../native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/ForegroundPolicy.swift)

镜像手机内容不暴露 AX，所以不能靠增加 AXPress 等动作解决手机内部点击。Pudding 的 `type_text` / `paste` 还要求 AXFocusedUIElement 是已启用的文本框/文本区/组合框；自绘/镜像没有对应控件时，会被明确拒绝。这与 Codex 本次“调用返回但预期文字未出现”是两种不同表现。[KeyboardInput.swift](../native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/KeyboardInput.swift)

### 5.4 之前 postToPid 失败意味着什么

[现有设计记录](computer-use-design.md) 的 2026-09-08 镜像实验写明：简单 `CGEvent.postToPid` 加目标窗口字段，后台和前台都没有打开时钟；Codex 对照点击能打开。

这支持“那份事件实现不完整或不兼容目标”的判断，不支持“后台坐标点击不可能”。本次增加了京东场景和用户的后台观察，应该把“可靠后台输入”列为明确技术差距，而不是用前台模式作为最终答案。

## 6. 与当前 Pudding 的逐项对照

| 项目 | Codex 当前接口/本次观察 | Pudding 当前代码 | 判断 |
| --- | --- | --- | --- |
| 目标选择 | `getApp` 返回可复用对象并带初始 AX | `use_app` 返回 PID 绑定窗口；`observe` 另调 | Codex 减少首次接入的一次模型往返；Pudding 多实例选择更显式 |
| 单步/多步 | JS 多次 await；可在同次调用取最终状态 | 统一 `actions[]`，1–32 项 | Pudding 已有批量；不用再次设计 press_sequence/action_sequence |
| observationID | 调用方无此参数；要求决策时刷新 AX 索引 | 无此参数、消费和 expiresAt 门槛 | 不应继续归因于旧快照限制 |
| 稳定目标 | 数字索引来自当前 AX 输出 | identifier/父作用域/语义或路径生成 ID，执行时 live resolve | 策略不同；不能保证任一侧跨任意重排永久稳定 |
| 动作后观察 | 可合并到同次 JS 调用 | act 不自动 observe，observe 是独立工具 | 可补显式可选的批末观察，不恢复强制逐步观察 |
| AX 输出 | 默认差分、可取完整树 | 有界完整元素数组，默认 200，最多 1000 | 大树可评估差分/局部查询；镜像则应减少无用 AX |
| 坐标 | `[x,y]`；本次按返回图坐标点击有效 | 窗口归一化 `[0,1)` 坐标 | 都能表达；后端路由比参数形式更关键 |
| 后台坐标 | 本次点击成功，用户确认后台 | 全局 HID＋foreground/topmost 校验 | 当前核心差距 |
| 文本输入 | 有 typeText/paste；镜像本次失败 | Unicode/物理键/粘贴已实现，但文本输入依赖 AX 可编辑控件 | 需要镜像/自绘场景专项验证，不等于再增加一个输入动作名 |
| 粘贴 | 原生文档承诺恢复剪贴板；本次出现未知副作用 | 明确覆盖并保留提供的文字 | 不应未经验证复制自动恢复；现有 Pudding 也未证明镜像兼容 |
| 失败结果 | 本次 paste 为明确超时错误，但副作用需另查 | completedCount/failedIndex/partial/unknown；停止、不重放前缀 | 保留 Pudding 的结构化失败语义 |
| 应用指导 | 镜像专属快捷键和操作规则 | 主要是通用 Computer Use skill | 可加短小、经过实测的应用指导 |
| 过程旁白 | JS 内可多动作，不必每步一段话 | skill 已要求不播报常规步骤 | 再叠一层相同提示词不是主要修复 |

Pudding 依据：[工具定义](../internal/tool/builtin.go)、[工具执行/解码](../internal/tool/computer.go)、[动作队列与校验](../internal/computer/manager.go)、[当前内置 skill](../internal/app/builtin.go)、[元素身份](../native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/ElementIdentity.swift)。

## 7. 优化建议与验收门槛

以下均是建议，不是本轮已实现的能力。顺序按本次发现的实际瓶颈排列。

### P0：验证并实现真正的后台定向输入

目标是同时满足“指定 App 有效果”和“用户前台操作不被影响”，不是只去掉错误提示。

1. 用签名 Fixture 记录收到的事件和实际状态，给后台输入做独立原生技术验证。对照现有全局 HID 和简单 PID 投递，查清窗口寻址、坐标空间、事件元数据、应用激活/焦点语义中的差异；不要在产品里排列一串碰运气的 fallback。
2. 分别验证普通 AppKit、Electron、iPhone 镜像。背景可见和完全遮挡分开测；点击、双击、右键、拖拽、滚轮、按键也分开验收，不能由单击推广到全部动作。
3. 记录动作前后 foreground PID、真实光标位置、目标窗口效果；并测试另一前台应用持续输入、窗口移动/缩放/关闭、取消及权限撤销。动态像素变化本身不等于正确业务效果。
4. 验证通过后，让 native dispatcher 根据**明确选择的输入模式**投递：AX 语义、后台定向指针、需用户配合的前台指针是不同能力边界。共用现有 session/App 授权、目标身份和队列，不能建立第二套会话事实源。
5. 后台路径不能执行时返回具体的“不支持/目标变化/结果未知”，不能暗中激活目标再重试。前台模式仍保留其 foreground/topmost 保护；后台模式去掉不适用的前台要求，但保留 PID/window/坐标与 App 授权校验。

若最终必须依赖非公开系统接口，先单独评估系统版本兼容、签名/发布限制和维护成本；本轮没有证明 Codex 使用了哪一个非公开接口，也不能承诺只替换一个 CGEvent 调用即可完成。

### P0：把镜像/自绘输入作为独立验收场景

- 不把“没有 AX 文本框”直接当成“用户界面不可输入”，但也不能简单放开当前文本输入 guard 后宣称安全可靠。
- 在已授权的精确目标窗口、用户明确选择的输入路径下，分别证明物理键、Unicode 和剪贴板能否到达真实输入目标；读回文字或检查图像后才报告完成。
- 先验证无密码、无支付的 Fixture 和镜像搜索框。没有 AX 时不能沿用“检查了 AX secure field 就肯定不是敏感输入”的结论。
- 粘贴失败一律允许“已产生副作用”的结果。跨设备读取时序、剪贴板归还、用户并发复制分别测试；不在超时后自动重试，不在未经验证时自动恢复旧剪贴板。

### P1：允许显式批末观察，减少模型往返

保留现有 `actions[]` 为唯一动作结构，默认不观察。可给该调用增加显式可选的“末尾取 AX / 图像 / 两者”，由 LLM 按任务决定；不是把 observe 伪装成动作数组里的另一种输入。

- 多个目标已知、不会因前序操作移动：一次批量动作，末尾可取一次结果。
- 下一步目标依赖转场：结束当前批次并取所需状态，再由模型决定。
- 观察失败要和动作结果分开，不能把已经完成的动作变成可整体重试。
- 超时、部分执行继续保留 completedCount/failedIndex/unknown；不要重放完成前缀。
- 初次 `use_app` 是否携带观察也应可选，用户只说“打开”时仍不要额外读取内容。

验收：同一组固定控件操作减少模型调用数，且不增加截图数；镜像新页面不能因省观察而盲点；部分失败不重放。

### P1：按通道观察，处理转场而非机械等待

- 现有 `includeScreenshot=true, includeAccessibility=false` 已可用于纯视觉观察。镜像内容缺少 AX 的事实明确后，优先该路径，不把“AX 无变化”解释成任务完成或页面没动。
- 在用户/模型明确请求观察时，可评估有上限的取帧稳定策略或条件等待，把转场中的低价值截图留在 native 内；不在每次动作后自动录屏、不默认增加等待，不把稳定帧标记成任务成功。
- 若增加 AX 差分，只作为 session/目标限定的输出压缩；完整树仍可重建，差分缓存不能成为动作授权或历史事实源。
- Pudding PiP 帧当前只用于展示，不直接拿它的旧帧当模型所需的最新观察；不要为省调用悄悄改变这一语义。

### P1：小范围修正工具文案与错误来源

当前 `NormalizeActions` 对错误动作类型的报错只枚举 `press,set_value,select,submit,click,drag,scroll`，而工具 schema 已包含 `focus,select_text,press_key,type_text,paste`。这是可直接从代码证明的错误说明漂移，应由同一动作定义生成枚举/报错，避免继续重复维护。

内置 App 简介仍强调“explicit Accessibility actions”，但当前已支持键盘/指针。可以同步纠正定位说明，并保留现有批量示例和 `actions[index]` 校验错误路径；不重新引入旧 action_sequence 格式。

### P2：精简应用专属指导与操作展示

- 在选择镜像时返回已验证的导航快捷键、滚动方式及 AX 内容有限的说明；不写死商家、商品或屏幕坐标。
- 技能里的“使用场景/决策原则”与 schema 的“字段/错误结构”各保留一处权威定义，减少大段重复规则。
- 继续利用工具行展示步骤与结果，用户接管时给出一个明确动作，不靠每个点击后生成自然语言旁白。

## 8. 后续验证清单

| 场景 | 核心断言 |
| --- | --- |
| AppKit / Electron 后台控件 | 用户前台与光标不变，目标控件状态确实改变 |
| 镜像后台指针 | 手机已连接；点中目标；另一 App 不收到点击；覆盖/移动窗口有明确结果 |
| 镜像滚轮 | 指定滚动区域确实位移；不把外围页面位移当商品列表位移 |
| 输入与剪贴板 | 中文/emoji/组合键分别验证；实际文字等于给定文字；超时不重放，不泄漏旧剪贴板 |
| 批量与观察 | 已知目标一次提交；依赖新页面时停止批次；部分失败不会重放前缀 |
| AX/图像通道 | AX 空树不阻断图像；单通道失败不抹掉另一通道结果 |
| 权限与取消 | 仅未执行的权限失败可以恢复；取消后不继续输入；不申请额外无关权限 |
| 环境 | 源码 Electron 以完整路径识别；不操作已安装发布版或用户正在运行的其他任务 |

本轮验证结果：完成 Codex 真实 UI 实验、Pudding 调用链检查及文档链接/diff 检查。没有跑 Pudding 全量测试、没有执行新的后台输入原型、没有验证其正式签名兼容性。不能将上表记作已通过。

## 9. 参考定位

- [OpenAI Computer Use 官方说明](https://learn.chatgpt.com/docs/computer-use)：公开能力和权限边界，已通过官方文档工具读取。
- 本次 `mcp__cua_repl.js` 首次返回的 Computer Use API/Workflow/Notes 与 iPhone Mirroring Instructions：当前方法与使用规则，版本变更后应重新获取，不直接套用旧工具说明。
- 本机只读检查：`/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/@oai/sky/dist/project/cua/sky_js/src/targets/mac/` 下的 `click.js`、`client.js`、`native-pipe.js`；这些是本次安装版的实现线索，不是 Pudding 可依赖的公开 SDK。
- [Pudding 现有设计与历史验证](computer-use-design.md)。
- [Pudding 动作语义测试](../internal/computer/manager_test.go)：包含不自动观察、失败停止、不重试、跨 session 串行等用例；本轮只检查代码，没有重跑。
- [Pudding 指针测试](../native/macos/computer-use-helper/Tests/PuddingComputerUseHelperTests/PointerActionTests.swift)：当前测试明确要求前台，需随经过验证的新输入模式同步调整，不能只删除测试。
