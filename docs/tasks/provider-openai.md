# 轨道 B:OpenAI-compatible provider

> 背景:daemon 的 turn 引擎(`internal/engine`)消费 provider 模型流;
> M0 只有 mock provider。你的任务是交付第一个真 provider。
> 通用纪律见 docs/phase-1-plan.md 第 6 节;硬约束见 AGENTS.md。

## 范围

交付 `internal/provider/openai` 包,实现 `provider.Client`,
对接 OpenAI-compatible Chat Completions(`POST {baseURL}/chat/completions`,
`stream: true`)。一个协议吃多家:OpenAI / OpenRouter / DeepSeek / Ollama 的
`/v1` 兼容端点都应能跑。

## 契约(只读,不得修改)

- `internal/provider/provider.go`。核心语义:
  - chunk 序列:`Delta* → (Done | Err)`,终止后**不得再发任何 chunk**;
  - ctx 取消必须尽快收流并以 `Err: ctx.Err()` 终止(engine 靠它实现 cancel);
  - channel 由实现负责 close。
- 行为参照:`internal/provider/mock/mock.go`(engine 单测跑在它上面)。

## 实现要求

1. **不引入第三方依赖**,net/http + bufio 手写 SSE 解析(`data:` 行,`[DONE]` 哨兵,
   取 `choices[0].delta.content`)。为什么:协议就一个字段,官方 SDK 会把
   provider 形状渗进我们的抽象,旧项目吃过这个亏。
2. 请求构造:`Request.System` → `messages[0]{role:"system"}`,后接 user/assistant
   消息原序。**每次请求全量构造,实现内不得缓存任何跨 turn 状态**。
   为什么:context 的唯一事实源是本地 canonical messages(AGENTS.md 硬约束 8/9),
   provider 端状态(如 Responses API 的 previous_response_id)是明确反模式。
3. **不做流级恢复/重试**:请求失败、流中断 → 直接 `Err` 终止。
   为什么:turn 的失败语义由 engine 统一处理(turn.failed + 半截输出保留),
   provider 自行重试会产生重复 delta。
4. 配置:`openai.New(Config{BaseURL, APIKey, HTTPClient?})`;
   main 接线从环境变量读:`PUDDING_OPENAI_BASE_URL`、`PUDDING_OPENAI_API_KEY`,
   model 沿用现有 `--model` flag。可以修改 `cmd/puddingd/main.go` 中
   provider 选择处(`--mock=false` 时构造 openai client,环境变量缺失则报错退出)。
   settings 表集成由主线后续做,本轨道不碰 settings。
5. 超时:连接与首字节设默认超时(建议 30s,可在 Config 覆盖);
   流建立后不设总超时(长回答合法),靠 ctx 取消兜底。
6. 非 2xx 响应:读 body 摘要进 error(截断,防止把整页 HTML 塞进 turn.failed)。
7. API key 是敏感值:不得出现在日志、error 字符串、测试 fixture 里。

## 验收

- `go test ./...` 全绿;新增单测基于 `net/http/httptest` 假服务:
  - 正常流:多 delta → `[DONE]` → Done;
  - 错误状态码(401/429/500)→ Err 带摘要;
  - 流中途断开 → Err;
  - **ctx cancel:服务端慢发时取消,client 须在百毫秒级内以 ctx.Err() 终止**;
  - system + 多轮 messages 的请求体形状断言。
- 手工 smoke(可选但推荐):对本机 Ollama `http://127.0.0.1:11434/v1` 或任一
  兼容端点,`./bin/puddingd --model <model>` + 环境变量,curl submit 走通真流。

## 禁区

- 不改 `provider.go` 契约、`engine`、`store`、`api`、web。契约问题 PR 里提。
- 不实现 OpenAI Responses API(已定为后续事项,且只允许无状态模式,
  见 docs/technology-decisions.md 第 5 节)。
- 不做 tool calling / 多模态(第一阶段 text-only)。

分支:`track/provider-openai`。
