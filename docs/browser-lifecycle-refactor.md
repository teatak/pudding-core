# Browser Lifecycle Refactor Plan

## 目标

浏览器是 session resource,不是 canvas widget。Canvas 只负责展示 browser surface,不拥有 tab、target、screencast 生命周期。

重构目标:

- session 切换不串 tab。
- canvas/browser 切换不销毁 tab。
- 所有 session 共享同一个 browser profile,确保登录态全局可用。
- browser process 模式是全局的,tab/slot 绑定仍然 session scoped。
- 外部授权后的 cookie/localStorage 必须能回到 internal browser,并被所有 session 复用。
- close tab 幂等、彻底、不会被旧 payload 恢复。
- WebSocket 断开、首帧超时、target 丢失都有明确恢复路径。
- 重启后只恢复可验证的真实 target;不存在真实 target 时不显示假画面。

## 边界

- daemon 拥有 browser manager:全局管理 profile/process,按 session 管理 slot。
- daemon 拥有 browser profile:cookie、localStorage、登录态事实源。
- daemon 拥有 browser process:同一时间只运行 headless 或 external 之一。
- session 拥有 browser slot:tab 绑定、URL 元数据、恢复状态。
- Chrome target 是运行态,可丢失、可重建,不能作为持久事实源。
- frontend 拥有 UI surface:当前显示 canvas 还是 browser,以及 stream 连接状态。
- canvas item 只能展示 browser,不能作为 browser state 事实源。

## External 授权模型

外部打开的核心意义是完成 internal/headless 难以处理的授权,并把授权态带回 internal 继续使用。登录态必须跨 session 复用,因此 profile 必须全局共享。

设计:

- profile 路径全局固定:`<home>/browser-profiles/default`。
- 默认使用一个全局 headless Chrome process,长期保持可恢复。
- 点击外部打开时:
  - 停止或挂起全局 headless process。
  - 使用同一个全局 profile 启动 headed/external Chrome process。
  - 将当前 session slot 绑定到 external process 的 target。
- 多个 session 不能拥有独立 external process,因为 Chrome profile lock 不允许同一 profile 被多个进程并发使用。
- 点击回到内部时:
  - 关闭全局 external process。
  - 使用同一个全局 profile 启动 headless process。
  - 恢复当前 session slot 的 target。
- 全局 headless 和 global external 不能同时运行,避免 Chrome profile lock 与状态竞争。

验收:

- session A 外部授权后,session B 打开同一站点应复用登录态。
- session A/B 的 tab 绑定不能互串,即 A 不显示 B 的 tab。
- session A 切 external 后,processMode 对所有 session 都是 external,但 tab 列表仍按 session 过滤。
- session A 回 internal 后,全局 processMode 回到 headless。
- UI 判断 external 必须看全局 processMode 和当前 session tab,不能跨 session 渲染 tab。

## 状态模型

后端 `BrowserSlot`:

```text
empty
  -> metadata_only
  -> live_internal
  -> live_external
  -> recovering
  -> lost
  -> closed
```

前端 stream:

```text
idle
  -> resolving_slot
  -> connecting_ws
  -> waiting_first_frame
  -> live
  -> external
  -> recovering
  -> failed
```

## P0 基线锁定

目标:不改用户可见行为,先把问题锁住。

任务:

1. 补后端测试:
   - session A/B browser slot 隔离。
   - metadata-only 不冒充 live tab。
   - external/internal 影响全局 processMode,但不影响 session tab 归属。
   - 外部授权回内部后保留全局 profile 登录态。
   - close tab 清理 state,重复 close 成功。
2. 补前端纯逻辑测试或可提取函数:
   - 当前 session 只选择当前 session 的 tab。
   - 旧 browser payload 不应触发关闭后的自动恢复。
3. 写手动验收清单:
   - session 切换。
   - canvas/browser 切换。
   - external 打开/回内部。
   - 重启恢复。
   - close tab。
   - 首帧超时。

验收:

- 测试能稳定复现当前关键风险。
- 不引入新 API。
- 不大改 `CanvasPane`。

## P1 后端 Close 语义

目标:关闭 tab 成为后端原子语义,前端不再拼 `release + clear state + delete canvas item`。

新增或收敛 API:

```text
POST /sessions/{id}/browser/close
```

语义:

- 关闭当前 session 的 browser slot。
- 若 live target 存在,关闭 target。
- 若 target 已不存在,仍返回成功。
- 清空 persisted browser state。
- slot 进入 `closed` 或 `empty`。
- 不清理全局 process/profile。
- 不影响其他 session 的 tab。
- 重复调用幂等成功。

验收:

- close 后 `GET /sessions/{id}/browser/state` 返回无 live state。
- close 后 `GET /sessions/{id}/browser/tabs` 不返回该 tab。
- close 后前端不会因旧 URL/payload 自动 reopen。

## P2 后端 Slot 状态机

目标:把 session browser slot 从 manager 隐式 map 中提出来。

当前过渡实现:

- 新增 `POST /sessions/{id}/browser/tabs/{tabID}/recover`。
- 前端 screencast 在 WS 断开、连接超时、首帧超时时进入 `recovering`。
- recovery 顺序固定为:停止当前 WS -> 后端校验/恢复真实 tab target -> 成功后重连 WS。
- 若 tab 不属于当前 session,或重启后没有可验证的真实 target,返回 not found,UI 不显示假画面。
- screencast 启停在单个 WS 连接内幂等:服务端不再连接时抢先 `start(0,0)`,相同尺寸重复 `start` 不触发 CDP,尺寸变化才 `stop+start`,连接结束重复 `stop` 安全。
- 该 API 是 P2 slot 状态机前的过渡层,后续会收敛进 slot snapshot/recover action。

任务:

1. 新增 slot snapshot:

```go
type SlotStatus string

const (
  SlotEmpty        SlotStatus = "empty"
  SlotMetadataOnly SlotStatus = "metadata_only"
  SlotLiveInternal SlotStatus = "live_internal"
  SlotLiveExternal SlotStatus = "live_external"
  SlotRecovering   SlotStatus = "recovering"
  SlotLost         SlotStatus = "lost"
  SlotClosed       SlotStatus = "closed"
)
```

2. API 统一返回 slot snapshot:

```json
{
  "sessionID": "...",
  "status": "live_internal",
  "tabID": "...",
  "url": "...",
  "title": "...",
  "processKind": "headless",
  "profileID": "...",
  "recoverable": false,
  "liveTarget": true,
  "version": 12
}
```

3. `version` 用来防止旧 WebSocket 或旧请求回写新状态。

验收:

- target 丢失后 slot 进入 `lost/recovering`,不会显示假 live。
- 重启后只绑定能验证的真实 target。
- provider/browser tool 操作必须显式 session scoped。

## P3 后端拆分

目标:拆小 `internal/browser/manager.go`,保持外部 API 不变。

目标结构:

```text
internal/browser/
  manager.go      // facade
  process.go      // Chrome process lifecycle
  slot.go         // session slot lifecycle
  target.go       // CDP target resolve/recover
  screencast.go   // stream bridge
  recovery.go     // restart/target lost recovery
```

验收:

- 拆分后 `go test ./internal/browser ./internal/api ./internal/tool` 通过。
- 每个文件职责单一。
- `manager.go` 不再承载所有生命周期细节。

## P4 前端 Browser 模块

目标:浏览器 UI 从 `CanvasPane.tsx` 移出。

目标结构:

```text
web/src/browser/
  api.ts
  types.ts
  slotMachine.ts
  useBrowserSlot.ts
  BrowserSurface.tsx
  BrowserToolbar.tsx
  BrowserStream.tsx
```

任务:

1. `CanvasPane` 只保存 surface 选择:

```ts
type CanvasSurface = "canvas" | "browser";
```

2. `BrowserSurface` 通过 `sessionID` 读取 slot。
3. `BrowserStream` 只根据 slot snapshot 连接 WebSocket。
4. browser toolbar 调 `POST /sessions/{id}/browser/close`,不直接操作 canvas item。

验收:

- 小组件 tab 切换不销毁 browser surface。
- session 切换只显示当前 session browser slot。
- close 后 UI 不残留旧截图、不自动重开。

## P5 删除旧耦合

目标:移除旧 browser-as-canvas-item 事实源。

任务:

- 删除 canvas item 中 browser payload 的事实源语义。
- 删除旧 repair/open-from-payload 逻辑。
- 删除旧 auto switch 对 browser tab 的隐式创建。
- 保留必要的展示入口,但展示入口只引用 session browser slot。

验收:

- localStorage 只保存 UI surface 偏好。
- canvas 数据不决定 browser tab 生命周期。
- `CanvasPane.tsx` 行数明显下降,不再含 screencast 细节。

## Close Tab 专项验收

关闭 tab 是核心生命周期场景,必须单独验收:

- 关闭当前 session 只影响当前 session。
- live target 存在时必须关闭 target。
- target 已丢失时关闭仍成功。
- persisted browser state 必须清空。
- screencast WS 必须停止,不能触发自动恢复。
- 旧 canvas payload/旧 URL 不能重新打开 tab。
- external 模式下关闭 tab 不影响其他 session 的 process。
- external 模式下关闭 tab 只关闭当前 session 的 process/target。
- 重复点击关闭不报错、不恢复旧 tab。
- session 切换后关闭只能关闭当前 session 的 tab。

## 禁止

- 禁止从全局 current/focus 推断 session。
- 禁止 session 切换触发任何 browser process 模式切换。
- 禁止使用全局 processMode 判断当前 session 是否 external。
- 禁止把 canvas item 当作 browser state 事实源。
- 禁止将不存在的 target 渲染成真实页面。
- 禁止前端组合多个接口来表达关闭 tab 的业务语义。
