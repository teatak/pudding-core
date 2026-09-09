# 后台定向输入原型与验收记录

更新：2026-09-09。状态：**后台普通单击已接入有限预览；通用默认后台能力和正式签名版本未通过完整验收**。

这是 [Codex 对比报告](computer-use-codex-comparison-2026-09-08.md) 之后的实施记录。第一至十轮仅做隔离实验；第十一轮开始接入现有工具和 Helper，保留默认前台模式的保护、权限与 session 授权。未操作京东或修改业务数据。

**最新进展（第十一轮）：系统计算器、邮件和日历的后台普通单击已显式接入 `actions[].delivery=background`；当前源码的 Manager → Electron bridge → 新 Helper/worker 日历月份往返回归通过。** 第十轮真实邮件 3 项、日历 4 项的原型结果保留；飞书点击后成为系统前台的已知失败仍未解决，因此不开放飞书。旧生命周期、真人并行样本不能代替新产品链路验收。前面各轮保留当时的失败证据，不应当作最新机制结论。

## 隔离原型行为边界

- 矩阵入口只能投递到测试 AppKit bundle 或本仓库源码 Electron 的具名测试窗口；校验 PID、完整可执行文件路径、窗口 ID 和当前窗口几何。另有显式的 `mirror-*` 手动测试入口，仅允许系统 iPhone Mirroring 主窗口，不接受任意 App 目标。
- 第十轮增加 `realApp=mail|feishu|calendar` 的显式手动单击测试，按固定安装路径及 bundle ID 校验真实应用；调用方、执行进程和恢复共用身份策略。未加入任意 App 或 LLM 工具入口。
- 只使用 `postToPid`。发送器不主动切换系统前台、不置顶、不移动真实鼠标、不走全局 HID、不重放动作；接收应用是否导致系统前台变化仍须实测，不能由发送器没有 activate 调用直接推断。第六轮的合成激活入口会临时改变接收端内部 active/key 状态，结束后发送去激活通知；目标已真的成为系统前台时不强行去激活。不能把这说成“目标内部完全没有焦点变化”。
- 每个实验显式选择一种事件构造方式；这些是对照组，不是产品里的 fallback 链。
- 测试搭建阶段会显示并激活两个临时测试窗口。动作期间，用另一个测试窗口保持前台，以 5 ms 定时采样和应用激活通知记录是否发生干扰。采样不是对任意短瞬态的数学保证。
- 权限只预检；不足时退出，不弹授权、不重置 TCC。使用已有启动宿主权限，不能把此结果当作 Pudding 正式签名验收。
- 所有构建、临时 bundle、Electron userData、日志均在新建临时目录；结束仅关闭本次创建的进程。证据目录保留，便于复核。

## 已证明的差异

### 1. PID 送达不代表正确的窗口和控件命中

同一 AppKit 窗口的 Increment 按钮：

| 构造方式 | 接收端记录 | 实际按钮效果 |
| --- | --- | --- |
| Quartz 鼠标事件＋窗口字段 91/92 | 接收端收到事件，但 `windowID=0` | 无 |
| NSEvent factory＋窗口字段 | 窗口 ID 正确，局部坐标为 `(-1, 433)` | 无 |
| factory＋显式窗口内坐标 | 窗口 ID 正确，局部坐标为 `(195, 340)` | 计数加一 |

公开 factory 改用窗口内坐标作为构造参数，也没有在本机修正跨进程接收端坐标。有效变体通过运行时解析的 `CGEventSetWindowLocation` 写入窗口局部位置；这是**非公开接口**，第十一轮有限单击预览仍依赖它。原型滚轮另外先发送目标进程内的 mouseMoved，更新接收端窗口定位；不更新真实桌面鼠标，未接入后台产品路径。

实验构造参考了 [axcli 的输入实现](https://github.com/andelf/axcli/blob/main/src/input.rs)，但没有采用它的激活路径，也没有把它的兼容性声明视作本项目的测试结果。公开 API 范围可对照 [Apple CGEvent 文档](https://developer.apple.com/documentation/coregraphics/cgevent)。**本实验不是 Codex 内部算法的证明。**

### 2. Command 标记会改变用户动作语义

显式带 Command 的对照组能够产生部分原本未生效的点击，但：

- AppKit 接收端记录 `modifierFlags=1048576`，按钮处理器也读到 Command。
- Electron DOM 收到 `isTrusted=true, metaKey=true`。

所以它不是无副作用的“后台路由标记”，而是真正的 Command+点击/滚动。该变体的所有结果都判为不合格；不会用它冒充普通点击。

### 3. Electron 默认的首次后台点击策略是另一层障碍

默认 Electron 测试窗口中，窗口内坐标变体的单击和拖拽没有进入 DOM；双击、右键和滚轮有响应。

**只在独立正向对照窗口设置 `acceptFirstMouse: true` 后，同一输入实现的五类动作全部通过。** 这隔离出了接收端策略的影响；不是坐标路由本身突然失效。Electron 官方说明该选项控制非活动窗口的点击能否继续传给网页，macOS 默认 false。[Electron BaseWindow 选项](https://github.com/electron/electron/blob/main/docs/api/structures/base-window-options.md)

这个开关属于被操作应用，不能假定第三方 Electron 应用都开启，也不能通过修改 Pudding 自己的窗口配置来修复它们。不采用“先补点一次”、偷偷带 Command 或自动切到前台的方式掩盖失败。

## 实测结果

环境：macOS 26.6.2（25G83），本机 arm64；临时 AppKit bundle 使用 ad-hoc 签名。Electron 使用当前仓库 `web/node_modules/electron/dist/Electron.app`，没有连接 Pudding daemon。

每个表格格子都分别测了后台可见和被 guard 窗口遮挡两种布局。以下是**不带修饰键、显式窗口内坐标**变体：

| 动作 | AppKit Fixture | Electron 默认 | Electron 开启首次点击（正向对照） |
| --- | --- | --- | --- |
| 单击 | 2/2 通过 | 0/2，无 DOM 效果 | 2/2 通过 |
| 双击 | 2/2 通过 | 2/2 通过 | 2/2 通过 |
| 右键 | 2/2 通过 | 2/2 通过 | 2/2 通过 |
| 拖拽 | 2/2 通过 | 0/2，无 DOM 效果 | 2/2 通过 |
| 滚轮 | 2/2 通过 | 2/2 通过 | 2/2 通过 |

通过的格子同时要求：实际控件效果、接收端输入事件、无意外修饰键、前台 PID 不变、激活通知无目标切换、真实鼠标位移小于 0.5 pt、guard 未收到输入。Electron 还校验 DOM 事件可信性。滚轮检查滚动位置实际改变，不能只看事件已收到。

三轮各有 5 个构造变体 × 5 个动作 × 2 种布局，共 150 个对照格；**不是 150 项全部通过**。有意义的合格结果分别为 AppKit 10、Electron 默认 6、Electron 正向对照 10。三种未写入局部位置的基线没有通过；Command 组因语义污染不通过。

第一次滚轮观察在 `scrollWheel` 回调内立即读取，得到相同的前后位置；后续日志显示 AppKit 在回调之后才更新滚动位置。因此测试改为等待动画 tick 后读实际位置，未修改事件投递去“修复”测试误判。

本机原始证据目录（临时目录，系统清理后需重跑）：

- `pudding-background-probe-ZZe4uq`：AppKit 的完整 50 格。
- `pudding-background-probe-JPJNTo`：默认 Electron 的完整 50 格。
- `pudding-background-probe-f6bxDA`：Electron 首次点击正向对照 50 格，另有投递前窗口覆盖关系记录。
- 最终候选变体另复测 30 格：`pudding-background-probe-xnZJiJ`（AppKit 10/10）、`pudding-background-probe-PsXNPl`（默认 Electron 6/10）、`pudding-background-probe-Cy16OH`（正向对照 10/10）。这轮全部通过 normal-window 覆盖关系核对，结论与完整矩阵一致。

覆盖关系的额外检查只比较普通应用窗口的层级和矩形，不冒充全局 AX 命中测试。本机 Dock 存在全屏大小的 layer-20 透明表面，直接按所有窗口矩形取第一个会误报；因此最终 runner 明确校验 normal-window 层级，保留“可见/遮挡”仅指两个测试窗口的布局定义。

## 实现与复测

- [原生 sender、AppKit receiver 与 monitor](../native/macos/background-input-probe/main.swift)
- [独立 Electron receiver](../electron/smoke/background-input-fixture.cjs)
- [构建与实验矩阵 runner](../scripts/computer-use-background-probe.cjs)
- [验收判定测试](../scripts/computer-use-background-probe.test.cjs)

从仓库根目录运行：

```sh
node --test scripts/computer-use-background-probe.test.cjs
node scripts/computer-use-background-probe.cjs appkit
node scripts/computer-use-background-probe.cjs electron
node scripts/computer-use-background-probe.cjs electron-click-through

# 定向复测一个显式变体，不顺序尝试其他变体。
node scripts/computer-use-background-probe.cjs appkit appkit-window-local
```

运行原生矩阵前，请暂停使用鼠标和切换应用。脚本不接受真实应用作为目标；每次输出 `Evidence directory`，其中 `status.json`、`events.jsonl`、`results.json` 保留预检、逐格原始事件、动作结果与前台/光标记录。脚本完成不代表产品可发布，最终始终明确返回 `productionReady: false`。若用户接管、前台/鼠标发生干扰，则停止，不继续抢焦点重试。

自动检查：Swift 编译及零输入自测通过（含 8 类无效请求拒绝、事件元数据和私有符号缺失时明确失败）；4 组判定测试通过，覆盖缺少 UI 效果、不可信或缺少可信性标记的 DOM 输入、重复效果、修饰键污染、短暂激活通知、鼠标移动、guard 被输入及滚轮无位移。`git diff --check` 通过。此次没有改生产模块，所以未运行全量 Go/Web 回归。

## 真实 iPhone 镜像追加验证

### 首轮：普通单击未生效

用户锁定手机后，镜像已连接并进入主屏幕。使用 [手动镜像 runner](../scripts/computer-use-mirror-probe.cjs)，没有使用 Codex 点击来冒充原型效果。权限沿用本次测试宿主，不是正式签名 Pudding 的验收。

测试入口先解决了两项实际搭建问题：

- CG 窗口列表首项是 66×20 的系统指示浮层，不是 446×978 的手机主窗口。现在读取 AX `iphone-mirroring-main` 的位置、尺寸，再与同 PID 的 CG 窗口严格匹配；缺失或歧义即失败。不能靠窗口列表顺序选择。
- 短生命周期截图 CLI 未初始化 AppKit 时，ScreenCaptureKit 触发 `CGS_REQUIRE_INIT` 断言。截图进程现在初始化 NSApplication 并设为 `.prohibited`，不激活窗口。坐标和 guard 布局使用镜像所在显示器，支持此次实际出现的负 Y 坐标。

结果：

| 场景 | 事件投递 | 前台/鼠标 | 实际画面 |
| --- | --- | --- | --- |
| 后台可见，单击主屏幕“设置” | 1 组 down/up，未带 Command | 121 次采样均为 guard；无激活通知；鼠标位移 0；guard 未收到输入 | 没有打开；立即及稍后截图均仍在主屏幕 |
| 后台双击 | 未投递：准备后前台被其他应用切走 | 未形成有效测试 | 未验收 |
| 遮挡、右键、拖拽、滚轮 | 未执行 | 未验收 | 未验收 |

前台切换的只读核对指向 ChatGPT 与源码 Electron；这不是“原型把镜像提到前台”的证据。手动测试入口在前台不符时不发送事件、不自动激活镜像。此次已关闭自建 guard，未关闭、移动或主动激活镜像；未更改手机设置、订单或支付。

证据目录 `pudding-background-probe-JaLJ44`：`mirror-results.json`、`mirror-events.jsonl`；`5-prepared.png`、`6-after.png`、`7-observed.png` 分别记录点击前、后及稍后画面。图片仅保留于本机临时目录，不加入仓库。

**结论仅限于：当前候选变体在真实镜像的普通后台单击未通过。** 不能由事件已投递断言接收端已经处理，也未证明唯一根因就是首次点击策略。需要继续隔离接收端事件语义，不能据此断言 macOS/Codex 不支持后台操作；不接入生产 `actions[]`，不补点、不附带 Command、不自动切前台。

复测入口（先暂停切换应用和移动鼠标；每条 `send` 都是人工确认的动作）：

```sh
node --test scripts/computer-use-background-probe.test.cjs scripts/computer-use-mirror-probe.test.cjs
node scripts/computer-use-mirror-probe.cjs
```

runner 接受逐行 JSON：`{"op":"prepare","covered":false}` 准备 guard 并返回截图；`{"op":"capture"}` 只观察；`send` 指定 `action`、归一化 `x/y`，拖拽可另给 `end:{x,y}`，滚轮可给非零且绝对值不超过 600 的 `scrollY`；`{"op":"quit"}` 关闭自建 guard。不内置手机图标坐标或业务导航脚本，不把截图当成已完成业务效果。

追加自动验证：6 组 Node 测试通过；Swift 自测扩充为 14 类无效请求拒绝，并覆盖浮层误选、窗口匹配歧义及显式拖拽终点。测试不发送真实输入。

### 第二轮：双击及部分滚动生效，遮挡下双击通过

用户同意继续测试后，沿用同一 `appkit-window-local` 变体与事件实现，没有修改 sender、附加 Command、把单击改成双击，或使用 Codex 点击来代替原型。单击与双击是独立的对照动作。

| 动作/场景 | 实际结果 | 验收边界 |
| --- | --- | --- |
| 后台可见，双击主屏幕“设置” | 设置打开 | 前台保持 guard，鼠标位移 0 |
| 后台可见，设置首页滚轮 | 列表上移，“通用”等条目出现 | 前台保持 guard，鼠标位移 0 |
| 后台可见，普通单击“通用” | 未打开 | 事件已投递，不算成功；双击后也不能假定普通单击可用 |
| 遮挡，双击“通用” | 通用打开，但鼠标位移约 820.43 pt | 用户确认当时移动了鼠标；该轮作废，不计无干扰通过 |
| 重测遮挡，通用页滚轮 | 没看到滚动 | 前台/鼠标稳定；移开 guard 后仍无位置变化，不算成功 |
| 后台可见，同一通用页滚轮 | 仍未看到滚动 | 不能把差异只归因于遮挡，也不能由首页成功推广所有页面 |
| 后台可见，通用页左缘向右拖拽 | 未看到返回 | 无干扰，但未通过；尚无同手势正向对照，不能断言镜像不支持拖拽 |
| 重测遮挡，双击通用页返回按钮 | 回到设置首页 | **通过**：144 次前台采样均为 guard，无激活通知，鼠标位移 0，guard 未收到输入 |
| 右键 | 未测试 | 未验收，不由 Fixture 结果推广 |

本轮共 8 次动作投递，其中 1 次因人工移动鼠标作废，剩余 7 次的前台/光标隔离检查通过，**不等于 7 次 UI 动作成功**。只有 3 次干净样本有对应的可见效果（双击打开、首页滚动、遮挡双击返回）。镜像不能像自建 Fixture 一样提供内部接收事件日志，因此这里的效果判断来自截图，不声称已读取镜像接收端事件。

遮挡测试的 guard 与镜像主窗口矩形相同；发送前的 normal-window 层级记录确认该坐标处最上层普通窗口属于 guard。截图仍是目标窗口捕获，不是穿过 guard 的桌面点击。发送器没有激活镜像、移动真实鼠标、使用全局 HID 或重放失败动作。

原始证据目录（均位于本机临时目录）：

- `pudding-background-probe-zXiNGg`：`1-prepared.png` 主屏幕，`2-after.png` 双击打开设置，`3-after.png` 首页滚动，`4-after.png` 普通单击未打开通用，`6-after.png` 受人工鼠标干扰的遮挡双击。
- `pudding-background-probe-ZhrnXk`：`1-prepared.png` 通用页；`2-after.png` 遮挡滚轮、`3-prepared.png` 移开遮挡、`4-after.png` 可见滚轮、`5-after.png` 拖拽均未改变导航/滚动位置；`6-prepared.png` 再遮挡，`7-after.png` 双击返回设置成功。
- 两个目录的 `mirror-results.json` / `mirror-events.jsonl` 保留动作、投递状态、覆盖关系、前台采样、激活通知、鼠标位移和截图路径。

测试结束已关闭两轮自建 guard，镜像停在设置首页，未修改设置项、消息、订单或支付。本轮只更新验证记录，未修改生产代码或原型实现；runner 构建时 Swift 零输入自测通过，证据核对与本文件空白检查通过，未重跑无关 Go/Web 测试。全仓 `git diff --check` 另报已有并行改动 `web/src/api/client.ts:1548` 的文件末尾空行，未修改该无关文件。

下一步应隔离普通单击的事件元数据/接收端首次点击策略，以及转场后的滚动路由，分别做正向对照。**这些仍是假设，不是已经证明的唯一根因。** 双击成功不能成为给单击自动补点的理由；后台可行性已得到真实镜像支持，但通用指针接入门槛仍未满足。

## 第三轮：全新进程隔离首次点击与双击计数

本轮只操作自建 AppKit / Electron 窗口，不操作镜像或 Pudding 会话。目的不是让按钮“总能点中”，而是区分发送次数、`clickCount`、`eventNumber` 和接收端首次点击策略。

### 实验设计

- 同一 Increment 按钮，独立改变 1/2 组 down/up、最后一组 `clickCount=1/2`、最后一组 `eventNumber=1/2`，共 8 种输入。两组输入的第一组固定为 `(clickCount=1, eventNumber=1)`；每个事件间隔 50 ms。
- 再分别改变首次点击允许/拒绝、后台可见/被 guard 遮挡，共 32 格/接收端。**每格启动全新接收进程**，两个接收端共 64 个进程，避免上一格丢弃的事件或渲染状态影响下一格。
- AppKit 按钮新增可观测的 `acceptsFirstMouse` 与 `mouseDown` 日志；不传实验开关时仍沿用 NSButton 默认策略。Electron 记录 DOM `detail`、目标、可信性、实际 click 与 dblclick 回调。
- 诊断入口 `click-probe-send` 只接受测试目标，限制 1–2 组输入、元数据值 1–2；不向镜像或正式 `actions[]` 暴露这些参数。原普通动作的事件计数规则未改。

本机环境同前，Electron 为 **43.2.0**。下表是两个接收端一致的按钮增量；每行已核对两种事件编号与两种遮挡布局，结果相同。

| 实际发送（每个数字是一组 down/up 的 clickCount） | 拒绝首次点击：按钮增量 | 允许首次点击：按钮增量 | Electron 额外 dblclick 回调 |
| --- | --- | --- | --- |
| `[1]`：一次普通单击 | 0 | 1 | 无 |
| `[1, 1]`：两次普通单击 | 0 | 2 | 无 |
| `[2]`：一组事件，标记为第二击（仅诊断） | 1 | 1 | **1 次** |
| `[1, 2]`：双击序列 | 2 | 2 | **1 次** |

### 证据与结论

1. **普通单击确实被首次点击策略挡住。** AppKit 在拒绝组收到正确窗口坐标 `(195, 340)` 的 down/up，记录 `acceptsFirstMouse=false`，却没有进入按钮 `mouseDown` 或执行计数。只把策略改成 true，同一普通单击进入按钮并加一。Electron 的正向对照也从无 DOM 事件变为一次可信的 click。
2. **不是“再发一次普通单击”就能解决。** 拒绝组的 `[1, 1]` 两次均未产生按钮效果；`[2]` 则进入按钮，AppKit 此时没有调用首次点击策略。`[1, 2]` 还出现两个按钮回调：不能理解为“第一下只激活、第二下只点一次”。全过程目标没有成为前台。
3. **改变点击计数会改变动作语义。** Electron 对仅一组 `[2]` 也产生 `click(detail=2)` 和 `dblclick(detail=2)`。因此禁止用伪造第二击替代普通单击；NSButton 没有独立的 DOM dblclick 事件，不代表其他控件也没有双击副作用。
4. **本轮 1/2 的事件编号差异没有改变结果。** 拆开编号与计数后，普通单击仍失败或成功于首次点击策略；不是仅将 eventNumber 改成 2 就能修好。该结论不外推到所有编号或事件类型。
5. **镜像的唯一根因仍未证明。** 现在在可控接收端复现并证明了这个机制，但没有镜像内部的首次点击回调日志，不能把相似现象直接当作同一根因，更不能据此声称知道 Codex 的实现。

64/64 格通过实验隔离检查：采样前台始终为 guard、无激活通知、鼠标位移 0、guard 未收到输入，正常窗口覆盖关系符合各自布局；收到的事件无修饰键污染，Electron DOM 事件可信。**这不是 64 次普通点击成功，也不是产品可发布验收。** AppKit 的按钮跟踪过程会在内部取走 mouseUp，因此不能用顶层 `sendEvent` 日志少一个 up 推断 sender 漏发；sender 保留完整计划及投递数量。

原始证据目录（本机临时目录中的 `click-factors.json` / `click-factors.jsonl`）：

- `pudding-background-probe-WbwGDL`：Electron，全新进程 32 格，32 个不同目标 PID。
- `pudding-background-probe-VjCUWC`：AppKit，全新进程 32 格，32 个不同目标 PID。
- `pudding-background-probe-F5MHQd` 是探索性的共享进程版本，不用作最终矩阵：跨样本出现额外 click，且早期汇总字段覆盖了首次点击开关。runner 已删除共享接收进程路径并修正字段，最终两轮没有该歧义；额外 click 的内部来源未单独追踪。
- `pudding-background-probe-XW9v2r` 首格鼠标位移约 37 pt、前台切换，实验立即停止，**作废**。用户同意暂停输入后才进行上述全新进程复核。

实现与复测：

- [点击因素矩阵 runner](../scripts/computer-use-click-factor-probe.cjs)
- [矩阵与接收日志判定测试](../scripts/computer-use-click-factor-probe.test.cjs)
- 复用上文的 Swift sender/AppKit receiver 与 Electron receiver；没有新增产品 fallback。

```sh
node --test scripts/computer-use-background-probe.test.cjs scripts/computer-use-mirror-probe.test.cjs scripts/computer-use-click-factor-probe.test.cjs
# 暂停鼠标及应用切换；两轮顺序运行，不并行抢占测试窗口。
node scripts/computer-use-click-factor-probe.cjs electron
node scripts/computer-use-click-factor-probe.cjs appkit
```

本轮 Swift 编译及零输入自测通过（14 类无效请求、4 类无效点击计划拒绝，计数与编号独立构造检查），8 组 Node 测试通过。测试窗口结束后关闭；未修改正式工具、原生生产输入路径或权限，未运行无关 Go/Web 全量回归。

后续优化应保持两条边界：有 AX 语义动作的控件优先验证已有后台语义路径；自绘界面的后台指针仍需找到不改变单击语义的投递方式并通过相同对照。`acceptFirstMouse` 是接收端配置，不能靠修改 Pudding 自己的窗口修好第三方 App。当前不实施补点、伪造双击或自动抢前台。

## 第四轮：系统计算器，Codex 与隔离原型同点对照（2026-09-08）

按用户选择改用系统计算器，不再要求临时 Fixture 接入 Codex。新增 [Calculator 只读观察/事件监听](../native/macos/background-input-probe/CalculatorProbe.swift)、[交互 runner](../scripts/computer-use-calculator-probe.cjs) 和 [判定测试](../scripts/computer-use-calculator-probe.test.cjs)。**没有修改生产输入逻辑**。

### 方法和边界

- 固定系统 `com.apple.calculator`、`/System/Applications/Calculator.app` 及其可执行文件；AX `main` 窗口与 WindowServer 几何必须唯一匹配。原型仅允许 `appkit-window-local` 的一组普通左键 down/up，不向该入口开放 Command、双击或其他手势。
- 两边均从显示 `0` 开始，单击同一 Seven 按钮。Codex 使用真实 `calc.click([34,211], {clickCount:1})`；原型由 runner 使用 AX 按钮中心转换出的同一屏幕点 `(1532,244)`。清零在测量区间之外，由 Codex 完成。
- [Apple 的进程事件 tap](https://developer.apple.com/documentation/coregraphics/cgevent/tapcreateforpid(pid:place:options:eventsofinterest:callback:userinfo:)) 使用 `listenOnly`，只监听计算器 PID 的左键 down/up；不捕获键盘，不修改/吞掉事件，不注入计算器，不请求或重置 TCC。先在已知原型发送上确认能够看到完整事件对，再观察 Codex。
- tap 是**路由层证据，不是计算器内部 SwiftUI/AppKit 回调**。记录屏幕位置、窗口号、事件编号、点击计数、修饰键及源/目标 PID。`NSEvent(cgEvent:)` 在监听进程解码的 `decodedX/Y` 不当作计算器接收端的窗口内坐标。
- 前台按每次样本开始时的实际 PID 记录，动作期间不得变化；不会为了发送输入重新激活任何应用。自建 guard 只用于布置与采样。最初强制 guard 为前台的尝试在发送前被拒绝，随后调整测试基准；原 fixture/mirror 入口仍限制 guard 身份。
- `covered` 仅记录 **guard 的请求布局**，不是实际可见性的断言。原型本轮场景日志显示目标点上方还有 ChatGPT 普通窗口，因此不能将 `covered:false` 写成“计算器无遮挡”。

### 已取得的结果

| 检查项 | Pudding 隔离原型 | Codex Computer Use |
| --- | --- | --- |
| 计算器变化 | `0 → 7` | `0 → 7` |
| 路由事件 | 一组 down/up | 一组 down/up |
| clickCount / flags | `1 / 0` | `1 / 0` |
| windowID / 目标 PID | `7350 / 81743` | `7350 / 81743` |
| 屏幕坐标 | `(1532,244)` | `(1532,244)` |
| eventNumber | `1` | `40` |
| 本轮隔离采样 | 94 次，前台始终为 guard，光标位移 0 | 3051 次，期间前台切换、光标位移约 1938.96 pt，**无干扰验收作废，后续复测见下** |

Codex 事件源 PID `64268` 只读核对为 `Codex Computer Use.app/Contents/MacOS/SkyComputerUseService`，不是原型代打。原型来源 PID `20639`。两边都看到了正确的计算器结果，但本轮只有原型取得干净隔离样本；不能把 Codex 的受干扰测量写成“抢焦点”，也不能写成“全程无干扰通过”。已向用户请求短时间停止输入后再复核。

此前一次独立 Codex 样本已验证 `0 → 7`、4458 次前台采样仅含 ChatGPT、无激活通知及光标位移 0；该次没有路由事件监听，不能与本轮拼成一条同时满足所有条件的样本。其记录是本机 `/private/tmp/pudding-codex-compare.XUE4Vn/RESULT.md`。

本轮证据目录为本机临时目录 `pudding-background-probe-7aFNzK` 中的 `calculator-events.jsonl` / `calculator-results.json`；`pudding-background-probe-MdUMOn` 是发送前被前台条件拒绝的准备轮，不计点击失败。

**结论：计算器上，两边都能用无修饰键的普通单击生效，未复现镜像上的失败。** eventNumber 虽不同，但两边均成功，不能将其认定为待修根因。现在也没有证据说明 Codex 在这次计算器操作中靠 Command 或伪造双击工作。该结果不外推微信、镜像或拒绝首次点击的控件；生产后台指针的接入门槛仍未通过。

复测入口：

```sh
node --test scripts/computer-use-background-probe.test.cjs scripts/computer-use-mirror-probe.test.cjs scripts/computer-use-click-factor-probe.test.cjs scripts/computer-use-calculator-probe.test.cjs
node scripts/computer-use-calculator-probe.cjs
```

runner 启动后输入 JSON 行：`{"op":"prepare","covered":false}` 布置测试；计算器清零后，`{"op":"prototype"}` 只发送一次 Seven 单击。Codex 对照先清零，输入 `{"op":"begin-codex"}`，由真正 CUA 单击截图确认的 Seven 坐标，再输入 `{"op":"finish-codex"}` 结束采样。`{"op":"quit"}` 关闭测试窗口和监听器，不关闭计算器。每次监听有 60 秒上限，超时/监听失效不算健康样本；runner 有 10 分钟上限。

验证：10 项 Node 测试通过；Swift 编译及零输入自测通过（14 类无效请求、4 类无效点击计划、3 类 Calculator 非普通单击请求被拒绝）；本轮 `git diff --check` 通过。没有运行无关 Go/Web 全量测试。

### 追加复测：同一轮两边均取得干净样本

用户再次要求复测后，复用同一测试程序与系统计算器，未调整输入实现。提示用户短暂停止鼠标/应用切换后，先测 Codex，再在测量区间外清零，最后测隔离原型。

| 检查项 | Codex Computer Use | Pudding 隔离原型 |
| --- | --- | --- |
| Seven 单击结果 | `0 → 7`，CUA 与 AX 读数一致 | `0 → 7`，AX 读数并由 CUA 复核 |
| 前台采样 | 3656 次，均为 guard PID `32018` | 97 次，均为同一 guard PID |
| 激活通知 / 鼠标最大位移 | 0 次 / 0 pt | 0 次 / 0 pt |
| 路由事件 | 一组 down/up，`clickCount=1`、`flags=0` | 一组 down/up，`clickCount=1`、`flags=0` |
| sourcePID / eventNumber | `64268 / 44` | `32286 / 1` |

两条样本的目标均为计算器 PID `81743`、窗口 `7350`、屏幕坐标 `(1532,244)`，窗口几何未变，计算器未成为前台，guard 未收到输入，监听未超时或失效。**本轮同时补齐了实际 UI 效果、普通单击元数据和无干扰检查**，不再需要拼接前后两轮证据。原型的层级日志仍显示目标点上方有 ChatGPT 普通窗口；不将请求布局当作实际无遮挡或特定 guard 遮挡的验收。

证据目录：本机临时目录 `pudding-background-probe-CQlgnC` 的 `calculator-results.json` / `calculator-events.jsonl`，共 2 条记录，均为 `isolated=true`、`tapHealthy=true`。测试结束已关闭自建 guard 和监听器；计算器保留数字 `7`。

本轮仅补测试记录，生产代码及测试实现未改。runner 启动时 Swift 编译、零输入自测通过；记录核对和 diff 空白检查通过，未重复无关全量测试。结论仍限定为：**Codex 与现有隔离原型均能后台普通单击计算器，不抢前台、不移动真实鼠标；镜像、微信及其他手势没有因此得到验收。**

## 第五轮：默认 Electron，Codex 与隔离原型全新进程对照

日期：2026-09-08。本轮完成此前建议的第 1 项，不把用户持续输入、完整鼠标语义或正式签名链路视作已验收。

### 测试隔离与方法

- Codex 按源码 Electron 完整路径选择应用时，仍选中已经运行的 Pudding 开发窗口，而不是新启动的 Fixture。因此没有点击该对象，也没有关闭开发版来腾出目标。
- 使用当前源码依赖中的 Electron **43.2.0** 创建临时副本，赋予独立 bundle ID `com.teatak.pudding.background-probe.electron` 并 ad-hoc 签名。两边均运行同一份 receiver，`acceptFirstMouse=false`，每个样本启动全新主进程、使用独立 userData；没有修改安装版、源码 Electron 本体或接收端首次点击策略。
- Codex 使用真实 `isolatedElectron.click([195,92], {mouseButton:"left", clickCount:1})`，随后读取 AX 和截图。坐标来自测试窗口截图，未使用 AXPress、双击或补点替代。原型使用未改变的 `appkit-window-local`，同一按钮中心、同一屏幕点 `(275,637)`，发送一组普通 down/up。
- 测量前把自建 guard 放在与目标相同的矩形 `(80,545,400,432)` 并保持前台。记录前台 PID、激活通知、真实鼠标最大位移、guard 接收事件，以及 Electron DOM 事件和按钮实际计数。原型另有 normal-window 层级日志确认该点由 guard 覆盖；Codex 样本没有等价的连续层级日志，不能声称已证明窗口排序从未发生瞬态变化。
- 测试准备中发现 macOS 返回 `/var/...`、Electron 返回 `/private/var/...`，原有字面路径比较拒绝了同一文件。仅对测试目标身份校验解析真实路径；仍要求精确 bundle ID、当前 probe 目录中的可执行文件和 PID。该准备错误发生在发送点击前，不计点击失败。

### 实测结果

| 检查项 | Codex Computer Use | Pudding 隔离原型 |
| --- | --- | --- |
| 全新目标 PID / windowID | `89758 / 21832` | `90497 / 21848` |
| `acceptFirstMouse` | `false` | `false` |
| 按钮实际计数增量 | **1**（截图和 DOM 一致） | **0**（按钮仍为 Increment） |
| DOM 接收事件 | 各 1 次 mousedown、mouseup、click | 0 次 |
| 普通点击语义 | 三个事件均 `detail=1`、`flags=0`、`isTrusted=true` | 没有 DOM 事件，不推断接收语义 |
| 按钮回调 / 双击回调 | 1 / 0 | 0 / 0 |
| 前台采样 | 5495 次，始终为 guard `89730` | 96 次，始终为同一 guard |
| 激活通知 / 真实鼠标最大位移 | 0 / 0 pt | 0 / 0 pt |
| guard 收到输入 / 目标几何变化 | 无 / 无 | 无 / 无 |

**已证明：Codex 在默认拒绝首次点击的 Electron 窗口中，仍能保持后台并产生一次正常、可信的 DOM 单击；现有原型在相同接收配置下未做到。** 因此不能将 `acceptFirstMouse=false` 当成后台点击的绝对平台限制，也不能用给第三方应用改配置、附带 Command、伪造第二击来解释或替代 Codex 的能力。

前面第三轮证明的“我们的候选投递被首次点击策略拦住”仍成立；本轮新增的是 **Codex 有效、原型无效** 的直接对照，不是对 Codex 原生实现的反向证明。尚未记录此 Electron 样本的 CGEvent 路由层数据，也没有 Electron/AppKit 内部的首次点击判定回调；**具体差异在事件构造、窗口路由还是其他原生处理，仍未定位**。DOM 的正常修饰键/点击计数只能证明网页实际收到的语义，不能推断之前所有原生事件字段都相同。

Codex 样本包含 `getApp`、AX/截图观察的准备过程，原型样本没有经过 Codex 的观察。因此这轮对比的是两条完整调用路径，尚未将“观察准备是否改变接收端状态”从“单击事件构造差异”中独立排除；后续应补相同预观察条件的对照。

[OpenAI 官方文档](https://learn.chatgpt.com/docs/computer-use)确认 macOS 可执行限定范围的后台任务，但没有公开输入后端算法。此次按 OpenAI Docs 的来源边界核对官方说明，没有把能力说明当作事件实现证据。下一步应复用这个可观测接收端补齐路由层事件与首次点击判定对照，而不是先放宽生产前台保护。

### 复测与交付

- [交互对照 runner](../scripts/computer-use-electron-compare.cjs)、[判定测试](../scripts/computer-use-electron-compare.test.cjs)。仅测试脚本、接收端观测和身份匹配有变化；生产工具、提示词、权限及输入后端未改。
- 运行 `node scripts/computer-use-electron-compare.cjs`；输入 `{"op":"prepare","arm":"codex","covered":true}` 后，按返回的独立 App 路径让 CUA 读取窗口。`{"op":"begin-codex"}` 开始采样，真正的 CUA 单击并观察后输入 `{"op":"finish-codex"}`。再用 `{"op":"prepare","arm":"prototype","covered":true}` 启动全新接收进程，`{"op":"prototype"}` 只发送一次普通单击。`{"op":"quit"}` 关闭本次创建的目标和 guard。每个 prepare 仅允许一个测量样本，runner 有 10 分钟上限。
- 证据：本机临时目录 `pudding-background-probe-yQu0M4` 的 `electron-compare.json` / `electron-compare.jsonl`。两条样本均 `isolated=true`；Codex `pass=true`，原型 `pass=false`，失败结果原样保留。
- 验证：12 项 Node 判定测试通过；Swift 编译及零输入自测通过（14 类无效请求、4 类点击计划、3 类非普通 Calculator 请求拒绝）；原始记录核对、diff 和新增文件空白检查通过。未运行无关 Go/Web 全量测试。
- 初始同路径目标探索目录 `pudding-electron-codex-target-8WhLnG` / `pudding-electron-codex-target-jJxpjg` 未发送测量点击，不计有效样本。所有本次创建的窗口和进程已通过各自关闭通道退出；不关闭计算器、镜像或用户 Pudding 会话。

## 第六轮：合成激活机制与一次普通点击（2026-09-08）

这轮不再扩大兼容性矩阵，而是只验证一个因果变量：**在原有窗口定向 down/up 前发送 AppKit 激活通知，结束后发送去激活通知。** 两个接收端每组都使用全新进程、同一个计数按钮，默认拒绝首次后台点击；前台 guard 覆盖目标窗口。没有调用 Codex 为原型预热目标。

### 机制依据及实现边界

前一轮对本机已安装 `SkyComputerUseService`（Codex Computer Use 26.831.1000926 / build 1000926）的只读导出符号与定点反汇编，发现 `syntheticallyActivateIfNeededForSendingClick`、`SyntheticAppFocusEnforcer.enforceActiveState`、`notifyAppActivated` / `notifyAppDeactivated`、`SystemFocusStealPreventer` 等链路。激活构造路径使用 `NSEvent.otherEvent` 和定向 `postToPid`。这是本机特定二进制版本的静态证据，不是官方公开 API 承诺，也不代表已还原其全部实现。

最小原型只使用 AppKit SDK 的 `.appKitDefined`、`.applicationActivated`、`.applicationDeactivated` 常量构造通知，`windowNumber=0`、无修饰键；沿用原有显式窗口内坐标及定向鼠标事件。流程为：通知接收端激活 → 等待 100 ms → 原有一组普通 down/up → 配对去激活。没有附加点击、伪造第二击、Command 修饰或失败重放，也没有调用 `NSRunningApplication.activate` / AXRaise。100 ms 只是当前实验固定等待，未证明是最低必需时延。

`focus-probe-send` 只接受自建 Fixture 的普通单击，拒绝镜像、Calculator、拖拽和诊断点击计划；这些限制只属于本次验证入口，不加入 LLM 工具。生产 `PointerService` 不变。通知使用公开常量不等于整个后台输入路径都是公开受支持方案：原有窗口内坐标 setter 仍是非公开接口。

### 实测结果

| 检查项 | AppKit（明确拒绝首次点击） | Electron 43.2.0（默认 false） |
| --- | --- | --- |
| 原有单击基线 | 收到 down/up，计数 0 | 无 DOM 事件，计数 0 |
| 仅加入合成激活后 | 按钮收到普通单击，计数 **1** | mousedown / mouseup / click 各 **1** 次，计数 **1** |
| 点击语义 | clickCount=1、无 Command | detail=1、flags=0、isTrusted=true；无 dblclick |
| 接收端内部状态 | active false→true→false，key 0→目标窗口→0 | 窗口 focus→blur，末态 active/key 均 false |
| 系统前台采样（基线 / 新方案） | 96 / 130 次，均为 guard | 97 / 118 次，均为 guard |
| 系统应用激活通知 / 鼠标位移 | 0 / 0 pt | 0 / 0 pt |
| guard 输入、窗口几何变化 | 均无 | 均无 |

**由此已证明：接收应用内部的激活状态与系统真正的前台应用可以分离；补齐前者后，原来失败的普通单击可以正常进入控件。** 这不是双击替代单击，也不是先真正抢焦点再切回。测量使用 5 ms 采样和激活通知，不声称排除了任意短的未观测瞬态。

AppKit 按钮跟踪循环可能在 `NSApplication.sendEvent` 之下消费 mouseUp（第三轮已记录）。最初新判定器错误要求顶层日志必须同时出现 down/up，导致 `PJniZt` 样本虽计数 1 仍标记失败。现改为同时核对 sender 恰好投递一组事件、接收端一次 `button-received`、一次 clickCount=1 的按钮效果及计数增量；允许顶层看不到跟踪循环已取走的 up，禁止额外 down/up 或效果。没有为使测试通过而改变投递语义，旧证据未改写，随后全新进程复测通过。

### 改动、复测与证据

- [原生 test-only sender / 接收日志](../native/macos/background-input-probe/main.swift)：新增通知构造、配对清理和零输入边界自测；[Electron receiver](../electron/smoke/background-input-fixture.cjs) 增加 focus/blur 与末态记录。
- [因果验证 runner](../scripts/computer-use-synthetic-focus-probe.cjs)、[验收判定测试](../scripts/computer-use-synthetic-focus-probe.test.cjs)：每轮只有原有基线与新方案两个独立进程；复用现有克隆 Electron / guard 设施，不新建生产后端。原有 Codex 交互对照入口保持不变。
- AppKit 原始记录：本机临时目录 `pudding-background-probe-rAPpkt/synthetic-focus.json`，基线 PID 94499、新方案 PID 94504、guard PID 94497；新方案 `pass=true`、`mechanismVerified=true`。
- Electron 原始记录：`pudding-background-probe-NpwhPp/synthetic-focus.json`，基线 PID 93946、新方案 PID 93972、guard PID 93945；新方案 `pass=true`。收紧 sender 配对判定后对原始日志只读重算仍通过，没有改写或重放该样本。
- Swift 编译与零输入自测通过（含 4 类合成激活入口无效请求）；14 项 Node 判定测试通过，覆盖无效果、重复输入、修饰键、缺失投递证据、未恢复 active/key、窗口变化、前台和鼠标干扰。真实实验的正常结束恢复已验证；异常退出、取消和用户并行输入未验证。

```sh
node --test scripts/computer-use-synthetic-focus-probe.test.cjs
# 先暂停鼠标和切换应用；只显示并操作自建测试窗口，顺序执行。
node scripts/computer-use-synthetic-focus-probe.cjs appkit
node scripts/computer-use-synthetic-focus-probe.cjs electron
```

### 接下来应实现的最小产品化环节

先解决合成激活的生命周期，而不是继续调点击次数：目标身份有效时保证配对恢复；用户把目标真正切到前台时不得反向去激活用户窗口；取消、异常退出或目标关闭时不能残留内部激活/按下状态。随后验证用户在前台持续输入时不会被抢焦点、吞输入或误投，再接入现有 `actions[]`。当前 `defer` 清理只是正常及可捕获失败路径；不保证 SIGKILL、窗口几何变化或全部焦点竞争场景。

## 第七轮：输入生命周期、用户接管与异常恢复（2026-09-08）

本轮继续只改隔离原型，没有接入生产 `PointerService`，没有放宽产品前台保护。原型中保留已证明的单击机制，重点验证其恢复边界。

### 已证明的问题与修复

1. **调用方被强制结束后，原来的 `defer` 不执行。** 原始样本 `pudding-background-probe-qMevJJ/focus-lifecycle.json`：在激活之后、点击之前结束发送器，目标计数仍为 0，但 active/key 均残留 true，系统前台未变。现在 [FocusLease.swift](../native/macos/background-input-probe/FocusLease.swift) 创建独立的短生命周期执行进程，独占鼠标事件和合成激活状态；调用方只持有控制管道。调用方正常结束、SIGTERM 或 SIGKILL 导致管道关闭时，执行进程检测 EOF，停止后续输入并做配对清理。没有在调用方维护第二套激活状态，也没有保留原先不受监督的 `focus-probe-send` 路径。
2. **同步等待令发送器看到过时的系统前台。** `pudding-background-probe-AcNiP4` / `pudding-background-probe-5Y4Kqd` 的接管样本中，guard 已记录系统激活目标，但 sender 仍点击了一次，并将目标内部状态改回 inactive；最终系统前台与目标内部状态互相矛盾。等待改为运行 RunLoop，让 `NSWorkspace` / `NSRunningApplication` 接收更新，再以相同系统 API 检查。`pudding-background-probe-5oTg8d` 复测确认发送器读到了目标 PID，停止点击，保留用户真实激活的 active/key 状态。没有用新的 UI overlay 猜测前台。
3. **日志写入失败会跳过恢复。** `pudding-background-probe-bS74yw/focus-lifecycle-output-closed.json`：仅关闭日志读取通道，进程以 SIGPIPE 结束，计数 0，active/key 遗留 true。现在执行进程忽略 SIGPIPE，诊断写入采用不抛出的失败处理；控制管道仍是取消的事实源。日志丢失不能打断 mouseUp / 去激活清理，也不能据日志缺失自动重放。
4. **窗口移动不应免除进程状态恢复。** 原来的清理条件复用了“原窗口几何完全一致”，移动后会跳过去激活。现将进程身份校验和继续输入的几何校验分开；移动会停止输入，但仍对原进程恢复状态。进程身份同时核对 PID、可执行文件、启动时间及原进程是否终止；已经关闭的窗口不再补发鼠标事件。

测试曾使用 SIGSTOP 固定故障注入时点，暂停的孤儿执行进程出现与实际运行不同的退出行为（`ZoRQ2w` / `5LjJy4`）。该注入方式已删除，失败记录未改写。最终只在真实的“激活已投递”或“mouseDown 已投递”日志边界结束调用方，执行进程始终正常运行。窗口接管由另一自建进程发起，再用系统激活通知和 PID 核对，不将请求切换当作已经切换。

### 最终受控结果

全部使用全新 AppKit 接收进程、拒绝首次点击的计数按钮；七项均通过各自验收条件。此处“通过”表示边界处理符合预期，不表示取消场景仍完成了用户任务。

| 场景 | 目标计数 | 验收结果 |
| --- | --- | --- |
| 激活后取消（调用方 SIGTERM） | 0 | 停止点击，active/key 恢复 false |
| 激活后调用方 SIGKILL | 0 | 执行进程检测管道关闭，配对去激活 |
| mouseDown 后调用方 SIGKILL | 1 | 补发一次 mouseUp、恢复焦点；**属于部分执行，不得自动重试** |
| 激活后窗口平移 20 pt | 0 | 几何校验中止输入，但未遗留激活状态 |
| 用户接管模拟：实际激活目标 | 0 | 停止输入，目标继续处于系统前台，active/key 保持 true |
| 仅关闭日志通道，控制通道仍在 | 1 | 普通点击与去激活完成，不被日志错误中断 |
| 合成激活期间向前台测试框输入 abc | 1 | 前台收到 3 次 keyDown 和完整 abc，目标仅收到普通鼠标单击 |

每项 72–132 次前台采样；除了明确的接管场景外，前台始终为 guard、无系统激活通知。所有场景真实鼠标位移为 0，未出现额外按钮效果。接管场景仅记录从 guard 到目标的预期切换；动作没有把窗口切回。

前台输入由自建 guard 校验自己仍为前台后，通过系统 HID 键盘路径发送固定 `abc`；后台指针仍只用 `postToPid`。这验证了可控的前台键盘路由，**不等于真人持续打字、移动鼠标、跨应用切换或 IME 组合输入的完整验收**。没有读取用户应用的键盘输入。

### 交付与复测

- [生命周期 runner](../scripts/computer-use-focus-lifecycle-probe.cjs)、[判定测试](../scripts/computer-use-focus-lifecycle-probe.test.cjs)。原生 Fixture 增加受控窗口移动、目标激活和固定前台输入的测试命令；这些不暴露给 LLM 或生产工具。
- 最终原始日志位于本机临时目录 `pudding-background-probe-MFFKsr` 的七个 `focus-lifecycle-<scenario>.json`，包含 sender、接收端、guard、系统前台采样和实际效果。所有七项 `assessment.pass=true`，`productionReady=false`。
- 生命周期修改后，默认 Electron 普通单击回归通过：`pudding-background-probe-0KMWMQ/synthetic-focus.json`，原有基线仍为 0，新方案计数 1，三种 DOM 事件各一次、前台和鼠标不变、末态恢复。
- Swift 编译与零输入自测通过（新增控制管道 EOF 取消检查）；17 项 Node 判定测试通过。diff、新增文件空白与文档引用检查通过；所有本轮测试窗口和执行进程均已退出，证据保留在临时目录。未运行无关的 Go / Web 全量测试。

```sh
node --test scripts/computer-use-focus-lifecycle-probe.test.cjs scripts/computer-use-synthetic-focus-probe.test.cjs
# 暂停移动鼠标和切换应用；只操作新建的测试窗口。
node scripts/computer-use-focus-lifecycle-probe.cjs all
```

剩余边界：当前覆盖的是**调用方**退出，执行进程自身被 SIGKILL / 崩溃尚无外部恢复保证；多会话同时控制同一目标、真人持续并行操作、任意瞬间的焦点竞争、目标退出/重启以及完整鼠标动作仍需验证。下一步应在这些边界及真实应用通过后，才将输入后端接入现有 `actions[]`，不以本轮通过为依据直接删除前台保护。

## 第八轮：执行进程崩溃、同 App 竞争与真人并行（2026-09-09）

本轮只扩展隔离测试入口和验收判定。输入算法、生产 `PointerService`、工具 schema、权限与前台保护未改；没有启动或重启 Pudding daemon。

### 实际执行结果

| 场景 | 实际结果 | 结论 |
| --- | --- | --- |
| 上轮七项生命周期场景，全新进程集中回归 | 各自预期计数、内部状态恢复、前台隔离均符合预期 | 7/7 通过 |
| 接收端已处理激活后，SIGKILL 执行进程 | 计数 0，但 active/key 残留 true；系统前台仍是 guard | 失败：没有外部恢复 |
| 接收端已收到 mouseDown 后，SIGKILL 执行进程 | 没有收到配对 mouseUp、没有按钮回调，active/key 残留 true | 失败：输入配对和焦点恢复均未完成 |
| 两个独立调用同时操作同一窗口 | 两个调用均退出 0、各报告一组 down/up；接收端仅一次按钮回调、计数增加 1 | 失败：同 App 竞争会丢动作 |
| 真人在前台测试框输入并移动鼠标，后台顺序点击 | 30.41 秒、57 次发送、57 次按钮回调，输入完整保留 | 通过本次真人样本 |

**崩溃根因**：当前 `FocusLease.swift` 让执行进程持有激活状态和配对 mouseUp，调用方只维持控制管道。该结构解决的是调用方退出；执行进程自身被 SIGKILL 时，其 `defer` 不可能执行，调用方仅报告失败，没有恢复动作。因此不能将“调用方退出已通过”推广成任意进程崩溃可恢复。

**并发根因**：两个 `focus-probe-send` 分别创建自己的执行进程，没有共同的目标 App 排他边界。记录显示两段合成激活区间重叠，投递顺序出现 down A → down B → up A → up B；接收端按钮追踪只完成一次回调。该实验模拟的是两个调用方争用同一 App，不是已接入真实 session 的集成测试。

### 排除一次测试假阳性

最初在“激活已投递”的发送日志出现时立即结束执行进程，`pudding-background-probe-1zQjeT` 看似恢复成功，但接收端根本没有收到激活通知。这不能证明崩溃恢复。现在必须等接收端记录 `focus-event / after / active=true`；按下后场景还要求 `button-received`，再结束从本次 `lease-start` 得到的执行 PID。判定同时检查故障确实注入及调用方失败退出。修正后的失败样本为 `2GsJGB`、`N1RD9k`；假阳性不计通过。

### 真人并行样本与边界

用户手动点击独立窗口的“开始 30 秒测试”，自行输入一次 `pudding 1234567890`，并在该窗口内移动鼠标。测试未合成键盘输入，也未读取其他应用的输入。

- 前台测试框最终字符串完全匹配，记录 18 次 keyDown；其中 9 次发生在接收端**实际完成内部激活**与去激活处理之前，不仅是与发送日志时间重叠。
- 后台计数增加 57，57 次普通按钮回调，未携带 Command、未额外双击，目标没有收到键盘事件。
- 6106 次采样的系统前台均为 guard PID 2394，没有系统激活通知；目标 PID 2382 末态 active/key 均为 false。
- 实际鼠标移动约 223.77 pt，符合本轮人为移动要求。该数值不作为“鼠标不动”验收，也不能独立归因每一点位移；这里只结合真实输入、前台记录和无鼠标移动调用的发送路径验证本次并行场景。
- 本次不覆盖 IME 组合输入、跨应用切换、前台拖拽，或第三方应用的完整兼容性。

### 交付、证据与后续边界

- 新增 [并发及真人 runner](../scripts/computer-use-parallel-probe.cjs)、[判定测试](../scripts/computer-use-parallel-probe.test.cjs)；扩展 [生命周期 runner](../scripts/computer-use-focus-lifecycle-probe.cjs) 的两个执行进程故障点。原生 Fixture 增加独立输入提示及统一单调时间日志，不改变输入投递行为。
- 最终九项集中回归：`pudding-background-probe-1HobUk/focus-lifecycle-<scenario>.json`，七项通过、两项失败。各项使用全新接收进程，收集失败后不对该接收进程重放，关闭后再运行下一独立场景。
- 并发失败：`pudding-background-probe-aoUKQR/parallel-contention.json`。
- 真人通过：`pudding-background-probe-Avnqw4/parallel-human.json`。最新判定器直接读取接收端激活区间重新评估，结果仍通过，无须让用户重复输入。
- 默认 Electron 全新进程回归：`pudding-background-probe-AVh4aH/synthetic-focus.json`，原基线没有效果；合成激活方案仍准确产生一次普通、可信 DOM 点击，前台、光标、窗口几何不变且末态恢复。
- Swift 编译及零输入自测通过，20 项 Node 判定测试通过。这里的单元测试通过包含正确识别失败样本，**不表示上表的崩溃和并发集成场景通过**。所有本轮临时窗口、发送器和执行进程均已退出；原始证据留在系统临时目录，未加入仓库。

下一步修复应针对这两项已证明的失败：**按 App 管理唯一输入所有者，覆盖从激活到恢复的完整区间；执行进程异常退出时，由仍存活的监督边界接管同一份配对状态并完成恢复。** 不能仅在每个调用里各加一把锁，也不能用失败重试补回被吞的点击；mouseDown 后恢复可能已经触发一次效果，必须保留部分执行语义。监督边界与执行进程同时被结束的边界仍需明确，不能承诺无限层进程崩溃都可恢复。本轮没有实现这些修复或增加生产 fallback。

```sh
node --test scripts/computer-use-focus-lifecycle-probe.test.cjs scripts/computer-use-parallel-probe.test.cjs
# 前两项需要暂时不动鼠标、不切换应用；失败时退出码为 1，保留证据。
node scripts/computer-use-focus-lifecycle-probe.cjs all
node scripts/computer-use-parallel-probe.cjs contention
# 显示独立窗口，用户点击开始后进行 30 秒真实输入；不使用生产数据。
node scripts/computer-use-parallel-probe.cjs human
```

## 第九轮：共享输入所有权与执行进程崩溃恢复（2026-09-09）

本轮修复第八轮的两项已证实问题。改动仅位于隔离原型、测试 runner 及本文档；没有修改生产 Helper、LLM 工具 schema、session 授权或前台保护，也没有新增 `observationID` / 强制重复观察要求。

### 实现

- [FocusLease.swift](../native/macos/background-input-probe/FocusLease.swift) 对同一目标进程实例建立一个 `flock` 互斥区间，覆盖激活、全部鼠标事件、配对恢复直至执行进程退出。其他调用在发送任何事件前等待，最多 3 秒；超时明确返回 busy，不重放。锁文件位于当前原型构建临时目录，键为目标 PID 与内核启动时间，不按窗口分别加锁。
- 调用方和执行进程继承**同一个打开的文件描述符**；退出只 `close`，不主动 `LOCK_UN`。任意一方退出时，另一方仍持有锁，排队调用不能趁恢复尚未完成时进入。没有增加第三层常驻 daemon。
- 同一个文件中的单字节 journal 是恢复阶段的唯一事实源：idle、激活意图、按下意图、已释放。执行进程存活期间由它更新；调用方仅在 `waitpid` 确认执行进程退出后读取并接管恢复。删除合成激活路径原来的本地 `notified` 和配对状态；非合成诊断输入仍保持原有行为。
- 激活/按下之前先写恢复意图；普通 mouseUp 已投递后记为已释放。执行进程在按下后崩溃，调用方补发一次 mouseUp 并去激活；已释放后崩溃只恢复激活状态，不再补发 mouseUp。调用方自身退出则仍由执行进程检测控制管道 EOF 后清理。
- 中断调用仍返回失败，提示结果可能部分执行、不要重放；恢复完成不冒充原操作正常完成。新的调用在上一个操作恢复结束后才执行。

### 实施时定位的两项问题

1. 自建进程的 `NSRunningApplication.launchDate` 实际返回 nil（`pudding-background-probe-xiiA9m`），不能用它作必须存在的跨进程身份键，也不能把两个 nil 相等当作进程实例证明。原型统一改用 `proc_pidinfo(PROC_PIDTBSDINFO)` 返回的启动秒/微秒，结合现有可执行文件、bundle 和存活检查；没有缺失时退回仅校验 PID 的路径。
2. `dup2(fd, fd)` 没有清除 macOS 的 close-on-exec 标记，journal 恰好占用 FD 3 时，执行进程中 `fstat(3)` 返回 EBADF（`pudding-background-probe-9ircLp`）。现在同编号使用 SDK 提供的 `posix_spawn_file_actions_addinherit_np`，不同编号使用 `adddup2`；其余描述符默认不继承，尤其不能继承控制管道写端。补了真实 exec 的零输入自测，验证 journal 字节共享及调用方写端关闭后的 EOF。

### 验证结果

| 验证 | 结果 |
| --- | --- |
| 十项生命周期：原有七项、执行进程在激活后/按下后/释放后 SIGKILL | 10/10；恢复状态、目标效果、前台隔离均符合各自预期 |
| 同 App 两个普通并发调用 | 计数增加 2，两个普通按钮回调，无激活区间重叠，确实观测到等待锁 |
| 同 App 并发，首个执行进程在按下后 SIGKILL | 首个调用失败并完成一次配对清理；第二个等待恢复后正常执行，总计两次按钮回调 |
| 同 App 并发，首个调用方在按下后 SIGKILL | 存活执行进程保留锁直至恢复；第二个调用随后执行，总计两次按钮回调 |
| 默认 Electron 全新接收进程 | 原基线仍无效果；合成激活方案普通单击一次、可信 DOM 事件三种各一次、末态恢复 |

十项生命周期每项 75–139 次前台采样，鼠标位移均为 0；除主动接管场景外没有前台切换，接管场景保留用户真正激活目标的状态。并发测试仍是隔离调用方模拟，不是正式 Pudding 多会话集成验收。

原始证据均保留于本机系统临时目录：

- `pudding-background-probe-AjXLaq/focus-lifecycle-<scenario>.json`：十项生命周期。
- `pudding-background-probe-D1l0oQ/parallel-contention.json`：普通并发。
- `pudding-background-probe-oIo3gU/parallel-contention-worker-killed.json`：执行进程崩溃交叉测试。
- `pudding-background-probe-BfFaP8/parallel-contention-sender-killed.json`：调用方崩溃交叉测试。
- `pudding-background-probe-DTAv2g/synthetic-focus.json`：最终默认 Electron 回归。

Swift 编译、零输入自测及 22 项 Node 判定测试通过。新增测试覆盖已释放后不重复补发、并发确实排队、恢复完成前不允许第二个调用进入。diff、新增文件空白及文档引用检查通过；本轮测试窗口、调用方和执行进程均已退出，临时证据保留。未运行无关 Go/Web 全量测试，也未把上轮真人样本说成本轮重新实测。

### 明确保留的边界

- 该锁目前只协调同一个原型构建目录的调用方，未接入正式 session/Helper 生命周期，不能视作全系统任意工具之间的仲裁。
- 两个持有者同时被 SIGKILL 时，无存活进程可立即恢复；若 journal 非 idle，之后的新调用明确拒绝，不覆盖旧状态、补点或偷偷重试。零输入自测覆盖遗留状态拒绝；没有声称该场景能自动恢复。
- journal 写入与 macOS 事件投递不是原子事务。若进程恰在意图写入与事件投递之间退出，恢复只能保守释放，不能承诺任意指令时点的严格 exactly-once。特别是 mouseUp 已投递、已释放状态尚未来得及写入的极小窗口，不能由本轮“收到释放日志后注入故障”的测试排除。恢复只发送必要的 release/deactivation，不重放 mouseDown。
- 恢复投递后保留短 RunLoop 等待，并保持互斥；这不是第三方应用的完成确认，也不是最低时延证明。接收端兼容性、权限撤销、多显示器/窗口变动、全部鼠标动作、真实应用与正式签名验收仍按下列门槛推进。

```sh
node --test scripts/computer-use-focus-lifecycle-probe.test.cjs scripts/computer-use-parallel-probe.test.cjs
node scripts/computer-use-focus-lifecycle-probe.cjs all
node scripts/computer-use-parallel-probe.cjs contention
node scripts/computer-use-parallel-probe.cjs contention-worker-killed
node scripts/computer-use-parallel-probe.cjs contention-sender-killed
node scripts/computer-use-synthetic-focus-probe.cjs electron
```

## 第十轮：邮件、飞书与日历真实应用（2026-09-09）

用户选择邮件和飞书，并确认条目均为已读；飞书出现前台切换后，用户要求停止该 App，改测系统日历。没有测试文本编辑，没有发送消息、删除邮件或创建/修改日程；最后邮件、飞书恢复原选中条目，日历恢复 2026 年 9 月的月视图。

### 测试入口与判定

- 新增 [真实应用身份策略与只读窗口列表](../native/macos/background-input-probe/RealAppProbe.swift)、[手动 runner](../scripts/computer-use-real-app-probe.cjs) 与 [判定测试](../scripts/computer-use-real-app-probe.test.cjs)。复用同一 `sendWithFocusLease → worker → send → restoreFocus`，没有复制鼠标实现、增加前台 fallback 或修改生产 `PointerService`。
- 普通单击仍然只有一组无修饰键 down/up，点击次数为 1；本入口不允许双击、右键、拖拽、滚轮。多窗口必须显式选择当前窗口 ID。飞书同时有两个同几何普通窗口，按标题和截图确认选择主窗口，而不是取列表第一项。
- 首次邮件样本的目标点实际上被 Codex 窗口盖住。虽然正文成功切换、前台与鼠标不变，但不算“无遮挡”通过；没有改写该失败的布局判定。之后仅在**准备阶段**显式显示目标 App，再由自建 guard 占据前台。测量阶段不再激活目标。
- `covered=false` 的小 guard 位于目标右上角，因此“无遮挡”仅指**点击位置**，不声称整个目标窗口无遮挡。`covered=true` 的 guard 与目标同矩形；发送前核对点击位置上方的普通窗口所属 PID。
- 5 ms 前台/光标采样、Workspace 激活通知、guard 输入日志和发送配对记录共同判定投递隔离；动作前后对目标窗口截图，人工核对真实页面变化。真实应用没有注入内部回调计数器，因此不将一组发送事件等同于内部回调恰好一次，也不声称采样排除了任意短的瞬态。
- 截图只保存在本机临时证据目录，未将邮件正文、聊天内容或图片加入仓库。权限仍仅预检，没有请求、重置或修改授权；没有使用 Codex 的点击替代原型点击。

### 实测结果

| 应用与场景 | UI 结果 | 隔离与恢复 | 验收 |
| --- | --- | --- | --- |
| 邮件：目标点无遮挡，单击已读邮件列表 | 正文切到另一封邮件 | 174 次采样均为 guard，鼠标 0，配对恢复完成 | 通过 |
| 邮件：完全遮挡，连续两次切换 | 两次正文均变化，最后回到原选中邮件 | 分别 156/158 次采样均为 guard，鼠标 0，未向 guard 输入 | 2 项通过 |
| 日历：目标点无遮挡，月→年→月 | 年/月视图分别正确显示 | 分别 155/177 次采样均为 guard，鼠标 0，配对恢复完成 | 2 项通过 |
| 日历：完全遮挡，9 月→10 月→9 月 | 每次只翻一个月，最后恢复原月视图 | 分别 159/158 次采样均为 guard，鼠标 0，未向 guard 输入 | 2 项通过 |
| 飞书：目标点无遮挡，切换已读聊天 | 聊天正确切换 | 前台从 guard 变为飞书，鼠标 0 | 未通过 |
| 飞书：完全遮挡，切回原聊天 | 原聊天恢复 | 再次从 guard 变为飞书，鼠标 0 | 未通过 |

邮件/日历共 **7 个有效单击样本通过**。另保留 1 个邮件布局不符样本和 2 个飞书失败样本；不把它们混算成全通过。连续导航指同一真实 App 中先后完成多次独立调用，不是本轮验证了并发或 `actions[]` 批处理。

### 飞书失败的定位与停止边界

为区分激活通知与鼠标动作，新增受同一 lease 管理的显式 `real-app-notify` 零点击对照。`Jz1Keb` 的只通知样本没有输入事件、界面不变、131 次采样始终为 guard；紧接的另一条聊天单击则触发真实前台切换。单击 mouseUp 投递日志时间为 `302257.22707691666`，guard 的 Workspace 激活通知为 `302257.2329182917`，相差约 **5.84 ms**。发送器没有调用实际 activate/raise。

这证明本次失败发生在点击后的前台切换阶段，不是“没点到”或鼠标移动；**尚未证明飞书内部哪个调用触发激活，也不能证明其他构造方式无法避免**。只通知样本当时的激活保持时长比完整单击短，因此不足以排除时序因素。当前诊断代码已补齐两段省略的 50 ms 等待，但用户此时要求停止飞书，`1dDNGY` 在发送任何动作前退出，未将准备完成算成新的实测。之后不再操作飞书。

目标已成为系统前台时，恢复代码按原有接管规则不强行去激活；`lease-restored` 只证明共享输入状态已清理，**不是飞书后台验收成功**。没有添加“先抢前台再切回”的补救路径。用户选择换 App 不会清除飞书这个已知兼容性问题。

### 证据与复测

临时目录公共前缀为 `pudding-background-probe-`，每个真实应用目录都有 `real-app-results.json` 和 `real-app-events.jsonl`：

- `pBoBkj`：邮件首个布局不符样本，`1-before.png → 2-after.png`。
- `PXIesY`：邮件三项有效样本；`1-before.png → 2-after.png`、`3-before.png → 4-after.png → 5-after.png`。
- `AVWeCN`：飞书无遮挡失败，`1-before.png → 2-after.png`。
- `Jz1Keb`：飞书零点击对照与遮挡失败，`1-before.png → 2-after.png → 3-after.png`；后者带激活时间戳。
- `1dDNGY`：用户要求更换 App 后退出，记录数组为空，没有发送动作。
- `JaV5Kk`：日历四项有效样本；`1-before.png → 2-after.png → 3-after.png`、`4-before.png → 5-after.png → 6-after.png`。
- `Dyb0wC/synthetic-focus.json`：默认 Electron 回归，旧基线无效果，新方案一次普通单击、3 个可信 DOM 事件及 1 次计数，隔离和末态恢复通过。
- `MxbakG/focus-lifecycle-*.json`：10 类生命周期回归，首轮 8 项完整通过；`worker-killed` / `worker-killed-after-down` 恢复和输入配对均通过，但检测到约 217/148 pt 鼠标位移，隔离不通过，保留原始失败结果。
- `b3H9nu/focus-lifecycle-worker-killed.json`、`2qkXgf/focus-lifecycle-worker-killed-after-down.json`：提示暂不移动鼠标后，仅对上面两项使用全新测试窗口复测，均完整通过。没有改写首轮样本，也没有再次执行真实应用中的失败点击。

24 项 Node 判定测试及 Swift 编译、零输入自测通过；新增覆盖真实 App 固定身份、非单击拒绝、多窗口歧义、激活变化/鼠标移动判定以及零输入对照不能算作点击通过。

最终 diff、相关脚本语法、新增文件空白和文档引用检查通过；只读进程核对确认本轮自建 guard、接收端、调用方与执行进程均已退出。真实应用保持打开，临时截图/日志保留；未运行无关 Go/Web 全量测试。

```sh
node scripts/computer-use-real-app-probe.cjs mail
node scripts/computer-use-real-app-probe.cjs calendar
# stdin：prepare（显式 windowID、covered）→ 检查截图 → click（0..1 的 x/y）→ quit
# 飞书入口仍保留失败复现用途；只在用户同意后使用，不自动进入。
```

本轮没有接入生产、没有对正式签名版本验收，没有测试真实 App 的双击/右键/拖拽/滚轮；未把旧的真人并行或全部生命周期样本写成本轮真实应用测试。

## 第十一轮：有限后台单击接入现有产品链路

用户要求开始接入。行为边界先限定为已验证系统应用的普通左键单击，不把原型的诊断选项、任意目标、其他手势带入产品，也不修改已安装的 Pudding。

### 修改范围

- [工具定义](../internal/tool/builtin.go)和内置 Computer Use 指引增加可选 `delivery`。保持单个 `actions[]` 接口，后台模式必须显式选择；不增加 observationID、快照时限或强制观察。
- [Go Manager](../internal/computer/manager.go)校验动作形状，透传模式并校验结果一致；沿用 session 路由、授权和串行化。Electron 使用原 `/computer/pointer`，后台不可用/输入仍被占用时保留具体错误和 `outcome`，不自动切换前台。
- [BackgroundClick.swift](../native/macos/computer-use-helper/Sources/PuddingComputerUseHelper/BackgroundClick.swift)提供固定系统应用身份策略、PID/窗口内单击、同一 Helper 可执行文件的 worker、共享恢复 journal 及独占输入锁。原型留作历史实验，不是产品备用投递路径。详细边界见[设计文档](computer-use-design.md#2026-09-09-后台普通单击有限预览)。
- worker 和 Helper 同路径执行以保持签名来源一致；这不是正式 TCC 归属已验证的结论。双方同时强制结束、事件与 journal 更新不能原子化等边界明确保留；不把“清理可能完成一次 mouseUp”当成可安全重放。

### 当前源码真实链路回归

[手动 runner](../scripts/computer-use-background-integration.cjs)启动临时 loopback bridge、实际权限协调器与新 Helper，并调用[Go 集成测试](../internal/computer/background_integration_test.go)中的真实 Manager。隔离原型仅提供遮挡用 guard 与监测，不负责发送点击。没有经过真实 LLM/Engine 会话，也没有连接正在运行的 daemon。

测试从同一份初始 AX 数据提取日历“下一月”“上一月”按钮的坐标。后续观察只用于证明界面效果，不是动作票据。只切月份，不操作日程；正常结束恢复初始月份，不对不确定的失败自动补点。

| 样本 | 日历效果 | 隔离结果 | 判定 |
| --- | --- | --- | --- |
| `pudding-background-probe-xnR0fg` | 9 月 → 10 月 → 9 月 | 前台稳定，645 次采样；鼠标约 399.01 pt 位移 | 失败，保留，不归因于人工或实现 |
| 用户确认不移动鼠标后，`pudding-background-probe-XPmMVB` | 9 月 → 10 月 → 9 月 | 前台始终为 guard；575 次采样，鼠标位移 0，guard 无误输入 | 本次回归通过 |

两个目录均保留 `production-chain.json`；位于本机临时目录，系统清理后需重跑。复测月份为 2026 年 9/10 月。采样不能排除任意短的瞬态，真实日历没有按钮内部计数探针，因此只确认这次月份往返和投递隔离。

### 自动化验证与未完成项

- `make test` 全部通过；有链接器重复库 warning，无测试失败。
- `npm run test:electron`：213/213 通过，覆盖新模式路由、错误映射及已有取消/权限恢复契约。
- Swift Helper：3 项 XCTest + 89 项 Swift Testing 通过。新增覆盖模式/固定身份拒绝、正常单击配对、中断阶段、恢复失败、真实用户激活保护、独占/遗留 journal 和事件构造。这里的崩溃恢复是状态注入单测，不冒充新 worker 的真实 SIGKILL 回归。
- 当前源码日历实测通过，但尚未完成正式 Developer ID 安装包的 TCC 身份/升级、权限撤销、多屏缩放，以及新链路真实崩溃、取消和持续并行操作验收。后台双击/右键/拖拽/滚轮和其他 App 未开放。

## 尚未通过的通用默认后台发布门槛

1. 不依赖第三方应用开启首次点击，仍能正确执行普通单击/拖拽。普通单击已在两个受控接收端及邮件、日历本轮场景通过；飞书点击后成为系统前台的问题未解决。第八轮通过一次真人打字和移动鼠标并行样本，合成激活下的拖拽、IME 及完整人工并行输入仍未验收，不能直接推广。
2. 对非公开窗口内坐标接口做系统版本、符号缺失、发布签名与维护边界评估。当前只证明一个 macOS 版本可用，符号缺失明确失败，无 fallback。
3. 镜像真实手机界面验收：双击已分别通过后台可见、遮挡样本；普通单击未通过，滚轮只在部分页面生效，边缘拖拽未看到效果，右键未测。不能以局部通过代替完整验收。
4. 第八轮复现的执行进程崩溃与同 App 并发已在第九轮修复并通过隔离复测；双方同时退出和事件投递原子性边界见上文。权限撤销、目标缩放/关闭/重启、多屏缩放及跨应用持续前台输入仍需验证；不能由现有样本声称所有异常场景均已验证。
5. 第十一轮已把有限普通单击接入现有 `actions[]`、session/App 授权及结构化部分失败链路；正式签名、完整生命周期和并行验收未完成，不能由源码接入直接认定可通用发布。批末可选观察、镜像文本输入优化仍未实施。

因此当前结论是：**有限后台普通单击已接入，且新链路日历往返通过；飞书仍有切到系统前台的已知失败。尚不是能替换现有前台指针后端的通用实现，也不是正式签名版本已完成发布验收。**
