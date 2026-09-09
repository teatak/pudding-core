# 用户问题收集

当前入口为 `builtin_request_user_input`。LLM 可见说明和 JSON schema 位于
`web/src/mcp/inputFlowTools.ts`；模型等待及答复路由由 `internal/engine/user_input.go` 管理。

## 两个独立计时器

| 项目 | 规则 |
| --- | --- |
| 模型等待 | `waitSeconds`，默认 60，整数 0–300；0 立即返回 `awaiting_user`，正数暂停当前 turn 的模型循环 |
| 面板收起 | 固定 60 秒无交互后收起；不由 `waitSeconds` 控制，也不结束仍有效的模型等待 |
| 用户交互 | 点击、选择、输入、按键重置面板计时，同时将**仍有效**的模型等待延长至当前时间 + waitSeconds |
| 倒计时 | 不在初次出现时显示。面板最后 10 秒显示；模型等待仅在剩余 `min(10, waitSeconds/2)` 秒显示 |
| 超时之后 | 继续操作或重新打开面板不会恢复已经结束的模型等待 |

例如 `waitSeconds=10`：无交互时模型在 10 秒后继续，面板仍保留至自身 60 秒空闲期限。
若 `waitSeconds=300`，面板可能先收起，模型仍等待；可在运行中的原工具行重新打开问题。
显式关闭面板会结束当前等待并返回 `dismissed`；取消 turn 会中断等待。

## 结果与补答

- 等待期间完整提交：返回 `status=answered`，`answer.text` / `answer.parts` 直接进入工具结果；不额外生成 user message。
- 异步返回或等待结束后提交：后端以原问题身份投递 user message。只有原 turn 仍接受引导时才 steer；原 turn 已结束则 submit，若别的 turn 在运行则进入其后续队列，不引导无关 turn。
- 补答幂等键固定为 `input-flow-{requestID}`；重复提交不得生成第二份答复。已经回答的工具行显示“已完成”。
- `timeout` / `dismissed` / `cancelled` 都是**没有收到回答**，不表示同意或拒绝。工具说明要求 LLM 不猜测依赖答案的决定，也不因超时重复提问。
- 请求失败只恢复表单供用户重试，不自动改道或重发。

## 状态与恢复

`requestID = turnID + ":" + callID`，REST 全部显式携带 sessionID。
等待中的上下文、截止时间和答复暂存于 engine，turn 收尾后释放。
跨 turn / 重启恢复只读取 canonical tool_use、tool_result，以及具有同一答复幂等键的 user message / queued input；不增加问题历史表。

前端 TanStack Query 缓存上述权威快照；Zustand 只保存当前可见面板和未提交的 UI 草稿。
收起后可从原工具行“回答问题”恢复草稿；重启应用后可以从 canonical 参数恢复**问题**，但未提交草稿不持久化。
工具调用尚未完成时，也可通过 turnID/callID 找回当前等待。

## 验证入口

- Go：`go test -tags 'sqlite_fts5 webrtcaec' ./internal/engine ./internal/api -run 'Test(UserInput|LateUserInput)'`。
- Web：`npm --prefix web test`、`npm --prefix web run build`。
- 隔离桌面：当前源码 Electron 运行 `electron/smoke/input-flow-smoke.cjs` 和 `electron/smoke/input-flow-delivery-smoke.cjs`；后者覆盖两个计时器、续期、补答、长等待下重开、草稿恢复、失败重试与无重复投递。HTTP 为模拟数据，不接真实 provider。
