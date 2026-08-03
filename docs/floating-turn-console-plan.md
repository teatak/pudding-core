# 浮动 Turn 控制台改造计划

> 状态:MVP 已实施,待 Electron 实机回归
> 日期:2026-08-03
> 范围:Electron 桌面端 Web UI 的 Agent Console floating presentation。
> 结论:浮动模式改为“当前 Turn 控制台”;完整会话历史继续由 full / dock-left / dock-right presentation 承载。
> 实施更新:浮动控制台固定在工作区底部居中,不支持拖动和缩放;展开仅向上延展 turn 卡片,输入栏位置不变。

## 0. 结论

当前 floating presentation 将完整 `ChatPane` 缩进可拖拽窗口,保留标题栏、完整
Transcript 和完整 Composer。它适合把聊天当主界面,但会遮挡 Workspace,也没有与停靠模式
形成清晰分工。

本次改造后:

- floating 默认折叠,只展示当前 turn 摘要、延迟 user input 提示和紧凑 Composer。
- 展开只展示当前或最后一个真实 turn,不展示整段会话历史。
- 当前 turn 的工具执行、思考、输出、等待确认等状态通过 SSE overlay 实时更新。
- running 状态下的普通提交继续进入延迟队列;显式引导继续 steer 精确的 running turn。
- 完整历史、跨 turn 搜索和旧消息浏览只保留在 full / dock presentation。
- 完整对话继续通过顶部布局切换控件进入,浮动 turn 卡片不重复提供入口。

目标结构:

```text
折叠
┌────────────────────────────────────────────────────┐
│ ● 正在编辑 App.tsx · 11s                       展开 │
├────────────────────────────────────────────────────┤
│ 1 条消息等待发送                              可选 │
├────────────────────────────────────────────────────┤
│ +  输入消息……                 模型  麦克风  发送/停止 │
└────────────────────────────────────────────────────┘

展开
┌────────────────────────────────────────────────────┐
│ ● 正在编辑 App.tsx · 11s                       收起 │
├────────────────────────────────────────────────────┤
│ 当前或最后一个 turn                               │
│ - user input                                      │
│ - thought / tool / approval / guide sequence      │
│ - assistant output / error / file changes         │
├────────────────────────────────────────────────────┤
│ 延迟 user inputs                                   │
├────────────────────────────────────────────────────┤
│ +  输入消息……                 模型  麦克风  发送/停止 │
└────────────────────────────────────────────────────┘
```

## 1. 当前基线

已有能力:

- `App.tsx` 使用 `react-rnd` 承载 floating Agent Console,支持拖拽、缩放、吸边和底边锚定。
- `ChatPane` 统一处理 session 选择、标题、状态、删除、重命名和分屏。
- `Conversation` 组合完整 Transcript、Composer、搜索、拖放和底部遮罩。
- `useTranscriptData` 已合并 canonical turns、assistant overlay、pending user input 和
  queued input。
- `useTranscriptViewModel` 已处理 live overlay 到 canonical turn 的对账、同 turn steer
  sequence 和 pending user input。
- `TurnParts` 已有 thought / tool 的紧凑过程归纳、工具显示名和 i18n 文案。
- Composer 已支持 running 时普通 submit 进入 `queued_inputs`,以及显式 steer 当前
  `runningTurnID`。
- queued input 已支持编辑、取消和提升为当前 turn 的引导输入。
- approval 与 input flow 已经在 Composer 上方提供可交互面板。

现有后端契约保持不变:

- 普通 `submit` 在已有 running turn 时进入 `queued_inputs`。
- steer 只接受 URL 中仍在运行的精确 `turnID`。
- steer 失败不会静默创建下一 turn,queued steer 失败时消息继续留在队列中。
- 引导输入属于原 turn,在安全采样边界进入 provider context。
- 队列按顺序提升;`editing` 的队首会阻塞后续输入越过它。

## 2. 产品目标

### 2.1 必须做到

- Workspace 是主工作面时,floating console 不大面积遮挡工作内容。
- 折叠状态能够说明当前 agent 正在做什么。
- 展开状态只显示一个 turn,但保留该 turn 内完整的 user / guide / tool / assistant
  顺序。
- 用户在 turn 运行中仍能明确选择“稍后发送”或“引导当前 turn”。
- 延迟 user input 必须可见、可编辑、可取消,运行时可提升为 guide。
- approval、request user input 和 input flow 不能被折叠隐藏而导致 turn 卡住。
- cancel、失败、SSE 重连和 overlay → canonical 替换期间不重复、不闪回、不丢状态。
- 切换 floating / dock 时保留 session draft、附件、模型选择和排队状态。

### 2.2 非目标

- 不新增 floating 专用后端 API。
- 不新增 backend focus、current session 或 daemon 级 floating 状态。
- 不在 floating 中提供完整历史浏览、历史分页、会话搜索或 split transcript。
- 不复制一套 Composer submit / steer / queue 业务逻辑。
- 不把 queued input 当作 canonical turn 渲染。
- 不保留“紧凑 floating”和“完整 floating”两个长期并行产品模式。

## 3. Presentation 边界

Agent Console presentation 定义为:

| presentation | 内容 | 用途 |
| --- | --- | --- |
| `full` | 完整 ChatPane + Transcript + Composer | Workspace 关闭时的主会话 |
| `dock-left` | 完整 ChatPane + Transcript + Composer | 左侧停靠完整会话 |
| `dock-right` | 完整 ChatPane + Transcript + Composer | 右侧停靠完整会话 |
| `floating` | Turn 摘要 + 单 turn + queued inputs + compact Composer | Workspace 上方的轻量控制台 |

floating 中不显示:

- 完整 session header。
- Conversation search。
- Transcript history loader 与 jump-to-latest。
- 多 turn 列表。
- split pane。

完整对话继续由页面顶部的布局切换控件进入;floating turn 卡片内不重复显示入口。

## 4. 浮动控制台状态模型

浮动控制台的 UI 状态只属于前端:

```ts
type FloatingTurnConsoleState = {
  expanded: boolean;
  width: number;
  expandedHeight?: number;
};
```

约束:

- `expanded` 默认 `false`。
- 是否持久化 `expanded` 属于 UI 偏好;第一版不持久化,每次进入 floating 默认折叠。
- width 和最后 dock 方向可以写 localStorage。
- sessionID 继续来自 Router 的显式 `selectedSessionID`。
- running turn、queued inputs、canonical turns 继续来自 session-scoped Query / SSE。
- 不向 daemon 写入任何 presentation 或 selection 状态。

## 5. 当前/最后 Turn 选择规则

新增纯选择器 `selectFloatingTurn(turnVMs, runningTurnID)`。

选择优先级:

1. 存在 `runningTurnID` 时,选择精确匹配该 ID 的 live / phase / canonical-ready turn。
2. overlay 尚未接受 turnID 时,可选择与 submitting `clientMessageID` 匹配的 pending turn。
3. 空闲时,从后向前选择最后一个有 `turnID` 的真实 turn。
4. 跳过独立 queued / editing pending input、compact pending marker 和无 turnID 的 UI marker。
5. steer / guided inputs 继续作为同一 turn 的 `sequence` 展示,不得拆成新 turn。

选择器只消费现有 `TranscriptTurnVM[]`,不自行拼接 canonical message 和 overlay,避免产生
第二套对账规则。

当 running turn 完成并发生 overlay → canonical 替换时:

- 选中的逻辑 turnID 保持不变。
- 展开状态保持不变。
- 单 turn 容器不得同时渲染 live 与 canonical 两份输出。
- canonical ready 后沿用现有 reveal / reconcile 机制清理 overlay。

## 6. 折叠摘要

### 6.1 摘要来源

折叠摘要只描述当前 running turn;没有 running turn 时显示最后 turn 的终态或简短完成信息。

数据优先级:

1. pending approval / input flow。
2. 当前 active tool。
3. streaming text。
4. active thought。
5. turn phase。
6. completed / failed / cancelled 终态。

### 6.2 共享归纳逻辑

`TurnParts.tsx` 内已有 `currentProcessPart`、`processCompactTitle`、
`processCompactLabel`、文件工具分组和 `toolDisplayName`。本次应抽取纯模块,例如:

```text
web/src/components/transcript/turnActivitySummary.ts
```

建议接口:

```ts
type TurnActivitySummary = {
  active: boolean;
  phase?: TurnPhase;
  label: string;
  detail?: string;
  elapsed?: string;
  failed?: boolean;
};

describeTurnActivity({ parts, phase, elapsed, t }): TurnActivitySummary
```

完整 transcript 的过程折叠行和 floating 摘要必须共用该函数。新增或改名工具时继续只维护一套
工具显示名与 i18n,不得在 floating 中暴露 `snake_case` 名称。

### 6.3 推荐文案

| 状态 | 示例 |
| --- | --- |
| submitting | 正在提交 |
| awaiting model | 正在等待模型 |
| thinking | 正在思考 · 8s |
| file read / slice | 正在打开 App.tsx · 11s |
| file write / patch | 正在编辑 App.tsx · 11s |
| file search | 正在搜索文件 · 11s |
| command | 正在运行命令 · 11s |
| browser | 正在操作浏览器 · 11s |
| streaming text | 正在生成回复 · 11s |
| approval | 需要确认 |
| input flow | 需要你的输入 |
| completed | 已完成 · 11s |
| failed | 执行失败 |
| cancelled | 已停止 |

路径只显示 basename 或 Project 相对短路径,不在折叠摘要暴露过长绝对路径。多个同类文件工具
继续使用现有“正在读取多个文件 / 正在更新多个文件”聚合语义。

## 7. 展开的单 Turn 区域

展开后复用 `TranscriptTurn`,只传入第 5 节选择出的一个 `TranscriptTurnVM`。

保留:

- 首个 user input。
- steer / guide sequence。
- thought 和 tool 的现有紧凑过程行。
- assistant live delta 与 canonical output。
- approval、错误、中断和 turn file changes。
- assistant duration / model meta。

不保留:

- 多 turn 虚拟列表。
- 加载更早历史。
- jump-to-latest。
- Conversation search。
- 全局 transcript 顶部/底部占位。

容器规则:

- 最大高度受 stage 限制,内容超出后只滚动单 turn 区域。
- 新 delta 到达且用户位于底部时保持贴底。
- 用户向上滚动后不强制抢回底部;摘要仍持续更新。
- 收起再展开时优先回到该 turn 最新位置。
- 切换 session 后重置单 turn 滚动位置和展开状态。

## 8. 延迟 User Input 与引导

### 8.1 语义保持

running turn 存在时:

- `Enter`:普通 submit,由后端进入 `queued_inputs`,产品文案为“稍后发送”。
- `⌘ Enter`:steer 当前精确 `runningTurnID`,产品文案为“引导当前任务”。
- 已排队消息可以点击“引导”原子提升到当前 turn。
- steer 竞态失败时不得丢失输入;直接 steer 保留草稿供重试,queued steer 保留原队列项。

空闲时:

- `Enter`:创建新 turn。
- 不显示 steer 动作。

### 8.2 Queued Input Strip

queued / editing inputs 独立显示在单 turn 区域与 Composer 之间,不得混入最后 turn。

折叠状态:

- 0 条时不占空间。
- 1 条时显示“1 条消息等待发送”。
- 多条时显示数量和队首简短预览。
- 点击后展开队列明细,不展开整段会话。

队列明细复用现有 `UserInput` 管理能力:

- 编辑。
- 保存并恢复 `queued`。
- 取消。
- running turn 存在时提升为 guide。
- 附件、文件夹、Project reference 和 UI context 不因紧凑模式丢失。

必须表达 `editing` 队首的阻塞语义:队首编辑未保存时,显示“编辑完成后继续发送”,后续队列
不能在 UI 中表现为即将越过它执行。

### 8.3 状态分层

- 顶部摘要描述 running turn,不被 queued input 覆盖。
- queued strip 描述未来 user turns。
- steer 成功后,消息从 queued strip 消失,进入当前 turn 的 guide sequence。
- queued input 自动提升为下一 turn 后,单 turn selector 切换到新的 running turn。
- cancel 当前 turn 不应在前端擅自取消 queued inputs;继续遵循后端 drain 语义。

## 9. Compact Composer

Composer 增加 presentation/variant 参数,例如:

```ts
type ComposerVariant = "default" | "floating";
```

业务逻辑保持单份:

- draft 与 `clientMessageID`。
- submit / queue / steer / cancel mutations。
- 附件上传、文件夹、Project reference、UI context。
- model / reasoning 选择。
- microphone / voice。
- slash command 和 mention。
- approval / input flow。

floating 仅改变结构与可见层级:

- 默认单行 textarea,输入增长时最多自动扩展到约 4 行。
- 左侧保留 Add。
- 右侧保留 model/reasoning、mic、send/stop。
- Project、UI context、background process、context usage 等次要控件收进 overflow 或 tooltip,
  不完全删除能力。
- 不展示 Mascot。
- 移除只为完整 Transcript 服务的上下渐隐遮罩。
- 附件 chips 仍显示在输入行上方。
- running 时明确展示 Enter“稍后发送”和 `⌘ Enter`“引导”的提示。

approval 与 input flow 是阻塞性动作:

- 出现时 floating console 自动展开到足以完成操作的高度。
- 摘要显示“需要确认”或“需要你的输入”。
- 用户处理完成后恢复此前的展开/折叠状态。
- 不把审批按钮塞进折叠摘要行。

## 10. 窗口布局与交互

当前 floating 默认宽 380px、高 560px、最小高 320px。改造后建议:

| 项目 | 建议值 |
| --- | --- |
| 默认宽度 | 600px |
| 最小宽度 | 420px |
| 最大宽度 | `min(760px, stage width - 32px)` |
| 折叠高度 | 内容实测,目标约 96–132px |
| 展开高度 | 320–420px,受 stage 限制 |
| stage inset | 16px |

交互规则:

- 顶部摘要条兼作 floating drag handle。
- 展开按钮、队列按钮和全部输入控件属于 `no-drag-region`。
- 折叠时只启用左右宽度调整;高度由内容决定。
- 展开时可启用顶部高度调整,底边保持锚定。
- 展开/收起时从底部向上生长,不能跳离用户已放置的位置。
- stage resize 后沿用现有 clamp / snap 规则。
- 左上角靠近 Electron traffic lights 时继续遵守安全 inset。
- 动画覆盖高度、阴影和圆角,但 drag / resize 期间禁用动画。

`react-rnd` 当前使用受控 width/height。建议由 floating shell 使用 `ResizeObserver` 测量
compact content 的目标高度,回传 `App` 更新 frame;不要同时让 `Rnd`、CSS auto height 和内部
组件分别拥有高度事实源。

## 11. 组件改造

建议组件边界:

```text
App
  AgentConsoleShell (Rnd / dock / floating geometry)
    ChatPane presentation
      Conversation presentation
        full/dock:
          Transcript
          Composer(default)
        floating:
          FloatingTurnConsole
            FloatingTurnSummary
            FloatingTurnDetail -> TranscriptTurn(one)
            FloatingQueuedInputs
            Composer(floating)
```

预计文件:

| 文件 | 改动 |
| --- | --- |
| `web/src/App.tsx` | floating 尺寸、展开高度与 presentation 透传 |
| `web/src/components/ChatPane.tsx` | floating 隐藏完整 header/search,保留 session 解析 |
| `web/src/components/Conversation.tsx` | 按 presentation 选择完整 Transcript 或 FloatingTurnConsole |
| `web/src/components/FloatingTurnConsole.tsx` | 新增摘要、单 turn、queue strip 组合 |
| `web/src/components/Composer.tsx` | 新增 floating variant,复用业务状态与 mutations |
| `web/src/components/ComposerToolbar.tsx` | floating 控件层级与 running shortcut 提示 |
| `web/src/components/transcript/turnActivitySummary.ts` | 抽取共享 turn/tool 摘要逻辑 |
| `web/src/components/transcript/TurnParts.tsx` | 改用共享摘要函数和工具显示名 |
| `web/src/components/transcript/floatingTurn.ts` | 纯 selector,选择 current/last real turn |
| `web/src/i18n/index.ts` | floating、queue、activity 的简体/繁体/英文文案 |

`Conversation` 中 Composer 应保持稳定 React identity。切换 presentation 时只切换其 variant 与
上方 view,避免重挂 Composer 导致上传 mutation、mention menu 或临时交互状态丢失。session draft
仍由 `sessionDraftStore` 兜底。

## 12. 实施阶段

### F0 选择器与摘要纯化

- 新增 current/last turn selector。
- 从 `TurnParts` 抽取 activity summary、file grouping 和 tool display name。
- 补齐 file open/edit、command、browser、streaming、approval、error/cancel 文案。
- 不改变现有完整 Transcript 的视觉行为。

验收:

- 现有 Transcript 过程折叠行无回归。
- floating 摘要不出现 snake_case。
- selector 不会把 queued input 选成最后 turn。

### F1 Floating Turn Console 骨架

- 透传 presentation。
- floating 隐藏完整 header 和 Transcript。
- 新增折叠摘要与展开的单 `TranscriptTurn`。
- 复用顶部布局切换控件进入完整对话。
- 接入 canonical/live reconciliation。

验收:

- 默认折叠。
- 展开永远只有一个 turn。
- running turn 优先于最后 completed turn。
- overlay → canonical 无重复内容。

### F2 Compact Composer 与窗口几何

- Composer 增加 floating variant。
- 改为单行核心控件布局,保留业务逻辑。
- 改造 floating width / dynamic height / bottom anchor。
- 摘要条接管拖拽,完善 no-drag regions。

验收:

- 输入、附件、发送、停止、模型和语音均可用。
- 展开/折叠不清空草稿。
- stage resize、吸边、拖拽和底边锚定稳定。

### F3 Queue / Steer / Blocking Interaction

- 新增 queued input strip。
- 接入编辑、取消、提升为 guide。
- 明确 Enter queue / `⌘ Enter` steer。
- approval 与 input flow 自动展开。
- 覆盖 direct steer 与 queued steer 的竞态失败。

验收:

- queued input 不混入 last turn。
- steer 后进入当前 turn sequence。
- editing 队首阻塞时 UI 不误导。
- approval/input flow 在 floating 中可完成。

### F4 回归与打磨

- 补齐三语言 i18n。
- 完成 light/dark、窄 stage、高 DPI 和 Electron shell 验证。
- 验证 full / dock presentation 无回归。
- 根据真实操作录屏调整高度、间距和动画。

## 13. 测试计划

### 13.1 纯函数测试

- runningTurnID 精确选择。
- pending submit 到 accepted turn 的选择切换。
- completed turn 回退选择。
- queued / editing / cancelled input 排除。
- guide sequence 保留在原 turn。
- file read / write / patch / multiple files 摘要。
- command、browser、unknown MCP/App tool 摘要。
- phase、elapsed、failed、cancelled 摘要。

### 13.2 组件测试

- 默认折叠、按钮 `aria-expanded` 和键盘操作。
- 展开只渲染最后一个 turn。
- live delta 增长与 canonical 替换。
- queued count、展开、编辑、保存、取消、guide。
- Enter queue、`⌘ Enter` steer。
- direct steer 409 时草稿保留。
- queued steer 409 时队列项保留。
- approval/input flow 自动展开与处理后恢复。
- Composer variant 切换时草稿和附件不丢失。

### 13.3 Electron / 布局回归

- floating 拖过 `<webview>` 时不丢 pointer events。
- 摘要条可拖,按钮/textarea 不触发拖拽。
- stage 变窄、变矮后的 clamp。
- 四边吸附和 bottom anchor。
- traffic lights 安全区域。
- floating → dock-left/right → floating 的尺寸和草稿恢复。
- Workspace 打开/关闭与 session 切换。

## 14. 完成标准

以下条件全部满足才算交付:

- floating 默认占用高度不超过约 132px,除非存在附件、审批或 input flow。
- 折叠摘要能实时表达当前 turn 的主要活动。
- 展开区域只显示当前或最后一个 turn。
- 完整历史只能通过 full / dock presentation 查看。
- running 时用户能区分“稍后发送”和“引导当前任务”。
- queued input 可见、可编辑、可取消、可提升,且不冒充 canonical turn。
- approval / input flow 不会因折叠而不可操作。
- SSE 重连和 overlay → canonical 不产生重复 turn。
- floating / dock 切换不丢草稿、附件和 session scope。
- 新 UI 文案覆盖简体中文、繁体中文和英文。
- 前端测试、类型检查和 Electron 布局 smoke 通过。

## 15. 估时与风险

预计 2–3 个工作日:

| 工作 | 估时 |
| --- | --- |
| selector 与共享摘要逻辑 | 0.5 天 |
| Floating Turn Console 与单 turn 展开 | 0.5 天 |
| Compact Composer 与窗口几何 | 0.5–1 天 |
| Queue / steer / approval / input flow | 0.5 天 |
| 测试、i18n 与 Electron 回归 | 0.5 天 |

主要风险:

- `react-rnd` 受控高度与内部内容实测高度互相争夺,导致展开时位置跳动。
- Composer presentation 切换重挂,导致临时上传或交互状态丢失。
- 重新实现工具摘要造成完整 Transcript 与 floating 文案漂移。
- queued input 被错误选为 last turn,或 steer 后短暂同时出现在 queue 和 turn sequence。
- approval/input flow 被过度简化后不可操作。

缓解方式:

- 窗口高度只保留 App/shell 一个事实源。
- Composer 保持同一组件 identity,只切 variant。
- 抽取并共享 activity summary 与 tool display name。
- last turn selector 只消费现有 `TranscriptTurnVM`,按 turnID 对账。
- 阻塞性动作自动展开并复用现有完整交互组件。
