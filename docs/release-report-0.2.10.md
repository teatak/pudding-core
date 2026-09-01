# Pudding 0.2.10 发版报告

日期：2026-09-02

对比基线：`v0.2.9`（`2ed86715`）

候选提交：`17107c86`

## 发版结论

**可以发布 0.2.10 正式版。** 本版将语音架构收敛为输入能力，删除旧 TTS、扬声器输出绑定及相关队列，并更新产品文档与界面细节。SQLite schema、自动更新协议、系统权限归属、签名身份和稳定通道规则均未变化。

## 改动摘要

- 语音能力收敛为麦克风采集、AEC/NS、VAD、ASR 文本和原音输入两条明确路径。
- 删除旧 TTS provider、播放队列、句子切分、barge-in 和 speaker output owner。
- 删除 `/sessions/{id}/audio/output` API、扬声器输出按钮及对应前端契约。
- 精简 `audio.yaml`，保留 driver、ASR、AEC 和 NS；旧文件中的 TTS 字段可被忽略，配置重写后不再保留。
- 更新 mascot 场景渲染与交互细节，并删除旧 mascot 实现和双轨路径。
- README 和项目文档调整为更聚焦的产品介绍。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 语音输出 | 中 | 旧 TTS、朗读、播放队列和扬声器输出绑定已移除。 |
| 语音输入 | 中 | 保留 ASR 与原音输入，内部 service 和配置结构已精简。 |
| 音频配置 | 低 | 旧 TTS 字段读取时被忽略，配置重写后自动清理。 |
| 桌面界面 | 低 | 更新 mascot 场景和少量设置文案。 |
| SQLite | 无 | schema 仍为 v13，本版没有迁移。 |
| 自动更新 | 无 | 更新检查、下载、安装、架构选择和通道逻辑未变化。 |
| 权限与签名 | 无 | Bundle identity、TCC 权限归属、Developer ID 签名与公证要求未变化。 |

## 数据与兼容性

- SQLite schema 保持 v13，本版不创建数据库迁移备份，也不执行迁移或备份清理。
- canonical sessions、messages、turns、projects、usage 和 provider 配置均不重写。
- 稳定版继续使用 `~/.pudding`，数据目录和更新通道规则不变。
- 旧 `audio.yaml` 可继续读取；TTS 和 `playback_min_energy` 字段不再进入运行时配置，下一次音频配置写入时会被清理。
- `/sessions/{id}/audio/output` 不再提供，内置桌面前端已同步迁移。

## 已完成验证

- Go 全量测试通过，包含 API、voice service、配置和 SQLite store。
- Electron 全量测试：194 项通过。
- `web/` TypeScript 检查和生产构建通过。
- `make schema-check` 通过，schema v13 release contract 有效。
- `git diff --check` 在修正文档行尾后通过。

## 剩余发版门禁

- 固定发布流水线重新运行全量 Go、Electron、schema 和 Web build。
- 完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到唯一 Draft Release 后再显式发布。

## Release Notes

### Voice Input Simplification

- Focus the voice runtime on microphone capture, AEC/NS, VAD, ASR transcription, and raw audio input.
- Remove the legacy TTS providers, playback queue, sentence splitting, barge-in, and speaker output ownership.
- Remove the deprecated audio output API and its desktop control while preserving session-scoped voice input.

### Configuration and Compatibility

- Simplify `audio.yaml` to the active driver, ASR, AEC, and NS settings.
- Keep existing audio configuration files readable and drop obsolete TTS fields when the configuration is rewritten.
- Keep SQLite schema v13 unchanged with no migration, backup, cleanup, or canonical-data rewrite.

### Interface and Documentation

- Refine the mascot scene rendering and interaction behavior while removing the superseded implementation.
- Update the repository overview and supporting documentation around the current desktop product.

### Updates and Distribution

- Preserve the existing automatic update protocol, app identity, permissions, signing, notarization, and stable-channel behavior.
