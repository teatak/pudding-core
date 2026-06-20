# Transcript 滚动与历史加载实施计划

> 目标:传统消息流,不做 sticky 用户气泡。先修好底部跟随,再做上滑历史加载。  
> 关键约束:支持上下分屏,滚动状态必须是 pane-local,数据与 SSE 必须是 session-scoped。

## 0. 当前进度

- P0:bottom mode + 分屏 SSE 边界已实现,还需要继续手测分屏 resize / streaming 场景。
- P2:上滑历史分页 + prepend anchor 已实现,还需要继续手测长会话 / 分屏场景。
- 下一步回到 P1:thought/tool parts 基础协议与渲染。

## 1. 当前状态

- `Conversation` 已经是 `Transcript + Composer` 的 flex 结构,composer 在会话 pane 底部,不是全局 fixed overlay。
- `Transcript` 已有基础 bottom mode:
  - `followingBottom`
  - 48px 底部阈值
  - streaming 文本增长时 `pinToBottom`
  - jump latest 按钮
  - `ResizeObserver` 在贴底态尝试滚到底部
- overlay 已有:
  - pending user message
  - assistant streaming overlay
  - `clientMessageID` 对账 canonical user message
  - terminal event 后 refetch messages
- 分屏已存在:
  - `App` 中主 pane / split pane 是两个独立 `ChatPane` 实例
  - 当前代码避免同一个 session 同时在主 pane 和 split pane 重复渲染
  - `SessionRail` 的 background SSE 会排除 active session

## 2. 当前问题

### 2.1 bottom mode 不是显式状态机

现在的 `followingBottom` 主要由 scroll geometry 反推:

```ts
distance = scrollHeight - scrollTop - clientHeight
followingBottom = distance < 48
```

这会导致几个问题:

- 用户在历史位置发送新消息时,如果 `followingBottom=false`,pending message 追加后不会主动跳到底部。
- `scrollToBottom` 走 `requestAnimationFrame`,可能先 paint 一帧再跳。
- resize / 分屏拖拽时只在 `ResizeObserver` 回调写一次 `scrollTop`,WebKit 下重排可能晚于回调。
- `node.scrollTop = node.scrollHeight` 依赖浏览器 clamp,语义不如显式设为 `scrollHeight - clientHeight`。
- 没有统一的命令表达"进入底部跟随模式";session 切换、发送、stream、jump latest 都散落处理。

### 2.2 没有历史锚点

用户不在底部时,resize / 未来 prepend older messages 都需要保持当前阅读位置。
现在没有稳定的 DOM anchor:

- message item 没有统一的 `data-message-id` / `data-transcript-item-id`。
- resize 时 history mode 没有恢复当前可见 message。
- prepend older messages 后会天然改变 `scrollHeight`,如果不恢复 anchor,视口会跳。

### 2.3 消息接口没有分页

当前:

```txt
GET /sessions/{id}/messages
```

后端一次返回全量 messages。store 只有:

```go
ListMessages(ctx, sessionID, limit)
```

缺少 `before` 游标,前端也还不能上滑加载更多历史。

### 2.4 分屏下的状态边界必须更硬

必须区分:

- session-scoped:messages query cache、overlay、running turn、SSE event source。
- pane-scoped:bottom/history 滚动模式、当前阅读 anchor、jump latest 是否显示。

滚动状态不能放进 Zustand,也不能按 `sessionID` 存。未来即使同一个 session 出现在两个 pane,两个 pane 也应该能一个在底部跟随,另一个停在历史位置。

SSE 也要保持每个 session 只有一个 active source。`turn.delta` 没有 seq,重复连接会导致 delta 重复 append。

## 3. 目标设计

### 3.0 旧项目参考取舍

旧项目可以作为踩坑记录,但不直接搬实现:

- 可参考:
  - offscreen session 用 retain count 管 SSE 生命周期。
  - visible/offscreen session 集合分开计算。
  - 每个 pane 使用独立 scroll store,滚动状态不按 session 共享。
  - 历史 prepend 期间要区分 older messages 与同时到达的 live append。
- 不照搬:
  - 不恢复旧 Runtime/focus 大结构。
  - 不让 active transcript 由每个 pane 各自开 SSE。
  - 不把 scroll 状态做成全局 singleton。
  - 不用事件流反推 canonical transcript。

新项目的取舍:

- **实时事件按 session 统一 ownership**:visible/background 都按 sessionID 去重,不是 pane 谁渲染谁开连接。
- **滚动状态按 pane 本地 ownership**:同一个 session 即使将来能出现在多个 pane,每个 pane 也有自己的 bottom/history/anchor。
- **canonical messages 仍是 transcript 事实源**:SSE 只承载 live overlay 与生命周期同步。
- **native scroll first**:Transcript 继续使用浏览器/WKWebView 原生 `overflow-y: auto`,不引入自定义滚动容器或反向列表。滚动控制只做三件事:判断 bottom/history、贴底校正、anchor 恢复。
- **实现保持小切片**:先修 bottom bug 与 resize anchor,再接 thought/tool,最后做历史分页。

原生滚动能规避旧项目里一部分 desktop 问题:trackpad momentum、系统滚动条、文本选择、rubber band、WKWebView focus/hover 残留都交给平台处理。但它不自动解决动态内容高度变化,所以 streaming append、composer 变高、分屏 resize、历史 prepend 仍然需要明确的 bottom/history 状态机与 DOM anchor。

### 3.1 保持传统消息流

渲染顺序就是 canonical 顺序 + live overlay:

```txt
user
assistant
user
assistant
pending user
assistant overlay
```

不做 sticky 用户气泡。turnID 只做数据关联,不影响布局。

### 3.2 pane-local scroll manager

新增一个本地 hook,例如:

```ts
type ScrollMode = "bottom" | "history";

function useTranscriptScroll(args: {
  sessionID: string;
  itemKeys: string[];
}) {
  return {
    viewportRef,
    contentRef,
    mode,
    showJumpLatest,
    enterBottomMode,
    onViewportScroll,
    stickToBottomIfNeeded,
    captureAnchor,
    restoreAnchor,
  };
}
```

核心规则:

- 初始进入 session:进入 `bottom`。
- 用户发送消息:进入 `bottom`。
- 用户点 jump latest:进入 `bottom`。
- 用户手动向上滚离底部阈值:进入 `history`。
- streaming / terminal refetch / composer 变高 / 分屏拖拽 / canvas 开关:
  - `bottom` 模式:保持底部。
  - `history` 模式:保持当前可见 message anchor。

底部滚动目标统一为:

```ts
scrollTop = Math.max(0, scrollHeight - clientHeight)
```

### 3.3 稳定贴底

bottom mode 下,需要把"贴底"当成一个短暂稳定过程:

- 在 `useLayoutEffect` 里先同步贴一次,避免一帧闪动。
- `ResizeObserver` 同时观察 viewport 和 content。
- resize / split drag / streaming 大 chunk 时,允许在后续 1-2 个 rAF 里再次校准。
- 只在 bottom mode 做连续校准,history mode 不抢滚动。

不要使用 smooth scroll。聊天底部跟随应该是 layout correction,不是动画。

### 3.4 稳定历史锚点

每个渲染项补稳定 DOM id:

```tsx
<div data-transcript-item-id={item.id}>
```

anchor 形状:

```ts
type ScrollAnchor = {
  itemID: string;
  offsetTop: number; // item top - viewport top
};
```

捕获策略:

- 取 viewport 顶部下方第一个可见 item。
- 记录它相对 viewport 的 offset。

恢复策略:

- DOM 更新后找到同一 item。
- 设定 `scrollTop += newOffset - oldOffset`。

适用场景:

- history mode resize。
- prepend older messages。
- terminal refetch 导致 overlay -> canonical 替换。

### 3.5 SSE 订阅所有权

建议把 active session SSE 从 `ChatPane` 上移到 `App` 或一个 session event owner:

```ts
useVisibleSessionEvents(activeSessionIDs, token)
```

原则:

- 每个 sessionID 同时最多一个 SSE source。
- visible session 用 `syncMessages=true`。
- background running session 用 `syncMessages=false`。
- 如果同一 session 同时出现在 visible 与 background 集合,visible 胜出。

这样分屏下不会因为组件实例数量影响 event source 数量。

## 4. 实施顺序

### 优先级结论

当前应该先做滚动底座,再做历史分页,最后接 thought/tool parts:

```txt
P0: bottom mode + 分屏 SSE 边界
P2: 上滑历史分页 + prepend anchor
P1: thought/tool parts 基础协议与渲染
```

原因:

- P2 直接复用 P0 已完成的 DOM anchor / bottom-history 状态机,不依赖 thought/tool 协议细节。
- 历史分页会改 messages API、Query 缓存形态和 transcript 数据合并方式;这些是后续 thought/tool 渲染也要站在上面的基础。
- thought/tool 会带来更多动态高度来源:thinking 展开/折叠、tool 卡片状态更新、tool 输出变长、assistant text 继续 streaming。等 P2 稳定后再接,可以直接验证这些动态内容不会破坏 bottom/history。
- 会话列表里的 thinking/tool 状态属于 session-scoped 展示,可以后续和 transcript part 渲染一起设计;事件事实源仍应来自同一套 turn/part 协议。

因此推荐拆成三段:

1. **P0 滚动底座**:Step 1-3。修当前 bug,为动态 part 高度打地基。
2. **P2 历史分页**:Step 4-5。上滑加载更多,prepend 后恢复 anchor。
3. **P1 thought/tool**:先做最小 part 协议和 transcript 渲染;rail 只显示粗粒度 running/thinking/tool 状态。

### Step 1: 固化分屏与 SSE 边界

目的:先保证 live 数据不会重复进入 overlay。

工作:

- 将 active session SSE ownership 从 `ChatPane` 移到 `App` 层或统一 hook。
- 保持 `SessionRail` background SSE 排除 active sessions。
- 给 hook 内部加 sessionID 去重。
- 保留 `syncMessages=true/false` 的差异。

验收:

- 主 pane 和 split pane 同时 streaming 不互相影响。
- background running session 仍然更新 rail running 状态。
- 没有重复 delta。

### Step 2: 抽出 pane-local `useTranscriptScroll`

目的:先修当前 bottom bug,暂不做历史分页。

工作:

- 把 `followingBottom` 改为显式 `mode`。
- `Transcript` 内滚动状态只存在组件实例内。
- 发送新消息、jump latest、session 切换显式调用 `enterBottomMode`。
- streaming 文本增长、items 变化、terminal refetch 通过 `stickToBottomIfNeeded` 处理。
- `scrollTop` 统一设为 `scrollHeight - clientHeight`。

验收:

- 在底部 streaming 时始终跟随。
- 在历史位置 streaming 时不抢滚动。
- 在历史位置发送新消息后跳到底部。
- jump latest 后恢复底部跟随。

### Step 3: 补 DOM item anchor

目的:为 resize 与未来 prepend 做准备。

工作:

- 给 canonical message、pending user、assistant overlay 都生成稳定 item key。
- 渲染节点加 `data-transcript-item-id`。
- history mode 下 resize 前后保持当前可见 item。
- overlay -> canonical 替换时尽量用 stable key 对齐:
  - user:优先 `clientMessageID`
  - assistant overlay:可用 `turnID`
  - canonical assistant:继续保留 `turnID` 作为 item key

验收:

- 用户停在中间阅读时拖动分屏高度,当前阅读内容不跳。
- canvas 开关导致宽度变化时,history mode 不跳到底。

### Step 4: 后端 messages 分页契约

目的:支持上滑加载历史。

建议接口:

```txt
GET /sessions/{id}/messages?limit=50
GET /sessions/{id}/messages?before=<messageID>&limit=50
```

响应:

```json
{
  "messages": [],
  "hasMore": true
}
```

store 增加:

```go
ListMessagesPage(ctx, sessionID string, beforeMessageID string, limit int)
```

排序规则继续保持:

```txt
created_at ASC, rowid ASC
```

`before` 查询时先查 anchor message 的 `(created_at,rowid)`,再取更早的 N 条,最后升序返回。

验收:

- 首屏只取最近 N 条。
- `before=oldestMessageID` 能取到更早一页。
- 同毫秒 user/assistant 顺序稳定。

### Step 5: 前端接 `useInfiniteQuery`

目的:真正上滑加载历史。

工作:

- `Transcript` messages query 改为 infinite query。
- flatten pages 后再合并 pending / assistant overlay。
- top sentinel 或 `scrollTop < threshold` 触发 `fetchPreviousPage`。
- fetch 前捕获 anchor,prepend 渲染后恢复 anchor。
- terminal refetch 只刷新当前可见范围;必要时先简单 invalidate 全部 pages,但必须保持 anchor。

验收:

- 上滑接近顶部自动加载 older messages。
- prepend 后当前阅读位置不跳。
- bottom mode 下新消息继续贴底。
- split 两个 pane 各自加载历史,互不影响。

### Step 6: 回归与手测矩阵

必须覆盖:

- 单 pane:空会话、短会话、长会话。
- 单 pane:streaming 中向上滚,不抢滚动。
- 单 pane:history mode 下发送新消息,跳到底部。
- 单 pane:composer 多行变高,底部模式跟随,历史模式保持 anchor。
- 分屏:上 pane bottom streaming,下 pane history 阅读。
- 分屏:拖动上下分隔条,两个 pane 各自保持自己的模式。
- canvas 开关:宽度变化后 bottom/history 都正确。
- terminal refetch:overlay 消失、canonical message 出现时不跳。
- background running session:rail 状态更新,active pane 不重复 delta。

## 5. 建议拆 PR

1. SSE ownership 与去重。
2. `useTranscriptScroll` + bottom mode 修复。
3. DOM anchor + resize history preservation。
4. 后端 messages pagination。
5. 前端 infinite query + prepend anchor。

如果只想先修当前明显 bug,最小切片是 PR 1-2-3。历史加载可以后置。
