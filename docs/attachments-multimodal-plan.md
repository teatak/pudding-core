# 附件与多模态输入实施计划

> 状态:计划 + M0/M1 后端基础已落地。  
> 范围:图片、音频文件、PDF、txt/文本类附件。  
> 目标:在 `pudding-core` 新架构里实现 session-scoped 附件上传、canonical message 附件事实源、provider 多模态投递与 transcript 展示。

## 1. 背景与原则

旧项目已有上传、附件存储、hot attachment window、provider 多模态 wire format 和前端附件 UI,可以作为参考。不能直接搬旧 `Runtime` / provider-local history 结构。

本项目必须遵守:

- 后端没有 focus 概念。
- 所有业务 API 显式带 `sessionID`。
- context 只来自 canonical messages。
- provider client 不保存跨 turn 事实源。
- submit 必须带 `clientMessageID`。
- pending overlay 最终以 SSE / refetch 对账。

因此附件能力要进入 canonical message parts,而不是成为 provider-local 临时状态。

## 0. 当前落地进度

已完成:

- 新增 session-scoped 上传/读取路由:`POST /sessions/{id}/attachments`,`GET /sessions/{id}/attachments/*path`。
- 附件落盘到 `<home>/attachments/sessions/{sessionID}/blobs/...`,并做 MIME 白名单、20 MiB 限制、路径归一校验。
- canonical message 新增 `attachment` content part;`BeginTurn`、`QueueInput`、`PromoteNextQueuedInput` 均透传附件。
- submit 支持 `attachments`,并校验附件归属当前 session。
- contextbuilder 先把附件转为稳定文本摘要,避免附件-only 消息变成空 provider input。
- web contract/client 支持附件类型、上传 client 和附件 submit payload。
- Composer 支持文件选择、粘贴、拖拽上传、上传态 chip、纯附件提交。
- Composer 和 transcript 可展示图片缩略图,其它附件展示 chip。
- contextbuilder 在模型支持图片时把图片附件转成 provider `image` part;不支持时 fallback 文本摘要。
- OpenAI Chat Completions / Responses、Google Gemini、Anthropic Messages 已支持图片附件 wire format。
- 测试覆盖 API 上传/读取/submit、SQLite queue promote、contextbuilder 附件摘要/图片 inline、provider 图片 wire format。

未完成:

- 文本/PDF 内容提取与预算内联。
- PDF/音频 provider 原生多模态 wire format。
- queued 附件编辑 UX。
- 附件 GC / session 删除清理策略。

## 2. 目标

用户可以在 composer 附加文件并发送:

- 图片:上传、预览、作为视觉输入给支持的模型。
- txt / md / csv / json / xml / 代码文本:上传、预览、按预算内联文本。
- PDF:上传、预览;支持原生 PDF 的 provider 走 inline,其它 provider 降级引用或文本提取。
- 音频文件:上传、展示;支持 audio input 的 provider 走 inline,其它 provider 保留引用,后续可接 ASR transcript。

历史消息里的附件必须可重放进 context:最近附件按预算 inline,更老或过大的附件以 `@message(...)` 风格引用提示保留可追溯性。

## 3. 非目标

- 不做旧接口兼容层。
- 不做无 session scope 的主路径 `/uploads`。
- 不做服务端 focus / current session。
- 不在 v1 做云端 Files API 长期文件库。
- 不在 v1 做大文件 OCR / PDF 完整解析管线。
- 不把音频麦克风实时输入并入本计划;这里只处理“文件附件”。

## 4. 数据与 API 设计

### 4.1 附件实体

新增中立附件元数据:

```json
{
  "id": "att_xxx",
  "name": "demo.png",
  "attachmentKey": "sessions/sess_x/blobs/2026_07_att_xxx.png",
  "url": "/sessions/sess_x/attachments/blobs/2026_07_att_xxx.png",
  "mime": "image/png",
  "size": 12345,
  "origin": "upload",
  "createdAt": "2026-07-03T..."
}
```

落盘位置:

```text
<home>/attachments/sessions/{sessionID}/blobs/YYYY_MM_att_<random>.<ext>
<home>/attachments/trash/...
```

规则:

- 原始文件名只作为展示名,不参与真实路径。
- attachmentKey 必须归一化,禁止 `..` / 绝对路径 / 反斜杠逃逸。
- 单文件默认上限 20 MiB。
- 白名单 MIME:image/*、audio/*、text/*、application/pdf、json、xml、yaml。
- 路由带 token 鉴权,与其它 API 一致。

### 4.2 REST 路由

新增:

| 端点 | 用途 |
| --- | --- |
| `POST /sessions/{id}/attachments` | multipart upload,字段 `file` |
| `GET /sessions/{id}/attachments/*path` | 读取本 session 附件 blob |
| `POST /sessions/{id}/submit` | body 增 `attachments[]` |
| `PATCH /sessions/{id}/queued-inputs/{clientMessageID}` | v1 只允许改 text / status,不改附件 |

不新增 `/uploads` 或全局 `/attachments` 主路径。

### 4.3 Message Part

扩展 `ContentPart`:

```ts
type AttachmentPart = {
  type: "attachment";
  id: string;
  name: string;
  attachmentKey: string;
  url: string;
  mime: string;
  size: number;
  origin?: string;
  createdAt?: string;
  audioTranscript?: string;
};
```

user message parts 形态:

```json
[
  { "type": "text", "text": "看下这张图" },
  { "type": "attachment", "id": "att_xxx", "name": "demo.png", "mime": "image/png", ... }
]
```

`message.text` 继续是文本部分的聚合,附件不拼进 `text`。附件通过 `parts` 成为 canonical context 事实源。

### 4.4 Queue

`queued_inputs` 必须保留附件,否则 running 时提交带附件会丢。

建议 schema 增列:

```sql
attachments TEXT NOT NULL DEFAULT '[]'
```

对应 `QueuedInput.Attachments []Attachment`。Promote 时将 queued attachments 转为 user message attachment parts。

## 5. Context Builder 策略

新增 provider-neutral part:

```go
const (
    PartText
    PartThought
    PartToolUse
    PartToolResult
    PartAttachmentImage
    PartAttachmentAudio
    PartAttachmentDocument
)
```

或保留单一 `PartAttachment` 并带 `Kind/MIME/Data/Caption`。实现时优先选择对 provider adapter 简单的形状。

默认 hot attachment policy:

| 参数 | 默认 |
| --- | --- |
| recent attachment turns | 10 |
| raw attachment budget | context window 的 4% |
| item limit | 4 |
| per attachment raw limit | 2 MiB |
| text inline cap | 32 KiB |

投影规则:

| MIME | Context 投影 |
| --- | --- |
| `image/*` | 模型支持 image 时读 bytes → image part;否则引用 |
| `audio/*` | 模型支持 audio 时读 bytes → audio part;否则引用 / transcript |
| `application/pdf` | 支持 PDF inline 时 document part;否则引用 / 文本提取 |
| `text/*`, json, xml, yaml | 小文件内联文本块 |
| 其它 | 引用 |

引用文本:

```text
Attachments are available on @message(msg_xxx).
- image/png demo.png (123 KB)
- application/pdf report.pdf (1.2 MB)
```

音频若有 transcript,追加:

```text
[Audio transcripts]
- voice.wav:
  ...
```

## 6. Provider 工作

### Google Gemini

- image/audio/pdf 都走 `inline_data`。
- caption 可作为相邻 text part。
- 需要测试 text + 多附件顺序。

### Anthropic

- image 走 `type: "image"` base64 source。
- PDF 走 `type: "document"` base64 source。
- audio 不支持,v1 降级引用 / transcript。

### OpenAI-compatible Chat Completions

- image 走 content array + `image_url` data URL。
- audio 只在 `capabilities.audio=true` 时走 `input_audio`。
- PDF v1 不强行 inline;降级引用 / 文本提取。后续可为 OpenAI Responses 或 Files API 单独扩。
- Mimo 等兼容端点的 audio data URL 变体可参考旧项目,但最好做成 provider option,不要硬编码厂商。

### OpenAI Responses

- image / audio 可后续按 Responses input content item 补齐。
- PDF v1 不作为首批阻塞项。

## 7. 前端工作

### Composer

- `+` 按钮选择文件。
- 支持拖拽文件到会话区域。
- 支持粘贴截图 / 文件。
- 上传期间显示 chips 和进度,上传未完成禁止发送。
- 允许“纯附件无文本”发送,后端 text 可为空但 attachments 必须非空。
- session draft store 增加 attachments,成功发送后清空。

### Transcript

- user VM 增加 attachments。
- pending / queued / canonical 三种来源统一展示附件卡片。
- 图片显示缩略图。
- 文本/PDF/音频显示类型、名称、大小。
- remove 只对 composer draft 生效;canonical 附件不可从 transcript 删除。

### API Contract

- `web/contracts/api.ts` 同步 `ContentPart`、`submitRequest`、`queuedInput`。
- 新增 `uploadAttachment()` API client,注意 multipart 时不要强制 `Content-Type: application/json`。
- i18n 增加上传失败、文件过大、类型不支持、附件移除等文案。

## 8. 后端模块拆分

建议新增:

```text
internal/attachment/
  store.go       # StoreAttachmentReader / path normalize / URL
  mime.go        # whitelist / ext fallback / size limit
  service.go     # session scoped service
internal/api/attachments.go
```

现有改动:

```text
internal/store/store.go
internal/store/schema.sql
internal/store/sqlitestore/sqlitestore.go
internal/store/memstore/memstore.go
internal/contextbuilder/builder.go
internal/provider/provider.go
internal/provider/google/google.go
internal/provider/anthropic/anthropic.go
internal/provider/openai/openai.go
web/contracts/api.ts
web/src/api/client.ts
web/src/components/Composer.tsx
web/src/components/transcript/*
```

## 9. 阶段计划

### M0:契约冻结(0.5-1 天)

- 定稿附件 JSON shape。
- 定稿 routes。
- 更新 `docs/contracts-checklist.md`。
- 加 store / API / web zod 类型。

验收:

- Go/TS contract 编译通过。
- 纯文本 submit 行为不变。

### M1:上传与 canonical 附件(2-3 天)

- session-scoped upload。
- 附件落盘与静态读取。
- submit 接收 attachments。
- BeginTurn / QueueInput / PromoteQueuedInput 保存附件 parts。
- duplicate submit 返回原 turn,不重复写附件。

验收:

- 上传图片/txt/pdf/audio 返回元数据。
- submit 后 `GET /sessions/{id}/turns` 能看到 attachment parts。
- running 时第二条带附件进入 queue,drain 后附件仍在。

### M2:前端 MVP(2-3 天)

- Composer 文件选择/拖拽/粘贴。
- 上传态、附件 chips、删除。
- pending/queued/canonical transcript 附件展示。
- 纯附件提交。

验收:

- 刷新后 canonical 附件仍显示。
- pending overlay 被 canonical message 通过 `clientMessageID` 替换。
- queued 附件展示正确。

### M3:contextbuilder 文本与图片(2-3 天)

- text/json/md/csv 小文件内联。
- image hot window inline。
- 超预算/不支持附件引用提示。
- usage 估算纳入附件粗略 token 成本。

验收:

- mock provider 能收到文本附件内容。
- image-capable mock/provider 能收到 image part。
- 非 image-capable 模型不会被塞 image bytes。

### M4:PDF 与音频 provider 支持(3-4 天)

- Google image/audio/pdf inline。
- Anthropic image/pdf inline。
- OpenAI-compatible image/audio inline。
- 不支持路径统一降级。

验收:

- provider request 单测覆盖 wire format。
- PDF 在 Google/Anthropic 走 document part。
- audio 在 `capabilities.audio=false` 时不 inline。

### M5:清理与增强(2-3 天)

- 未引用附件 scan / trash / purge。
- PDF 文本提取 fallback 评估。
- 音频 transcript metadata 预留或接本地 ASR。
- 大文件/错误 UX 收口。

验收:

- 删除 session 后附件可进入 trash 或被 GC。
- 不存在的 blob 不导致 contextbuilder 崩溃。

## 10. 工作量估算

| 范围 | 估算 |
| --- | --- |
| 图片 + 文本 MVP | 4-6 天 |
| 图片 + 文本 + queue + transcript 完整闭环 | 6-8 天 |
| 加 PDF/audio provider inline | 8-12 天 |
| 加 GC、PDF 文本提取、音频 transcript | 12-17 天 |

建议第一刀交付 M0-M3:上传、canonical、前端展示、文本/图片可用。PDF/audio 在 M4 单独收口,风险更可控。

## 11. 测试清单

后端:

- MIME whitelist / ext fallback / 文件名清洗。
- path traversal 拒绝。
- upload size limit。
- submit text-only 兼容。
- submit attachment-only。
- duplicate submit 幂等。
- queued attachment promote。
- delete session 后附件清理策略。

contextbuilder:

- text attachment inline cap。
- image/pdf/audio hot window budget。
- 不支持 capability 时引用降级。
- missing blob 降级。
- compact 后只看 effective messages。

provider:

- Google `inline_data`。
- Anthropic image/document block。
- OpenAI image_url / input_audio。
- unsupported PDF/audio 不生成非法 wire format。

前端:

- click select / drag / paste。
- 上传失败保留草稿。
- 发送成功清空附件。
- pending/queued/canonical 展示一致。
- 纯附件可发送。

## 12. 风险与决策点

- PDF 对 OpenAI-compatible 不统一:v1 不强行 inline。
- 音频 input 能力必须走 model capabilities,不能按 provider 名猜。
- queued input 必须保存附件,否则功能会在并发 turn 下不完整。
- 附件读取端点要带 session scope,避免跨 session 猜 key 读取。
- 大文件不能进入 Zustand/localStorage;前端只存元数据。
- provider request 中 bytes 只由 contextbuilder 在请求时读取,不落 SQLite。
