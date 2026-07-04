# Voice Migration Plan

> 状态:已开始。范围已收敛为单用户语音对话,旧项目只作为参考实现。

## 目标

把旧项目里的语音对话能力迁到新项目,但只保留单用户语音闭环:

```text
mic capture -> VAD -> ASR text -> engine.Submit -> assistant delta -> TTS -> playback
```

## 不迁移

- 声纹识别 / speaker recognition。
- Gemini Live / realtime audio transport。
- meeting / diarize / 多人分段。
- speaker tools / identity context。
- raw audio 先不做。
- KWS 先暂缓。

## 架构原则

- 硬件资源归 daemon:mic、speaker、driver、ASR、TTS、playback queue。
- 业务 turn 归 engine:ASR 结果必须走 `engine.Submit`。
- 后端不新增 focus / active session 概念。
- 所有音频 API 必须显式带 `sessionID`。
- canonical messages 仍然只由 engine/store 写入。
- 不搬旧 `Runtime` 大结构。
- 不把 WebSocket 当普通 submit/SSE 的替代品。

## API 契约

新增:

| Method | Path | 用途 |
| --- | --- | --- |
| `GET` | `/sessions/{id}/audio/bindings` | 查看 daemon 音频 owner 快照 |
| `POST` | `/sessions/{id}/audio/input` | 绑定/释放听写输入 |
| `POST` | `/sessions/{id}/audio/output` | 绑定/释放语音输出 |

`/sessions/{id}/audio/bindings` 是 daemon resource snapshot,不表示 UI focus;路径仍显式带 `sessionID`,避免新增无 session scope 的业务主路径。

建议响应:

```json
{
  "bindings": {
    "inputOwner": "sess_x",
    "outputOwner": "sess_x"
  }
}
```

`POST /sessions/{id}/audio/input`:

```json
{ "enabled": true }
```

语义:

- `enabled=true`:抢占 input owner。
- `enabled=false`:仅当当前 owner 是该 session 时释放。
- 删除 session 时自动释放其 input/output owner。
- 切前端 selected session 不改变 owner。

## 后端模块

建议新增:

```text
internal/audio/
  frame/
  driver/
  asr/
  tts/
  queue/
  voice/
```

其中 `voice` 是新项目的 daemon audio service,只做路由和编排:

- 管理 session audio binding。
- 管理 capture/playback lifecycle。
- ASR sentence 生成 `clientMessageID`,调用 `engine.Submit`。
- 订阅 session event,把 assistant text delta 聚句后送 TTS。
- cancel/stop 时清 playback,并调用 `engine.Cancel(sessionID)`。

不要把 `voice` 写成旧项目 `Runtime`。

## 当前已落地

- `internal/audio/frame`:PCM16 frame 与 format 基础契约。
- `internal/audio/driver`:daemon-owned driver interface 与 noop driver。
- `internal/audio/driver/portaudio`:正式 mic capture driver,使用默认输入设备输出 16k/mono/PCM16。
- `internal/audio/queue`:串行 playback queue,支持按 session / turn 清理。
- `internal/audio/asr`:ASR client/event interface 与 fake client。
- `internal/audio/asr/sherpa`:真实 sherpa-onnx SenseVoice + Silero VAD client。
- `internal/audio/tts`:TTS client/event interface、noop/fake client、macOS `say` client;已迁入 TTS 文本清洗,会跳过纯标点/emoji 片段。
- `internal/audio/voice`:session input/output binding manager、sentence splitter 与 AudioService 骨架。
- AudioService:
  - input binding 会启动真实 capture + ASR backend;释放 input owner 时停止 capture。
  - ASR sentence 只在 session 拥有 input binding 时调用 `engine.Submit`。
  - output owner 订阅 session event,把 `turn.delta` text 聚句后送入 playback queue。
  - TTS 播放结束后的短窗口会抑制 ASR sentence,减少 speaker 回灌尾巴变成用户输入。
  - `/sessions/{id}/cancel` 会同步调用 `voice.CancelSession`,清理该 session 的 TTS 队列、丢弃未 flush 的 turn buffer,并取消当前属于该 session 的 TTS 播放。
  - ASR sentence 在本 session 正在播 TTS 时会触发 barge-in:
    - turn 仍在运行:先停 TTS / 清队列,再 best-effort `engine.Cancel`,然后提交 ASR 文本。
    - turn 已完成但 TTS 仍在播:停 TTS / 清队列,忽略 `ErrNoRunningTurn`,继续提交 ASR 文本。
  - `turn.completed` 会 flush 未成句尾巴。
  - `turn.failed` / `turn.cancelled` 会取消对应 TTS turn。
- API:
  - `GET /sessions/{id}/audio/bindings`
  - `POST /sessions/{id}/audio/input`
  - `POST /sessions/{id}/audio/output`
- daemon 启动时创建 `voice.Service`;删除 session 时释放其 input/output owner。
- macOS daemon 默认使用真实 `macsay` TTS,默认 rate=230;非 macOS 安全降级为 noop。
- `macsay` 会保留 stderr 到错误日志,便于排查系统 voice / audio device 问题。
- daemon 默认使用 PortAudio 作为 capture driver;input binding 时才初始化并打开麦克风。
- macOS desktop dev/release bundle 已写入 `NSMicrophoneUsageDescription`;PortAudio 初始化前会主动请求 mic 权限。
- `make desktop-dev` 的 macOS dev app 需要稳定本机 code signing identity,默认名称为 `Pudding Dev Local`;没有时先跑一次 `make dev-cert`,避免 TCC 把每次重建的 `.app` 当成新身份反复申请权限。
- daemon 只从 `<home>/runtime/models` 加载 sherpa 模型:
  - `asr/model.int8.onnx`
  - `asr/tokens.txt`
  - `vad/silero_vad.onnx`
- 找不到模型时不会启用 fake ASR。
- 已补诊断日志:
  - mic 权限检查/授权/拒绝/超时。
  - PortAudio 初始化、capture start/stop、读取失败。
  - Sherpa ASR 启动、VAD segment、ASR sentence decode。
  - voice input/output binding、ASR sentence submit、submit 失败原因。
- web contract/client/query key 已同步。
- web Composer 已接入真实 input/output binding 图标开关;每次调用都显式传当前 `sessionID`。

## 当前验证

- `GOCACHE=/tmp/pudding-go-cache go test ./...`
- `npx tsc -b`
- `GOCACHE=/tmp/pudding-go-cache go test ./internal/audio/tts ./internal/audio/tts/macsay ./internal/audio/voice ./internal/daemon`
- `GOCACHE=/tmp/pudding-go-cache go test ./internal/audio/driver/portaudio ./internal/audio/voice ./internal/daemon`
- 真实 smoke:
  - 临时 home 启动 daemon。
  - `POST /sessions/{id}/audio/input {"enabled":true}` 能成功绑定 input owner。
  - PortAudio 能打开默认麦克风,Sherpa 能启动并产生 ASR 事件。
  - 临时 home 没有真实 provider profile,所以 ASR submit 到 engine 后停在 provider config unavailable;这不算完整演示。
  - PortAudio `Input overflowed` 已改为丢帧继续读,避免 capture loop 退出。
- 真实 E2E:
  - 临时 home 复用真实 provider profile,不写 release 数据目录。
  - 真实用户语音已跑出 ASR -> `engine.Submit` -> provider -> canonical messages。
  - output owner 打开后,正式 `/sessions/{id}/submit` 触发 provider response -> session event -> `macsay` 播放,无 `macsay` 错误。
  - input owner 单独打开复测成功,PortAudio/Sherpa 能在当前机器启动。
  - input/output 复测后已释放 owner。

## 演示口径

- 不做 fake 演示。
- 可演示的最低门槛是真实 `macsay` 输出接通。
- 完整语音演示必须满足:本机 mic 权限 + PortAudio capture + sherpa 模型 + session audio input/output binding 都真实可用。
- 当前状态:不是 fake,已具备本机受控演示能力;完整麦克风对话演示时需要现场开真 mic 说一句确认当前输入环境。

## 剩余风险

- macOS TCC 权限被拒后需要用户在系统设置里重新打开,或开发机执行 `tccutil reset Microphone com.teatak.pudding.dev` 后重启 `make desktop-dev`。
- 如果 `make desktop-dev` 提示缺少 `Pudding Dev Local`,先跑一次 `make dev-cert`;不需要 Apple 开发者证书。
- PortAudio 默认输入设备受系统当前设备影响;后续需要做设备枚举/选择。
- 当前 TTS 仍是 `macsay`;旧系统更自然的 TTS 建议在 cancel/barge-in 稳定后迁移。
- 未接 AEC/NS,强扬声器外放环境仍可能触发回采;当前先按用户 barge-in 处理,后续需要接 AEC/NS 降低喇叭回灌误触发。

## 迁移阶段

### 0. 契约设计

预计 0.5 天。

- 固化 API / binding / 前端 contract。
- 明确 input/output owner 与 session 删除语义。

### 1. 音频基础层

预计 1-1.5 天。

先迁最小接口和 fake 实现:

- `audio/frame`
- `audio/driver` + noop/fake
- `audio/asr` interface + fake
- `audio/tts` interface + fake
- `audio/queue`
- `wavenc` 可选

先不接真实 driver / sherpa。

### 2. AudioService

预计 1.5-2 天。

实现 daemon audio service:

- binding manager。
- ASR -> `engine.Submit`。
- session event -> TTS。
- playback queue。
- stop/cancel 编排。

### 3. 听写输入闭环

预计 1.5-2 天。

链路:

```text
mic frame -> VAD -> ASR sentence -> engine.Submit(sessionID, clientMessageID, text)
```

验收:

- A session 开听写,B session 不受影响。
- 切 selected session 不改变 input owner。
- running 时 ASR 新句子按现有 queued input 规则排队。
- audio service 生成稳定 `clientMessageID`。

### 4. TTS 输出闭环

预计 1.5-2 天。

链路:

```text
turn.delta(text) -> sentence splitter -> tts.Speak -> playback queue
```

迁移:

- `tts_sanitize`。
- sentence splitter。
- playback queue。

验收:

- 只有 output owner 的 session 会播放。
- 关闭 output 后不继续合成旧文本。
- cancel / failed / completed 时收尾正确。

### 5. 真实后端接入

预计 2-3 天。

接入:

- PortAudio。
- sherpa ASR。
- macOS say 或旧 TTS 实现。
- VAD 最小版。

不接 malgo。AEC/NS 先不接,后续单独做。

### 6. 前端控件

预计 1-1.5 天。

- 听写开关。
- 播音开关。
- session rail 只读 input/output owner 图标。
- i18n 文案。

规则:

- API 调用显式传 `sessionID`。
- 禁止从全局 current session store 隐式取 target。

### 7. 打断与测试

预计 2-3 天。

- 用户 stop:`/sessions/{id}/cancel` + 清 playback。
- barge-in:用户说话时清 TTS/playback,再 cancel 当前 turn。
- 单测覆盖:
  - binding 抢占。
  - 删除 session 释放 binding。
  - ASR submit 不串 session。
  - TTS 只播放 output owner session。
  - cancel 清音频状态。

### 8. 更好的 TTS

建议在 barge-in/cancel 稳定后迁移,不要阻塞当前 PortAudio 语音闭环。

优先级:

1. Edge TTS:旧系统默认方案,中文自然度最好,默认 `zh-CN-YunxiaNeural`,speed `1.2`。
2. sherpa 本地 TTS:可选离线方案,但模型体积和质量不适合作为默认 release 资源。

迁移要求:

- 适配新项目 `tts.Client` interface。
- TTS profile 来自 config,不是 SQLite。
- playback/cancel/barge-in 仍由 `voice.Service` 统一编排。

## 时间估算

- 最小闭环:3-5 天。
- 可日常用:6-8 天。
- 稳定打磨版:约 2 周。
