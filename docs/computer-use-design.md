# macOS Computer Use 设计与实施计划

> 状态:C0–C3 已实现；源码支持后台单击/双击/右键/拖拽/滚轮，无兼容 App 白名单。本次后台默认值和提示词调整尚未打包发布；历史候选包验收不覆盖本次修改。
>
> 更新:2026-09-09
>
> 范围:macOS 桌面应用观察与操作。
>
> 不包含:操作录制、示范学习、工作流录制回放、Windows。
>
> 目标工期:1 名熟悉项目的工程师约 6 周交付可用 MVP,10–14 周达到产品级稳定性。

## 0. 决策摘要

1. Computer Use 作为内置 App 按需加载,不把桌面控制工具常驻全部会话。
2. Go daemon 负责 session 路由、工具生命周期、审批和全局动作串行化。
3. Electron main 负责 macOS 原生能力和权限界面,通过签名 Swift Helper 调用 Accessibility 与 ScreenCaptureKit。
4. 所有操作显式携带 `sessionID`、`appID` 和 `windowID`;后端不保存 observation、“当前应用”或“当前焦点”。
5. 原生控件操作支持 `press`、`set_value`、`select`、`submit`、`focus`、`select_text`；键盘和显式前台指针操作验证指定 PID、窗口及前台状态；指针默认后台投递，范围和恢复边界见下节。
6. 写操作不自动重试。结果不确定时先检查当前状态，不重放；只有窗口失效时才通过 `use_app` 重新获取窗口。
7. 当前能力不监听键盘或鼠标、不录制工作流；支持明确请求的物理按键、组合键、Unicode 文本输入及粘贴。
8. 应用生命周期采用 session ownership:只有当前 session 新启动并取得 `launchID` 的进程才能普通退出;已运行应用永不归属,永不强杀。

### 2026-09-09 后台指针投递

已接入当前源码的 `builtin_computer_act.actions[] → Go Manager → Electron bridge/权限协调 → Swift Helper → 同一可执行文件的短生命周期 worker`。这是现有生产代码路径的接入，不是已安装 release 的发布验收；没有替换或重启正在运行的开发/发布应用。

- 每项 pointer 省略 `delivery` 或传 `delivery: "background"` 均走后台路径；只有显式 `delivery: "foreground"` 才走前台 HID 路径。Go action 规范化和原生 CLI/protocol 校验均落实此默认值，Electron 只透传、不引入另一套默认策略。后台支持左/右键单击、左键双击、左键拖拽和双轴像素滚动。已删除原系统计算器/邮件/日历兼容白名单；仍保留 session/App 审批、既有受保护 App 策略、真实 PID 启动标记及 bundle/可执行文件身份校验。没有自动前台 fallback；移除白名单不代表所有 App 都兼容。后台失败本身不证明必须前台：窗口失效先重新获取窗口，权限失败交由权限引导，效果未知先检查而非重放。键盘目前仍要求前台；可用后台 `set_value` 或其他后台动作完成时，不应为方便而切到键盘/前台。只有真正需要前台或用户明确要求时才使用前台路径，切换焦点需事先获准。这是工具默认值与模型行为约束，不新增第二套运行时授权状态。
- 复用原有 session/App 审批、全局动作队列、`completedCount` / `failedIndex` / `outcome`，不增加工具名称、observationID、过期时间、单次消费或强制观察。观察仍由模型决定。结果中的 `delivery` 必须与请求匹配，不能把前台投递冒充后台成功。
- 事件使用窗口内坐标及 PID 定向投递，围绕确定的手势事件计划发送合成 AppKit 激活/去激活通知；不调用实际 activate/raise、全局鼠标移动或全局 HID。目标 App 仍可能自行激活；检测到系统前台变化后停止后续事件并返回不确定结果，不能承诺任意 App 都不置顶。它会临时影响目标内部 active/key 状态，不能表述为“内部焦点完全不变”。用户真正激活目标后，不反向去激活。
- 每步检查进程启动标记、窗口身份/几何、权限及系统前台是否变化。检查的是执行时事实，不要求模型刷新快照。窗口内坐标依赖非公开 `CGEventSetWindowLocation`，符号缺失直接失败，不推断其他 macOS 版本兼容。
- 操作开始时绑定 App 和真实窗口；执行及恢复中由 `proc_pidinfo` 启动标记和 `proc_pidpath` 可执行路径确认进程身份，不反复依赖 `NSRunningApplication` 的瞬时 App 元数据。身份读取失败不等于目标已退出，不能因此清空 journal；已确认 PID 消失或身份改变才跳过对旧目标的恢复。worker 向已退出父进程写回结果使用可抛 Swift I/O，不再因 broken pipe 触发未捕获的 Objective-C 异常。
- 窗口身份和可见性共用 WindowServer 全窗口查询。新输入仍要求正常可见窗口及原几何；恢复允许同一 PID/window ID 的隐藏、最小化窗口收到一次对应 mouseUp。窗口不可见不等于已销毁；查询不可用或元数据不完整时抛错并保留未解决 journal，不将查询失败当作空列表。`NSWindow.close` 与系统窗口销毁不是同一时刻，窗口仍存在时允许清理，确认不存在后不再向旧窗口投递。
- Helper 和 worker 共用每用户临时目录中的单字节恢复 journal；以 PID + 进程启动标记加独占锁，记录激活/按下/释放阶段；低两位为阶段，高六位为当前按下/拖拽事件索引。父子进程共享同一确定性事件计划，异常释放使用对应按钮、点击次数和最后 journaled 坐标。双击按 `1/1/2/2` 发送，拖拽发送 down、8 个中间位置和 up；滚动没有按住按钮。AX、键盘和前台指针也取得同一目标输入锁，防止 Helper 重启后与尚在恢复的 worker 重叠。它不是第二套权限或会话状态。
- 调用方退出后，worker 通过控制管道 EOF 停止后续事件并恢复；worker 退出后，调用方必须先确认其已退出才接管恢复。取消仍沿用 Host 的有界 drain，已在途手势可能完成，不保证瞬时撤销。中断后可能由清理的 mouseUp 完成点击或拖放，返回 `unknown`，不重放。双方同时强制退出不能保证立即恢复；遗留非 idle journal 会拒绝继续输入，须重启目标应用。进程退出与事件投递之间不提供指令级恰好一次保证。

最小示例（窗口和坐标须替换成实际已知目标）：

```json
{"appID":"com.apple.iCal","windowID":42,"actions":[{"type":"click","x":0.5,"y":0.4}]}
```

后台双击使用 `type:"click", clickCount:2`；右键使用 `type:"click", button:"right"`。拖拽使用 `type:"drag", x, y, toX, toY`，滚动使用 `type:"scroll", x, y, deltaX, deltaY`。每项均可省略 `delivery`，与单击共用原 `actions[]`；无需 observationID 或每步重新观察。

通用手势的隔离真实回归（实际源码 Helper 投递，原型只作接收/监测）：

```sh
node scripts/computer-use-background-pointer-smoke.cjs /absolute/current/Helper appkit
node scripts/computer-use-background-pointer-smoke.cjs /absolute/current/Helper electron
node scripts/computer-use-background-pointer-recovery.cjs /absolute/current/Helper
node scripts/computer-use-background-window-lifecycle.cjs /absolute/current/Helper
```

本次默认值回归（2026-09-09）：`make test`、216 项 Electron 测试、102 项原生测试通过；Go 与原生 CLI/protocol 覆盖省略、显式后台、显式前台三种输入，参数错误和后台失败不自动观察、重试或激活。临时开发签名 Helper 通过上面两个 pointer smoke，在 AppKit/Electron 独立接收窗口各执行 5 种省略 `delivery` 的手势，10/10 实际效果通过；共 1228 次采样，鼠标最大位移 0，前台和窗口遮挡顺序不变，前台守卫无误输入。未替换或重启现有 Pudding；这不是已安装 release 或真实 LLM 的验收。

以下日历和候选包记录属于本次扩展之前的有限单击阶段，不是本次新增手势的 release 验收。

真实链路回归入口：

```sh
node scripts/computer-use-background-integration.cjs /absolute/path/to/current-source/PuddingComputerUseHelper
```

该手动测试要求已有权限、中文系统日历月视图及唯一窗口；只切换下一月/上一月，以独立前台窗口遮挡目标并采样。Go 测试默认跳过，runner 才显式启用。新 Helper 的日历回归确认 `2026年9月 → 10月 → 9月`，575 次采样前台不变、鼠标位移 0、guard 无误输入。首轮鼠标位移约 399 pt 的失败记录保留，没有覆盖或当成通过，详见[第十一轮记录](computer-use-background-input-probe.md#第十一轮有限后台单击接入现有产品链路)。

随后使用当前源码 Helper 完成 [9 项真实进程回归](computer-use-background-input-probe.md#第十二轮新-helper-真实崩溃取消和竞争回归)：正常单击；Helper/worker 在三个 journal 阶段被 SIGKILL；Host signal 取消；两个独立 Host 同目标竞争。9/9 通过，恢复 journal 均为 idle，日历恢复原月份，3006 次采样前台不变、鼠标位移 0。取消时已在途单击仍可能完成，正确返回 `unknown`，不是回滚保证。此测试不经过完整 Engine 会话，也没有接收端内部 active/key 探针。

Engine 取消链路补充：工具已经返回的实际结果必须先进入 canonical messages 并发布，再结束取消 turn；不能丢失完成前缀或未知效果。Manager 每个批内动作派发前检查现有 context，停止未开始项，保留在取消 drain 期间完成的项。5 个确定性 Engine/审批/Manager 场景及 20 轮竞态回归通过；重新构造 Engine 后可从 canonical 历史读到原部分结果，不自动重放。见[第十三轮](computer-use-background-input-probe.md#第十三轮engine-会话链路与取消记录)。

上述 runner 加 `engine` 参数的完整日历往返已通过：脚本模型经过 App 加载、一次 session/App 审批、访问已有窗口和一次观察，以单个 `actions[]` 完成 `9月 → 10月 → 9月`；canonical 工具结果包含两项后台成功投递。真实 Engine/Manager/Electron bridge/Helper/worker 链路测试用时 3.20 秒，799 次采样前台不变、鼠标位移 0、遮挡窗口无误输入，临时进程已清理。使用隔离 store，未操作当前 daemon 或安装包 UI；不等于真实 LLM 或 release 验收。

随后 runner 的 `engine-sessions` 模式（Go `-race`）完成[原生多会话/取消三项回归](computer-use-background-input-probe.md#第十四轮原生-engine-多会话与取消)：两个会话串行、取消排队会话不派发、第二步按下时取消在途批次。3/3 通过，2228 次采样前台不变、鼠标位移 0。已在途第二次点击仍完成，但第三步未派发，canonical 保留第一项完成和第二项 unknown；测试观察实际月份后另发反向导航恢复原月份。仍使用脚本模型和隔离 store，不承诺取消回滚或任意瞬间恰好一次。

当前源码 0.3.0 的 arm64/x64 Developer ID 签名、Apple 公证及完整包校验已通过；两架构 Helper 与已安装 0.2.11 的签名身份一致。产物与证据见[第十五轮](computer-use-background-input-probe.md#第十五轮签名候选包准备与公证边界)，未安装或公开发布。

仍待完成：新签名包交互、父/子进程 TCC 归属和升级、权限撤销、多屏缩放、目标关闭/重启及持续真人并行回归。签名身份一致不能代替实际权限与升级验收。双方同时强制退出及投递原子性限制仍在；三款应用也仅证明指定控件场景，不代表所有控件。这些是原有限单击候选包的验收边界；本次扩展仍需独立签名包验收，不作为通用兼容承诺。

### 2026-09-06 第一轮能力补齐

- `use_app` 保留必填 `appID`，新增 `appPath`、`pid`。原生层校验路径的 bundle ID；多个匹配实例返回 `computer_app_ambiguous`，不再按前台或最小 PID 猜选。`list_apps.instances` 返回运行实例的路径和 PID。会话授权、启动所有权仍只有原有事实源。
- `act.actions` 新增 `focus`、`select_text`、`press_key`、`type_text`、`paste`。`select_text` 使用唯一精确子串及 UTF-16 范围，拒绝歧义；键盘输入逐次验证前台 PID、窗口和非安全控件。输入中断后若已发送部分文本，结果为 `unknown`，不重放。
- `press_key` 发送物理按键，遵循当前输入法；`type_text` 发送已提交的 Unicode 文本。`paste` 替换系统剪贴板并发送 Cmd+V，提供的文字会留在剪贴板中。
- `observe` 默认仍读取 AX。设置 `includeScreenshot=true, includeAccessibility=false` 可只截图；请求两种通道时独立返回结果或 `observationError` / `screenshotError`，一项失败不会丢弃另一项。
- 按 [Electron 官方接口](https://www.electronjs.org/docs/latest/tutorial/accessibility) 启用 `AXManualAccessibility`。真实回归证明首次建树、焦点和选择范围更新均异步；只在首次启用时等待建树，写入后轮询确认状态，均不重复发送动作。
- 保持同一 session + app 的一次授权、全局写队列及失败后停止语义；新增动作的审批说明覆盖简中、繁中和英文。

回归入口：

```bash
make computer-use-helper-test
make computer-use-product-smoke
make computer-use-electron-smoke
# 当前已选中 macOS 简体拼音时运行；测试不会切换系统输入法。
make computer-use-electron-ime-smoke
```

已验证：两个同 bundle ID 的独立 Electron 实例、路径不匹配拒绝、精确 PID 操作、另一实例未改变、首次 AX 建树、图片独立读取、中文/emoji 输入和选择替换、粘贴、Tab、Cmd+A、Cmd+F 菜单、Esc 关闭对话框、非前台和密码控件输入拒绝。拼音回归走真实模型工具 → Engine → Manager → Electron bridge → Swift helper，验证 `nihao → 你好` 及原生 composition 事件；原有 AppKit Fixture 产品回归通过。

尚未覆盖：其他输入法和键盘布局、更多第三方应用、大文本吞吐与长时间稳定性。下一轮再做 AX 子树查找/条件等待、观察输出体积和长任务恢复；持久脚本仍属后续范围，后台指针最新进展见上节有限预览。

### 2026-09-06 Computer Use 窗口预览

- 工作区收起且有产物时，在产物卡片下方独立悬浮窗口预览；没有产物或工作区展开时，预览悬浮在聊天区域右上角，不为预览预留侧栏宽度，不改变正文和输入框布局。工作区展开仅收起外部产物卡片，画中画复用原画面节点和采集进程，不重置活动截止时间。长产物列表滚动，预览不作为产物或工作区标签；多个预览向会话区域内侧叠放，避免越过边界。
- 画中画只显示有效窗口画面，不附加顶部标题栏、应用图标或标题栏状态指示；应用名和状态保留在悬停提示与无障碍描述中。加载期间不创建卡片；窗口失效或采集失败立即移除卡片，不显示“窗口预览已停止”或保留旧图，工具错误仍由对话展示。预览按窗口当前内容宽高比显示；读取每帧的 `contentRect` 和 Retina `scaleFactor` 裁去采集缓冲区留白，实时跟随窗口横竖比例变化。画面最大宽度 240px，高度不超过 `min(240px, 30vh)`，同时受可用宽度限制，始终等比缩小。
- 同一 session/turn 按 App 保留各自最近操作的窗口，多个 App 预览以每层 12px 偏移相互遮挡，最近操作的 App 在最上层；同一 App 换窗口替换自己的卡片，不影响其他 App。每个 App 从自己最后一次 Computer Use 活动起独立计时 30 秒，超时关闭自己的采集并移除卡片；普通截图帧不续期、不改变层叠顺序。活动顺序和到期时间均由 Electron 维护，通过 IPC 传给前端，前端不另设续期事实源。
- 仅用于展示。点击通过原生桥接将同一 bundle ID、PID、window ID 的窗口置前，不转发键盘、指针输入，也不自动展开工作区。
- 执行请求从 `Call.TurnID` 透传私有请求头，原生桥接通过权限协调后发布显式 session/turn/window 活动。Electron 负责采集生命周期，前端沿用现有运行 turn 和工作区状态，不向 daemon 写入前台或工作区展开状态。
- 签名 Helper 的独立只读进程通过 ScreenCaptureKit 采集单个窗口，最多 5 FPS、640×480；显式使用 BGRA 采集并以 PNG 传输，保留窗口圆角和半透明边缘，避免默认 YUV 格式把透明区域变黑。单帧 JSON/base64 上限 2 MiB。画面仅在内存中经 preload IPC 展示，不写文件，不加入消息、附件或模型上下文；每个渲染端至多一个未确认帧，只保留最新画面。
- 页面不可见时停止采集；恢复可见后恢复尚未超时的同一 PID 窗口。工作区展开、专注、收起只改变布局，不改变采集订阅。取消、会话切换、页面重载、窗口关闭及 Electron 退出均释放采集；正常 turn 完成后停止采集，最后一帧只保留到各 App 原有的活动截止时间，不重新计时。新的 turn 清除上一轮保留画面，普通聊天不会重新创建预览。采集失败不自动重试原生操作。

隔离桌面回归（需先构建开发 daemon、Helper 和 Fixture）：

```bash
PUDDING_SMOKE_SCENARIO=computer-preview \
  web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron \
  electron/smoke/workspace-dev-smoke.cjs
```

已验证：真实 AppKit 窗口帧、内容变化及横竖窗口实时缩放，20 个产物时列表滚动和预览布局，窄窗口与两种主题，点击精确窗口置前、跨 session/turn 隔离、取消/完成/应用退出清理。2026-09-09 隔离源码桌面回归另覆盖：失效目标从未创建预览 DOM、两个不同 bundle ID 的真实 App 卡片层叠及操作置前、独立 30 秒到期释放、单 App 失效不影响另一个、普通聊天不复活历史预览。另通过会话切换、历史阅读位置、流式跟随和对话搜索回归。

布局补充回归：1440px / 720px 窗口下关闭全部产物后，预览距会话区域顶部、右侧各 16px，`data-activity-rail` 不再因预览单独启用；预览移除前后正文及输入框列的位置和宽度完全一致。有产物时，预览在卡片下方且不属于卡片内容，出现预览也不改变聊天列布局。该检查运行于临时数据的源码 Electron/Vite/daemon，不代表安装包已更新。

工作区连续性回归：展开、进入／退出专注、收起时，卡片及图片 DOM 节点、原生采集子进程、活动截止时间保持一致；展开后修改测试 App 内容仍收到新帧。预览在聊天右上角可见，不进入工作区或外部产物卡片。实际隐藏测试窗口会停止采集，重新显示后恢复同一目标且不续期。此可见性检查临时恢复 Electron 默认后台节流：关闭节流会让隐藏窗口的 `document.visibilityState` 仍为 `visible`，不能用于验证页面隐藏逻辑。

2026-09-09 此轮前端 33 项、Electron 预览 11 项、构建及隔离桌面 15 项断言通过；任务完成后展开／收起也保留原图片节点，不重启采集，沿用原截止时间。桌面证据位于 `/private/tmp/pudding-preview-workspace-v6V28x/result.json`。该轮另出现测试 App 的 `computer_window_raise_failed` 日志（AXRaise 返回成功但目标仍被判定遮挡），原因未定位，不能将断言通过解释为原生置前链路无异常；本次未修改该链路，也未打包发布。

0.3.1 候选收尾补强：旧 smoke 只等目标 App active，没有等待整个 reveal 请求；现在逐次等待原生请求最终成功，并将全部请求/失败记录入结果，拒绝额外请求。隔离诊断轮一次、严格回归四次点击成功；后者覆盖同一 bundle 的两个进程及第二个 App，16 项断言通过（`/private/tmp/pudding-release-preview-strict-BACtYF/result.json`）。没有复现历史错误，也没有据此增加重试或降低可见性判定；历史异常仍列入[最新签名包验收](release-report-0.3.1.md)。

### 0.1 当前 C0 落地

已实现 `native/macos/computer-use-helper`:

- 无提示读取 Accessibility 与 Screen Recording 权限状态,权限请求必须通过显式参数触发。
- 列出已安装的前台 GUI App 身份与运行状态,不读取窗口标题或内容;窗口只由 `use_app` 返回。
- 观察指定 bundle ID 的有限 AX 树,表格和 outline 优先返回可见行,普通值截断,secure text field 永不返回值。
- 使用当前元素路径与语义特征生成指纹 ID;界面结构变化时动作拒绝匹配,不按旧遍历序号误操作。
- 支持控件动作、键盘动作与当前窗口归一化坐标的单击、双击、右键、拖拽和滚轮。
- 在一次原生请求中观察显式 `windowID` 并按需截取同一窗口到 PNG,不录屏、不监听用户输入、不录制或回放工作流。
- 按 bundle ID 启动应用,并按精确 bundle ID + PID 发出普通退出请求;原生层不保存 session ownership。

开发命令:

```bash
make computer-use-helper-test
make computer-use-helper-dev
./bin/Pudding\ Computer\ Use.app/Contents/MacOS/PuddingComputerUseHelper permissions
./bin/Pudding\ Computer\ Use.app/Contents/MacOS/PuddingComputerUseHelper list-apps
```

`computer-use-helper-test` 同时运行 debug 和 release 两种配置。Swift 包将实现模块与薄 CLI 入口分离，测试只链接实现模块，避免 release 测试包同时包含 Helper 与测试 Runner 的入口；对外产品仍是同名 `PuddingComputerUseHelper`，Bundle ID、协议和 worker 的同可执行文件行为不变。问题复现及验证见[第十五轮](computer-use-background-input-probe.md#第十五轮签名候选包准备与公证边界)。

开发构建默认使用本机 `Pudding Dev Local` 代码签名证书保持 TCC 身份稳定;可通过 `PUDDING_COMPUTER_USE_DEV_IDENTITY` 指定其它开发证书。缺少该证书时回退到 ad-hoc 签名,辅助功能权限需要手动添加且重新构建后可能失效。

当前也已完成 C1 原生 Host:

- Helper 支持串行 NDJSON 常驻协议,每个请求和响应显式携带 request ID。
- Electron `ComputerUseHost` 按需启动 Helper并将所有请求排成单队列,负责消息大小、超时、取消、崩溃和退出清理。
- 超时、取消或协议失步会终止当前 Helper;已发送写操作返回 `outcome=unknown`,只读操作返回 `outcome=not_started`,均不自动重试。
- 开发启动自动构建并使用 `Pudding Computer Use.app`;arm64/x86_64 发布 runtime 都包含该后台 App。
- Mach-O 内嵌固定 identifier `com.teatak.pudding.computer-use-helper`,发布校验拒绝身份漂移。

当前也已完成 C2 daemon 与模型工具:

- Electron 暴露独立 loopback bearer-token bridge;除权限查询外所有请求必须显式带 `sessionID`。
- Go `computer.Manager` 不保存 observation;所有 session 的写动作全局串行。
- 内置 App ID 为 `computer-use`,包含 `list_apps`、`use_app`、`quit_app`、`observe`、`act` 五个 Work 模式工具。
- 每个 session 首次访问一个 app 时统一审批观察和操作;同 session 后续访问同 app 不再重复审批,项目的“完全允许”模式也不能跳过首次审批。
- 动作传输中断返回 `outcome=unknown` 且不可重试;动作成功后不自动观察,由模型按任务需要决定何时重新读取界面。
- transcript 已有可读显示名、图标、分组和简中/繁中/英文文案。

当前已通过确定性 Fixture App 和 Calculator 验证真实 `press`/`set_value`/`select`/`submit`、session-owned quit 与已运行 App 非 ownership 分支。签名安装包和跨版本升级实测仍属于 C4。

## 1. 背景与现有基础

Pudding 已具备 Computer Use 上层闭环的大部分基础:

- [`internal/tool/desktop_screenshot.go`](../internal/tool/desktop_screenshot.go):桌面截图进入 session attachment 和模型上下文。
- [`internal/tool/browser.go`](../internal/tool/browser.go):已有 observe、screenshot、click、type、scroll 工具语义。
- [`electron/browser-bridge-server.cjs`](../electron/browser-bridge-server.cjs):已有 Electron 与 daemon 之间的 loopback token bridge 模式。
- [`internal/engine/approval.go`](../internal/engine/approval.go):已有 session-scoped 审批事件和工具调用暂停/恢复。
- [`internal/app/builtin.go`](../internal/app/builtin.go):已有按需加载的内置 App 机制。

当前缺口集中在 macOS 原生控制层:

- 枚举已安装应用,并在明确使用某个 App 时发现其窗口。
- 读取并归一化 Accessibility 树。
- 对 AX 元素执行点击和赋值。
- 按应用或窗口截图。
- 引导并检查屏幕录制、辅助功能权限。
- 应用授权、安全拦截、错误恢复和真实应用验收。

## 2. 目标

MVP 支持:

- 枚举当前可操作的 macOS 应用。
- 观察指定应用和窗口,返回有限的 AX 文本树与交互元素。
- 截取指定窗口或应用画面并作为 session attachment 送入模型上下文。
- 对支持 `AXPress` 的 AX 元素执行一次 press。
- 设置可编辑 AX 元素值。
- 选择可选中的表格行或列表项。
- 对支持 `AXConfirm` 的输入框执行确认提交。
- observation 是按需读取的界面状态快照;动作后是否重新观察由模型根据任务需要决定。
- 用户可随时取消当前 turn,取消尚未开始的动作和正在等待的原生调用。

产品级目标:

- 多窗口、多显示器和常见缩放比例稳定工作。
- 应用退出、窗口关闭、布局变化、权限撤销时返回可诊断错误。
- 敏感和破坏性操作进入审批。
- Helper 签名、notarization、升级和崩溃恢复稳定。

## 3. 非目标

- 不录制用户操作,不从视频学习流程。
- 不生成或持久化可回放 workflow。
- 不在第一阶段支持 Windows。
- 不自动控制 Pudding 自身、终端、系统安全授权界面或管理员认证。
- 不绕过 CAPTCHA、浏览器安全警告或操作系统权限提示。
- 不把 Computer Use 当作 REST、MCP、内置 Browser 或 CLI 的替代品。存在结构化接口时优先使用结构化接口。
- 不新增后端 focus/current app/current window 状态。
- 不为失败动作增加第二套 AX/坐标/脚本 fallback。

## 4. 架构与所有权

```text
session turn
  -> Computer Use built-in App tools
      -> Go computer.Manager
          -> explicit session/app/window routing
          -> approval policy
          -> one global write queue
          -> token-authenticated loopback bridge
              -> Electron ComputerUseHost
                  -> signed Swift Helper over stdio
                      -> AXUIElement
                      -> ScreenCaptureKit
```

### 4.1 Go daemon

负责:

- 工具定义、参数校验和结构化结果。
- 显式 session 路由。
- 动作审批、取消和超时。
- 全局写操作队列。
- 截图 attachment 存储和 canonical turn part。
- session 关闭时尽力普通退出 session-owned app。

不负责:

- 保存当前前台应用或窗口。
- 直接调用 macOS Accessibility API。
- 直接持有跨 turn AX 元素引用。
- 在不确定动作结果后自动重试。

### 4.2 Electron main

负责:

- 启动、监督和停止签名 Helper。
- macOS TCC 权限检查与权限设置入口。
- 对 daemon 暴露仅 loopback、带启动 token 的 Computer bridge。
- 对 renderer 暴露最小权限状态和设置 IPC,不向 renderer 暴露任意原生操作接口。

### 4.3 Swift Helper

负责:

- 调用 Accessibility 和 ScreenCaptureKit。
- 生成归一化 observation。
- 针对当前 Accessibility 树实时解析 `elementID` 并执行唯一指定动作。
- 在动作前再次验证应用、窗口和元素。
- 通过 NDJSON stdin/stdout 与 Electron main 通讯,不监听网络端口。

Helper 不保存业务 session、模型上下文或用户消息。

### 4.4 全局桌面资源

本机应用 UI 是全局资源,不能按 session 并行写入:

- 原生读写请求统一经过 Electron Host 单队列,与串行 Helper 保持一致。
- 所有写操作经过 daemon 全局队列。
- daemon bridge 请求必须显式携带 `sessionID`;Helper 请求携带独立 request ID。
- 取消 session 只移除该 session 的待执行项,不能影响其它 session。
- 不存在 daemon 级 active session 或 focus session。

## 5. 原生实现

### 5.1 Helper 形态

新增签名 Swift Helper,并作为具有固定 bundle ID 的 `Pudding Computer Use.app` 嵌套到 Pudding.app:

```text
native/macos/computer-use-helper/
  Package.swift
  Sources/PuddingComputerUseHelper/
```

选择独立 Helper 而不是 Node native addon:

- Swift 可直接使用现代 macOS API。
- 原生崩溃不拖垮 Electron main。
- 不绑定 Electron Node ABI。
- 可独立测试、签名和限制通讯面。

开发版使用 `com.teatak.pudding.dev.computer-use-helper`,发布版使用 `com.teatak.pudding.computer-use-helper`。Helper 的 bundle identifier、签名 identity 和 designated requirement 必须稳定,否则版本升级可能导致 TCC 权限重新授权。

### 5.2 观察

使用 AXUIElement 获取:

- bundle ID、进程 ID、应用名称。
- window identifier、标题、frame、focused 状态。
- element role、subrole、title、description、identifier、value 摘要、enabled、focused、frame。
- 支持的 AX actions,例如 `AXPress` 与 `AXConfirm`,以及可写 selection 属性。

归一化时:

- 只观察指定窗口,按广度优先遍历并使用统一节点预算（默认 200、最多 1000）,不再另设隐藏深度上限。预算不足时返回 `truncated=true`。table/outline 使用 `AXVisibleRows`,并以每行的绝对 `AXIndex` 生成路径。缺少唯一绝对索引的可见行子树仅可读取,不暴露动作。
- secure text field 只返回角色和位置,绝不返回值或任何动作。
- 普通文本设置 UTF-8 字节上限,AX value 超限时标记 `valueTruncated`。
- `elementID` 在父级身份作用域内优先由 AX identifier 生成,其次由 role/subrole + label 生成;无 identifier 的行使用绝对索引路径,其余无 identifier/label 元素使用结构路径。父级作用域不变时,frame、value 和元素自身路径变化不会改变有 identifier/label 的 ID。窗口标题不参与根身份,避免改标题后所有子元素重新编号。
- 不同父级下的同名控件可分别定位;同一作用域内仍无法唯一解析的控件拒绝动作,不使用位置或其它隐式 fallback 猜测目标。
- 不把原始 AX 指针暴露给 daemon 或模型。

### 5.3 截图

使用 ScreenCaptureKit 截取明确的 window:

- 默认只截目标窗口,不截取其它应用。
- 返回逻辑坐标、像素尺寸和 scale factor。
- 截图存储继续复用 session attachment 主链路。
- 模型不需要视觉信息时只使用 AX observation,减少隐私暴露和 token 成本。
- 模型请求截图时,AX observation 与截图在同一个 Helper 请求内完成,不在两次调用之间暴露可插入的 Computer Use 操作。

Computer Use 窗口截图由 Helper 的 ScreenCaptureKit 单一路径实现。现有全桌面截图是不同能力,不作为 Computer Use 失败后的 fallback。

Helper 在观察、截图和操作目标窗口期间保持一个只针对该 `windowID` 的轻量 ScreenCaptureKit stream。每步结束后采用 5 秒空闲释放;同一窗口的新操作会重置计时,避免连续操作时系统标识反复闪烁。stream 帧直接丢弃,不录制、不保存;它只用于让 macOS 显示系统窗口共享标识。标识由系统跟随窗口移动和缩放,也可能出现在普通桌面截图中。Pudding 不再自绘第二套悬浮提示。

### 5.4 动作唯一实现

| action | 唯一实现 | 完成条件 |
| --- | --- | --- |
| `press` | AX `AXPress` | action 返回成功且目标仍属于指定 app/window |
| `set_value` | AX set value | 重新读取值与期望一致 |
| `select` | AX `AXSelected=true` 或父容器 `AXSelectedRows` | 重新读取 selection 包含目标元素 |
| `submit` | 优先 AX `AXConfirm`;否则仅向目标 PID 发送一次 Return key-down/key-up | 元素必须已聚焦、启用、非安全且为可编辑单行文本控件，不额外激活 App |
| `click` / `drag` / `scroll`，显式 `delivery=foreground` | 目标 App 已前台时，通过系统 HID 发送受限指针事件 | 接受当前窗口左上角为 `0,0`、右下角趋近 `1,1` 的归一化坐标;执行前实时校验目标 App 前台且目标窗口在坐标处最上层;成功只代表事件已投递 |
| `click` / `drag` / `scroll`，默认或 `delivery=background` | 同 Helper worker 向指定 PID/窗口投递，无兼容 App 白名单 | 左/右单击、左双击、左拖拽、双轴滚动；允许遮挡；校验真实应用身份、进程启动标记、窗口几何和权限；无前台 fallback，成功仅代表投递 |

规则:

- 任一动作失败都不自动改用坐标、键盘、AppleScript 或剪贴板;坐标点击必须由模型显式选择。
- 所有写操作执行前由 Helper 基于实时系统状态验证目标应用、窗口和元素。
- 所有调用都传 `actions` 数组:单动作传 1 项,连续动作传 2–32 项。工具不自动观察,模型仅在目标未知、必须检查 UI 变化或结果不确定时调用 observe。
- 每一步都针对实时 AX 树或窗口几何重新验证。语义动作只遍历身份字段,在目标上检查动作能力,不读取所有控件的 value、frame、selection 等完整观测数据;完整数据仅在显式 observe 时读取,不跨动作缓存 AX 对象。仅在目标均已知且中间状态无需检查时连续执行；前一步可能移动窗口或目标时不得批量执行后续 pointer 动作。
- 多步调用不是事务:遇到首个失败立即停止,已完成动作不回滚、不重试。模型输入和结果项统一使用 `type`;结果返回 `completedCount`、从 0 开始的 `failedIndex` 和已确认动作。`result.failure.outcome` 保留失败项的真实状态;已完成前缀且失败项未执行时,工具顶层 `outcome=partial`、`retryable=false`,不能重放整个批次。

## 6. Observation 契约

示例:

```json
{
  "sessionID": "sess_x",
  "appID": "com.apple.TextEdit",
  "windowID": 42,
  "observedAt": "2026-08-13T08:00:00Z",
  "screenshot": null,
  "text": "window 'Untitled'\n[1] AXTextArea ...",
  "elements": [
    {
      "elementID": "ax_...",
      "role": "AXTextArea",
      "label": "Text",
      "enabled": true,
      "actions": ["set_value"],
      "frame": {"x": 120, "y": 90, "width": 720, "height": 520}
    }
  ]
}
```

约束:

- observation 是无服务端状态的界面快照,不是授权令牌或动作前置条件。
- `elementID` 由元素的稳定 Accessibility 身份生成;模型可在同一 App 窗口仍存在时复用已知 ID。
- Helper 在每次语义动作前重新遍历指定窗口并按稳定身份解析元素;找不到、不唯一、不再支持该动作或属于 secure 控件时拒绝执行。
- 模型自主决定观察时机：仅在目标未知、必须检查 UI 变化或结果不确定时 observe。已知稳定目标可直接放入一次 `actions` 调用。
- pointer 不依赖 observation token;坐标使用当前窗口归一化空间。默认后台投递采用上节的进程、窗口、权限及前台变化校验；显式前台投递验证前台 App 和最上层目标窗口。
- daemon 不保存 observation registry 或过期时间;当前 AX 树和当前窗口几何是唯一事实源。

## 7. 模型工具

Computer Use 作为 `computer-use` 内置 App,在 Work 模式按需加载。当前提供五个模型工具:

### `builtin_computer_list_apps`

仅在需要从应用名发现 `appID` 时使用。列出已安装前台应用的身份、运行状态和可控状态,不返回权限、窗口标题或窗口内容。

### `builtin_computer_use_app`

按 `appID` 使用应用：默认在后台启动或复用现有进程，不激活、不抬升已运行 App，并返回 `windowStatus` 与 PID 绑定的当前窗口。用户明确要求显示、聚焦或切换到该 App，或者操作确实不能在后台完成且已获准切换焦点时，才传 `foreground=true`；未获准时先询问。session/App 审批不等同于切换焦点授权。后台语义动作、指针和观察不需要激活，目标已在前台也不重复激活。该模式可激活或重新打开窗口。只有 `windowStatus=ready` 时才能继续观察。仅当当前 session 确实新启动该进程时返回 `launchID + PID`;应用原本已运行时不返回 `launchID`,不获得关闭权。

显式前台请求和预览窗口的显示请求共用 `ForegroundPolicy`：已经前台且目标窗口未被其他普通应用窗口遮挡时直接成功，不调用 AXRaise，也不额外要求辅助功能权限。只有窗口尚未显示到前面时才尝试一次 AXRaise；最终以实际前台及窗口顺序判断成功，而非仅凭 AX 返回值。失败保留 AX 原生错误码。过程中失去前台则停止，不循环激活争抢焦点。

AX 语义操作保持后台执行能力。前台坐标/键盘动作的 `computer_app_not_foreground` 表示该失败项未执行，不自动重试或重新激活；模型应请用户恢复目标窗口或明确同意切回。观察不是解决前台冲突的必经步骤。后台点击、拖拽和滚动复用同一个指针接口，不自动切换到前台事件路径。

2026-09-08 iPhone 镜像验证记录（不是通用兼容性结论）：

- 独立诊断进程使用 `CGEvent.postToPid`，向已连接镜像的主屏幕时钟组件投递单次左键 down/up，附带目标窗口字段。后台试验中前台应用、鼠标位置未变，但时钟未打开。
- 用户手动切到镜像前台后，同样的 PID 定向事件也未打开时钟，因此不能仅凭该结果断言是“后台状态”导致；该路径未验证有效，没有加入产品或自动 fallback。
- 用现有 Codex Computer Use 点击同一坐标，时钟打开；随后恢复主屏幕。此对照证明目标可点击，不代表 Pudding 的 PID 事件路径已通过。
- 当前源码编译的 Helper 在镜像已前台时执行 `use-app --foreground` 成功返回 `windowStatus=ready`。更早的一次激活未达到前台，返回独立的 `computer_activation_failed`，没有继续执行输入。没有替换或重启开发/发布应用。

### `builtin_computer_quit_app`

只接受当前 session 持有的 `launchID`,并对其对应的 bundle ID + PID 发出普通退出请求。绝不 force quit。返回 `closed=false` 时保留 ownership,停止自动操作并请用户处理未保存内容或确认窗口。

### `builtin_computer_observe`

参数:

```json
{
  "appID": "com.apple.TextEdit",
  "windowID": 42,
  "includeScreenshot": false
}
```

返回 observation。`windowID` 必填,不能使用当前焦点猜测。`includeScreenshot=true` 时,AX 观察和窗口截图在同一个 Helper 请求内完成。

### `builtin_computer_act`

参数:

```json
{
  "appID": "com.apple.TextEdit",
  "windowID": 42,
  "actions": [
    {"type": "set_value", "elementID": "ax_...", "value": "hello"}
  ]
}
```

`actions` 必须包含 1–32 项,每项用 `type` 指定动作。不存在顶层单动作字段或特殊 sequence 类型。`set_value` 的 `value` 与语义动作的 `elementID` 均放在对应项中。指针动作禁止 `elementID`/`value`,使用当前窗口归一化坐标,原点为窗口左上角,右下角趋近 `1,1`。`click` 支持单次左/右键和左键双击；`drag` 使用左键起止坐标；`scroll` 使用正数向下/向右的像素 delta。默认指针在后台投递；显式 `delivery=foreground` 支持相同的点击、拖拽、滚动参数，但要求目标 App 已在前台且目标位置无遮挡。两者均只确认事件投递,不宣称 App 状态已改变。`delivery` 不能用于语义或键盘动作。`submit` 只会出现在已聚焦、启用、非安全、可编辑的单行文本控件上。单一数组协议让审批、串行化和 transcript 展示共用一个入口。

连续动作使用完全相同的数组协议:

```json
{
  "appID": "com.apple.calculator",
  "windowID": 42,
  "actions": [
    {"type": "press", "elementID": "ax_clear"},
    {"type": "press", "elementID": "ax_4"},
    {"type": "press", "elementID": "ax_plus"},
    {"type": "press", "elementID": "ax_4"},
    {"type": "press", "elementID": "ax_equals"}
  ]
}
```

每项执行前均在实时 AX 树或当前窗口几何中解析和验证目标。中途失败不会回滚已完成动作,也不会自动尝试剩余动作。

已知窗口、前台状态和目标坐标仍有效时直接复用,不固定执行“激活 → 截图 → 动作”。仅在需要切换前台时调用 `use_app(foreground=true)`,仅在视觉信息缺失或已发生相关变化时请求截图。

新增工具时必须同步:

- `internal/app/builtin.go` 的 Computer App 定义。
- transcript 工具显示名和图标。
- 简体中文、繁体中文、英文 i18n。
- turn activity summary。

### 本机应用显示名

审批卡片和 transcript 应用图标提示共用 `useDesktopApplicationIdentity`。请求显式携带 Pudding 当前语言，查询缓存按 `appID + locale` 隔离；切换语言无需重新授权，旧语言请求晚返回不会覆盖当前名称。

Electron 将语言传给 Helper 的 `app_identity`；Helper 从目标应用的 `InfoPlist.loctable` 或语言目录下的 `InfoPlist.strings` 读取名称，按系统语言匹配规则处理简繁体。没有本地化名称时保留应用原名，不维护应用翻译表。显示名不写回应用清单缓存，不参与 App ID 或 session/App 审批判断。

原生测试覆盖本地化资源和真实计算器的简体、繁体、英文名称；Web/Electron 测试覆盖缓存键和语言传递。桌面回归使用当前源码 Electron、实际 preload/IPC/Helper 和审批组件，检查语言切换及旧请求晚返回：

```sh
web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/application-identity-smoke.cjs
```

运行前需构建当前开发 daemon 和 Helper（`bin/puddingd`、`bin/computer-use-helper-build/debug/PuddingComputerUseHelper`）。脚本使用临时数据目录和独立 Vite 服务，仅读应用元数据，不申请权限、不提交审批、不操作已安装 Pudding 的会话；截图和报告写入脚本输出的临时目录。

## 8. Bridge 契约

daemon 与 Electron 使用独立 `ComputerBridgeServer`,不复用 Browser CDP 操作实现,也不经 renderer 转发。

建议路由:

| 路由 | 用途 |
| --- | --- |
| `POST /computer/apps/list` | 列已安装应用身份与运行状态,不读窗口内容 |
| `POST /computer/apps/use` | 默认后台启动或复用一个明确 bundle ID；经用户请求显示/聚焦或允许必要的焦点切换后，显式 `foreground=true` 激活或重新打开，并返回当前可发现窗口及 window ID |
| `POST /computer/apps/quit` | 普通退出一个明确 bundle ID + PID |
| `POST /computer/observe` | AX observation |
| `POST /computer/observe-capture` | 同一次 Helper 请求内完成 AX observation 与窗口截图 |
| `POST /computer/act` | 执行一个显式动作 |
| `POST /computer/pointer` | 执行一次使用当前窗口归一化坐标的 click、drag 或 scroll |
| `GET /computer/permissions` | 读取权限状态 |

所有请求:

- 仅接受启动时生成的 bearer token。
- 除 permissions 外必须携带 `sessionID`。
- body 设置严格大小上限。
- 不接受任意脚本、shell command 或 AppleScript。
- 错误返回稳定 `code`、`message`、`retryable` 和 `outcome`;权限错误另带机器可读的 `permission` 或 `permissions`。

原生单步 `outcome`:

- `not_started`:确认动作没有执行,调用方根据错误决定下一步,不强制重新观察。
- `completed`:动作已完成。
- `unknown`:动作可能已执行但响应丢失,必须重新观察,禁止直接重试。

模型工具的多步结果另外使用顶层 `partial` 表示“已完成前缀,失败项未开始”;若失败项效果未知,顶层保持 `unknown`。失败项自己的 `outcome` 不会被改写成 `completed`。`partial` 由结果推导,不写入原生错误或引入额外会话状态。

## 9. 错误语义

首版稳定错误码:

- `computer_unavailable`
- `computer_permission_required`
- `computer_permission_denied`
- `computer_app_not_found`
- `computer_app_not_installed`
- `computer_launch_failed`
- `computer_activation_failed`
- `computer_window_raise_failed`
- `computer_use_failed`
- `computer_launch_not_owned`
- `computer_window_required`
- `computer_window_not_found`
- `computer_pointer_target_changed`
- `computer_app_not_foreground`
- `computer_element_not_found`
- `computer_element_not_actionable`
- `computer_secure_input_blocked`
- `computer_action_blocked`
- `computer_action_timeout`
- `computer_action_cancelled`
- `computer_helper_crashed`
- `computer_capture_failed`

超时或连接断开不能统一标记 retryable。`not_started` 只表示该动作未执行，不代表可以原样重试；需先解决具体前提，是否观察取决于缺少什么信息。启动、激活和置顶错误分别报告；后两者不能表述为“应用未启动”，因为应用可能已经运行。这些生命周期失败保持 `outcome=unknown` 和 `retryable=false`。

## 10. 权限与安全

### 10.1 macOS 权限

需要:

- Screen & System Audio Recording:识别目标窗口并按需读取单帧画面。
- Accessibility:读取 AX 树并执行 AX 动作。

当前不监听键盘或鼠标,因此不申请 Input Monitoring。只发送两类受限合成事件：目标进程的单次 Return，以及目标 App 已前台时基于当前窗口归一化坐标的单击、左键双击、右键、左键拖拽和滚轮；不开放任意键盘或组合键。

Swift Helper 是 Computer Use 两项权限的系统事实源;Pudding 主进程的桌面截图权限单独显示,不得代替 Helper 权限。只有实际 Computer Use 请求缺少相关权限时才显示结构化引导,模型不生成权限说明。引导和设置页读取同一 Electron 权限控制器;用户切回 Pudding 后自动复检,齐全时恢复原请求。只有系统已报告授权、重启 Helper 后仍返回同一权限错误时才显示 Pudding 重启按钮。Pudding 不尝试自动点击系统权限提示或管理员认证。

### 10.2 应用策略

应用硬性禁止策略由 Swift Helper 单一实现。daemon 在 SQLite 中持久化 `sessionID + appID` 授权,在内存中保存短期 observation 和 session-owned launch registry。

首版规则:

- Helper 当前硬拒绝 Pudding 自身/父应用、终端、常见密码管理器、Keychain Access 和系统安全授权进程。
- 其余可控应用由模型显式指定 bundle ID 和 window ID。目标 GUI App 的启动和退出必须使用 Computer Use 工具,不得使用 shell `open`、`osascript` 或 AppleScript。每个 session 首次启动、观察、操作或退出一个 app 时申请一次确认,该确认同时授权当前 session 后续启动、观察、操作和 session-owned 退出同一 app。
- `apps` 是已安装应用身份清单,不是授权列表;窗口只以 `use_app` 返回值为准,不得用 `list_apps` 刷新窗口或引导用户去设置页添加应用。
- 浏览器任务优先使用 Pudding Browser;只有用户明确要求现有外部浏览器登录状态时才使用 Computer Use。
- 目标应用在动作前和动作后都必须与请求 bundle ID 一致。

### 10.3 数据保护

- secure text field 的值永不进入 observation、日志、tool result 或 attachment。
- Helper 日志不记录输入文本、截图 bytes 或完整 AX value。
- 截图只保存到当前 session attachment 目录。
- AX 值与 app/window 标题按 UTF-8 字节上限截断,结果标记值截断状态;Helper 响应另有整体大小上限,错误日志不输出完整 AX tree。
- 取消 turn 后停止新截图和新动作。

### 10.4 审批

MVP:

- observe/screenshot 与 launch/quit/act 共用 SQLite 中同一份 `sessionID + appID` 授权。
- 当前 session 首次访问 app 时触发一次 Computer Use 对话审批;批准后,该 session 启动、观察、操作和 session-owned 退出同一 app 都不再询问;Pudding 重启后仍然有效。
- 不同 session 或不同 app 必须重新审批;只有删除 session 时才通过外键级联清除其 app 授权。
- `computer_act` 进入独立 Computer Use 风险分类,不能沿用文件路径审批推断。
- secure input、系统权限、管理员认证和明确禁止应用始终拒绝。

产品级补充:

- 对发送消息、上传文件、删除、购买、提交协议、账号与安全设置建立 action-time confirmation。
- 审批内容展示目标 app/window、动作、元素标签和待输入文本摘要。
- 不把网页、文档或应用界面中的指令当作用户授权。

## 11. 状态与持久化

不新增:

- backend focus/current app/current window。
- provider-local Computer Use history。
- SQLite 中的跨 turn AX 元素或截图缓存。

事实源:

| 数据 | 事实源 |
| --- | --- |
| 当前 UI | macOS AX tree / ScreenCaptureKit |
| session 和 turn | SQLite canonical session/turn/message |
| tool call/result | canonical message parts |
| observation | Go daemon 内存短期 registry |
| session-owned launch | Go daemon 内存 launch registry |
| session app 授权 | SQLite `computer_app_grants(session_id, app_id)` |
| 固定禁止应用 | Swift Helper `AppPolicy` |
| macOS 权限 | TCC/System Settings |

截图 tool result 继续作为 canonical attachment 进入上下文;普通 AX observation 作为 tool result 进入当前 turn,不另建长期数据库表。

## 12. UI

新增设置区:

- Screen Recording 权限状态。
- Accessibility 权限状态。

执行时:

- transcript 显示“查看应用”“操作应用”等可读工具名。
- 显示当前正在操作的应用和窗口。
- 提供停止按钮,调用现有 session cancel。
- sensitive approval 使用现有 approval overlay,不增加第二套确认系统。
- 目标窗口标题栏显示 macOS 自带的窗口共享标识。
- 不实现录制红点、时间轴或 workflow 编辑器。

## 13. 实施阶段与工期

### C0 契约与原生可行性,3–4 天

- 冻结 observation、action、error schema。
- Swift Helper 完成 TextEdit/Calculator 的 AX 读取 PoC。
- 验证签名开发构建的 TCC 权限在重启后保持。
- 验证 ScreenCaptureKit window capture 与坐标映射。

验收:

- 能列出目标窗口和交互元素。
- 能对 TextEdit 执行一次 `AXPress`/set value。
- 没有 AppleScript 或自动坐标 fallback;受限坐标点击只能显式使用同一截图 observation。

### C1 Helper 与 Electron Host,7–9 天

- 实现 NDJSON Helper 协议。
- 实现 app/window list、observe、window screenshot。
- 实现 AX press/set value/select、受限 submit 和当前窗口归一化坐标的受限指针动作;不开放通用 CGEvent 输入动作。
- 实现 Helper 生命周期、超时、崩溃和取消。
- 完成 Helper 嵌套签名和 notarization 验证。

### C2 Daemon、工具与 session 路由,5–7 天（已实现）

- 新增 `internal/computer` manager 和 Electron bridge client。
- 新增 Computer Use 内置 App 和五个工具。
- 新增全局写队列、session release 和结构化错误。
- 截图接入 attachment/canonical context。
- 增加 stale observation 和 unknown outcome 契约测试。

### C3 权限、安全与 UI,5–7 天（已实现）

- 设置页显示 Accessibility 与 Screen Recording 状态,可显式申请权限或打开对应系统设置;不显示或申请 Input Monitoring。
- 应用读取和操作不做设置页白名单,统一使用现有对话审批;同一 session 对同一 app 只确认一次。
- 固定禁止应用策略由 Helper 单一实现,覆盖 Pudding 自身/父应用、终端、系统安全界面和常见密码管理器。
- 已添加 transcript 显示名、图标、activity summary、审批说明和三语 i18n。

### C4 稳定性与验收,7–10 天

- 多窗口、多显示器、Retina 和窗口移动测试。
- Helper crash、应用退出、权限撤销、session cancel 测试。
- 针对 TextEdit、Calculator、Notes 和一个确定性 fixture app 做 smoke。
- 完成打包、签名、notarization 和升级测试。

当前已新增 `PuddingComputerUseFixture`，提供可编辑文本框、500 行虚拟表格、secure field、按钮、checkbox 和两个窗口。以下命令通过实际签名 Helper 验证 launch、窗口发现、可见行遍历、`set_value`、`submit`、`select`、`press`、secure field 脱敏和普通 quit：

```bash
make computer-use-fixture-smoke
```

完整产品链路使用确定性 scripted provider，覆盖 App 加载、同一 session+app 一次授权、Engine、Manager、Electron bridge、签名 Helper、fixture 操作和 session-owned quit：

```bash
make computer-use-product-smoke
```

真实 Calculator 验收会通过同一产品链路执行两阶段清除后再输入 `1 → + → 1 → =`，确认显示结果为 `2`，并使用本 session 获得的 `launchID` 正常关闭：

```bash
make computer-use-calculator-smoke
```

已运行 App 的非 ownership 分支由测试夹具先启动 Calculator，再让 session 完成同一操作。`use_app` 必须不返回 `launchID`，任务结束后 Calculator 必须仍以同一 PID 运行；最后仅由测试夹具清理：

```bash
make computer-use-calculator-existing-smoke
```

这些 smoke 都依赖本机已向开发版 `Pudding Computer Use.app` 授予 Accessibility 与 Screen Recording，不进入无 TCC 环境的普通单元测试。目标 App 必须在运行前关闭，测试不会接管或关闭原本已运行的 App。

自动回归已覆盖：Helper crash 后重启、请求 timeout、在途 cancel 终止 Helper、原生请求串行化、排队请求取消不影响在途请求、权限撤销或目标 App 退出后不重试旧动作,以及 observe+capture 单请求协议。

发布链路会在签名前把嵌套 Helper 的版本同步为外层 Pudding 版本。`make desktop-verify` 对 staged app、ZIP 和 DMG 中的 Helper 统一检查 bundle ID、Developer ID/Team ID、designated requirement、`LSUIElement`、屏幕捕获用途说明、架构和可移植依赖。

`make desktop-update-test` 允许从尚未包含 Helper 的旧版本升级，但要求升级后的 App 包含并通过上述 Helper 校验。首个含 Helper 的签名版本发布后，后续版本使用以下严格门禁，要求升级前后 Helper 的 identifier、Team ID 和完整 designated requirement 完全一致：

```bash
make desktop-computer-use-update-test
```

Preview 对应使用 `make desktop-preview-update-test` 和 `make desktop-preview-computer-use-update-test`。严格门禁必须先在 `/Applications/Pudding.app` 安装一个更旧、且已经包含 Helper 的同通道签名版本。

总计约 27–37 个工程日。并行投入原生与 Go/Web 两名工程师时,日历时间可压到约 4 周,但产品级兼容性验证不能等比例压缩。

## 14. 测试策略

### 单元测试

- observation 归一化、截断和 secure field 脱敏。
- action 参数互斥、stale observation、错误映射。
- 全局队列的 session 隔离和 cancel。
- bridge token、body limit 和 Helper crash。
- tool result attachment 与 canonical context。
- Computer Use 的 `sessionID + appID` 一次审批、session 隔离和固定 denylist。

### 原生集成测试

新增确定性 macOS fixture app,包含:

- button、checkbox、text field、secure field。
- scroll view。
- 两个窗口。
- 点击后改变明确状态的按钮。
- 能分别记录单击、双击、右键、拖拽和滚轮的指针靶区。
- 可移动和缩放的窗口。

测试不依赖第三方应用的易变布局。TextEdit、Calculator、Notes 只作为发布 smoke。

### 关键回归

- 错误 appID 不产生任何输入。
- stale observation 不产生任何输入。
- action timeout 不执行第二种动作。
- 多 session 同时写入时严格串行且不串目标。
- session cancel 后没有迟到点击或输入。
- 单击、双击、右键、拖拽和滚轮分别到达前台 Fixture，后台目标一律拒绝。
- secure field 内容不出现在日志、结果和截图元数据中。
- Pudding 自身和终端始终无法成为动作目标。

## 15. MVP 完成标准

满足以下条件才算完成:

1. release 签名构建可完成权限授权,重启和升级后状态符合预期。
2. 模型能在显式指定的 TextEdit/fixture app 中连续完成 observe、press、set value、select 和 submit。
3. 每个动作都显式带 session/app/window/observation,不存在后端 focus 状态。
4. 多 session 并发请求不会把动作发送到错误应用或窗口。
5. 写操作失败没有自动 fallback 或自动重试。
6. session cancel 后无迟到输入。
7. secure field 和禁止应用策略有自动测试。
8. 新工具的 transcript 名称、activity summary 和三语 i18n 已同步。
9. `go test ./...`、Electron tests、Web build、Swift tests、签名 desktop smoke 全部通过。

## 16. 延后事项

以下能力在 MVP 稳定后单独设计,不提前加入状态或兼容分支:

- Windows UI Automation。
- 跨设备或锁屏后台控制。
- drag、文本选择和辅助 AX action。
- 操作录制与 workflow 回放。
- 自动恢复布局变化。
- 基于视觉模型的元素定位。
- 外部应用专用适配器。
