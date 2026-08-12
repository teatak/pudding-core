# Transcript 会话窗口虚拟化开发计划

> 状态:已实现,进入持续回归。
> 日期:2026-08-12。
> 范围:Electron 桌面端会话内容窗口,不包含左侧 SessionRail,也不包含 FloatingTurnConsole。
> 结论:以 turn 为虚拟化单位,采用 `@tanstack/react-virtual` 作为无头虚拟化内核;Pudding 继续拥有滚动意图、搜索定位和历史加载策略,不自研通用虚拟列表算法。

本文承接 `docs/transcript-scroll-plan.md` 已落地的产品行为,并取代其中“全部已加载 turn 常驻 DOM + 手写 DOM anchor”的实现方式。旧文档继续作为滚动契约的历史记录,不再指导虚拟列表实现。

## 实施记录

2026-08-12 已完成正式单路径替换:

- 锁定 `@tanstack/react-virtual@3.14.9`。
- 所有已获取 pages 进入 `TranscriptTurnVM[]`,删除 `visibleTurnCount` 渲染节流。
- turn 使用稳定 key、动态测量和 3 turn overscan;footer/composer clearance 进入同一 total size。
- 虚拟窗口整体定位,窗口内已挂载 turn 使用正常文档流堆叠,宽度重排期间也不会互相覆盖。
- 删除旧 IntersectionObserver、手写 prepend anchor、bottom distance、多帧 rAF stick 和自研 smooth scroll。
- search/reveal 改为 `turnID -> index -> scrollToIndex -> mounted turn 内精确定位`。
- `Cmd/Ctrl+A` 与普通复制只作用于当前已挂载 turn,不额外序列化未挂载内容。
- 宽度重排时需要同步提交 virtualizer 的测量结果,避免 `scrollTop` 与 turn transform 相差一帧;显式使用 `useFlushSync: true`。
- latest 模式下动态内容增长额外监听 virtualizer total-size 容器变化并重发 `scrollToEnd`;它只消费产品 follow 意图,不自行计算 item 几何。
- viewport 宽高变化时,latest 意图在同一帧重发 `scrollToEnd`;history 意图不移动。
- 发送后进入最新由 Composer 显式提交信号驱动,不从 pending 数组顺序推断。

真实页面回归结果:

- 47-turn/669-message 会话分两次 prepend 到 40/47 turn,可见锚点保持,同时仅 mount 约 9 个 turn。
- 临时 500-turn/1000-message fixture 全部加载后仅 mount 7-11 个 turn;第 1、250、500 个 turn 搜索定位与 Jump Latest 通过。
- latest 状态展开/收起底部动态详情保持贴底;阅读历史时不会回弹。
- `npm --prefix web run build` 通过。500-turn fixture 只存在临时测试库,未进入仓库。

## 0. 决策摘要

这项工作长期需要做,但不能只把 `turns.map` 换成虚拟列表。

当前 Transcript 已经具备一套完整的滚动契约:

- 首次进入定位最新消息。
- 用户停在最新位置时跟随流式输出。
- 用户阅读历史时不被新消息拉走。
- 从顶部加载旧 turn 后保持阅读位置。
- 分屏和窗口 resize 后保持 bottom/history 状态。
- 搜索结果与 SessionRail turn 定位可以跳到精确消息。
- thought、tool、代码、图片和 disclosure 展开后动态改变高度。
- Transcript 内支持作用域选择与复制。

虚拟化必须替换底层几何和 DOM 可见性路径,同时保留这些产品行为。最终只保留一套虚拟列表路径,不增加 feature flag、旧列表 fallback 或双轨滚动状态。

选型结论:

1. 使用 `@tanstack/react-virtual`。
2. 使用正常的从旧到新 DOM 顺序,不使用 `column-reverse` 或反向 transform。
3. `TranscriptTurn` 是唯一业务虚拟项;不对 message、content part、Markdown block 或 tool row 做二级虚拟化。
4. TanStack Virtual 是滚动几何的唯一事实源;删除当前手写的列表高度、可见 turn、prepend 补偿和 bottom distance 并行计算。
5. Pudding 自己维护产品命令:发送后进入最新、用户点击跳到最新、搜索/turn reveal、历史加载触发。
6. 已用临时测试库验证 Electron + React 19 + 动态高度,fixture 未进入主线。

开发前预计完整开发量为 **8-12 人天**;当前功能路径已完成,仍可补充长期 Performance trace 基线。

## 1. 当前实现与规模证据

### 1.1 数据加载已经分页,DOM 仍会持续增长

`web/src/components/transcript/useTranscriptTurns.ts` 当前:

- 每页 20 个 turn。
- 首屏展示最近 20 个 turn。
- 每次上滑把可见数量增加 20。
- 搜索或 turn reveal 会继续取页,直到目标 turn 被包含。

`web/src/components/transcript/TranscriptList.tsx` 最终仍然执行:

```tsx
turns.map((turn) => <TranscriptTurn key={turn.key} ... />)
```

因此分页控制了首次请求量,但用户加载过的历史 turn 会持续留在 DOM。分页和虚拟化解决的是两个不同问题:

- 分页限制网络与 canonical 数据加载。
- 虚拟化限制 React mount、DOM、样式计算、布局与绘制。

正式虚拟化后删除 `visibleTurnCount` 这层渲染节流。`useInfiniteQuery` 中已取得的 pages 全部进入有序 turn 数据,是否 mount 只由 virtualizer 决定。

### 1.2 当前真实数据只能说明收益开始出现,不能代表长期上限

2026-08-12 对本地开发库做只读统计:

| 指标 | 当前值 |
| --- | ---: |
| session 总数 | 16 |
| turn 总数 | 128 |
| canonical message 总数 | 1503 |
| 单 session 最大 turn 数 | 47 |
| 最大 session 的 message 数 | 669 |

当前首屏只有 20 个 turn,所以短会话收益有限。但单个 turn 可以包含多段 assistant sequence、thought、tool call/result、代码、图片和文件变化;DOM 成本不能只用 turn 数判断。长期会话超过 100-500 个 turn 后,现有累积 mount 模式不可持续。

### 1.3 已有优化不能替代虚拟化

`TranscriptTurn` 已经使用 `memo` 和细粒度 equality comparator。它能避免未变化 turn 的 React 重渲染,但不能减少:

- 已挂载节点数量。
- Markdown、Shiki、tool details 和图片相关 DOM。
- resize 时的样式重算与布局范围。
- 搜索 highlight 对全部已挂载 message root 的 DOM 扫描。
- IntersectionObserver 和 ResizeObserver 的观察成本。

因此虚拟化的主要收益是限制 mount 和 layout 范围,不是优化流式 turn 本身的 React 更新。

### 1.4 当前滚动逻辑与真实 DOM 深度耦合

`TranscriptList.tsx` 当前约 1370 行,其中多处依赖所有历史 turn 已挂载:

- `IntersectionObserver` 维护可见 turn element 集合。
- history prepend 前捕获 turn DOM top,更新后用 `scrollTop += delta` 恢复。
- resize 时在 assistant Markdown 子节点中选择可见 anchor。
- turn reveal 和搜索先 `querySelector` 找真实 DOM,再修正 `scrollTop`。
- CSS Highlight API 扫描已挂载的 message root。
- bottom stick 使用 `scrollHeight - clientHeight - scrollTop`。
- disclosure open/close 与多帧 rAF 共同修正 bottom。

这些路径不能原样叠加在 virtualizer 上,否则会出现两个几何事实源。正式迁移必须逐项替换并删除旧实现。

## 2. 目标与非目标

### 2.1 必须做到

- 每个 pane 拥有独立 virtualizer 和滚动意图,不按 sessionID 全局共享。
- 已加载 100-500 个 turn 后,DOM 中只 mount viewport、overscan 和必要辅助项。
- bottom 模式下,流式 token、图片加载、tool 展开和 composer 高度变化后仍保持最新位置。
- history 模式下,新消息、流式增长、prepend history、窗口 resize 和分屏 resize 不抢滚动。
- 搜索结果和 turn reveal 可以定位未 mount、甚至尚未加载的 turn。
- canonical pages、live overlay 和 pending user 的合并规则保持不变。
- 普通滚轮、触控板、系统滚动条、键盘滚动和文本局部选择继续使用 Electron Chromium 原生行为。
- 主 pane 与 split pane 行为一致且互不影响。
- 迁移完成后删除旧的可见 element、手写 prepend anchor 和列表级 content height 路径。

### 2.2 本次不做

- 不修改后端 turn 分页 API 和 page size。
- 不一次性预取全部历史。
- 不虚拟化单个 turn 内部的 message、tool 或 Markdown block。
- 不处理一个异常巨大 turn 的内部性能;这应通过 tool output 折叠、代码渲染或内容上限单独解决。
- 不修改 FloatingTurnConsole;它只渲染当前 turn,没有长列表问题。
- 不把滚动位置写入后端、Zustand 或 session runtime。
- 不新增移动端兼容分支。
- 不用 `content-visibility` 作为虚拟列表 fallback。

## 3. 三方库还是自研

### 3.1 候选对比

| 方案 | 优点 | 主要问题 | 结论 |
| --- | --- | --- | --- |
| `@tanstack/react-virtual` | 无头 API;动态测量;稳定 key;end anchor;prepend、streaming、scrollToIndex/End;可保留现有 DOM 与产品控制器 | 需要自己接搜索、选择和产品滚动命令;chat API 较新,必须先在 Electron 验证 | **采用** |
| `react-virtuoso` | 高层组件;自动动态高度;prepend、follow output 和双向加载能力完整 | 会接管 scroller、item wrapper 和较多滚动策略;与现有 1370 行行为迁移边界重叠,更容易形成双轨 | 不采用 |
| `react-window` | API 小,生态成熟 | 动态高度成本更高;缺少针对 chat prepend、end anchor 和 streaming 的完整语义,需要补更多自研逻辑 | 不采用 |
| 完全自研 | API 可完全贴合现状 | 需要长期维护 range、测量 cache、ResizeObserver、估算误差补偿、prepend 和 scroll correction;最难的部分正是通用算法 | 不采用 |

官方资料:

- [TanStack Virtual Chat 指南](https://tanstack.com/virtual/latest/docs/chat)
- [TanStack Virtualizer API](https://tanstack.com/virtual/latest/docs/api/virtualizer)
- [React Virtuoso 能力说明](https://virtuoso.dev/react-virtuoso/)
- [react-window 官方仓库](https://github.com/bvaughn/react-window)
- [`@tanstack/react-virtual` npm 包](https://www.npmjs.com/package/@tanstack/react-virtual)

### 3.2 为什么选择 TanStack Virtual

当前项目已经使用 TanStack Query 和 Router,但这不是决定性理由。真正匹配的是它的职责边界:

TanStack Virtual 负责:

- visible/overscan range 计算。
- item measurement 与 measurement cache。
- 未测量 item 的尺寸估算。
- 稳定 key 下的 prepend 位置保持。
- end anchor 与 streaming item 高度增长。
- `scrollToIndex`、`scrollToEnd`、`isAtEnd` 和 total size。
- viewport 与 item ResizeObserver 集成。

Pudding 负责:

- 何时加载更早历史。
- 用户发送消息时强制进入最新位置。
- Jump Latest 的产品行为。
- search target / turn reveal 的两段定位。
- disclosure 状态和 tool 交互。
- 会话级选择/复制语义。
- 新消息数量和按钮显示。

这个边界只保留一个滚动几何事实源,同时避免高层聊天组件重写产品行为。

### 3.3 版本策略

文档编写时 npm 当前版本为 `3.14.9`,MIT License。正式开发时:

1. PoC 显式安装并锁定已验证的准确版本,不直接跟随 `latest`。
2. 将版本写入 `web/package.json` 和 `web/package-lock.json`。
3. 验证 React 19 下 `useFlushSync` 行为;以 resize 时 `scrollTop` 与 turn transform 同帧一致为验收标准。
4. 后续升级 virtualizer 必须单独跑 Transcript 滚动回归,不作为普通依赖批量升级的一部分。

## 4. 目标数据与渲染结构

```text
TanStack Query infinite pages
  -> flatten all fetched pages in canonical order
  -> merge canonical turns + pending/live overlay
  -> TranscriptTurnVM[]
  -> pane-local Transcript virtualizer
  -> mounted TranscriptTurn subset
```

状态所有权:

| 状态 | 唯一事实源 |
| --- | --- |
| canonical turns/messages | TanStack Query infinite pages |
| pending/live turn | Zustand session overlay |
| turn view model | `useTranscriptViewModel` 的派生结果 |
| item 高度与 virtual offset | pane-local TanStack Virtual instance |
| latest/history 判断 | `virtualizer.isAtEnd()` |
| 当前搜索结果 | ConversationSearch query/state |
| disclosure open state | 当前 pane 的 Transcript 本地状态 |

### 4.1 虚拟化单位

一个 `TranscriptTurnVM` 对应一个虚拟项:

```ts
count: turns.length
getItemKey: (index) => turns[index].key
```

选择 turn 而不是 message 的原因:

- turn 是 canonical 分页和 overlay 对账边界。
- user、assistant sequence、tool 和 file changes 需要保持一个交互上下文。
- 搜索与 SessionRail reveal 已经使用 turnID 定位。
- `TranscriptTurn` 已经 memoized。
- message 级虚拟化会增加嵌套 measurement、选择、tool disclosure 和流式 overlay 的复杂度。

`turn.key` 必须在 canonical、pending 和 overlay 替换期间保持现有稳定语义。禁止使用 index key。

### 4.2 动态高度

每个虚拟 turn wrapper 使用 `virtualizer.measureElement`,由 virtualizer 观察实际高度变化。虚拟窗口整体按首项 offset 定位,窗口内 turn 以正常文档流和统一 gap 堆叠;禁止为每个动态 turn 单独使用绝对定位。

初始配置建议:

- `anchorTo: "end"`
- `followOnAppend: false`,由 Pudding 的 latest 意图显式控制 append/动态增长跟随
- `scrollEndThreshold: 8`
- `overscan: 3`
- `gap: 22`
- `paddingStart: 22`
- 尾部虚拟辅助项包含列表底部留白与 composer overlay 高度
- `useFlushSync: true`

`estimateSize` 先使用一个由混合 fixture 测得的统一保守值。没有测量证据前不按 message 类型建立复杂高度预测器。

overscan 从 3 个 turn 起步,最终值由快速滚动时是否出现空白和 mount 成本共同决定。不要根据 session 长度动态切换两套策略。

### 4.3 辅助内容

业务虚拟项只有 turn。提交错误和底部 composer clearance 作为列表尾部辅助布局处理,不能伪装成 canonical turn,也不能影响 turn index/turn key 映射。

尾部辅助布局必须进入同一个 total-size 计算路径;不能再用 virtualizer 之外的 `scrollHeight` 作为第二事实源。

## 5. 滚动行为迁移

### 5.1 初始化与 Jump Latest

- session 首次有数据后调用一次 `scrollToEnd({ behavior: "instant" })`。
- 用户点击 Jump Latest 使用 `scrollToEnd({ behavior: "auto" })`,避免动态测量列表的长距离 smooth 反复校正。
- programmatic layout correction 不使用 smooth。
- 删除当前自研 180ms rAF smooth scroll。

### 5.2 latest/history 状态

- `virtualizer.isAtEnd(8)` 是唯一 latest 判断。
- `onLatestChange` 和新消息计数从该值派生。
- 用户发送消息时显式调用 `scrollToEnd`,即使发送前正在阅读历史。
- 普通 append 只在原本已 at end 时跟随,由 pane-local latest 意图处理。
- 用户在历史位置时,末尾 streaming turn 不强制 mount;它只更新数据,不应增加离屏 DOM 成本。

删除:

- `distanceFromBottom` 的业务判断。
- `autoStickRef` 与 virtualizer pinned 状态的并行事实。
- 为内容增长执行的多帧 bottom correction。
- 仅为区分浏览器/programmatic scroll 保留的重复几何状态。

如果 PoC 证明 library 无法覆盖某个已存在行为,先用最小复现确认 library 边界,再决定单一扩展点;不保留整套旧控制器兜底。

### 5.3 历史 prepend

加载触发条件改为首个 virtual item index 接近 0,并沿用现有一次只加载一页的网络策略。

稳定 key + `anchorTo: "end"` 负责 prepend 后保持同一 turn 的视觉位置。删除:

- `pendingHistoryAnchorRef`。
- `captureHistoryViewportAnchor`。
- `restoreHistoryAnchor`。
- `previousFirstTurnKeyRef` 的手写 delta 修正。

历史 loading UI 不进入 canonical turn 数组。

### 5.4 动态内容与 resize

以下高度变化统一进入 item measurement:

- 流式 Markdown 增长。
- thought/tool details 展开与收起。
- 图片完成加载。
- 代码高亮或字体导致重新排版。
- pane 宽度、窗口高度和 composer 高度变化。

virtualizer 负责 item size 和 offset 调整。删除旧列表几何 ResizeObserver、IntersectionObserver 和基于可见 assistant 子 DOM 的 resize anchor。保留的 total-size ResizeObserver 只在 latest 意图有效时重发 `scrollToEnd`,不维护高度 cache、offset 或第二套 anchor。

virtualizer 的 viewport/item observer 已覆盖 pane resize。Transcript 不再订阅 `onAgentConsoleResizePhase`,旧 emitter/listener 已删除。

## 6. 搜索与 turn reveal

目标 turn 可能处于三种状态:

1. 已加载且已 mount。
2. 已加载但未 mount。
3. 尚未加载。

统一定位流程:

```text
reveal/search target turnID
  -> useTranscriptTurns.revealTurn 按页加载直到找到或确认不存在
  -> 建立 turnID/key -> index 映射
  -> virtualizer.scrollToIndex(index, align center)
  -> 等待目标 turn mount + measure
  -> 在该 turn 内定位 message/occurrence DOM
  -> 做一次局部像素修正和 highlight
```

第一段由 virtualizer 定位 turn,第二段只解决超长 turn 内的精确 message/文本 occurrence。局部修正不能重新计算整个列表位置。

搜索 highlight 只作用于当前 mounted turn。搜索结果总数继续来自后端搜索,不能通过扫描 mounted DOM 推导。切换搜索结果时必须先 mount 目标 turn,再创建 CSS Highlight Range。

删除依赖“所有历史 message root 都在 DOM”这一前提的全列表 highlight 扫描。

## 7. 选择、复制与可访问性

虚拟列表无法支持跨越未 mount turn 的原生拖拽选择。这是虚拟化的固有限制,不能用隐藏的完整 Transcript DOM 规避,否则会抵消虚拟化收益。

本次明确采用以下行为:

- 普通拖拽选择和复制支持当前 mounted 内容。
- `Cmd/Ctrl+A` 与普通复制只覆盖当前已挂载内容,不提供隐藏的全会话复制路径。
- 如果产品要求复制“全部历史”而不是“已加载历史”,应提供独立的后端/前端导出动作,不能在快捷键时静默拉取所有 pages。
- 虚拟 turn wrapper 补充稳定列表语义和 position/set size 信息,并在 Electron 的辅助功能检查中验证键盘导航。

这部分属于正式迁移范围,不能在上线时静默退化。

## 8. 代码组织

当前结构:

```text
transcript/
├─ TranscriptList.tsx                 虚拟项组合与渲染
├─ TranscriptTurn.tsx                 单个 turn,继续 memo
└─ useTranscriptTurns.ts              infinite pages,不再维护 visibleTurnCount
```

约束:

- 不新增通用 `VirtualList` 包装层;当前只有 Transcript 的行为需要这些配置。
- 不把 virtualizer instance 放入 Zustand。
- 不让 `TranscriptTurn` 读取全局 selected session。
- 不直接修改 shadcn 组件。
- `TranscriptList.tsx` 统一持有 pane-local virtualizer、滚动契约与 mounted DOM 定位 helper,避免跨文件产生第二套状态。
- 迁移完成后检查并删除旧 helper、旧 ref、重复 observer 和临时 PoC 代码。

## 9. 开发阶段与工作量

### Phase 0:隔离 PoC 与基线,原估算 1-2 人天

目标:证明所选版本在真实 Electron 环境可用。

验证:

- 20/100/500 turn 混合 fixture。
- 动态高度、图片延迟加载、tool disclosure。
- end anchor、streaming 增长、prepend 稳定。
- React 19 console warning。
- 单 pane、上下分屏和快速 resize。

输出:

- Performance trace 与 DOM mount 数基线。
- 已验证的准确依赖版本和初始配置。
- PoC 结论。PoC 代码不合并到正式路径。

### Phase 1:数据与虚拟渲染,原估算 1.5-2 人天

- 引入锁定版本。
- 删除 `visibleTurnCount`,flatten 已获取 pages。
- 以 turn 为 item 接入动态 measurement。
- 接好 padding、gap、footer 和 composer clearance。
- 保持 `TranscriptTurn` memo 边界。

### Phase 2:滚动契约替换,原估算 2-3 人天

- 初始化/latest/jump latest。
- streaming 与动态内容高度变化。
- prepend history 与加载触发。
- pane/window/composer resize。
- 删除旧 bottom、history anchor、IntersectionObserver 和 ResizeObserver 路径。

### Phase 3:定位、搜索与复制,原估算 2-3 人天

- search/turn reveal 两段定位。
- mounted range highlight。
- scoped select-all 与已挂载内容复制语义。
- 新消息计数与 disclosure 回归。

### Phase 4:验收与清理,原估算 1.5-2 人天

- Electron 自动化 smoke。
- 性能 trace 对比。
- 单/分屏手测矩阵。
- 删除临时 fixture、旧 helper、未使用状态和重复机制。
- `npm --prefix web run build` 与全仓测试。

合计:**8-12 人天**。

## 10. 测试与验收

### 10.1 数据 fixture

至少准备:

| Fixture | 用途 |
| --- | --- |
| 20 个普通 turn | 首屏与短会话无回归 |
| 100 个混合 turn | 常规长会话、分页和快速滚动 |
| 500 个混合 turn | DOM/memory 上界与远距离定位 |
| 单个超高 turn | 明确 turn 内部非虚拟化边界 |
| 持续 streaming turn | end anchor 与用户阅读历史 |
| 延迟图片 + 展开 tool | 异步高度变化 |

混合 turn 必须包含 user text、assistant Markdown、thought、tool use/result、代码、图片、file changes 和 queued/live overlay。

### 10.2 行为验收

- 初次进入 session 显示最新内容。
- latest 状态 streaming 始终贴底。
- history 状态 streaming 不移动当前阅读位置。
- history 状态发送新消息后进入最新。
- prepend 20 个旧 turn 后,原锚点 settle 后视觉偏移不超过 2px。
- disclosure、图片和代码高度变化不造成错误跳底。
- Jump Latest 正确到达尾部并恢复 follow。
- 搜索与 SessionRail reveal 能定位第 1、50、500 个 turn 内的 user/assistant occurrence。
- 主 pane streaming、split pane history 时互不影响。
- 快速 resize、拖动分隔线和切换 Workspace 不出现空白、抖动或错误跳转。
- session 切换后不复用另一个 pane/session 的 measurement 或 scroll state。
- Cmd/Ctrl+A 和复制只作用于当前已挂载的 turn。

### 10.3 性能验收

- 500-turn fixture 已全部加载时,单 pane mounted turn 数由可见 turn + 两侧 overscan 决定,不随已加载总数增长。
- 记录改造前后 DOM node 数、JS heap、Layout/Recalculate Style 时间和长任务。
- 快速滚动不出现持续空白区域。
- latest streaming 不因每个 token 重建全部虚拟项。
- 搜索远距离目标不需要先 mount 中间所有 turn。

在 Phase 0 记录基线后给 trace 指标设置最终阈值;没有真实基线前不写任意 FPS 或耗时数字。

## 11. 合并策略

不在主线长期保留 feature flag 或两套列表。

推荐:

1. 隔离 PoC 只用于验证和测量,结论写回本文,代码丢弃。
2. 正式实现使用单独开发分支,在一个完整 PR 中原子替换 Transcript 长列表路径。
3. PR 合并前必须同时删除旧滚动/observer/anchor 代码,不能以“后续清理”为由保留 fallback。
4. 如果工作量需要分提交,每个提交可以组织代码,但最终 PR 不允许存在运行时双轨。

## 12. 开发前确认清单

- [x] 确认 `@tanstack/react-virtual` 的准确锁定版本。
- [x] 完成 Electron 下 20/500 turn 回归和真实混合会话回归。
- [ ] 记录当前 DOM、heap 和 Performance trace 基线。
- [x] 确认复制边界:只复制当前已挂载 turn。
- [x] 确认 submit error/footer 的 total-size 结构。
- [x] 确认 search target 两段定位并完成 500-turn 回归。
- [ ] 建立单 pane 与 split pane 自动化 smoke。
- [x] 完成正式单路径替换。
