# Voice Input Architecture

> 状态:当前只保留语音输入能力。TTS / speaker output 已移除,后续如需重新引入必须重新设计并评估服务条款与发布方案。
> 更新:2026-09-01。

## 当前范围

```text
mic capture -> WebRTC AEC/NS -> VAD -> ASR text -> engine.Submit
```

保留:

- PortAudio 麦克风采集。
- Sherpa ONNX SenseVoice + Silero VAD。
- WebRTC AEC/NS capture processing。
- session-scoped input binding。
- ASR 音频保存与原音消息模式。

不包含:

- TTS、speaker output owner、assistant delta 朗读。
- playback queue、sentence splitter、barge-in。
- Edge TTS、macOS `say` 或其他语音合成 provider。
- 声纹识别、Gemini Live、meeting、diarization、KWS。

PortAudio 的 playback 接口仍仅用于打开麦克风前的输入路由提示音,不是助手语音输出能力。

## 架构原则

- 麦克风等硬件资源归 daemon。
- 业务 turn 归 engine;ASR 结果必须走 `engine.Submit`。
- 后端没有 focus / active session 概念。
- 音频 API 显式带 `sessionID`。
- canonical messages 只由 engine/store 写入。
- 切换前端 selected session 不改变 input owner。

## API 契约

| Method | Path | 用途 |
| --- | --- | --- |
| `GET` | `/sessions/{id}/audio/bindings` | 查看输入 owner 快照 |
| `POST` | `/sessions/{id}/audio/input` | 绑定或释放语音输入 |

绑定请求:

```json
{ "enabled": true, "mode": "transcribe" }
```

`mode` 可选 `transcribe` 或 `raw`。删除 session 时自动释放其 input owner。

## 配置与运行资产

配置文件是 `<home>/config/audio.yaml`,当前包含:

- PortAudio driver。
- Sherpa ASR 与 Silero VAD。
- WebRTC AEC/NS。

模型只从 `<home>/runtime/models` 加载:

- `asr/model.int8.onnx`
- `asr/tokens.txt`
- `vad/silero_vad.onnx`

找不到模型时不会启用 fake ASR;前端会先进入 runtime 下载流程。

## 验证

- `GOCACHE=/tmp/pudding-go-cache go test ./...`
- `npm run build` (在 `web/` 目录)
- 真机验证麦克风权限、input binding、VAD/ASR 与 `engine.Submit` 链路。

## 后续重新引入语音输出的前置条件

- 选择明确允许该产品分发方式的 TTS provider 或本地模型。
- 单独设计输出路由、取消语义、回声处理和 UI。
- 不恢复已删除的旧 output owner / queue / fallback 路径。
